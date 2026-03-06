# MicroCoreOS — Go Edition

Port del core de [MicroCoreOS](https://github.com/theanibalos/MicroCoreOS) a Go.
Misma arquitectura "Atomic Microkernel", cero dependencias externas (solo stdlib).

## Requisitos

- **Go 1.22+** ([instalar](https://go.dev/dl/))

## Ejecutar

```bash
go run .
```

Output esperado:

```
--- [Kernel] Starting System ---
[HttpServer] Configuring on port 5000...
[Container] Tool registered: http
[Kernel] Tool ready: http
[Kernel] Plugin ready: PingPlugin
--- [Kernel] System Ready ---
[HttpServer] Server active → http://localhost:5000

🚀 [MicroCoreOS] System Online. (Ctrl+C to exit)
```

## Probar

```bash
# Health (built-in)
curl http://localhost:5000/health

# Ping plugin
curl http://localhost:5000/ping
```

## Configuración

| Variable    | Default | Descripción         |
| ----------- | ------- | ------------------- |
| `HTTP_PORT` | `5000`  | Puerto del servidor |

```bash
HTTP_PORT=8080 go run .
```

## Estructura

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

## Cómo crear un Tool nuevo

```go
// tools/mytool/mytool.go
package mytool

import "microcoreos-go/core"

func init() {
    core.RegisterTool(func() core.Tool { return &MyTool{} })
}

type MyTool struct {
    core.BaseToolDefaults  // no-op defaults para OnBootComplete, Shutdown, etc.
}

func (t *MyTool) Name() string  { return "mytool" }
func (t *MyTool) Setup() error  { /* init resources */ return nil }
func (t *MyTool) GetInterfaceDescription() string { return "..." }
```

Luego en `main.go` agregar el import:

```go
_ "microcoreos-go/tools/mytool"
```

## Cómo crear un Plugin nuevo

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
    p.http.AddEndpoint("/users", "POST", p.Execute)
    return nil
}

func (p *CreateUserPlugin) Execute(data map[string]any, ctx *httptool.HttpContext) map[string]any {
    name, _ := data["name"].(string)
    return map[string]any{"success": true, "data": map[string]any{"name": name}}
}
```

Luego en `main.go` agregar el import:

```go
_ "microcoreos-go/domains/users"
```

## Reglas (mismas que Python)

1. **Nunca modificar el core** — el kernel auto-descubre via `init()`
2. **1 archivo = 1 feature** — plugins auto-contenidos
3. **Sin imports entre dominios** — usar event bus para comunicación
4. **Response contract**: `{"success": bool, "data": ..., "error": ...}`

## Build (binario)

```bash
go build -o microcoreos .
./microcoreos
```
