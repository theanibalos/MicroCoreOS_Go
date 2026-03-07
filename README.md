# MicroCoreOS — Go Edition

Port of the core of [MicroCoreOS](https://github.com/theanibalos/MicroCoreOS) to Go.
Same "Atomic Microkernel" architecture, zero external dependencies (stdlib only).

## Requirements

- **Go 1.22+** ([install](https://go.dev/dl/))

## Run

To auto-discover tools/plugins and run:

```bash
go generate
go run .
```

Expected output:

```
--- [Kernel] Starting System ---
[DEBUG] Tool registered  name=http
[INFO] Tool ready  name=http
[DEBUG] Tool registered  name=logger
[INFO] Tool ready  name=logger
[AuthTool] Initializing Security Infrastructure...
[AuthTool] Ready (algorithm=HS256, expiry=60min).
[ContextTool] Ready — will generate AI_CONTEXT.md after boot.
[DEBUG] Tool registered  name=config
[INFO] Tool ready  name=config
[DEBUG] Tool registered  name=auth
[INFO] Tool ready  name=auth
[EventBus] Online.
[DEBUG] Tool registered  name=context_manager
[INFO] Tool ready  name=context_manager
[DEBUG] Tool registered  name=event_bus
[INFO] Tool ready  name=event_bus
[SqliteTool] Opening database.db...
[SqliteTool] Ready (WAL mode, FK enabled).
[DEBUG] Tool registered  name=db
[INFO] Tool ready  name=db
time=2026-03-07T07:08:42.463Z level=INFO msg="LoggerTool promoted to system logger"
time=2026-03-07T07:08:42.463Z level=INFO msg="Plugin ready" name=health
time=2026-03-07T07:08:42.463Z level=INFO msg="Plugin ready" name=PingPlugin
time=2026-03-07T07:08:42.463Z level=INFO msg="Plugin ready" name=CreateUserPlugin
time=2026-03-07T07:08:42.463Z level=INFO msg="Plugin ready" name=LoginPlugin
time=2026-03-07T07:08:42.463Z level=INFO msg="Plugin ready" name=ChatPlugin
time=2026-03-07T07:08:42.464Z level=INFO msg="Plugin ready" name=LogoutPlugin
time=2026-03-07T07:08:42.464Z level=INFO msg="Plugin ready" name=DicePlugin
time=2026-03-07T07:08:42.464Z level=INFO msg="Plugin ready" name=GetMePlugin
[ContextTool] ✅ AI_CONTEXT.md written.
[SqliteTool:db] Checking for pending migrations in "domains"...
time=2026-03-07T07:08:42.464Z level=INFO msg="Listening on http://localhost:5000" port=5000
time=2026-03-07T07:08:42.464Z level=INFO msg="--- [Kernel] System Ready ---"
🚀 [MicroCoreOS] System Online. (Ctrl+C to exit)
```

## Test

```bash
# Health (built-in)
curl http://localhost:5000/health

# Ping plugin
curl http://localhost:5000/ping
```

## Configuration

| Variable    | Default | Description |
| ----------- | ------- | ----------- |
| `HTTP_PORT` | `5000`  | Server port |

```bash
HTTP_PORT=8080 go run .
```

## Structure

```
microcoreos-go/
├── main.go                         # Entry point (blank imports = wiring)
├── core/
│   ├── tool.go                     # Tool interface
│   ├── plugin.go                   # Plugin interface
│   ├── container.go                # Service locator (DI container)
│   ├── registry.go                 # Health tracking
│   └── kernel.go                   # Boot orchestrator + self-registration
├── tools/
│   └── httptool/
│       └── httptool.go             # HTTP server (stdlib net/http)
└── domains/
    └── ping/
        └── ping_plugin.go          # Sample plugin
```

## How to create a new Tool

```go
// tools/mytool/mytool.go
package mytool

import "microcoreos-go/core"

func init() {
    core.RegisterTool(func() core.Tool { return &MyTool{} })
}

type MyTool struct {
    core.BaseToolDefaults  // no-op defaults for OnBootComplete, Shutdown, etc.
}

func (t *MyTool) Name() string  { return "mytool" }
func (t *MyTool) Setup() error  { /* init resources */ return nil }
func (t *MyTool) GetInterfaceDescription() string { return "..." }
```

Then, to register it automatically in the system (`imports_gen.go`), run in your terminal:

```bash
go generate
```

## How to create a new Plugin

```go
// domains/users/create_user_plugin.go
package users

import (
    "microcoreos-go/core"
    "microcoreos-go/tools/httptool"
)

func init() {
    core.RegisterPlugin(func() core.Plugin { return &CreateUserPlugin{} })
}

type CreateUserPlugin struct {
    core.BasePluginDefaults
    http httptool.HttpTool
}

func (p *CreateUserPlugin) Inject(c *core.Container) error {
    tool, err := c.Get("http")
    if err != nil {
        return err
    }
    p.http = tool.(httptool.HttpTool)
    return nil
}

func (p *CreateUserPlugin) OnBoot() error {
    p.http.AddEndpoint("/users", "POST", p.Execute, nil)
    return nil
}

func (p *CreateUserPlugin) Execute(ctx *httptool.HttpContext) any {
    // Read JSON body (example)
    // var data map[string]any
    // json.NewDecoder(ctx.Request.Body).Decode(&data)

    // name, _ := data["name"].(string)
    return map[string]any{"success": true, "data": map[string]any{"name": "test"}}
}
```

Just like with the tools, register it automatically by running:

```bash
go generate
```

## Rules (Same as Python)

1. **Never modify the core** — the kernel auto-discovers via `init()`
2. **1 file = 1 feature** — self-contained plugins
3. **No imports between domains** — use event bus for communication
4. **Response contract**: `{"success": bool, "data": ..., "error": ...}`

## Build (binary)

```bash
go generate
go build -o microcoreos .
./microcoreos
```
