package httptool

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMiddlewareChain(t *testing.T) {
	var sequence []string

	m1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sequence = append(sequence, "m1-start")
			next.ServeHTTP(w, r)
			sequence = append(sequence, "m1-end")
		})
	}

	m2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sequence = append(sequence, "m2-start")
			next.ServeHTTP(w, r)
			sequence = append(sequence, "m2-end")
		})
	}

	h := NewHttpServerTool()
	h.AddMiddleware(m1)
	h.AddMiddleware(m2)

	// Add a dummy endpoint to trigger the chain
	h.AddEndpoint("/test", "GET", func(ctx *HttpContext) (any, error) {
		sequence = append(sequence, "handler")
		return nil, nil
	}, nil)

	// Simulate boot complete to build the chain
	h.registerAllEndpoints()

	// Build handler chain manually for testing (mimicking OnBootComplete)
	var finalHandler http.Handler = h.mux
	for i := len(h.middlewares) - 1; i >= 0; i-- {
		finalHandler = h.middlewares[i](finalHandler)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	finalHandler.ServeHTTP(w, req)

	expected := []string{"m1-start", "m2-start", "handler", "m2-end", "m1-end"}
	if len(sequence) != len(expected) {
		t.Fatalf("expected sequence length %d, got %d", len(expected), len(sequence))
	}

	for i, v := range expected {
		if sequence[i] != v {
			t.Errorf("at index %d: expected %s, got %s", i, v, sequence[i])
		}
	}
}

func TestAuthValidator(t *testing.T) {
	h := NewHttpServerTool()

	validToken := "good-token"
	mockClaims := map[string]any{"user_id": 123}

	validator := func(token string) map[string]any {
		if token == validToken {
			return mockClaims
		}
		return nil
	}

	h.AddEndpoint("/protected", "GET", func(ctx *HttpContext) (any, error) {
		claims, _ := ctx.Request.Context().Value(AuthClaimsKey{}).(map[string]any)
		return claims, nil
	}, validator)

	h.registerAllEndpoints()

	t.Run("Unauthorized request (no token)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		w := httptest.NewRecorder()
		h.mux.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("Authorized request (valid token)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		w := httptest.NewRecorder()
		h.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestPathParams(t *testing.T) {
	h := NewHttpServerTool()

	h.AddEndpoint("/users/{id}", "GET", func(ctx *HttpContext) (any, error) {
		id := ctx.Request.PathValue("id")
		return map[string]string{"captured_id": id}, nil
	}, nil)

	h.registerAllEndpoints()

	req := httptest.NewRequest("GET", "/users/42", nil)
	w := httptest.NewRecorder()
	h.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	expectedBody := `{"captured_id":"42"}` + "\n"
	if string(body) != expectedBody {
		t.Errorf("expected %q, got %q", expectedBody, string(body))
	}
}

func TestHttpContext(t *testing.T) {
	h := NewHttpServerTool()

	h.AddEndpoint("/ctx", "GET", func(ctx *HttpContext) (any, error) {
		ctx.SetStatus(201)
		ctx.SetHeader("X-Test", "works")
		ctx.SetCookie(&http.Cookie{Name: "test-cookie", Value: "sweet"})
		return nil, nil
	}, nil)

	h.registerAllEndpoints()

	req := httptest.NewRequest("GET", "/ctx", nil)
	w := httptest.NewRecorder()
	h.mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}

	if w.Header().Get("X-Test") != "works" {
		t.Errorf("expected header works, got %s", w.Header().Get("X-Test"))
	}

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "test-cookie" && c.Value == "sweet" {
			found = true
			break
		}
	}
	if !found {
		t.Error("test-cookie not found in response")
	}
}
func TestCORS(t *testing.T) {
	h := NewHttpServerTool()
	h.AddEndpoint("/cors", "GET", func(ctx *HttpContext) (any, error) { return "ok", nil }, nil)
	h.registerAllEndpoints()

	handler := h.corsMiddleware(h.mux)

	t.Run("OPTIONS returns 204", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/cors", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("missing CORS headers")
		}
	})

	t.Run("GET returns CORS headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/cors", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("missing CORS headers on GET")
		}
	})
}

