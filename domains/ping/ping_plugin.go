package ping

import (
	"microcoreos-go/core"
	"microcoreos-go/tools/httptool"
)

// PingResponse is the explicit struct model for this endpoint.
type PingResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Message string `json:"message"`
	} `json:"data"`
}

// PingPlugin is a sample plugin that registers a /ping endpoint.
// Demonstrates the plugin lifecycle: Inject → OnBoot → handler.
type PingPlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
}

func init() {
	core.RegisterPlugin(func() core.Plugin { return &PingPlugin{} })
}

func (p *PingPlugin) Name() string { return "PingPlugin" }

func (p *PingPlugin) Inject(c *core.Container) error {
	var err error
	p.http, err = core.GetTool[httptool.HttpTool](c, "http")
	return err
}

func (p *PingPlugin) OnBoot() error {
	p.http.AddEndpoint("/ping", "GET", p.Execute, nil)
	return nil
}

// Execute handles GET /ping → {"success": true, "data": {"message": "pong 🏓"}}
func (p *PingPlugin) Execute(ctx *httptool.HttpContext) (any, error) {
	return PingResponse{
		Success: true,
		Data: struct {
			Message string `json:"message"`
		}{
			Message: "pong 🏓",
		},
	}, nil
}
