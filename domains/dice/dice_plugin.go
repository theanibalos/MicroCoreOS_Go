package dice

import (
	"fmt"
	"math/rand/v2"

	"microcoreos-go/core"
	"microcoreos-go/tools/httptool"
)

// DiceRollResponse is the explicit struct model for this endpoint.
type DiceRollResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Roll  int `json:"roll"`
		Sides int `json:"sides"`
	} `json:"data"`
}

// DicePlugin registers a /dice endpoint that rolls a die.
// Supports optional "sides" query param (default: 6).
type DicePlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
}

func init() {
	core.RegisterPlugin(func() core.Plugin { return &DicePlugin{} })
}

func (p *DicePlugin) Name() string { return "DicePlugin" }

func (p *DicePlugin) Inject(c *core.Container) error {
	var err error
	p.http, err = core.GetTool[httptool.HttpTool](c, "http")
	return err
}

func (p *DicePlugin) OnBoot() error {
	p.http.AddEndpoint("/dice", "GET", p.Execute, nil)
	return nil
}

// Execute handles GET /dice?sides=6 → {"success": true, "data": {"roll": 4, "sides": 6}}
func (p *DicePlugin) Execute(ctx *httptool.HttpContext) any {
	sides := 6

	rawSides := ctx.Request.URL.Query().Get("sides")
	if rawSides != "" {
		n := 0
		if _, err := fmt.Sscanf(rawSides, "%d", &n); err == nil && n >= 2 {
			sides = n
		}
	}

	roll := rand.IntN(sides) + 1

	return DiceRollResponse{
		Success: true,
		Data: struct {
			Roll  int `json:"roll"`
			Sides int `json:"sides"`
		}{
			Roll:  roll,
			Sides: sides,
		},
	}
}
