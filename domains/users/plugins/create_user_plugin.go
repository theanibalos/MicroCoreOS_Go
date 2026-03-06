package plugins

import (
	"encoding/json"
	"fmt"

	"microcoreos-go/core"
	"microcoreos-go/tools/authtool"
	"microcoreos-go/tools/dbtool"
	"microcoreos-go/tools/httptool"
)

// CreateUserRequest is the expected JSON payload for POST /users
type CreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// CreateUserResponse for success/error messages
type CreateUserResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CreateUserPlugin registers a new user in the system.
type CreateUserPlugin struct {
	core.BasePluginDefaults
	http httptool.HttpTool
	db   dbtool.DbTool
	auth authtool.AuthTool
}

func init() {
	core.RegisterPlugin(func() core.Plugin { return &CreateUserPlugin{} })
}

func (p *CreateUserPlugin) Name() string { return "CreateUserPlugin" }

func (p *CreateUserPlugin) Inject(c *core.Container) error {
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

func (p *CreateUserPlugin) OnBoot() error {
	p.http.AddEndpoint("/users", "POST", p.execute, nil)
	return nil
}

func (p *CreateUserPlugin) execute(ctx *httptool.HttpContext) any {
	var req CreateUserRequest
	if err := json.NewDecoder(ctx.Request.Body).Decode(&req); err != nil {
		ctx.SetStatus(400)
		return CreateUserResponse{Success: false, Error: "Invalid JSON body"}
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		ctx.SetStatus(400)
		return CreateUserResponse{Success: false, Error: "Missing required fields"}
	}

	hash, err := p.auth.HashPassword(req.Password)
	if err != nil {
		ctx.SetStatus(500)
		return CreateUserResponse{Success: false, Error: "Internal server error hashing password"}
	}

	id, err := p.db.Exec("INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)", req.Username, req.Email, hash)
	if err != nil {
		ctx.SetStatus(409)
		return CreateUserResponse{Success: false, Error: "Username or email already exists"}
	}

	return CreateUserResponse{Success: true, Message: fmt.Sprintf("User created successfully with ID %d", id)}
}
