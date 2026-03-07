package configtool

import (
	"os"
	"testing"
)

func TestConfigTool(t *testing.T) {
	tool := New()

	t.Run("Require existing keys", func(t *testing.T) {
		os.Setenv("TEST_REQUIRED", "present")
		defer os.Unsetenv("TEST_REQUIRED")
		if err := tool.Require("TEST_REQUIRED"); err != nil {
			t.Errorf("Require failed for existing key: %v", err)
		}
	})

	t.Run("Require missing keys fails", func(t *testing.T) {
		if err := tool.Require("NON_EXISTENT_KEY_999"); err == nil {
			t.Error("Require should have failed for missing key")
		}
	})

	t.Run("Get with default", func(t *testing.T) {
		if val := tool.Get("MISSING", "default"); val != "default" {
			t.Errorf("Expected 'default', got %s", val)
		}
		os.Setenv("FOUND", "hello")
		defer os.Unsetenv("FOUND")
		if val := tool.Get("FOUND", "default"); val != "hello" {
			t.Errorf("Expected 'hello', got %s", val)
		}
	})

	t.Run("GetInt", func(t *testing.T) {
		if val := tool.GetInt("MISSING_INT", 42); val != 42 {
			t.Errorf("Expected 42, got %d", val)
		}
		os.Setenv("VAL_INT", "100")
		defer os.Unsetenv("VAL_INT")
		if val := tool.GetInt("VAL_INT", 0); val != 100 {
			t.Errorf("Expected 100, got %d", val)
		}
		os.Setenv("BAD_INT", "not-a-number")
		defer os.Unsetenv("BAD_INT")
		if val := tool.GetInt("BAD_INT", 7); val != 7 {
			t.Errorf("Expected default 7 for bad int, got %d", val)
		}
	})

	t.Run("GetBool", func(t *testing.T) {
		tests := []struct {
			envVal     string
			defaultVal bool
			expected   bool
		}{
			{"true", false, true},
			{"1", false, true},
			{"YES", false, true},
			{"false", true, false},
			{"0", true, false},
			{"", true, true},
			{"random", false, false},
		}

		for _, tt := range tests {
			if tt.envVal != "" {
				os.Setenv("VAL_BOOL", tt.envVal)
			} else {
				os.Unsetenv("VAL_BOOL")
			}
			if val := tool.GetBool("VAL_BOOL", tt.defaultVal); val != tt.expected {
				t.Errorf("For %s, expected %v, got %v", tt.envVal, tt.expected, val)
			}
		}
	})
}
