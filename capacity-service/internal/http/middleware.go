package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/metrics"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/tracing"
)

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			baseLog := log
			if baseLog == nil {
				baseLog = logger.Global()
			}

			if traceID := tracing.GetTraceID(r.Context()); traceID != "" {
				baseLog = baseLog.WithTraceID(traceID)
			}

			requestLogger := baseLog
			requestLogCore := requestLogger.With().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Str("user_agent", r.UserAgent()).
				Logger()
			requestLogger = &logger.Logger{Logger: &requestLogCore}

			r = r.WithContext(logger.WithContext(r.Context(), requestLogger))
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			if r.URL.Path != "/health" && r.URL.Path != "/ready" && r.URL.Path != "/metrics" {
				requestLogger.Info().Msg("http request received")
			}
			next.ServeHTTP(rec, r)

			if r.URL.Path != "/health" && r.URL.Path != "/ready" && r.URL.Path != "/metrics" {
				requestLogger.Info().
					Int("status_code", rec.status).
					Int("response_bytes", rec.bytes).
					Dur("duration", time.Since(start)).
					Msg("http request completed")
			}
		})
	}
}

// CORSMiddleware handles CORS
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Trace-ID")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps ResponseWriter to capture the HTTP status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// MetricsMiddleware records HTTP request counts and latency for Prometheus.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" || r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		metrics.HttpRequestsTotal.WithLabelValues(
			metrics.ServiceName, r.Method, r.URL.Path, fmt.Sprintf("%d", rec.status),
		).Inc()
		metrics.HttpRequestDuration.WithLabelValues(
			metrics.ServiceName, r.Method, r.URL.Path,
		).Observe(time.Since(start).Seconds())
	})
}

// AuthenticationMiddleware validates JWT tokens
func AuthenticationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: Extract token from Authorization header
		// TODO: Validate token with Keycloak
		// TODO: Add user info to context

		next.ServeHTTP(w, r)
	})
}
