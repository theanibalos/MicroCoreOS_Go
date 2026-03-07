package contexttool

import (
	"os"
	"strings"
	"testing"

	"microcoreos-go/core"
)

type mockTool struct {
	core.BaseToolDefaults
}

func (m *mockTool) Name() string                    { return "test_tool" }
func (m *mockTool) Setup() error                    { return nil }
func (m *mockTool) GetInterfaceDescription() string { return "Mock description" }

func TestContextTool(t *testing.T) {
	tempFile := "TEST_CONTEXT.md"
	os.Setenv("AI_CONTEXT_PATH", tempFile)
	defer os.Unsetenv("AI_CONTEXT_PATH")
	defer os.Remove(tempFile)

	tool := NewContextTool()
	container := core.NewContainer()

	// Register a real tool in the container, and status in registry
	container.Register(&mockTool{})
	container.Registry.RegisterTool("test_tool", "OK")
	container.Registry.RegisterPlugin("test_plugin", &core.PluginStatus{Status: "READY", Class: "TestPlugin"})

	if err := tool.OnBootComplete(container); err != nil {
		t.Fatalf("OnBootComplete failed: %v", err)
	}

	content, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	sContent := string(content)
	if !strings.Contains(sContent, "# 📜 SYSTEM MANIFEST") {
		t.Error("Output missing header")
	}
	if !strings.Contains(sContent, "test_tool") {
		t.Error("Output missing test_tool")
	}
	if !strings.Contains(sContent, "test_plugin") {
		t.Error("Output missing test_plugin")
	}
}
