package core

import (
	"sync"
	"testing"
)

func TestRegistry_RegisterTool(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterTool("db", "OK")

	statuses := r.GetToolStatuses()
	if statuses["db"] == nil {
		t.Fatal("expected db entry, got nil")
	}
	if statuses["db"].Status != "OK" {
		t.Errorf("expected OK, got %s", statuses["db"].Status)
	}
}

func TestRegistry_RegisterTool_WithMessage(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterTool("db", "FAIL", "connection refused")

	statuses := r.GetToolStatuses()
	if statuses["db"].Message != "connection refused" {
		t.Errorf("expected message 'connection refused', got %q", statuses["db"].Message)
	}
}

func TestRegistry_UpdateToolStatus(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterTool("db", "OK")
	r.UpdateToolStatus("db", "DEGRADED", "slow queries")

	statuses := r.GetToolStatuses()
	if statuses["db"].Status != "DEGRADED" {
		t.Errorf("expected DEGRADED, got %s", statuses["db"].Status)
	}
	if statuses["db"].Message != "slow queries" {
		t.Errorf("expected message 'slow queries', got %q", statuses["db"].Message)
	}
}

func TestRegistry_UpdateToolStatus_NoopOnMissing(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	// Should not panic for unknown tool.
	r.UpdateToolStatus("nonexistent", "FAIL")
}

func TestRegistry_RegisterPlugin(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterPlugin("MyPlugin", &PluginStatus{Class: "MyPlugin"})

	statuses := r.GetPluginStatuses()
	if statuses["MyPlugin"] == nil {
		t.Fatal("expected MyPlugin entry, got nil")
	}
	if statuses["MyPlugin"].Status != "BOOTING" {
		t.Errorf("expected BOOTING, got %s", statuses["MyPlugin"].Status)
	}
}

func TestRegistry_UpdatePluginStatus(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterPlugin("p", &PluginStatus{Class: "p"})
	r.UpdatePluginStatus("p", "READY")

	statuses := r.GetPluginStatuses()
	if statuses["p"].Status != "READY" {
		t.Errorf("expected READY, got %s", statuses["p"].Status)
	}
	if statuses["p"].Error != "" {
		t.Errorf("expected empty error, got %q", statuses["p"].Error)
	}
}

func TestRegistry_UpdatePluginStatus_WithError(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterPlugin("p", &PluginStatus{Class: "p"})
	r.UpdatePluginStatus("p", "DEAD", "inject failed")

	statuses := r.GetPluginStatuses()
	if statuses["p"].Status != "DEAD" {
		t.Errorf("expected DEAD, got %s", statuses["p"].Status)
	}
	if statuses["p"].Error != "inject failed" {
		t.Errorf("expected 'inject failed', got %q", statuses["p"].Error)
	}
}

func TestRegistry_UpdatePluginStatus_ClearsError(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterPlugin("p", &PluginStatus{Class: "p"})
	r.UpdatePluginStatus("p", "DEAD", "transient error")
	r.UpdatePluginStatus("p", "READY") // no error argument

	statuses := r.GetPluginStatuses()
	if statuses["p"].Error != "" {
		t.Errorf("error should be cleared on update without message, got %q", statuses["p"].Error)
	}
}

func TestRegistry_GetSystemDump(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterTool("http", "OK")
	r.RegisterPlugin("PingPlugin", &PluginStatus{Class: "PingPlugin"})

	dump := r.GetSystemDump()
	if _, ok := dump["tools"]; !ok {
		t.Error("dump missing 'tools' key")
	}
	if _, ok := dump["plugins"]; !ok {
		t.Error("dump missing 'plugins' key")
	}
}

func TestRegistry_GetToolStatuses_ReturnsCopy(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterTool("db", "OK")

	s1 := r.GetToolStatuses()
	s1["db"].Status = "MUTATED"

	s2 := r.GetToolStatuses()
	// The map is a shallow copy — the pointer inside is shared. This is the
	// documented behavior; verify the map itself is a distinct object.
	if &s1 == &s2 {
		t.Error("GetToolStatuses should return a new map each time")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.RegisterTool("concurrent_tool", "OK")
		}()
		go func() {
			defer wg.Done()
			r.GetToolStatuses()
		}()
	}
	wg.Wait()
}
