package core

// Tool is the interface that all system tools must implement.
// Tools are infrastructure components (HTTP server, database, event bus, etc.)
// that are booted before plugins and provide services to them.
type Tool interface {
	// Name returns the unique identifier used for DI (e.g. "http", "db", "event_bus").
	Name() string
	// Setup initializes the tool (called in parallel with other tools).
	Setup() error
	// GetInterfaceDescription returns a human-readable description of the tool's API.
	GetInterfaceDescription() string
	// OnBootComplete is called after ALL tools and plugins have booted.
	OnBootComplete(c *Container) error
	// Shutdown is called during system shutdown for resource cleanup.
	Shutdown() error
}

// FirstShutdown is an optional interface for Tools that must fully shut down
// before plugins begin their shutdown sequence.
//
// Implement this on tools that hold connections plugins depend on at runtime.
// The canonical example is the HTTP server: it must stop accepting new requests
// and drain in-flight ones before plugins release their DB handles or bus subscriptions.
//
// The Kernel calls ShutdownFirst() on all tools that implement this interface,
// waits for them to complete, then shuts down plugins, then calls Shutdown() on
// all remaining tools. Tools implementing FirstShutdown should make Shutdown() a no-op.
type FirstShutdown interface {
	ShutdownFirst() error
}

// BaseToolDefaults provides no-op defaults for optional Tool lifecycle methods.
// Embed this in your tool struct to only implement the required methods:
//
//	type MyTool struct {
//	    core.BaseToolDefaults
//	}
type BaseToolDefaults struct{}

func (BaseToolDefaults) OnBootComplete(_ *Container) error { return nil }
func (BaseToolDefaults) Shutdown() error                   { return nil }
func (BaseToolDefaults) GetInterfaceDescription() string   { return "" }
