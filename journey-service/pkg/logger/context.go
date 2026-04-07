package logger

import "context"

type contextKey string

const loggerContextKey contextKey = "logger"

// WithContext stores a logger in context for downstream layers.
func WithContext(ctx context.Context, log *Logger) context.Context {
	if ctx == nil || log == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey, log)
}

// FromContext returns the logger stored in context, or the global logger.
func FromContext(ctx context.Context) *Logger {
	if ctx != nil {
		if log, ok := ctx.Value(loggerContextKey).(*Logger); ok && log != nil {
			return log
		}
	}
	return Global()
}
