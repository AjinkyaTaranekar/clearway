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
	graphStatus := h.graph.Status()

	healthResp := HealthResponse{
		Status:      "healthy",
		Version:     "1.0.0",
		Timestamp:   time.Now().Format(time.RFC3339),
		GraphLoaded: graphStatus.LoadedFromDB,
		GraphSource: graphStatus.Source,
		GraphError:  graphStatus.LastError,
	}

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
		response.Error(w, appErrors.InternalError("graph unavailable", nil), traceID)
		return
	}
	if !graphStatus.LoadedFromDB {
		healthResp.Status = "ready_with_fallback"
	}

	response.Success(w, healthResp, traceID)
}
