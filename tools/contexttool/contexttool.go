/*
Context Tool — Go-First implementation for MicroCoreOS
======================================================

Generates a live AI_CONTEXT.md after full system boot.

The file describes:
  - All active tools with their interface descriptions and health status.
  - All registered domains and their plugin statuses.

This is the primary artifact that an AI coding assistant reads to understand
what tools are available, their signatures, and the current system health.
It is regenerated on every boot, so it always reflects the live system.

No configuration required. Just drop this tool in and run go generate.
*/
package contexttool

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"microcoreos-go/core"
)

// ContextTool writes AI_CONTEXT.md after all tools and plugins have booted.
type ContextTool struct {
	core.BaseToolDefaults
	outputPath string
}

func init() {
	core.RegisterTool(func() core.Tool { return NewContextTool() })
}

// NewContextTool creates a ContextTool. Output path defaults to AI_CONTEXT.md.
func NewContextTool() *ContextTool {
	path := os.Getenv("AI_CONTEXT_PATH")
	if path == "" {
		path = "AI_CONTEXT.md"
	}
	return &ContextTool{outputPath: path}
}

func (c *ContextTool) Name() string { return "context_manager" }

func (c *ContextTool) Setup() error {
	fmt.Println("[ContextTool] Ready — will generate AI_CONTEXT.md after boot.")
	return nil
}

func (c *ContextTool) GetInterfaceDescription() string {
	return `Context Manager Tool (context_manager): Generates AI_CONTEXT.md after every boot.
The file is a live system manifest: active tools with their interface descriptions,
registered plugins, and their health status. AI assistants read this file to
understand what tools are available before writing new plugins.`
}

// OnBootComplete fires after all tools AND plugins are ready.
// It reads the registry snapshot and writes the manifest.
func (c *ContextTool) OnBootComplete(container *core.Container) error {
	toolStatuses := container.Registry.GetToolStatuses()
	pluginStatuses := container.Registry.GetPluginStatuses()

	var sb strings.Builder

	// ── Header ────────────────────────────────────────────────────────────────
	sb.WriteString("# 📜 SYSTEM MANIFEST\n\n")
	sb.WriteString("> **NOTICE:** This is a LIVE inventory generated on boot.\n\n")

	// ── Architecture quick ref ────────────────────────────────────────────────
	sb.WriteString("## 🏗️ Architecture\n")
	sb.WriteString("- **Pattern**: `Inject(container)` → `OnBoot()` → handler methods.\n")
	sb.WriteString("- **DI**: in `Inject()`, use `core.GetTool[ToolInterface](c, \"tool_name\")` — type-safe, no assertions.\n")
	sb.WriteString("  ```go\n")
	sb.WriteString("  func (p *MyPlugin) Inject(c *core.Container) error {\n")
	sb.WriteString("      var err error\n")
	sb.WriteString("      if p.http, err = core.GetTool[httptool.HttpTool](c, \"http\"); err != nil { return err }\n")
	sb.WriteString("      if p.db,   err = core.GetTool[dbtool.DbTool](c, \"db\");     err != nil { return err }\n")
	sb.WriteString("      p.auth, err = core.GetTool[authtool.AuthTool](c, \"auth\")\n")
	sb.WriteString("      return err\n")
	sb.WriteString("  }\n")
	sb.WriteString("  ```\n")
	sb.WriteString("- **Registration**: every tool/plugin calls `core.RegisterTool/RegisterPlugin` in its `init()` func.\n")
	sb.WriteString("- **Adding a plugin**: create `domains/<domain>/<name>_plugin.go`, implement `core.Plugin` (Name/Inject/OnBoot/Shutdown), add `init()`, run `bash gen-imports.sh`.\n")
	sb.WriteString("- **Plugin interface**: `Name() string` · `Inject(*Container) error` · `OnBoot() error` · `Shutdown() error`.\n")
	sb.WriteString("- **No plugin-to-plugin imports**: plugins communicate exclusively via the EventBus.\n\n")

	// ── Tools ─────────────────────────────────────────────────────────────────
	sb.WriteString("## 🛠️ Active Tools\n\n")

	toolNames := container.ListTools()
	sort.Strings(toolNames)

	for _, name := range toolNames {
		tool, err := container.Get(name)
		if err != nil {
			sb.WriteString(fmt.Sprintf("### 🔧 `%s` ❌\n> Error: %v\n\n", name, err))
			continue
		}

		desc := strings.TrimSpace(tool.GetInterfaceDescription())
		if desc == "" {
			desc = "(no interface description provided)"
		}

		status := "✅"
		if ts, ok := toolStatuses[name]; ok && ts.Status != "OK" {
			status = "⚠️ " + ts.Status
			if ts.Message != "" {
				status += ": " + ts.Message
			}
		}

		sb.WriteString(fmt.Sprintf("### 🔧 `%s` %s\n", name, status))
		sb.WriteString("```text\n")
		sb.WriteString(desc)
		sb.WriteString("\n```\n\n")
	}

	// ── Plugins ───────────────────────────────────────────────────────────────
	sb.WriteString("## 🧩 Active Plugins\n\n")

	pluginNames := make([]string, 0, len(pluginStatuses))
	for name := range pluginStatuses {
		pluginNames = append(pluginNames, name)
	}
	sort.Strings(pluginNames)

	statusEmoji := map[string]string{"READY": "✅", "RUNNING": "🔄", "DEAD": "💀", "BOOTING": "⏳"}
	for _, name := range pluginNames {
		ps := pluginStatuses[name]
		emoji := statusEmoji[ps.Status]
		if emoji == "" {
			emoji = "❓"
		}
		sb.WriteString(fmt.Sprintf("- %s **%s** (`%s`)", emoji, name, ps.Status))
		if ps.Error != "" {
			sb.WriteString(fmt.Sprintf(" — Error: %s", ps.Error))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// ── Domains hint ─────────────────────────────────────────────────────────
	sb.WriteString("## 📦 Domains\n\n")
	sb.WriteString("Each domain lives in `domains/<domain>/`. A domain contains one or more plugins.\n")
	sb.WriteString("To add a plugin: create `domains/<domain>/<name>_plugin.go`, implement `core.Plugin`, add `init()`, run `go generate ./...`.\n\n")

	if entries, err := os.ReadDir("domains"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				sb.WriteString(fmt.Sprintf("- `%s` → `domains/%s/`\n", e.Name(), e.Name()))
			}
		}
	}

	// ── Write file ────────────────────────────────────────────────────────────
	if err := os.WriteFile(c.outputPath, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("context_tool: failed to write %s: %w", c.outputPath, err)
	}
	fmt.Printf("[ContextTool] ✅ %s written.\n", c.outputPath)
	return nil
}
