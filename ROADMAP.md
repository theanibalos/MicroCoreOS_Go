# MicroCoreOS Go — Roadmap

## Completado

- Core arquitectura (Kernel, Container, Registry, DI generico)
- Tools: HTTP, DB/SQLite, Auth, EventBus, ContextTool, LoggerTool
- gen-imports.sh + pre-commit hook + stale check en boot
- Tests del core: Container, Registry, Kernel lifecycle (boot/shutdown/FirstShutdown)

---

## Fase 1 — Hardening

Tests de tools — la deuda mas urgente.

| Target | Que testear |
|---|---|
| `httptool` | middleware chain, auth validator, path params, HttpContext |
| `dbtool` | `ScanOne[T]` nil row, tipo incorrecto, transacciones |
| `eventbus` | trace ID propagation, `Request` timeout, wildcard `*` |
| `authtool` | JWT expirado, bcrypt incorrecto, claims malformados |

---

## Fase 2 — Observabilidad

- Migrar `fmt.Printf` del Kernel/Container a LoggerTool
- LoggerTool tomado directamente por el Kernel en boot (no via DI de plugins)
- Resultado: logs estructurados desde el primer boot, configurables via `LOG_LEVEL` / `LOG_FORMAT`

---

## Fase 3 — Developer Experience

- **ConfigTool** — esquema centralizado con validacion en boot
  - Vale cuando haya 5+ tools con config propia
  - Falla rapido con mensaje claro si falta una variable requerida
- **CLI de scaffolding** — `mcoreos new plugin orders/create_order`
  - Genera boilerplate de plugin con `init()`, `Inject`, `OnBoot` y estructura correcta

---

## Fase 4 — Production Readiness

- **Test de integracion** — boot completo con SQLite in-memory, HTTP real, EventBus
- **Endpoint /health** — expone el Registry como JSON (tools + plugins con su status)
- **Graceful shutdown timeout** — matar forzado si shutdown tarda mas de N segundos

---

## Deuda tecnica (no atacar hasta Fase 3+)

- EventBus data tipada — linter de eventos con `golang.org/x/tools/go/analysis`
  - Convencion actual: eventos `"dominio.accion"`, keys en snake_case — suficiente hasta ~10 eventos
- ConfigTool con validacion de esquema estilo Zod para env vars
