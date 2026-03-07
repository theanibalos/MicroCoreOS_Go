package eventbus

import (
	"sync"
	"testing"
	"time"
)

func TestWildcardSubscriptions(t *testing.T) {
	bus := NewInMemoryEventBusTool()
	var wg sync.WaitGroup
	wg.Add(2)

	received := make(map[string]int)
	var mu sync.Mutex

	bus.Subscribe("*", func(data map[string]any) {
		mu.Lock()
		received["*"]++
		mu.Unlock()
		wg.Done()
	})

	bus.Publish("user.created", map[string]any{"name": "alice"})
	bus.Publish("user.updated", map[string]any{"name": "bob"})

	// Wait with timeout
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		if received["*"] != 2 {
			t.Errorf("expected 2 messages on wildcard, got %d", received["*"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for wildcard messages")
	}
}

func TestRequestReply(t *testing.T) {
	bus := NewInMemoryEventBusTool()

	// Handler that replies
	bus.SubscribeRPC("get.time", func(data map[string]any) map[string]any {
		return map[string]any{"time": "12:00"}
	})

	t.Run("Successful request", func(t *testing.T) {
		resp, err := bus.Request("get.time", nil, 500*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp["time"] != "12:00" {
			t.Errorf("expected 12:00, got %v", resp["time"])
		}
	})

	t.Run("Request timeout", func(t *testing.T) {
		// Requesting something with no subscriber
		_, err := bus.Request("get.nothing", nil, 100*time.Millisecond)
		if err == nil {
			t.Error("expected timeout error, got nil")
		}
	})
}

func TestTraceIDPropagation(t *testing.T) {
	bus := NewInMemoryEventBusTool()

	var wg sync.WaitGroup
	wg.Add(1)

	var capturedTraceID any
	bus.Subscribe("step.2", func(data map[string]any) {
		capturedTraceID = data["_trace_id"]
		wg.Done()
	})

	bus.Publish("step.2", map[string]any{"foo": "bar"})
	wg.Wait()

	if capturedTraceID == nil || capturedTraceID == "" {
		t.Error("expected _trace_id to be auto-injected, but it was missing or empty")
	}
}

func TestCancelSubscription(t *testing.T) {
	bus := NewInMemoryEventBusTool()
	counter := 0
	var mu sync.Mutex

	cancel := bus.Subscribe("test.cancel", func(data map[string]any) {
		mu.Lock()
		counter++
		mu.Unlock()
	})

	bus.Publish("test.cancel", nil)

	// Wait a bit for async delivery
	time.Sleep(50 * time.Millisecond)

	cancel()

	bus.Publish("test.cancel", nil)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if counter != 1 {
		t.Errorf("expected only 1 message after cancel, got %d", counter)
	}
	mu.Unlock()
}
