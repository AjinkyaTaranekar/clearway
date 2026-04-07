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

// Health handles GET /health. Always returns 200 — degraded DB is reported
// in the body but does not take the service down (existing JWTs still work).
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	dbStatus := "connected"
	if err := h.db.PingContext(r.Context()); err != nil {
		dbStatus = "disconnected"
	}
	response.Success(w, healthResponse{
		Status:        "healthy",
		DB:            dbStatus,
		UptimeSeconds: int64(time.Since(h.startAt).Seconds()),
	}, traceID)
}

// Readiness handles GET /ready. Returns 503 if either the DB ping fails or
// the RSA signing key has not been loaded — both are required before the
// service can handle any meaningful request.
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	if err := h.db.PingContext(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","reason":"database unreachable"}`)) //nolint:errcheck
		return
	}

	if h.keyReady != nil && !h.keyReady() {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","reason":"RSA signing key not loaded"}`)) //nolint:errcheck
		return
	}

	response.Success(w, healthResponse{
		Status:        "ready",
		DB:            "connected",
		UptimeSeconds: int64(time.Since(h.startAt).Seconds()),
	}, traceID)
}
