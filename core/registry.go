package core

import "sync"

// Registry maintains thread-safe in-memory state for architectural awareness.
// Tracks health and metadata of tools, plugins, and domains.
type Registry struct {
	tools   map[string]*ToolStatus
	plugins map[string]*PluginStatus
	mu      sync.RWMutex
}

// ToolStatus tracks the health of a tool.
type ToolStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// PluginStatus tracks the health and metadata of a plugin.
type PluginStatus struct {
	Status       string   `json:"status"`
	Error        string   `json:"error,omitempty"`
	Class        string   `json:"class"`
	Domain       string   `json:"domain,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// NewRegistry creates a fresh Registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:   make(map[string]*ToolStatus),
		plugins: make(map[string]*PluginStatus),
	}
}

func (r *Registry) RegisterTool(name, status string, message ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := &ToolStatus{Status: status}
	if len(message) > 0 {
		entry.Message = message[0]
	}
	r.tools[name] = entry
}

func (r *Registry) UpdateToolStatus(name, status string, message ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.tools[name]; ok {
		entry.Status = status
		if len(message) > 0 {
			entry.Message = message[0]
		}
	}
}

func (r *Registry) RegisterPlugin(name string, info *PluginStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info.Status = "BOOTING"
	r.plugins[name] = info
}

func (r *Registry) UpdatePluginStatus(name, status string, errMsg ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.plugins[name]; ok {
		entry.Status = status
		if len(errMsg) > 0 {
			entry.Error = errMsg[0]
		} else {
			entry.Error = ""
		}
	}
}

// GetSystemDump returns a snapshot of all system state as a generic map (suitable for JSON).
func (r *Registry) GetSystemDump() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]any{
		"tools":   r.tools,
		"plugins": r.plugins,
	}
}

// GetToolStatuses returns a typed copy of the tool health map.
func (r *Registry) GetToolStatuses() map[string]*ToolStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copy := make(map[string]*ToolStatus, len(r.tools))
	for k, v := range r.tools {
		copy[k] = v
	}
	return copy
}

// GetPluginStatuses returns a typed copy of the plugin health map.
func (r *Registry) GetPluginStatuses() map[string]*PluginStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copy := make(map[string]*PluginStatus, len(r.plugins))
	for k, v := range r.plugins {
		copy[k] = v
	}
	return copy
}
