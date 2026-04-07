package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/tracing"
)

// OccupancyHandler serves the segment occupancy endpoint consumed by the Map Service.
type OccupancyHandler struct {
	svc *service.ReservationService
	log *logger.Logger
}

// NewOccupancyHandler creates a new OccupancyHandler.
func NewOccupancyHandler(svc *service.ReservationService, log *logger.Logger) *OccupancyHandler {
	return &OccupancyHandler{svc: svc, log: log}
}

// Occupancy godoc
// @Summary      Get current occupancy for all road segments
// @Description  Returns real-time occupancy percentages and trend for every registered segment. Consumed by the Map Service to render the admin traffic map.
// @Tags         Capacity
// @Produce      json
// @Success      200 {array}  model.OccupancyInfo
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/segments/occupancy [get]
func (h *OccupancyHandler) Occupancy(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	infos, err := h.svc.GetOccupancy(r.Context())
	if err != nil {
		h.log.Error().Err(err).Str("trace_id", traceID).Msg("occupancy: service error")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to retrieve occupancy data"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(infos)
}
