package logger

import (
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Logger wraps zerolog.Logger
type Logger struct {
	*zerolog.Logger
}

// Config holds logger configuration
type Config struct {
	Level  string
	Format string
}

// New creates a new Logger instance
func New(cfg Config) *Logger {
	var output io.Writer = os.Stdout

	// Set format
	if cfg.Format == "pretty" || cfg.Format == "console" {
		output = zerolog.ConsoleWriter{Out: os.Stdout}
	}

	// Set level
	level := parseLevel(cfg.Level)
	zerolog.SetGlobalLevel(level)

	logger := zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()

	return &Logger{Logger: &logger}
}

// parseLevel converts string level to zerolog.Level
func parseLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// WithTraceID adds trace ID to logger context
func (l *Logger) WithTraceID(traceID string) *Logger {
	logger := l.Logger.With().Str("trace_id", traceID).Logger()
	return &Logger{Logger: &logger}
}

// Global returns the global logger
func Global() *Logger {
	return &Logger{Logger: &log.Logger}
}
