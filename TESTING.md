# Guía de Pruebas (Testing) — MicroCoreOS-Go

Este proyecto utiliza el estándar de Go para pruebas. Hemos dividido las pruebas en tres niveles para garantizar la estabilidad.

## 1. Pruebas de Herramientas (Unit Tests)

Estas pruebas verifican que cada pieza de infraestructura (`db`, `http`, `auth`, `bus`) funcione de forma aislada.

```bash
# Correr tests de una herramienta específica
go test -v ./tools/dbtool
go test -v ./tools/httptool
go test -v ./tools/eventbus
go test -v ./tools/authtool

# Correr todos los tests de herramientas de una vez
go test -v ./tools/...
```

## 2. Pruebas de Integración (Full System)

Verifican el arranque del Kernel, el registro de plugins y el cierre ordenado.

```bash
# Correr el test de integración completo
go test -v integration_test.go
```

_Nota: Este test simula un arranque real en el puerto 5001 y comprueba el endpoint `/health`._

## 3. Pruebas del Core

Verifican el motor interno (Contenedor de DI y Registro).

```bash
go test -v ./core/...
```

---

## Tips Pro:

1. **Clean Slate**: Si has modificado plugins o herramientas, recuerda regenerar los imports siempre:

   ```bash
   bash gen-imports.sh && go test -v ./...
   ```

2. **Ver logs de tests**: Go cachea los tests exitosos. Para forzar la ejecución y ver todos los logs:

   ```bash
   go test -count=1 -v ./...
   ```

3. **Race Detection**: Para sistemas basados en eventos como el `eventbus`, es recomendable usar el detector de carreras:
   ```bash
   go test -race ./tools/eventbus
   ```
