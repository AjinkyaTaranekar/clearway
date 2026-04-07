package handlers

import (
	"net/http"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/tracing"
)

// HealthHandler handles health check operations
type HealthHandler struct{}

// NewHealthHandler creates a new health handler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
}

// Health godoc
// @Summary Health check
// @Description Check if the service is healthy and running
// @Tags Health
// @Accept json
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Debug().
		Str("handler", "HealthHandler.Health").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("health check request received")

	healthResp := HealthResponse{
		Status:    "healthy",
		Version:   "1.0.0",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	log.Debug().
		Str("handler", "HealthHandler.Health").
		Str("status", healthResp.Status).
		Msg("health check response ready")

	response.Success(w, healthResp, traceID)
}

// Readiness godoc
// @Summary Readiness check
// @Description Check if the service is ready to accept requests
// @Tags Health
// @Accept json
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /ready [get]
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Debug().
		Str("handler", "HealthHandler.Readiness").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("readiness check request received")

	// TODO: Check database connections, external services, etc.
	healthResp := HealthResponse{
		Status:    "ready",
		Version:   "1.0.0",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	log.Debug().
		Str("handler", "HealthHandler.Readiness").
		Str("status", healthResp.Status).
		Msg("readiness check response ready")

	response.Success(w, healthResp, traceID)
}
