package http

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/metrics"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

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

func CORSMiddleware(next http.Handler) http.Handler {
	allowedOrigins := parseAllowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isAllowedOrigin(origin, allowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if len(allowedOrigins) == 0 {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Trace-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
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

func parseAllowedOrigins() []string {
	raw := os.Getenv("IAM_CORS_ALLOWED_ORIGINS")
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isAllowedOrigin(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
	}
	return false
}
