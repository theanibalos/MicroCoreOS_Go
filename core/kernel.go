package core

import (
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// ─── Global self-registration ─────────────────────────────────────────────────
// Populated by init() in tool/plugin packages (same pattern as database/sql drivers).

var (
	registeredToolFactories   []func() Tool
	registeredPluginFactories []func() Plugin
	registrationMu            sync.Mutex
)

// RegisterTool registers a tool factory. Called from init() in tool packages.
func RegisterTool(factory func() Tool) {
	registrationMu.Lock()
	defer registrationMu.Unlock()
	registeredToolFactories = append(registeredToolFactories, factory)
}

// RegisterPlugin registers a plugin factory. Called from init() in plugin packages.
func RegisterPlugin(factory func() Plugin) {
	registrationMu.Lock()
	defer registrationMu.Unlock()
	registeredPluginFactories = append(registeredPluginFactories, factory)
}

// ─── Kernel ───────────────────────────────────────────────────────────────────

// Kernel is the system orchestrator.
// It boots tools, injects dependencies into plugins, and manages the lifecycle.
type Kernel struct {
	Container *Container
	plugins   map[string]Plugin
}

// NewKernel creates a new Kernel with an empty Container.
func NewKernel() *Kernel {
	return &Kernel{
		Container: NewContainer(),
		plugins:   make(map[string]Plugin),
	}
}

// checkStaleImports compares the number of core.Register* calls found in source
// against the factories actually registered via init(). A mismatch means
// someone added a tool/plugin but forgot to run `bash gen-imports.sh`.
func checkStaleImports(registeredTools, registeredPlugins int) {
	expectedTools := countSourceRegistrations("core.RegisterTool(")
	expectedPlugins := countSourceRegistrations("core.RegisterPlugin(")
	if expectedTools == registeredTools && expectedPlugins == registeredPlugins {
		return
	}
	fmt.Println("[Kernel] ⚠️  ─────────────────────────────────────────────────")
	fmt.Printf("[Kernel] ⚠️  STALE IMPORTS DETECTED\n")
	fmt.Printf("[Kernel] ⚠️  Source declares: %d tools, %d plugins\n", expectedTools, expectedPlugins)
	fmt.Printf("[Kernel] ⚠️  Actually booted: %d tools, %d plugins\n", registeredTools, registeredPlugins)
	fmt.Println("[Kernel] ⚠️  → Run: bash gen-imports.sh && go build .")
	fmt.Println("[Kernel] ⚠️  ─────────────────────────────────────────────────")
}

// countSourceRegistrations walks tools/ and domains/ counting occurrences of pattern.
func countSourceRegistrations(pattern string) int {
	count := 0
	for _, root := range []string{"tools", "domains"} {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error { //nolint:errcheck
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			count += strings.Count(string(content), pattern)
			return nil
		})
	}
	return count
}

