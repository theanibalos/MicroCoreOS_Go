package core

// Plugin is the interface that all domain plugins must implement.
// Plugins are business logic units that consume tools via dependency injection.
//
// Lifecycle:
//  1. Inject(container) — resolve tool dependencies
//  2. OnBoot()          — register endpoints, subscriptions, etc.
//  3. Shutdown()        — cleanup (optional)
type Plugin interface {
	// Name returns the plugin's unique identifier used for logging and the registry.
	Name() string
	// Inject receives the Container to resolve tool dependencies.
	// Return an error if a required tool is missing.
	Inject(c *Container) error
	// OnBoot is called after injection. Register endpoints, event subscriptions, etc.
	OnBoot() error
	// Shutdown is called during system shutdown for cleanup.
	Shutdown() error
}

// BasePluginDefaults provides a no-op Shutdown for plugins that don't need cleanup.
type BasePluginDefaults struct{}

func (BasePluginDefaults) Shutdown() error { return nil }
