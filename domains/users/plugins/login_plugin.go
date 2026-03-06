package plugins

import (
	"encoding/json"
	"net/http"
	"time"

	"microcoreos-go/core"
	"microcoreos-go/tools/authtool"
	"microcoreos-go/tools/dbtool"
	"microcoreos-go/tools/httptool"
)

// LoginRequest is the expected JSON payload for POST /auth/login
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse holds the JWT token and success status
type LoginResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"`
	Error   string `json:"error,omitempty"`
}

// loginRow is the typed projection used when reading credentials from the DB.
// Defined here (not in models) because password_hash must never leave this plugin.
type loginRow struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

// LoginPlugin handles user authentication and session creation.
type LoginPlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
	db   dbtool.DbTool
	auth authtool.AuthTool
}

func init() {
	core.RegisterPlugin(func() core.Plugin { return &LoginPlugin{} })
}

func (p *LoginPlugin) Name() string { return "LoginPlugin" }

func (p *LoginPlugin) Inject(c *core.Container) error {
	var err error
	if p.http, err = core.GetTool[httptool.HttpTool](c, "http"); err != nil {
		return err
	}
	if p.db, err = core.GetTool[dbtool.DbTool](c, "db"); err != nil {
		return err
	}
	p.auth, err = core.GetTool[authtool.AuthTool](c, "auth")
	return err
}

func (p *LoginPlugin) OnBoot() error {
	p.http.AddEndpoint("/auth/login", "POST", p.execute, nil)
	return nil
}

func (p *LoginPlugin) execute(ctx *httptool.HttpContext) any {
	var req LoginRequest
	if err := json.NewDecoder(ctx.Request.Body).Decode(&req); err != nil {
		ctx.SetStatus(400)
		return LoginResponse{Success: false, Error: "Invalid JSON body"}
	}

	row, err := p.db.QueryOne("SELECT id, username, password_hash FROM users WHERE username = ?", req.Username)
	if err != nil || row == nil {
		ctx.SetStatus(401)
		return LoginResponse{Success: false, Error: "Invalid credentials"}
	}

	creds, err := dbtool.ScanOne[loginRow](row)
	if err != nil {
		ctx.SetStatus(500)
		return LoginResponse{Success: false, Error: "Internal server error"}
	}

	if !p.auth.VerifyPassword(req.Password, creds.PasswordHash) {
		ctx.SetStatus(401)
		return LoginResponse{Success: false, Error: "Invalid credentials"}
	}

	userID := creds.ID

	token, err := p.auth.CreateToken(map[string]any{
		"sub": userID,
		"usr": req.Username,
	})
	if err != nil {
		ctx.SetStatus(500)
		return LoginResponse{Success: false, Error: "Error generating token"}
	}

	// Set browser cookie
	ctx.SetCookie(&http.Cookie{
		Name:     "session_token",
		Value:    token,
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
	})

	return LoginResponse{Success: true, Token: token}
}
