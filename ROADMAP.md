# MicroCoreOS Go — Roadmap

## Completado

- Core: Kernel, Container, Registry, DI generico (`GetTool[T]`)
- Tools: HttpTool, PostgresTool, SqliteTool, AuthTool, EventBus, LoggerTool, ConfigTool, ContextTool
  - PostgresTool y SqliteTool: paquetes atomicos self-contained, mismo API ($1 placeholders), migrations automaticas
- Tests: core (Container, Registry, Kernel), todos los tools (unit + integracion)
- Test de integracion full-boot con SQLite in-memory + HTTP real
- Endpoint `/health` expone Registry como JSON
- CLI de scaffolding: `go run ./cmd/mcoreos new plugin domain/name`
- gen-imports.sh + pre-commit hook
- docker-compose.yml para Postgres dev
- docs/swap-database.md — guia de migracion entre DBs

---

## Fase 1 — Middlewares HTTP

El `AddMiddleware` ya existe en HttpTool pero no hay implementaciones. Todo API necesita esto.

| Middleware        | Descripcion                                             | Prioridad |
| ----------------- | ------------------------------------------------------- | --------- |
| Panic recovery    | Captura panics en handlers, responde 500, loggea stack  | Alta      |
| CORS              | Headers de cross-origin configurables via env           | Alta      |
| Request ID        | Genera `X-Request-ID` por request, lo propaga al logger | Media     |
| Rate limiting     | Por IP, configurable (requests/segundo)                 | Media     |
| Request timeout   | Cancela requests que tarden mas de N segundos           | Media     |

Todos van como plugins que llaman `http.AddMiddleware(fn)` en `OnBoot` — sin tocar el core.

---

## Fase 2 — Observabilidad

- Migrar `fmt.Printf` del Kernel/Container a LoggerTool
  - El Kernel toma LoggerTool directamente despues de que los tools arrancan
  - Resultado: logs estructurados desde el primer boot, `LOG_LEVEL` / `LOG_FORMAT` desde el inicio

---

## Fase 3 — Developer Experience

- **Plugin test utilities** — `tools/testutils/`
  - `MockHttpTool`, `MockDbTool`, `MockEventBus` para testear plugins en aislamiento
  - Sin levantar el Kernel ni herramientas reales
  - El gap mas doloroso para quien escribe plugins nuevos
- **ContextTool — Schema Versioning**
  - Parsear `domains/*/migrations/*.sql` y listar tablas + columnas en `AI_CONTEXT.md`
  - Resultado: la IA sabe exactamente el schema sin leer archivos
- **ContextTool — Event Map**
  - Escanear usos de `Subscribe` y `Publish` para listar el flujo de eventos
  - Resultado: la IA puede razonar sobre comunicacion entre dominios

---

## Fase 4 — Production Readiness

- **Graceful shutdown timeout** — matar forzado si shutdown tarda mas de N segundos
- **WebSocket support** — gorilla/websocket ya esta en go.mod, falta la implementacion en HttpTool
- **Health check con status codes reales** — `/health` devuelve 503 si algun tool critico esta DEAD

---

## Deuda tecnica consciente (no atacar hasta Fase 3+)

- EventBus data tipada — linter con `golang.org/x/tools/go/analysis`
  - Convencion actual: eventos `"dominio.accion"`, keys en snake_case — suficiente hasta ~10 eventos
- Multi-region / multi-instance EventBus — hoy es in-process solamente
