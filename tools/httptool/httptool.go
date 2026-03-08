/*
HTTP Server Tool — Go Implementation for MicroCoreOS
=====================================================

All methods are called in OnBoot(). The server starts after all plugins have booted.

ENDPOINTS:

	p.http.AddEndpoint("/users",     "POST", p.create, nil)               // public
	p.http.AddEndpoint("/users/me",  "GET",  p.getMe,  auth.ValidateToken) // protected
	p.http.AddEndpoint("/items/{id}","GET",  p.getOne, nil)               // path param

HANDLER SIGNATURE:

	func (p *MyPlugin) handle(ctx *httptool.HttpContext) (any, error) {
	    id  := ctx.Request.PathValue("id")           // {id} from path
	    q   := ctx.Request.URL.Query().Get("search") // query string
	    ctx.SetStatus(201)                           // override status (default 200)
	    return MyResponse{ID: id}, nil               // auto JSON-serialized
	    // return nil, err                           // triggers automatic 500
	}

AUTH CLAIMS (protected endpoints only):

	claims, ok := ctx.Request.Context().Value(httptool.AuthClaimsKey{}).(map[string]any)
	userID := int64(claims["sub"].(float64)) // JWT always encodes numbers as float64

MIDDLEWARE (cross-cutting: rate limiting, request IDs, logging):

	func (p *MyPlugin) OnBoot() error {
	    p.http.AddMiddleware(p.myMiddleware)
	    return nil
	}
	func (p *MyPlugin) myMiddleware(next http.Handler) http.Handler {
	    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	        // before handler
	        next.ServeHTTP(w, r)
	        // after handler
	    })
	}

WEBSOCKETS:

	p.http.AddWebsocket("/ws/chat", p.chatLoop)

	func (p *MyPlugin) chatLoop(ws *websocket.Conn, r *http.Request) {
	    for {
	        _, msg, err := ws.ReadMessage()
	        if err != nil { return }
	        ws.WriteMessage(websocket.TextMessage, msg)
	    }
	}
*/
package httptool

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/gorilla/websocket"

	"microcoreos-go/core"
)

// ─── Handler types ──────────────────────────────────────────────────────────

// HandlerFunc is the signature for REST HTTP endpoint handlers.
// Returns (response, nil) on success — response is auto-serialized to JSON.
// Returns (nil, err) to signal an unexpected error — httptool replies with 500 automatically.
// Use ctx.SetStatus() for intentional non-200 codes (400, 401, 409, etc.).
type HandlerFunc func(ctx *HttpContext) (any, error)

// WsHandlerFunc is the signature for bidirectional WebSocket connections.
// The plugin is entirely responsible for the Read/Write loop on the conn.
// When this function returns, the WebSocket connection is closed.
type WsHandlerFunc func(ws *websocket.Conn, r *http.Request)

// AuthValidator is an optional callback plugins can pass to AddEndpoint.
// It receives the extracted Bearer or session token and should return the claims map
// if valid, or nil if invalid. The HTTP Server handles 401s automatically.
type AuthValidator func(token string) map[string]any

// ─── HttpContext ────────────────────────────────────────────────────────────

// HttpContext provides the raw request and response manipulation for handlers.
type HttpContext struct {
	Request    *http.Request
	statusCode int
	headers    map[string]string
	cookies    []*http.Cookie
}

// NewHttpContext creates a context with default 200 status and the original request.
func NewHttpContext(r *http.Request) *HttpContext {
	return &HttpContext{
		Request:    r,
		statusCode: 200,
		headers:    make(map[string]string),
	}
}

// SetStatus overrides the HTTP response status code.
func (c *HttpContext) SetStatus(code int) { c.statusCode = code }

// SetHeader adds a custom response header.
func (c *HttpContext) SetHeader(key, value string) { c.headers[key] = value }

// SetCookie adds a cookie to the response.
func (c *HttpContext) SetCookie(cookie *http.Cookie) {
	c.cookies = append(c.cookies, cookie)
}

// StatusCode returns the current status code.
func (c *HttpContext) StatusCode() int { return c.statusCode }

// applyTo writes all accumulated headers and cookies to the ResponseWriter.
func (c *HttpContext) applyTo(w http.ResponseWriter) {
	for k, v := range c.headers {
		w.Header().Set(k, v)
	}
	for _, cookie := range c.cookies {
		http.SetCookie(w, cookie)
	}
}

