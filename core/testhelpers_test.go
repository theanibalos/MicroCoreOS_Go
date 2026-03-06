package core

// mockTool is a minimal Tool implementation for use in core tests.
type mockTool struct {
	BaseToolDefaults
	name    string
	setupFn func() error
}

func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Setup() error {
	if m.setupFn != nil {
		return m.setupFn()
	}
	return nil
}

// firstShutdownMock is a mockTool that also implements FirstShutdown.
type firstShutdownMock struct {
	mockTool
	firstShutdownCalled bool
}

func (f *firstShutdownMock) ShutdownFirst() error {
	f.firstShutdownCalled = true
	return nil
}

// Override Shutdown as a no-op (documented convention for FirstShutdown tools).
func (f *firstShutdownMock) Shutdown() error { return nil }

// mockPlugin is a minimal Plugin implementation for use in core tests.
type mockPlugin struct {
	BasePluginDefaults
	name     string
	injectFn func(*Container) error
	bootFn   func() error
}

func (m *mockPlugin) Name() string { return m.name }
func (m *mockPlugin) Inject(c *Container) error {
	if m.injectFn != nil {
		return m.injectFn(c)
	}
	return nil
}
func (m *mockPlugin) OnBoot() error {
	if m.bootFn != nil {
		return m.bootFn()
	}
	return nil
}

// withGlobals temporarily replaces the global registration lists and restores
// them after the test. Kernel tests must NOT call t.Parallel().
func withGlobals(t interface{ Cleanup(func()) }, tools []func() Tool, plugins []func() Plugin) {
	origTools := registeredToolFactories
	origPlugins := registeredPluginFactories
	registeredToolFactories = tools
	registeredPluginFactories = plugins
	t.Cleanup(func() {
		registeredToolFactories = origTools
		registeredPluginFactories = origPlugins
	})
}
