// Event Bus Tool — Go-First Implementation for MicroCoreOS
//
// PUBLIC CONTRACT (what plugins use):
//
//	cancel := bus.Subscribe("user.created", p.onUserCreated)
//	defer cancel() // removes exactly this subscription — no ambiguity
//
//	cancelRPC := bus.SubscribeRPC("user.validate", p.onUserValidate)
//	defer cancelRPC()
//
//	bus.Publish("user.created", map[string]any{"id": 42})
//
//	reply, err := bus.Request("user.validate", map[string]any{"email": "a@b.com"}, 5*time.Second)
//
// HANDLER SIGNATURES:
//
//	// Pub/Sub — fire-and-forget
//	func (p *MyPlugin) onUserCreated(data map[string]any) { ... }
//
//	// Request/Reply — must return a non-nil map to reply
//	func (p *MyPlugin) onUserValidate(data map[string]any) map[string]any {
//	    return map[string]any{"exists": true}
//	}
//
// WILDCARD:
//
//	cancel := bus.Subscribe("*", p.onAnyEvent) // observability — cannot reply in RPC
//
// TRACE IDs:
//
//	Every Publish/Request automatically injects "_trace_id" into the data delivered
//	to handlers. All subscribers of the same publish call share the same trace ID.
//	To propagate a trace across a chain of events, forward "_trace_id" in derived publishes:
//
//	    func (p *MyPlugin) onUserCreated(data map[string]any) {
//	        p.bus.Publish("audit.log", map[string]any{
//	            "_trace_id": data["_trace_id"], // propagate the chain
//	            "action":    "user.created",
//	        })
//	    }
//
//	A LoggingPlugin can subscribe to "*" and log every event with its trace ID,
//	giving full observability of any event chain without coupling plugins.
//
// Note: All handlers are plain functions — no async/await coloring needed in Go.
// Each handler runs in its own goroutine.
package eventbus

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"microcoreos-go/core"
)

// HandlerFunc is a fire-and-forget subscriber handler.
type HandlerFunc func(data map[string]any)

// ReplyHandlerFunc is a subscriber that returns a reply for Request/Reply RPC.
type ReplyHandlerFunc func(data map[string]any) map[string]any

// CancelFunc cancels a subscription when called. Safe to call multiple times.
type CancelFunc func()

// ─── EventBusTool interface ───────────────────────────────────────────────────

// EventBusTool is the interface plugins use for Pub/Sub and Request/Reply.
// Resolve in Inject() using:
//
//	p.bus, err = core.GetTool[eventbus.EventBusTool](c, "event_bus")
type EventBusTool interface {
	// Subscribe registers a fire-and-forget handler for an event.
	// Returns a CancelFunc that removes exactly this subscription.
	// Use "*" as eventName for a wildcard (observability only, cannot reply in RPC).
	Subscribe(eventName string, handler HandlerFunc) CancelFunc
	// SubscribeRPC registers a request/reply handler.
	// Returns a CancelFunc that removes exactly this subscription.
	SubscribeRPC(eventName string, handler ReplyHandlerFunc) CancelFunc
	// Publish fires an event to all subscribers. Non-blocking: each handler
	// runs in its own goroutine. Returns immediately.
	Publish(eventName string, data map[string]any)
	// Request fires an event and blocks until the first RPC handler replies
	// or the timeout elapses. Returns the reply or an error.
	Request(eventName string, data map[string]any, timeout time.Duration) (map[string]any, error)
}

// ─── InMemoryEventBusTool ─────────────────────────────────────────────────────

// subscriber holds a handler and its unique ID (used for O(n) cancel).
type subscriber struct {
	id         uint64
	handler    HandlerFunc
	rpcHandler ReplyHandlerFunc
}

// InMemoryEventBusTool implements EventBusTool using goroutines and channels.
// Swap backend: create a new package (e.g. tools/natsbus), use name = "event_bus",
// implement EventBusTool — plugins require zero changes.
type InMemoryEventBusTool struct {
	core.BaseToolDefaults
	mu          sync.RWMutex
	subscribers map[string][]subscriber
	seq         atomic.Uint64 // monotonic ID counter for subscriptions
}

func init() {
	core.RegisterTool(func() core.Tool { return NewInMemoryEventBusTool() })
}

// NewInMemoryEventBusTool creates a ready-to-use in-memory event bus.
func NewInMemoryEventBusTool() *InMemoryEventBusTool {
	return &InMemoryEventBusTool{
		subscribers: make(map[string][]subscriber),
	}
}

func (b *InMemoryEventBusTool) Name() string { return "event_bus" }

func (b *InMemoryEventBusTool) Setup() error {
	fmt.Println("[EventBus] Online.")
	return nil
}

func (b *InMemoryEventBusTool) GetInterfaceDescription() string {
	return `Event Bus Tool (event_bus): In-memory Pub/Sub and Request/Reply.
- cancel := Subscribe(event, HandlerFunc)            — fire-and-forget subscription. Call cancel() to unsubscribe.
- cancel := SubscribeRPC(event, ReplyHandlerFunc)    — RPC subscription (must return a reply map). Call cancel() to unsubscribe.
- Publish(event, data)                               — broadcast, non-blocking, each handler in its goroutine.
- Request(event, data, timeout) (map, error)         — RPC: blocks until first reply or timeout.
- Wildcard: Subscribe("*", handler)                  — receives all events, cannot reply in RPC.
- Trace: "_trace_id" is auto-injected into every delivery. All handlers of the same
  Publish/Request share the same trace ID. Propagate it in derived events:
    bus.Publish("other.event", map[string]any{"_trace_id": data["_trace_id"], ...})
  Subscribe("*", ...) + read "_trace_id" for full event chain observability.`
}

