package plugins

import (
	"microcoreos-go/core"
	"microcoreos-go/domains/users/models"
	"microcoreos-go/tools/authtool"
	"microcoreos-go/tools/httptool"
	"microcoreos-go/tools/postgresdbtool"
)

// GetMeResponse handles errors
type GetMeResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GetMePlugin retrieves the profile of the currently authenticated user.
type GetMePlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
	db   postgresdbtool.PostgresTool
	auth authtool.AuthTool
}

func init() {
	core.RegisterPlugin(func() core.Plugin { return &GetMePlugin{} })
}

func (p *GetMePlugin) Name() string { return "GetMePlugin" }

func (p *GetMePlugin) Inject(c *core.Container) error {
	var err error
	if p.http, err = core.GetTool[httptool.HttpTool](c, "http"); err != nil {
		return err
	}
	if p.db, err = core.GetTool[postgresdbtool.PostgresTool](c, "db"); err != nil {
		return err
	}
	p.auth, err = core.GetTool[authtool.AuthTool](c, "auth")
	return err
}

func (p *GetMePlugin) OnBoot() error {
	p.http.AddEndpoint("/users/me", "GET", p.execute, p.auth.ValidateToken)
	return nil
}

func (p *GetMePlugin) execute(ctx *httptool.HttpContext) (any, error) {
	claims, ok := ctx.Request.Context().Value(httptool.AuthClaimsKey{}).(map[string]any)
	if !ok {
		ctx.SetStatus(500)
		return GetMeResponse{Success: false, Error: "Failed to read auth claims"}, nil
	}

	// JWT encodes numbers as float64.
	var userID int64
	switch v := claims["sub"].(type) {
	case float64:
		userID = int64(v)
	case int64:
		userID = v
	default:
		ctx.SetStatus(500)
		return GetMeResponse{Success: false, Error: "Invalid user ID format in claims"}, nil
	}

	row, err := p.db.QueryOne("SELECT id, username, email, created_at FROM users WHERE id = $1", userID)
	if err != nil || row == nil {
		ctx.SetStatus(404)
		return GetMeResponse{Success: false, Error: "User not found"}, nil
	}

	user, err := postgresdbtool.ScanOne[models.User](row)
	if err != nil {
		return nil, err
	}
	return user, nil
}