func TestPanicRecovery(t *testing.T) {
	h := NewHttpServerTool()
	h.AddEndpoint("/panic", "GET", func(ctx *HttpContext) (any, error) {
		panic("intentional boom")
	}, nil)
	h.registerAllEndpoints()

	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	h.mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "Internal server error") {
		t.Errorf("unexpected error body: %s", string(body))
	}
}

func TestRouteOrdering(t *testing.T) {
	h := NewHttpServerTool()
	var winner string

	// Register parameterized first to test ordering logic
	h.AddEndpoint("/{name}", "GET", func(ctx *HttpContext) (any, error) {
		winner = "parameterized"
		return nil, nil
	}, nil)

	h.AddEndpoint("/static", "GET", func(ctx *HttpContext) (any, error) {
		winner = "static"
		return nil, nil
	}, nil)

	h.registerAllEndpoints()

	req := httptest.NewRequest("GET", "/static", nil)
	w := httptest.NewRecorder()
	h.mux.ServeHTTP(w, req)

	if winner != "static" {
		t.Errorf("expected static route to win, got %s", winner)
	}
}

func TestAuthCookie(t *testing.T) {
	h := NewHttpServerTool()
	h.AddEndpoint("/cookie-auth", "GET", func(ctx *HttpContext) (any, error) { return "ok", nil }, func(token string) map[string]any {
		if token == "secret-cookie" {
			return map[string]any{"sub": 1.0}
		}
		return nil
	})
	h.registerAllEndpoints()

	req := httptest.NewRequest("GET", "/cookie-auth", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "secret-cookie"})
	w := httptest.NewRecorder()
	h.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 via cookie, got %d", w.Code)
	}
}

func TestBodyLimit(t *testing.T) {
	h := NewHttpServerTool()
	h.AddEndpoint("/limit", "POST", func(ctx *HttpContext) (any, error) {
		body, err := io.ReadAll(ctx.Request.Body)
		if err != nil {
			return err.Error(), nil
		}
		return len(body), nil
	}, nil)
	h.registerAllEndpoints()

	// 11MB body (limit is 10MB)
	bigBody := strings.Repeat("a", 11*1024*1024)
	req := httptest.NewRequest("POST", "/limit", strings.NewReader(bigBody))
	w := httptest.NewRecorder()
	h.mux.ServeHTTP(w, req)

	// MaxBytesReader returns an error during Read, not immediately
	if w.Code != http.StatusOK { // Handler is called, read fails inside
		t.Errorf("expected 200 (handler start), got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "http: request body too large") {
		t.Errorf("expected body too large error, got %s", string(body))
	}
}

func TestWebSocket(t *testing.T) {
	h := NewHttpServerTool()
	h.port = "0" // dynamic port

	h.AddWebsocket("/ws", func(ws *websocket.Conn, r *http.Request) {
		for {
			mt, message, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if err := ws.WriteMessage(mt, message); err != nil {
				return
			}
		}
	})

	// Start server on a listener to get the dynamic port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	h.port = fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
	h.registerAllWebsockets()
	h.server = &http.Server{Handler: h.mux}

	go h.server.Serve(ln)
	defer h.server.Close()

	// Connect client
	url := "ws://127.0.0.1:" + h.port + "/ws"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Round-trip test
	msg := []byte("hello websocket")
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	_, reply, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(reply) != string(msg) {
		t.Errorf("expected %s, got %s", string(msg), string(reply))
	}
}

func TestServerLifecycle(t *testing.T) {
	h := NewHttpServerTool()
	h.port = "0"

	// Mock boot complete (non-blocking server start)
	if err := h.Setup(); err != nil {
		t.Error(err)
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	h.server = &http.Server{Handler: h.mux}
	go h.server.Serve(ln)

	time.Sleep(10 * time.Millisecond) // let it start

	err := h.ShutdownFirst()
	if err != nil {
		t.Errorf("ShutdownFirst failed: %v", err)
	}
}
