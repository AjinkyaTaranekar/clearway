package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/tracing"
)

// ClosuresHandler serves the segment closure management endpoints.
type ClosuresHandler struct {
	svc *service.ClosureService
	log *logger.Logger
}

// NewClosuresHandler creates a new ClosuresHandler.
func NewClosuresHandler(svc *service.ClosureService, log *logger.Logger) *ClosuresHandler {
	return &ClosuresHandler{svc: svc, log: log}
}

// List godoc
// @Summary      List all segment closures
// @Description  Returns all segment closures, active first. Used by the admin Segment Closures page.
// @Tags         Capacity
// @Produce      json
// @Success      200 {array}  model.Closure
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/closures [get]
func (h *ClosuresHandler) List(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	closures, err := h.svc.ListClosures(r.Context())
	if err != nil {
		h.log.Error().Err(err).Str("trace_id", traceID).Msg("closures list: service error")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to list closures"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(closures)
}

// Create godoc
// @Summary      Create a segment closure
// @Description  Marks a road segment as closed for a specified time window. Subsequent Reserve calls for that segment in the same window will be rejected with reason "segment_closed".
// @Tags         Capacity
// @Accept       json
// @Produce      json
// @Param        body body model.CreateClosureRequest true "Closure details"
// @Success      201 {object} model.Closure
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/closures [post]
func (h *ClosuresHandler) Create(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	var req model.CreateClosureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	closure, err := h.svc.CreateClosure(r.Context(), &req)
	if err != nil {
		h.log.Error().Err(err).Str("trace_id", traceID).Msg("closures create: service error")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(closure)
}
