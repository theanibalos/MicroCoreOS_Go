package loggertool

import (
	"os"
	"testing"
)

func TestLoggerTool(t *testing.T) {
	os.Setenv("LOG_LEVEL", "DEBUG")
	os.Setenv("LOG_FORMAT", "TEXT")
	defer os.Unsetenv("LOG_LEVEL")
	defer os.Unsetenv("LOG_FORMAT")

	tool := New()
	if err := tool.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	if tool.Name() != "logger" {
		t.Errorf("Expected name 'logger', got %s", tool.Name())
	}

	// Just verify these don't panic
	tool.Debug("test debug")
	tool.Info("test info", "key", "value")

	child := tool.With("context", "request123")
	child.Info("child log")

	if desc := tool.GetInterfaceDescription(); desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestLoggerToolJSON(t *testing.T) {
	os.Setenv("LOG_FORMAT", "JSON")
	defer os.Unsetenv("LOG_FORMAT")

	tool := New()
	if err := tool.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	tool.Info("json log test")
}
