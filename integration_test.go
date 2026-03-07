package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"microcoreos-go/core"
	_ "microcoreos-go/domains/system/plugins"
	_ "microcoreos-go/tools/httptool"
	_ "microcoreos-go/tools/loggertool"
)

func TestFullSystemIntegration(t *testing.T) {
	// Setup env for test
	os.Setenv("HTTP_PORT", "5001") // Use different port for test
	os.Setenv("AUTH_SECRET_KEY", "integration-test-secret")
	defer os.Unsetenv("HTTP_PORT")
	defer os.Unsetenv("AUTH_SECRET_KEY")

	k := core.NewKernel()

	// Start boot in goroutine because we want to test it while running
	// In a real integration test, we might wait for "System Ready" log or a signal
	errCh := make(chan error, 1)
	go func() {
		if err := k.Boot(); err != nil {
			errCh <- err
		}
	}()

	// Wait for system to be ready (max 5s)
	// We check /health endpoint
	var resp *http.Response
	var err error
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		resp, err = http.Get("http://localhost:5001/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
	}

	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("System failed to boot or /health not reachable: %v", err)
	}

	// Verify Registry content
	body, _ := io.ReadAll(resp.Body)
	var data map[string]any
	json.Unmarshal(body, &data)

	tools := data["tools"].(map[string]any)
	plugins := data["plugins"].(map[string]any)

	if tools["http"].(map[string]any)["status"] != "OK" {
		t.Errorf("HttpTool status expected OK, got %v", tools["http"])
	}

	if plugins["health"].(map[string]any)["status"] != "READY" {
		t.Errorf("HealthPlugin status expected READY, got %v", plugins["health"])
	}

	fmt.Println("Integration test: System healthy, shutting down...")

	// Verify Graceful Shutdown
	k.Shutdown()
}
