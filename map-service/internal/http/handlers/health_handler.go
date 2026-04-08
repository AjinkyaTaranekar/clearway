package handlers

import (
	"net/http"
	"time"

	appErrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/tracing"
)

// HealthHandler handles health check operations
type HealthHandler struct {
	graph *GraphStore
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(graph *GraphStore) *HealthHandler {
	return &HealthHandler{graph: graph}
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status      string `json:"status"`
	Version     string `json:"version"`
	Timestamp   string `json:"timestamp"`
	GraphLoaded bool   `json:"graph_loaded"`
	GraphSource string `json:"graph_source"`
	GraphError  string `json:"graph_error,omitempty"`
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

	graphStatus := h.graph.Status()

	healthResp := HealthResponse{
		Status:      "healthy",
		Version:     "1.0.0",
		Timestamp:   time.Now().Format(time.RFC3339),
		GraphLoaded: graphStatus.LoadedFromDB,
		GraphSource: graphStatus.Source,
		GraphError:  graphStatus.LastError,
	}
	log.Debug().
		Str("handler", "HealthHandler.Health").
		Bool("graph_loaded", healthResp.GraphLoaded).
		Str("graph_source", healthResp.GraphSource).
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

	graphStatus := h.graph.Status()

	healthResp := HealthResponse{
		Status:      "ready",
		Version:     "1.0.0",
		Timestamp:   time.Now().Format(time.RFC3339),
		GraphLoaded: graphStatus.LoadedFromDB,
		GraphSource: graphStatus.Source,
		GraphError:  graphStatus.LastError,
	}

	if !graphStatus.Available {
		log.Warn().
			Str("handler", "HealthHandler.Readiness").
			Str("graph_source", graphStatus.Source).
			Str("graph_error", graphStatus.LastError).
			Msg("readiness failed: graph unavailable")
		response.Error(w, appErrors.InternalError("graph unavailable", nil), traceID)
		return
	}
	if !graphStatus.LoadedFromDB {
		healthResp.Status = "ready_with_fallback"
		log.Warn().
			Str("handler", "HealthHandler.Readiness").
			Str("graph_source", graphStatus.Source).
			Msg("readiness using fallback graph")
	}

	log.Debug().
		Str("handler", "HealthHandler.Readiness").
		Str("status", healthResp.Status).
		Msg("readiness check response ready")

	response.Success(w, healthResp, traceID)
}
