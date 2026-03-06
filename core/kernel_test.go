package core

// Kernel tests manipulate global registration state — do NOT call t.Parallel()
// in any test here. The stale-import check may print warnings to stdout when
// test factories are registered (since tools/ and domains/ dirs don't exist
// relative to the core/ package dir). This is expected noise; tests still pass.

import (
	"errors"
	"testing"
)

func TestKernel_Boot_NoToolsNoPlugins(t *testing.T) {
	withGlobals(t, nil, nil)
	k := NewKernel()
	if err := k.Boot(); err != nil {
		t.Fatalf("Boot() failed: %v", err)
	}
}

func TestKernel_Boot_ToolSuccess(t *testing.T) {
	tool := &mockTool{name: "testtool"}
	withGlobals(t, []func() Tool{func() Tool { return tool }}, nil)

	k := NewKernel()
	if err := k.Boot(); err != nil {
		t.Fatalf("Boot() failed: %v", err)
	}

	if !k.Container.HasTool("testtool") {
		t.Error("tool should be registered in the container after successful Setup()")
	}

	statuses := k.Container.Registry.GetToolStatuses()
	if statuses["testtool"] == nil {
		t.Fatal("tool should have a registry entry")
	}
	if statuses["testtool"].Status != "OK" {
		t.Errorf("expected status OK, got %s", statuses["testtool"].Status)
	}
}

func TestKernel_Boot_ToolSetupFails(t *testing.T) {
	tool := &mockTool{
		name:    "badtool",
		setupFn: func() error { return errors.New("setup failed") },
	}
	withGlobals(t, []func() Tool{func() Tool { return tool }}, nil)

	k := NewKernel()
	_ = k.Boot()

	if k.Container.HasTool("badtool") {
		t.Error("failed tool should not be registered in the container")
	}
	statuses := k.Container.Registry.GetToolStatuses()
	if statuses["badtool"] == nil {
		t.Fatal("failed tool should still have a registry entry (for observability)")
	}
	if statuses["badtool"].Status != "FAIL" {
		t.Errorf("expected FAIL, got %s", statuses["badtool"].Status)
	}
}

func TestKernel_Boot_PluginInjectFails(t *testing.T) {
	plugin := &mockPlugin{
		name:     "badplugin",
		injectFn: func(*Container) error { return errors.New("inject failed") },
	}
	withGlobals(t, nil, []func() Plugin{func() Plugin { return plugin }})

	k := NewKernel()
	_ = k.Boot()

	statuses := k.Container.Registry.GetPluginStatuses()
	if statuses["badplugin"] == nil {
		t.Fatal("plugin should have a registry entry even if Inject fails")
	}
	if statuses["badplugin"].Status != "DEAD" {
		t.Errorf("expected DEAD, got %s", statuses["badplugin"].Status)
	}
}

func TestKernel_Boot_PluginOnBootFails(t *testing.T) {
	plugin := &mockPlugin{
		name:   "bootfailplugin",
		bootFn: func() error { return errors.New("boot failed") },
	}
	withGlobals(t, nil, []func() Plugin{func() Plugin { return plugin }})

	k := NewKernel()
	_ = k.Boot()

	statuses := k.Container.Registry.GetPluginStatuses()
	if statuses["bootfailplugin"].Status != "DEAD" {
		t.Errorf("expected DEAD after OnBoot failure, got %s", statuses["bootfailplugin"].Status)
	}
}

func TestKernel_Boot_PluginReady(t *testing.T) {
	plugin := &mockPlugin{name: "readyplugin"}
	withGlobals(t, nil, []func() Plugin{func() Plugin { return plugin }})

	k := NewKernel()
	_ = k.Boot()

	statuses := k.Container.Registry.GetPluginStatuses()
	if statuses["readyplugin"].Status != "READY" {
		t.Errorf("expected READY, got %s", statuses["readyplugin"].Status)
	}
}

func TestKernel_Shutdown_NoError(t *testing.T) {
	tool := &mockTool{name: "shutdowntool"}
	plugin := &mockPlugin{name: "shutdownplugin"}
	withGlobals(t,
		[]func() Tool{func() Tool { return tool }},
		[]func() Plugin{func() Plugin { return plugin }},
	)

	k := NewKernel()
	_ = k.Boot()
	k.Shutdown() // should not panic
}

func TestKernel_Shutdown_FirstShutdown(t *testing.T) {
	tool := &firstShutdownMock{mockTool: mockTool{name: "prioritytool"}}
	withGlobals(t, []func() Tool{func() Tool { return tool }}, nil)

	k := NewKernel()
	_ = k.Boot()
	k.Shutdown()

	if !tool.firstShutdownCalled {
		t.Error("ShutdownFirst() should have been called on tool implementing FirstShutdown")
	}
}

func TestKernel_Boot_MultipleToolsAndPlugins(t *testing.T) {
	toolA := &mockTool{name: "toolA"}
	toolB := &mockTool{name: "toolB"}
	pluginA := &mockPlugin{name: "pluginA"}
	pluginB := &mockPlugin{name: "pluginB"}

	withGlobals(t,
		[]func() Tool{
			func() Tool { return toolA },
			func() Tool { return toolB },
		},
		[]func() Plugin{
			func() Plugin { return pluginA },
			func() Plugin { return pluginB },
		},
	)

	k := NewKernel()
	_ = k.Boot()

	if !k.Container.HasTool("toolA") || !k.Container.HasTool("toolB") {
		t.Error("both tools should be registered")
	}
	statuses := k.Container.Registry.GetPluginStatuses()
	for _, name := range []string{"pluginA", "pluginB"} {
		if statuses[name].Status != "READY" {
			t.Errorf("plugin %s: expected READY, got %s", name, statuses[name].Status)
		}
	}
}
