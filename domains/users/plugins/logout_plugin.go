package plugins

import (
	"net/http"
	"time"

	"microcoreos-go/core"
	"microcoreos-go/tools/authtool"
	"microcoreos-go/tools/httptool"
)

// LogoutResponse for success messages
type LogoutResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// LogoutPlugin handles clearing the user session cookie.
type LogoutPlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
	auth authtool.AuthTool
}

func init() {
	core.RegisterPlugin(func() core.Plugin { return &LogoutPlugin{} })
}

func (p *LogoutPlugin) Name() string { return "LogoutPlugin" }

func (p *LogoutPlugin) Inject(c *core.Container) error {
	var err error
	if p.http, err = core.GetTool[httptool.HttpTool](c, "http"); err != nil {
		return err
	}
	p.auth, err = core.GetTool[authtool.AuthTool](c, "auth")
	return err
}

func (p *LogoutPlugin) OnBoot() error {
	p.http.AddEndpoint("/auth/logout", "POST", p.execute, p.auth.ValidateToken)
	return nil
}

func (p *LogoutPlugin) execute(ctx *httptool.HttpContext) (any, error) {
	ctx.SetCookie(&http.Cookie{
		Name:     "session_token",
		Value:    "",
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	return LogoutResponse{Success: true, Message: "Logged out successfully"}, nil
}
