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
	"time"
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
	logger    Logger
}

// NewKernel creates a new Kernel with an empty Container.
func NewKernel() *Kernel {
	return &Kernel{
		Container: NewContainer(),
		plugins:   make(map[string]Plugin),
		logger:    &defaultLogger{}, // Start with a basic logger that prints to stdout
	}
}

// defaultLogger is a fallback that uses fmt.Printf until a real LoggerTool is promoted.
type defaultLogger struct{}

func (l *defaultLogger) Debug(msg string, args ...any) { l.log("DEBUG", msg, args...) }
func (l *defaultLogger) Info(msg string, args ...any)  { l.log("INFO", msg, args...) }
func (l *defaultLogger) Warn(msg string, args ...any)  { l.log("WARN", msg, args...) }
func (l *defaultLogger) Error(msg string, args ...any) { l.log("ERROR", msg, args...) }
func (l *defaultLogger) With(args ...any) Logger       { return l }

func (l *defaultLogger) log(level, msg string, args ...any) {
	if len(args) == 0 {
		fmt.Printf("[%s] %s\n", level, msg)
		return
	}
	// Simple key=value formatting for the fallback logger
	var pairs []string
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			pairs = append(pairs, fmt.Sprintf("%v=%v", args[i], args[i+1]))
		} else {
			pairs = append(pairs, fmt.Sprintf("%v=?", args[i]))
		}
	}
	fmt.Printf("[%s] %s  %s\n", level, msg, strings.Join(pairs, " "))
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
	k.logger.Info("--- [Kernel] Starting System ---")
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
				k.logger.Error("🚨 Tool failed", "name", t.Name(), "error", err)
				return
			}
			k.Container.Register(t)
			k.Container.Registry.RegisterTool(t.Name(), "OK")
			k.logger.Info("Tool ready", "name", t.Name())
		}(tool)
	}
	toolWg.Wait()

	// ── Phase 1.5: Promote Logger Tool ──────────────────────────────────────
	// If a tool named "logger" was registered and implements core.Logger,
	// the Kernel adopts it for all subsequent system logs.
	if logTool, err := k.Container.Get("logger"); err == nil {
		if l, ok := logTool.(Logger); ok {
			k.logger = l
			k.Container.SetLogger(l)
			k.logger.Info("LoggerTool promoted to system logger")
		}
	}

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
				k.logger.Error("🚨 Plugin aborted", "name", n, "error", err)
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
				k.logger.Warn("⚠️  Failure in plugin", "name", n, "error", err)
				k.Container.Registry.UpdatePluginStatus(n, "DEAD", err.Error())
				return
			}
			k.logger.Info("Plugin ready", "name", n)
			k.Container.Registry.UpdatePluginStatus(n, "READY")
		}(rp.plugin, rp.name)
	}
	bootWg.Wait()

	// ── Phase 3: Post-boot hooks ────────────────────────────────────────────
	for _, toolName := range k.Container.ListTools() {
		tool, _ := k.Container.Get(toolName)
		if err := tool.OnBootComplete(k.Container); err != nil {
			k.logger.Error("🚨 Post-boot failure in tool", "name", toolName, "error", err)
			k.Container.Registry.UpdateToolStatus(toolName, "DEGRADED", err.Error())
		}
	}

	k.logger.Info("--- [Kernel] System Ready ---")
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
	k.logger.Info("--- [Kernel] Shutting down ---")

	// Hard timeout for shutdown to prevent hanging in production
	timer := time.AfterFunc(10*time.Second, func() {
		k.logger.Error("🚨 Shutdown timed out! Forcing exit.")
		os.Exit(1)
	})
	defer timer.Stop()

	// Phase 1: priority tools — must drain before plugins release their resources.
	for _, toolName := range k.Container.ListTools() {
		tool, _ := k.Container.Get(toolName)
		fs, ok := tool.(FirstShutdown)
		if !ok {
			continue
		}
		if err := fs.ShutdownFirst(); err != nil {
			k.logger.Error("Error draining tool", "name", toolName, "error", err)
		} else {
			k.logger.Info("Tool drained", "name", toolName)
		}
	}

	// Phase 2: plugins.
	for name, plugin := range k.plugins {
		if err := plugin.Shutdown(); err != nil {
			k.logger.Error("Error closing plugin", "name", name, "error", err)
		}
	}

	// Phase 3: remaining tools.
	for _, toolName := range k.Container.ListTools() {
		tool, _ := k.Container.Get(toolName)
		if err := tool.Shutdown(); err != nil {
			k.logger.Error("Error closing tool", "name", toolName, "error", err)
		} else {
			k.logger.Info("Tool closed", "name", toolName)
		}
	}

	k.logger.Info("[MicroCoreOS] Shutdown complete. See you soon!")
}
