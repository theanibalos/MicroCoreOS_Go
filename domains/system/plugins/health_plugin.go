package plugins

import (
	"microcoreos-go/core"
	"microcoreos-go/tools/httptool"
)

func init() {
	core.RegisterPlugin(func() core.Plugin { return NewHealthPlugin() })
}

type HealthPlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
	c    *core.Container
}

func NewHealthPlugin() *HealthPlugin {
	return &HealthPlugin{}
}

func (p *HealthPlugin) Name() string { return "health" }

func (p *HealthPlugin) Inject(c *core.Container) error {
	var err error
	p.c = c
	p.http, err = core.GetTool[httptool.HttpTool](c, "http")
	return err
}

func (p *HealthPlugin) OnBoot() error {
	p.http.AddEndpoint("/health", "GET", func(ctx *httptool.HttpContext) (any, error) {
		return p.c.Registry.GetSystemDump(), nil
	}, nil)

	return nil
}

func (p *HealthPlugin) Shutdown() error {
	return nil
}
