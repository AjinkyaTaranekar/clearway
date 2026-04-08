package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

// HealthHandler handles /health and /ready.
type HealthHandler struct {
	db       *sql.DB
	startAt  time.Time
	keyReady func() bool // returns true when the RSA signing key is loaded
}

// NewHealthHandler creates a HealthHandler.
// keyReady is called on every /ready request to verify the RSA key is loaded.
// Pass jwksSvc.IsReady from main.go.
func NewHealthHandler(db *sql.DB, keyReady func() bool) *HealthHandler {
	return &HealthHandler{db: db, startAt: time.Now(), keyReady: keyReady}
}

type healthResponse struct {
	Status        string `json:"status"`
	DB            string `json:"db"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// Health godoc
// @Summary Health check
// @Description Returns service health status and database connectivity.
// @Tags Health
// @Produce json
// @Success 200 {object} healthResponse
// @Router /health [get]
// Health handles GET /health. Always returns 200 - degraded DB is reported
// in the body but does not take the service down (existing JWTs still work).
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Debug().
		Str("handler", "HealthHandler.Health").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("health check request received")

	dbStatus := "connected"
	if err := h.db.PingContext(r.Context()); err != nil {
		dbStatus = "disconnected"
		log.Warn().
			Str("handler", "HealthHandler.Health").
			Err(err).
			Msg("database ping failed during health check")
	}

	log.Debug().
		Str("handler", "HealthHandler.Health").
		Str("db_status", dbStatus).
		Int64("uptime_seconds", int64(time.Since(h.startAt).Seconds())).
		Msg("health check response ready")

	response.Success(w, healthResponse{
		Status:        "healthy",
		DB:            dbStatus,
		UptimeSeconds: int64(time.Since(h.startAt).Seconds()),
	}, traceID)
}

// Readiness godoc
// @Summary Readiness check
// @Description Returns ready when both database and RSA signing key are available.
// @Tags Health
// @Produce json
// @Success 200 {object} healthResponse
// @Failure 503 {object} map[string]string
// @Router /ready [get]
// Readiness handles GET /ready. Returns 503 if either the DB ping fails or
// the RSA signing key has not been loaded - both are required before the
// service can handle any meaningful request.
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Debug().
		Str("handler", "HealthHandler.Readiness").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("readiness check request received")

	if err := h.db.PingContext(r.Context()); err != nil {
		log.Warn().
			Str("handler", "HealthHandler.Readiness").
			Err(err).
			Msg("readiness failed: database unreachable")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","reason":"database unreachable"}`)) //nolint:errcheck
		return
	}

	if h.keyReady != nil && !h.keyReady() {
		log.Warn().
			Str("handler", "HealthHandler.Readiness").
			Msg("readiness failed: rsa signing key not loaded")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","reason":"RSA signing key not loaded"}`)) //nolint:errcheck
		return
	}

	log.Debug().
		Str("handler", "HealthHandler.Readiness").
		Int64("uptime_seconds", int64(time.Since(h.startAt).Seconds())).
		Msg("readiness check passed")

	response.Success(w, healthResponse{
		Status:        "ready",
		DB:            "connected",
		UptimeSeconds: int64(time.Since(h.startAt).Seconds()),
	}, traceID)
}