// Boot starts the system: tools first (parallel), then plugins (parallel),
// then post-boot hooks for tools.
func (k *Kernel) Boot() error {
	fmt.Println("--- [Kernel] Starting System ---")
	checkStaleImports(len(registeredToolFactories), len(registeredPluginFactories))

	// ── Phase 1: Boot Tools (parallel) ──────────────────────────────────────
	var toolWg sync.WaitGroup
	for _, factory := range registeredToolFactories {
		tool := factory()
		toolWg.Add(1)
		go func(t Tool) {
			defer toolWg.Done()
			if err := t.Setup(); err != nil {
				k.Container.Registry.RegisterTool(t.Name(), "FAIL", err.Error())
				fmt.Printf("[Kernel] 🚨 Tool '%s' failed: %v\n", t.Name(), err)
				return
			}
			k.Container.Register(t)
			k.Container.Registry.RegisterTool(t.Name(), "OK")
			fmt.Printf("[Kernel] Tool ready: %s\n", t.Name())
		}(tool)
	}
	toolWg.Wait()

	// ── Phase 2: Boot Plugins (parallel Inject + parallel OnBoot) ───────────
	// Tools are all registered. Plugins are independent of each other.
	// No race condition: each plugin only reads from the Container (RLock).
	type readyPlugin struct {
		name   string
		plugin Plugin
	}
	ready := make(chan readyPlugin, len(registeredPluginFactories))

	var injectWg sync.WaitGroup
	for _, factory := range registeredPluginFactories {
		plugin := factory()
		name := plugin.Name()
		k.Container.Registry.RegisterPlugin(name, &PluginStatus{Class: name})

		injectWg.Add(1)
		go func(p Plugin, n string) {
			defer injectWg.Done()
			if err := p.Inject(k.Container); err != nil {
				fmt.Printf("[Kernel] 🚨 Plugin %s aborted: %v\n", n, err)
				k.Container.Registry.UpdatePluginStatus(n, "DEAD", err.Error())
				return
			}
			k.Container.Registry.UpdatePluginStatus(n, "RUNNING")
			ready <- readyPlugin{name: n, plugin: p}
		}(plugin, name)
	}

	// Close the channel once all injects are done.
	go func() {
		injectWg.Wait()
		close(ready)
	}()

	// Collect surviving plugins and boot them in parallel.
	var bootWg sync.WaitGroup
	for rp := range ready {
		k.plugins[rp.name] = rp.plugin
		bootWg.Add(1)
		go func(p Plugin, n string) {
			defer bootWg.Done()
			if err := p.OnBoot(); err != nil {
				fmt.Printf("[Kernel] ⚠️  Failure in %s: %v\n", n, err)
				k.Container.Registry.UpdatePluginStatus(n, "DEAD", err.Error())
				return
			}
			fmt.Printf("[Kernel] Plugin ready: %s\n", n)
			k.Container.Registry.UpdatePluginStatus(n, "READY")
		}(rp.plugin, rp.name)
	}
	bootWg.Wait()

	// ── Phase 3: Post-boot hooks ────────────────────────────────────────────
	for _, toolName := range k.Container.ListTools() {
		tool, _ := k.Container.Get(toolName)
		if err := tool.OnBootComplete(k.Container); err != nil {
			fmt.Printf("[Kernel] 🚨 Post-boot failure in '%s': %v\n", toolName, err)
			k.Container.Registry.UpdateToolStatus(toolName, "DEGRADED", err.Error())
		}
	}

	fmt.Println("--- [Kernel] System Ready ---")
	return nil
}

// WaitForShutdown blocks until SIGINT or SIGTERM, then shuts down gracefully.
func (k *Kernel) WaitForShutdown() {
	fmt.Println("\n🚀 [MicroCoreOS] System Online. (Ctrl+C to exit)")
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	k.Shutdown()
}

// Shutdown gracefully stops the system in dependency-safe order:
//  1. Tools implementing FirstShutdown (e.g. HTTP server drains in-flight requests)
//  2. Plugins (release business logic resources, cancel subscriptions)
//  3. Remaining tools (DB connections, event bus, etc.)
func (k *Kernel) Shutdown() {
	fmt.Println("\n--- [Kernel] Shutting down ---")

	// Phase 1: priority tools — must drain before plugins release their resources.
	for _, toolName := range k.Container.ListTools() {
		tool, _ := k.Container.Get(toolName)
		fs, ok := tool.(FirstShutdown)
		if !ok {
			continue
		}
		if err := fs.ShutdownFirst(); err != nil {
			fmt.Printf("[Kernel] Error draining tool '%s': %v\n", toolName, err)
		} else {
			fmt.Printf("[Kernel] Tool '%s' drained.\n", toolName)
		}
	}

	// Phase 2: plugins.
	for name, plugin := range k.plugins {
		if err := plugin.Shutdown(); err != nil {
			fmt.Printf("[Kernel] Error closing plugin '%s': %v\n", name, err)
		}
	}

	// Phase 3: remaining tools.
	for _, toolName := range k.Container.ListTools() {
		tool, _ := k.Container.Get(toolName)
		if err := tool.Shutdown(); err != nil {
			fmt.Printf("[Kernel] Error closing tool '%s': %v\n", toolName, err)
		} else {
			fmt.Printf("[Kernel] Tool '%s' closed.\n", toolName)
		}
	}

	fmt.Println("[MicroCoreOS] Shutdown complete. See you soon!")
}
