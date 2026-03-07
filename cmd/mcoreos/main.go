package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func main() {
	if len(os.Args) < 4 || os.Args[1] != "new" || os.Args[2] != "plugin" {
		fmt.Println("Usage: mcoreos new plugin [domain]/[name]")
		fmt.Println("Example: mcoreos new plugin orders/create_order")
		os.Exit(1)
	}

	target := os.Args[3]
	parts := strings.Split(target, "/")
	if len(parts) != 2 {
		fmt.Println("Error: target must be in format [domain]/[name]")
		os.Exit(1)
	}

	domain := parts[0]
	name := parts[1]

	// Convert snake_case to CamelCase for the struct name
	structName := toCamelCase(name) + "Plugin"
	fileName := name + "_plugin.go"
	dir := filepath.Join("domains", domain, "plugins")
	filePath := filepath.Join(dir, fileName)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		os.Exit(1)
	}

	// Check if file exists
	if _, err := os.Stat(filePath); err == nil {
		fmt.Printf("Error: plugin file already exists: %s\n", filePath)
		os.Exit(1)
	}

	template := generateTemplate(domain, name, structName)
	if err := os.WriteFile(filePath, []byte(template), 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Plugin created: %s\n", filePath)
	fmt.Println("👉 Run: bash gen-imports.sh && go build .")
}

func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			runes := []rune(p)
			runes[0] = unicode.ToUpper(runes[0])
			parts[i] = string(runes)
		}
	}
	return strings.Join(parts, "")
}

func generateTemplate(domain, name, structName string) string {
	return fmt.Sprintf(`package plugins

import (
	"fmt"
	"microcoreos-go/core"
	"microcoreos-go/tools/httptool"
)

func init() {
	core.RegisterPlugin(func() core.Plugin { return New%s() })
}

type %s struct {
	core.BasePluginDefaults
	http httptool.HttpTool
}

func New%s() *%s {
	return &%s{}
}

func (p *%s) Name() string { return "%s" }

func (p *%s) Inject(c *core.Container) error {
	var err error
	p.http, err = core.GetTool[httptool.HttpTool](c, "http")
	return err
}

func (p *%s) OnBoot() error {
	fmt.Println("[%s] Booting...")
	
	p.http.AddEndpoint("/%s/%s", "GET", func(ctx *httptool.HttpContext) any {
		return map[string]any{"message": "Hello from %s"}
	}, nil)

	return nil
}

func (p *%s) Shutdown() error {
	fmt.Println("[%s] Shutting down...")
	return nil
}
`, structName, structName, structName, structName, structName, structName, name, structName, structName, structName, domain, name, name, structName, structName)
}
