package configtool

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"microcoreos-go/core"
)

func init() {
	core.RegisterTool(func() core.Tool { return New() })
}

// ConfigTool provides centralized environment variable management and validation.
type ConfigTool interface {
	// Require ensures that all given keys exist in the environment.
	// Fails early by returning an error if any key is missing.
	Require(keys ...string) error
	// Get retrieves a string value, returning the default if missing.
	Get(key string, defaultValue string) string
	// GetInt retrieves an integer value.
	GetInt(key string, defaultValue int) int
	// GetBool retrieves a boolean value (true if "true", "1", "yes").
	GetBool(key string, defaultValue bool) bool
}

type tool struct {
	core.BaseToolDefaults
}

func New() *tool {
	return &tool{}
}

func (t *tool) Name() string { return "config" }

func (t *tool) Setup() error {
	return nil
}

func (t *tool) GetInterfaceDescription() string {
	return `Config Tool (config): Centralized ENV management.
- Require(keys...) error  — fails if any key is missing.
- Get(key, default)       — returns string.
- GetInt(key, default)    — returns int.
- GetBool(key, default)   — returns bool.`
}

func (t *tool) Require(keys ...string) error {
	var missing []string
	for _, k := range keys {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (t *tool) Get(key string, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}

func (t *tool) GetInt(key string, defaultValue int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func (t *tool) GetBool(key string, defaultValue bool) bool {
	val := strings.ToLower(os.Getenv(key))
	if val == "" {
		return defaultValue
	}
	return val == "true" || val == "1" || val == "yes"
}
