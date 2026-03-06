package core

import (
	"testing"
)

func TestContainer_RegisterAndGet(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	tool := &mockTool{name: "foo"}
	c.Register(tool)

	got, err := c.Get("foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tool {
		t.Fatal("got unexpected tool instance")
	}
}

func TestContainer_GetNotFound(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	_, err := c.Get("missing")
	if err == nil {
		t.Fatal("expected error for missing tool, got nil")
	}
}

func TestContainer_HasTool(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	c.Register(&mockTool{name: "bar"})

	if !c.HasTool("bar") {
		t.Error("HasTool should return true for registered tool")
	}
	if c.HasTool("missing") {
		t.Error("HasTool should return false for unregistered tool")
	}
}

func TestContainer_ListTools(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	c.Register(&mockTool{name: "a"})
	c.Register(&mockTool{name: "b"})

	names := c.ListTools()
	if len(names) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(names))
	}
}

func TestContainer_MustGet_Panics(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustGet should panic when tool is not found")
		}
	}()
	c.MustGet("missing")
}

func TestContainer_Register_Overwrites(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	first := &mockTool{name: "dup"}
	second := &mockTool{name: "dup"}
	c.Register(first)
	c.Register(second)

	got, _ := c.Get("dup")
	if got != second {
		t.Error("second registration should overwrite first")
	}
}

// ─── GetTool ─────────────────────────────────────────────────────────────────

type fooIface interface {
	Foo() string
}

type fooTool struct {
	mockTool
}

func (f *fooTool) Foo() string { return "foo" }

func TestGetTool_Success(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	c.Register(&fooTool{mockTool: mockTool{name: "foo"}})

	got, err := GetTool[fooIface](c, "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Foo() != "foo" {
		t.Errorf("unexpected Foo() value: %s", got.Foo())
	}
}

func TestGetTool_TypeMismatch(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	c.Register(&mockTool{name: "plain"})

	_, err := GetTool[fooIface](c, "plain")
	if err == nil {
		t.Fatal("expected type mismatch error, got nil")
	}
}

func TestGetTool_NotFound(t *testing.T) {
	t.Parallel()
	c := NewContainer()
	_, err := GetTool[fooIface](c, "missing")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
}
