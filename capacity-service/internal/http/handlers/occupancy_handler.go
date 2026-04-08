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

// Segments godoc
// @Summary      List all registered road segments
// @Description  Returns static metadata for every road segment seeded in the capacity service. Intended for Map Service startup validation (XC-02): Map Service calls this on boot to verify every segment ID in its graph has a corresponding capacity row, failing readiness if any are missing.
// @Tags         Capacity
// @Produce      json
// @Success      200 {array}  model.Segment
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/segments [get]
func (h *OccupancyHandler) Segments(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "OccupancyHandler.Segments").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("segments listing request received")

	segments, err := h.svc.GetAllSegments(r.Context())
	if err != nil {
		log.Error().
			Str("handler", "OccupancyHandler.Segments").
			Err(err).
			Msg("segments listing failed")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to retrieve segments"})
		return
	}

	log.Info().
		Str("handler", "OccupancyHandler.Segments").
		Int("segment_count", len(segments)).
		Msg("segments listing request completed")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(segments)
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "OccupancyHandler.Occupancy").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("occupancy request received")

	infos, err := h.svc.GetOccupancy(r.Context())
	if err != nil {
		log.Error().
			Str("handler", "OccupancyHandler.Occupancy").
			Err(err).
			Msg("occupancy request failed")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to retrieve occupancy data"})
		return
	}

	log.Info().
		Str("handler", "OccupancyHandler.Occupancy").
		Int("segment_count", len(infos)).
		Msg("occupancy request completed")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(infos)
}
