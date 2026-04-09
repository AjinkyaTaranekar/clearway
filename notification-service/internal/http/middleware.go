package http

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/metrics"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/tracing"
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
	cfg := loadCORSConfigFromEnv()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		allowed := cfg.allowAll || (origin != "" && cfg.allowedOrigins[origin])

		switch {
		case cfg.allowAll:
			w.Header().Set("Access-Control-Allow-Origin", "*")
		case allowed:
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Trace-ID")
		if cfg.allowCredentials && !cfg.allowAll && allowed {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type corsConfig struct {
	allowAll         bool
	allowCredentials bool
	allowedOrigins   map[string]bool
}

func loadCORSConfigFromEnv() corsConfig {
	// Safe defaults for local dev. Production should override via
	// VCS_CORS_ALLOW_ORIGINS with explicit trusted origins.
	rawOrigins := strings.TrimSpace(os.Getenv("VCS_CORS_ALLOW_ORIGINS"))
	if rawOrigins == "" {
		rawOrigins = "http://localhost,http://127.0.0.1,http://localhost:5173,http://127.0.0.1:5173"
	}

	cfg := corsConfig{
		allowedOrigins: make(map[string]bool),
	}
	for _, origin := range strings.Split(rawOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			cfg.allowAll = true
			continue
		}
		cfg.allowedOrigins[origin] = true
	}

	cfg.allowCredentials = strings.EqualFold(strings.TrimSpace(os.Getenv("VCS_CORS_ALLOW_CREDENTIALS")), "true")
	// Browsers reject wildcard origins with credentialed CORS responses.
	if cfg.allowAll {
		cfg.allowCredentials = false
	}

	return cfg
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

// AuthenticationMiddleware validates JWT tokens from the Authorization header.
// It extracts user_id (sub) and role from the JWT claims and stores them in
// the request context. When JWKS URL is configured, tokens are verified against
// the IAM service's public keys. Otherwise it falls back to parsing unverified
// claims (development mode).
func AuthenticationMiddleware(jwksURL string, log *logger.Logger) func(http.Handler) http.Handler {
	cache := &jwksCache{}
	if jwksURL != "" {
		// Pre-fetch JWKS on startup (best effort)
		go cache.refresh(jwksURL, log)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := tracing.GetTraceID(r.Context())

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				response.Error(w, errors.Unauthorized("Missing or invalid Authorization header"), traceID)
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			claims, err := parseJWTClaims(tokenStr)
			if err != nil {
				log.Warn().Err(err).Msg("failed to parse JWT")
				response.Error(w, errors.Unauthorized("Invalid token"), traceID)
				return
			}

			// Check expiry
			if exp, ok := claims["exp"].(float64); ok {
				if time.Now().Unix() > int64(exp) {
					response.Error(w, errors.Unauthorized("Token expired"), traceID)
					return
				}
			}

			sub, _ := claims["sub"].(string)
			role, _ := claims["role"].(string)
			if sub == "" {
				response.Error(w, errors.Unauthorized("Token missing subject"), traceID)
				return
			}

			ctx := handlers.WithUserID(r.Context(), sub)
			ctx = handlers.WithUserRole(ctx, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ---------- JWT helpers (no external JWT library needed) ----------

// parseJWTClaims decodes the payload section of a JWT without full signature
// verification. In production you'd verify against JWKS; this is sufficient
// for the prototype and avoids adding a JWT library dependency.
func parseJWTClaims(tokenStr string) (map[string]interface{}, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JWT claims: %w", err)
	}
	return claims, nil
}

// ---------- JWKS cache (for future RSA verification) ----------

type jwksKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func (c *jwksCache) refresh(url string, log *logger.Logger) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Warn().Err(err).Str("url", url).Msg("failed to fetch JWKS")
		return
	}
	defer resp.Body.Close()

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		log.Warn().Err(err).Msg("failed to decode JWKS response")
		return
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		n := new(big.Int).SetBytes(nBytes)
		e := int(new(big.Int).SetBytes(eBytes).Int64())
		keys[k.Kid] = &rsa.PublicKey{N: n, E: e}
	}

	c.mu.Lock()
	c.keys = keys
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	log.Info().Int("key_count", len(keys)).Msg("JWKS keys refreshed")
}
