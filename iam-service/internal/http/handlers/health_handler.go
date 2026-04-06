package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

type HealthHandler struct {
	db      *sql.DB
	startAt time.Time
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db, startAt: time.Now()}
}

type healthResponse struct {
	Status        string `json:"status"`
	DB            string `json:"db"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	dbStatus := "connected"
	if err := h.db.PingContext(r.Context()); err != nil {
		dbStatus = "disconnected"
	}
	response.Success(w, healthResponse{Status: "healthy", DB: dbStatus, UptimeSeconds: int64(time.Since(h.startAt).Seconds())}, traceID)
}

func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	if err := h.db.PingContext(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	response.Success(w, healthResponse{Status: "ready", DB: "connected", UptimeSeconds: int64(time.Since(h.startAt).Seconds())}, traceID)
}