// ─── Subscribe ────────────────────────────────────────────────────────────────

// Subscribe registers a fire-and-forget handler. Returns a cancel func that
// removes exactly this subscription — safe even if the same method is subscribed
// multiple times (each gets a unique ID).
func (b *InMemoryEventBusTool) Subscribe(eventName string, handler HandlerFunc) CancelFunc {
	id := b.seq.Add(1)
	b.mu.Lock()
	b.subscribers[eventName] = append(b.subscribers[eventName], subscriber{id: id, handler: handler})
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { b.cancel(eventName, id) })
	}
}

// SubscribeRPC registers a request/reply handler. Returns a cancel func.
func (b *InMemoryEventBusTool) SubscribeRPC(eventName string, handler ReplyHandlerFunc) CancelFunc {
	id := b.seq.Add(1)
	b.mu.Lock()
	b.subscribers[eventName] = append(b.subscribers[eventName], subscriber{id: id, rpcHandler: handler})
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { b.cancel(eventName, id) })
	}
}

func (b *InMemoryEventBusTool) cancel(eventName string, id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[eventName]
	for i, s := range subs {
		if s.id == id {
			b.subscribers[eventName] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

// ─── Publish ──────────────────────────────────────────────────────────────────

// Publish broadcasts data to all direct and wildcard subscribers.
// Automatically injects "_trace_id" so all handlers of this publish share the same trace.
// Each subscriber runs in its own goroutine — truly fire-and-forget.
func (b *InMemoryEventBusTool) Publish(eventName string, data map[string]any) {
	data = withTraceID(data) // all handlers for this Publish share the same trace ID
	direct, wildcard := b.collect(eventName)
	for _, s := range append(direct, wildcard...) {
		s := s
		if s.handler != nil {
			go func() {
				defer b.recover(eventName)
				s.handler(data)
			}()
		}
		// RPC handlers in Publish are called fire-and-forget (reply ignored)
		if s.rpcHandler != nil {
			go func() {
				defer b.recover(eventName)
				s.rpcHandler(data)
			}()
		}
	}
}

// ─── Request / Reply ──────────────────────────────────────────────────────────

// Request fires an event and blocks until the first RPC subscriber replies
// or timeout elapses. Only SubscribeRPC handlers can reply.
// Wildcard subscribers observe the event but cannot reply.
// Automatically injects "_trace_id" shared across all handlers for this request.
func (b *InMemoryEventBusTool) Request(eventName string, data map[string]any, timeout time.Duration) (map[string]any, error) {
	data = withTraceID(data) // all handlers + the reply share the same trace ID
	direct, wildcard := b.collect(eventName)

	// Wildcard subs observe only — fire and forget
	for _, s := range wildcard {
		s := s
		if s.handler != nil {
			go func() {
				defer b.recover(eventName)
				s.handler(data)
			}()
		}
	}

	replyCh := make(chan map[string]any, 1)
	rpcCount := 0
	for _, s := range direct {
		s := s
		if s.rpcHandler == nil {
			if s.handler != nil {
				go func() {
					defer b.recover(eventName)
					s.handler(data)
				}()
			}
			continue
		}
		rpcCount++
		go func() {
			defer b.recover(eventName)
			reply := s.rpcHandler(data)
			if reply != nil {
				select {
				case replyCh <- reply:
				default:
				}
			}
		}()
	}

	if rpcCount == 0 {
		return nil, fmt.Errorf("event_bus: no RPC subscriber for %q", eventName)
	}

	select {
	case reply := <-replyCh:
		return reply, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("event_bus: request %q timed out after %s", eventName, timeout)
	}
}

// ─── Internal ─────────────────────────────────────────────────────────────────

func (b *InMemoryEventBusTool) collect(eventName string) (direct, wildcard []subscriber) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	direct = append([]subscriber(nil), b.subscribers[eventName]...)
	if eventName != "*" {
		wildcard = append([]subscriber(nil), b.subscribers["*"]...)
	}
	return
}

func (b *InMemoryEventBusTool) recover(eventName string) {
	if r := recover(); r != nil {
		fmt.Printf("[EventBus] 💥 Panic in handler for %q: %v\n", eventName, r)
	}
}

// ─── Trace helpers ────────────────────────────────────────────────────────────

// withTraceID returns a shallow copy of data with "_trace_id" injected.
// If "_trace_id" already exists in data (propagated from a parent event), it is preserved.
// The original map is never mutated.
func withTraceID(data map[string]any) map[string]any {
	enriched := make(map[string]any, len(data)+1)
	for k, v := range data {
		enriched[k] = v
	}
	if _, ok := enriched["_trace_id"]; !ok {
		enriched["_trace_id"] = newTraceID()
	}
	return enriched
}

// newTraceID generates a cryptographically random 16-char hex string.
func newTraceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "err-trace-id"
	}
	return hex.EncodeToString(b)
}
