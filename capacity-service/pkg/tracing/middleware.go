package tracing

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type contextKey string

const (
	// TraceIDKey is the context key for trace ID
	TraceIDKey contextKey = "trace_id"
	// TraceIDHeader is the HTTP header name for trace ID
	TraceIDHeader = "X-Trace-ID"
)

// Middleware adds trace ID to request context
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to get trace ID from header
		traceID := r.Header.Get(TraceIDHeader)

		// Generate new trace ID if not present
		if traceID == "" {
			traceID = uuid.New().String()
		}

		// Add trace ID to context
		ctx := context.WithValue(r.Context(), TraceIDKey, traceID)
		r = r.WithContext(ctx)

		// Add trace ID to response header
		w.Header().Set(TraceIDHeader, traceID)

		next.ServeHTTP(w, r)
	})
}

// GetTraceID extracts trace ID from context
func GetTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok {
		return traceID
	}
	return ""
}