// ─── HttpTool interface ─────────────────────────────────────────────────────

// MiddlewareFunc wraps an http.Handler. Plugins register middleware in OnBoot()
// to add cross-cutting behavior (rate limiting, custom logging, tracing) without
// modifying the tool. Middleware is applied in registration order: first registered
// is the outermost wrapper (runs first on every request).
//
// Example — a RateLimitPlugin:
//
//	func (p *RateLimitPlugin) OnBoot() error {
//	    p.http.AddMiddleware(p.rateLimit)
//	    return nil
//	}
//
//	func (p *RateLimitPlugin) rateLimit(next http.Handler) http.Handler {
//	    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//	        if !p.allow(r.RemoteAddr) {
//	            w.WriteHeader(http.StatusTooManyRequests)
//	            return
//	        }
//	        next.ServeHTTP(w, r)
//	    })
//	}
type MiddlewareFunc func(next http.Handler) http.Handler

// HttpTool is the interface plugins use to register HTTP and WebSocket endpoints.
// Resolve in Inject() using:
//
//	p.http, err = core.GetTool[httptool.HttpTool](c, "http")
type HttpTool interface {
	AddEndpoint(path, method string, handler HandlerFunc, authValidator AuthValidator)
	AddWebsocket(path string, handler WsHandlerFunc)
	// AddMiddleware registers a middleware that wraps every request.
	// Call in OnBoot(). First registered = outermost = runs first.
	AddMiddleware(fn MiddlewareFunc)
}

// ─── HttpServerTool ─────────────────────────────────────────────────────────

var pathParamRegex = regexp.MustCompile(`\{(\w+)\}`)

// pendingEndpoint buffers a REST endpoint until OnBootComplete.
type pendingEndpoint struct {
	path          string
	method        string
	handler       HandlerFunc
	paramNames    []string
	authValidator AuthValidator
}

// pendingWsEndpoint buffers a WebSocket endpoint until OnBootComplete.
type pendingWsEndpoint struct {
	path    string
	handler WsHandlerFunc
}

// HttpServerTool implements the HTTP server using Go's stdlib net/http.
type HttpServerTool struct {
	core.BaseToolDefaults
	mux          *http.ServeMux
	server       *http.Server
	port         string
	pending      []pendingEndpoint
	pendingWs    []pendingWsEndpoint
	middlewares  []MiddlewareFunc
	logger       core.Logger
	shutdownOnce sync.Once
}

func init() {
	core.RegisterTool(func() core.Tool { return NewHttpServerTool() })
}

// NewHttpServerTool creates a new HTTP server tool.
func NewHttpServerTool() *HttpServerTool {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "5000"
	}
	return &HttpServerTool{
		mux:  http.NewServeMux(),
		port: port,
	}
}

func (h *HttpServerTool) Name() string { return "http" }

func (h *HttpServerTool) Setup() error {
	return nil
}

func (h *HttpServerTool) OnBootComplete(c *core.Container) error {
	if log, err := core.GetTool[core.Logger](c, "logger"); err == nil {
		h.logger = log
	}

	h.registerAllEndpoints()
	h.registerAllWebsockets()

	ln, err := net.Listen("tcp", ":"+h.port)
	if err != nil {
		return fmt.Errorf("httptool: failed to listen on :%s: %w", h.port, err)
	}

	var handler http.Handler = h.mux
	for i := len(h.middlewares) - 1; i >= 0; i-- {
		handler = h.middlewares[i](handler)
	}
	h.server = &http.Server{Handler: h.corsMiddleware(handler)}

	msg := "Listening on http://localhost:" + h.port
	if h.logger != nil {
		h.logger.Info(msg, "port", h.port)
	} else {
		fmt.Printf("[HttpServer] %s\n", msg)
	}

	go func() {
		if err := h.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			if h.logger != nil {
				h.logger.Error("Server error", "error", err)
			} else {
				fmt.Printf("[HttpServer] 🚨 Server error: %v\n", err)
			}
		}
	}()

	return nil
}

