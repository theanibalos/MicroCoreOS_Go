package core

import (
	"fmt"
	"sync"
)

// GetTool resolves a tool from the container and type-asserts it to T in one step.
// Use in Inject() to replace the two-step Get + type-assert pattern:
//
//	p.http, err = core.GetTool[httptool.HttpTool](c, "http")
func GetTool[T any](c *Container, name string) (T, error) {
	tool, err := c.Get(name)
	if err != nil {
		var zero T
		return zero, err
	}
	typed, ok := tool.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("tool '%s' does not implement the required interface (%T)", name, zero)
	}
	return typed, nil
}

// Container is the service locator for tools.
// Single responsibility: register, get, and list tools.
// Thread-safe for concurrent access during boot and runtime.
type Container struct {
	tools    map[string]Tool
	Registry *Registry
	mu       sync.RWMutex
}

// NewContainer creates an empty Container with a fresh Registry.
func NewContainer() *Container {
	return &Container{
		tools:    make(map[string]Tool),
		Registry: NewRegistry(),
	}
}

// Register adds a tool to the container, keyed by its Name().
func (c *Container) Register(tool Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools[tool.Name()] = tool
	fmt.Printf("[Container] Tool registered: %s\n", tool.Name())
}

// Get retrieves a tool by name. Returns an error if not found.
func (c *Container) Get(name string) (Tool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tool, ok := c.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool '%s' not found", name)
	}
	return tool, nil
}

// MustGet retrieves a tool by name, panicking if not found.
// Use in Inject() where a missing tool is a fatal boot error.
func (c *Container) MustGet(name string) Tool {
	tool, err := c.Get(name)
	if err != nil {
		panic(err)
	}
	return tool
}

// HasTool checks if a tool with the given name is registered.
func (c *Container) HasTool(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.tools[name]
	return ok
}

// ListTools returns the names of all registered tools.
func (c *Container) ListTools() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.tools))
	for name := range c.tools {
		names = append(names, name)
	}
	return names
}
