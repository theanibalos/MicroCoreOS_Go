package loggertool

import (
	"log/slog"
	"microcoreos-go/core"
	"os"
	"strings"
)

func init() {
	core.RegisterTool(func() core.Tool { return New() })
}

// LoggerTool provides structured logging to plugins and tools.
//
// Usage in Inject():
//
//	p.log, err = core.GetTool[loggertool.LoggerTool](c, "logger")
type LoggerTool interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	// With returns a child logger with the given key-value pairs pre-attached.
	// Follows slog conventions: alternating key, value or slog.Attr.
	With(args ...any) LoggerTool
}

type loggerTool struct {
	core.BaseToolDefaults
	logger *slog.Logger
}

// New creates an unconfigured loggerTool. Setup() must be called before use.
func New() *loggerTool {
	return &loggerTool{}
}

func (l *loggerTool) Name() string { return "logger" }

func (l *loggerTool) Setup() error {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	format := strings.ToLower(os.Getenv("LOG_FORMAT"))

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	l.logger = slog.New(handler)
	return nil
}

func (l *loggerTool) GetInterfaceDescription() string {
	return `Logger Tool (logger): Structured logging via log/slog.
- Debug(msg, args...) — debug-level structured log.
- Info(msg, args...)  — info-level structured log.
- Warn(msg, args...)  — warn-level structured log.
- Error(msg, args...) — error-level structured log.
- With(args...) LoggerTool — child logger with pre-attached fields.
Args follow slog conventions: alternating key/value pairs or slog.Attr.
Config: LOG_LEVEL=DEBUG|INFO|WARN|ERROR (default INFO), LOG_FORMAT=text|json (default text).`
}

func (l *loggerTool) Debug(msg string, args ...any) { l.logger.Debug(msg, args...) }
func (l *loggerTool) Info(msg string, args ...any)  { l.logger.Info(msg, args...) }
func (l *loggerTool) Warn(msg string, args ...any)  { l.logger.Warn(msg, args...) }
func (l *loggerTool) Error(msg string, args ...any) { l.logger.Error(msg, args...) }

func (l *loggerTool) With(args ...any) LoggerTool {
	return &loggerTool{logger: l.logger.With(args...)}
}

func parseLevel(s string) slog.Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