func (h *HttpServerTool) GetInterfaceDescription() string {
	return `HTTP Server Tool (http): REST API, WebSockets, and middleware pipeline.
All methods are called in OnBoot(). The server starts after all plugins have booted.

── ENDPOINTS ──────────────────────────────────────────────────────────────────
  AddEndpoint(path, method, HandlerFunc, authValidator)
    path:          "/users/{id}" — {name} captures path params (Go 1.22 stdlib).
    method:        "GET", "POST", "PUT", "DELETE", etc.
    HandlerFunc:   func(ctx *httptool.HttpContext) (any, error)
                   Return (struct, nil) → auto-serialized to JSON.
                   Return (nil, err)   → automatic 500 {"success":false,"error":"..."}.
    authValidator: nil = public. Pass auth.ValidateToken to protect the endpoint.
                   On failure the tool returns 401 automatically.

  Handler example:
    func (p *MyPlugin) handle(ctx *httptool.HttpContext) (any, error) {
        id := ctx.Request.PathValue("id")        // path param from {id}
        q  := ctx.Request.URL.Query().Get("q")   // query string
        ctx.SetStatus(201)                        // override default 200
        ctx.SetHeader("X-Custom", "value")
        return MyResponse{ID: id}, nil
    }

── AUTH CLAIMS (protected endpoints) ──────────────────────────────────────────
  When authValidator is set, validated JWT claims are injected into the request context.
  Read them in the handler:
    claims, ok := ctx.Request.Context().Value(httptool.AuthClaimsKey{}).(map[string]any)
    userID := int64(claims["sub"].(float64))  // JWT encodes numbers as float64

── HTTCONTEXT API ──────────────────────────────────────────────────────────────
  ctx.Request              *http.Request — full stdlib request
  ctx.SetStatus(code int)                — override HTTP status (default 200)
  ctx.SetHeader(key, value string)       — add response header
  ctx.SetCookie(cookie *http.Cookie)     — add response cookie

── MIDDLEWARE ──────────────────────────────────────────────────────────────────
  AddMiddleware(fn MiddlewareFunc)
    MiddlewareFunc: func(next http.Handler) http.Handler
    First registered = outermost = runs first on every request.
    Use for rate limiting, request IDs, custom logging, etc.
    The tool applies: CORS → plugin middlewares → auth → handler.

  Middleware example (rate limiter plugin):
    func (p *RateLimitPlugin) OnBoot() error {
        p.http.AddMiddleware(p.limit)
        return nil
    }
    func (p *RateLimitPlugin) limit(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !p.allow(r.RemoteAddr) {
                w.WriteHeader(http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }

── WEBSOCKETS ──────────────────────────────────────────────────────────────────
  AddWebsocket(path string, handler WsHandlerFunc)
    WsHandlerFunc: func(ws *websocket.Conn, r *http.Request)
    The handler owns the connection. It must run a Read/Write loop.
    When it returns the connection is closed automatically.`
}

// ShutdownFirst implements core.FirstShutdown.
func (h *HttpServerTool) ShutdownFirst() error {
	var err error
	h.shutdownOnce.Do(func() {
		if h.server != nil {
			err = h.server.Shutdown(context.Background())
		}
	})
	return err
}

// Shutdown is a no-op: the server is already drained by ShutdownFirst.
// If ShutdownFirst was never called (e.g. in tests), it drains here instead.
func (h *HttpServerTool) Shutdown() error {
	return h.ShutdownFirst()
}

// ─── Public API ─────────────────────────────────────────────────────────────

// AddEndpoint buffers a REST endpoint for registration during OnBootComplete.
// If authValidator is non-nil, the endpoint requires a valid Bearer token or cookie.
func (h *HttpServerTool) AddEndpoint(path, method string, handler HandlerFunc, authValidator AuthValidator) {
	paramNames := extractPathParamNames(path)
	h.pending = append(h.pending, pendingEndpoint{
		path:          path,
		method:        strings.ToUpper(method),
		handler:       handler,
		paramNames:    paramNames,
		authValidator: authValidator,
	})
}

// AddMiddleware buffers a middleware for application during OnBootComplete.
// First registered = outermost wrapper = runs first on every request.
func (h *HttpServerTool) AddMiddleware(fn MiddlewareFunc) {
	h.middlewares = append(h.middlewares, fn)
}

// AddWebsocket buffers a WebSocket endpoint for registration during OnBootComplete.
func (h *HttpServerTool) AddWebsocket(path string, handler WsHandlerFunc) {
	h.pendingWs = append(h.pendingWs, pendingWsEndpoint{
		path:    path,
		handler: handler,
	})
}

// ─── Internal ───────────────────────────────────────────────────────────

func (h *HttpServerTool) registerAllEndpoints() {
	// Static paths first, parameterized after (same logic as Python version)
	static := make([]pendingEndpoint, 0, len(h.pending))
	parameterized := make([]pendingEndpoint, 0, len(h.pending))
	for _, ep := range h.pending {
		if len(ep.paramNames) > 0 {
			parameterized = append(parameterized, ep)
		} else {
			static = append(static, ep)
		}
	}

	for _, ep := range append(static, parameterized...) {
		h.registerEndpoint(ep)
	}
	h.pending = nil
}

// registerAllWebsockets registers buffered websocket handlers.
func (h *HttpServerTool) registerAllWebsockets() {
	var upgrader = websocket.Upgrader{
		// Permissive check for MicroCoreOS dev environment
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	for _, ep := range h.pendingWs {
		pattern := "GET " + ep.path // WebSockets handshake is always an HTTP GET
		handler := ep.handler

		h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				fmt.Printf("[HttpServer] WebSocket Upgrade failed for %s: %v\n", r.URL.Path, err)
				return
			}
			defer conn.Close()

			fmt.Printf("[HttpServer] WS Connected: %s\n", r.URL.Path)

			// Hand over the connection completely to the plugin handler (blocks)
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						fmt.Printf("[HttpServer] 💥 Panic in WebSocket %s: %v\n", r.URL.Path, rec)
					}
				}()
				handler(conn, r)
			}()

			fmt.Printf("[HttpServer] WS Disconnected: %s\n", r.URL.Path)
		})
	}
	h.pendingWs = nil
}

func (h *HttpServerTool) registerEndpoint(ep pendingEndpoint) {
	pattern := fmt.Sprintf("%s %s", ep.method, ep.path)
	handler := ep.handler

	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		// Prevent OOM: limit request body to 10MB
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

		// ── 1. Auth validation (if endpoint is protected) ───────────────
		if ep.authValidator != nil {
			token := extractBearerToken(r)
			claims := ep.authValidator(token)
			if claims == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Unauthorized"})
				fmt.Printf("[HttpServer] %s %s → 401 (auth failed)\n", r.Method, r.URL.Path)
				return
			}
			// Inject validated claims into the request context for handlers to read.
			r = r.WithContext(context.WithValue(r.Context(), AuthClaimsKey{}, claims))
		}

		// ── 2. Create context ───────────────────────────────────────────
		ctx := NewHttpContext(r)

		// ── 3. Call handler ─────────────────────────────────────────────
		var result any
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					fmt.Printf("[HttpServer] 💥 Panic in handler: %v\n", rec)
					ctx.SetStatus(500)
					result = map[string]any{"success": false, "error": "Internal server error"}
				}
			}()
			var handlerErr error
			result, handlerErr = handler(ctx)
			if handlerErr != nil {
				ctx.SetStatus(500)
				result = map[string]any{"success": false, "error": handlerErr.Error()}
			}
		}()

		// ── 4. Send response ────────────────────────────────────────────
		w.Header().Set("Content-Type", "application/json")
		ctx.applyTo(w)
		w.WriteHeader(ctx.statusCode)
		json.NewEncoder(w).Encode(result)

		fmt.Printf("[HttpServer] %s %s → %d\n", r.Method, r.URL.Path, ctx.statusCode)
	})
}

// AuthClaimsKey is the context key used to store validated JWT claims.
// Handlers read claims with: ctx.Request.Context().Value(httptool.AuthClaimsKey{})
type AuthClaimsKey struct{}

// extractBearerToken extracts a JWT from the Authorization header or the session_token cookie.
func extractBearerToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if cookie, err := r.Cookie("session_token"); err == nil {
		return cookie.Value
	}
	return ""
}

// corsMiddleware adds permissive CORS headers (same as Python version).
func (h *HttpServerTool) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractPathParamNames parses "{name}" tokens from a path template.
func extractPathParamNames(path string) []string {
	matches := pathParamRegex.FindAllStringSubmatch(path, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	return names
}
