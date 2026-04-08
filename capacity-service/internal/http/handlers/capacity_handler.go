package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/tracing"
)

// CapacityHandler handles reservation and availability check endpoints.
type CapacityHandler struct {
	svc *service.ReservationService
	log *logger.Logger
}

// NewCapacityHandler creates a new CapacityHandler.
func NewCapacityHandler(svc *service.ReservationService, log *logger.Logger) *CapacityHandler {
	return &CapacityHandler{svc: svc, log: log}
}

// Reserve godoc
// @Summary      Reserve capacity across multiple road segments
// @Description  Atomically reserves slots on all requested segments. Returns 201 on success or 200 with a failed_segment on capacity exhaustion.
// @Tags         Capacity
// @Accept       json
// @Produce      json
// @Param        body body model.ReserveRequest true "Reserve request"
// @Success      201 {object} model.ReserveSuccessResponse
// @Success      200 {object} model.ReserveFailResponse
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/reserve [post]
func (h *CapacityHandler) Reserve(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "CapacityHandler.Reserve").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("reserve request received")

	var req model.ReserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().
			Str("handler", "CapacityHandler.Reserve").
			Err(err).
			Msg("reserve request validation failed: invalid body")
		h.writeError(w, http.StatusBadRequest, "invalid request body", traceID)
		return
	}

	log.Info().
		Str("handler", "CapacityHandler.Reserve").
		Str("journey_id", req.JourneyID).
		Str("idempotency_key", req.IdempotencyKey).
		Str("vehicle_type", string(req.VehicleType)).
		Int("segment_count", len(req.Reservations)).
		Msg("invoking reservation service reserve")

	result, statusCode, err := h.svc.Reserve(r.Context(), &req)
	if err != nil {
		log.Error().
			Str("handler", "CapacityHandler.Reserve").
			Err(err).
			Str("journey_id", req.JourneyID).
			Int("status_code", statusCode).
			Msg("reservation service reserve failed")
		if statusCode == 400 {
			h.writeError(w, http.StatusBadRequest, err.Error(), traceID)
		} else {
			h.writeError(w, http.StatusInternalServerError, "internal error", traceID)
		}
		return
	}

	log.Info().
		Str("handler", "CapacityHandler.Reserve").
		Str("journey_id", req.JourneyID).
		Int("status_code", statusCode).
		Msg("reserve request completed")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(result)
}

// Check godoc
// @Summary      Check capacity availability for a segment and time window
// @Description  Returns current availability for a single road segment. Results are cached for 30 seconds.
// @Tags         Capacity
// @Produce      json
// @Param        segment_id         query string true  "Segment ID"
// @Param        time_window_start  query string true  "Window start (RFC3339)"
// @Param        time_window_end    query string true  "Window end (RFC3339)"
// @Success      200 {object} model.CheckResponse
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Router       /api/v1/capacity/check [get]
func (h *CapacityHandler) Check(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "CapacityHandler.Check").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("check availability request received")

	segmentID := r.URL.Query().Get("segment_id")
	startStr := r.URL.Query().Get("time_window_start")
	endStr := r.URL.Query().Get("time_window_end")

	if segmentID == "" || startStr == "" || endStr == "" {
		log.Warn().
			Str("handler", "CapacityHandler.Check").
			Str("segment_id", segmentID).
			Str("time_window_start", startStr).
			Str("time_window_end", endStr).
			Msg("check validation failed: missing required query parameters")
		h.writeError(w, http.StatusBadRequest, "segment_id, time_window_start, and time_window_end are required", traceID)
		return
	}

	windowStart, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		log.Warn().
			Str("handler", "CapacityHandler.Check").
			Err(err).
			Str("segment_id", segmentID).
			Str("time_window_start", startStr).
			Msg("check validation failed: invalid window start")
		h.writeError(w, http.StatusBadRequest, "time_window_start must be RFC3339 format", traceID)
		return
	}
	windowEnd, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		log.Warn().
			Str("handler", "CapacityHandler.Check").
			Err(err).
			Str("segment_id", segmentID).
			Str("time_window_end", endStr).
			Msg("check validation failed: invalid window end")
		h.writeError(w, http.StatusBadRequest, "time_window_end must be RFC3339 format", traceID)
		return
	}
	if !windowEnd.After(windowStart) {
		log.Warn().
			Str("handler", "CapacityHandler.Check").
			Str("segment_id", segmentID).
			Time("window_start", windowStart).
			Time("window_end", windowEnd).
			Msg("check validation failed: window end is not after start")
		h.writeError(w, http.StatusBadRequest, "time_window_end must be after time_window_start", traceID)
		return
	}

	log.Info().
		Str("handler", "CapacityHandler.Check").
		Str("segment_id", segmentID).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("invoking reservation service availability check")

	resp, err := h.svc.CheckAvailability(r.Context(), segmentID, windowStart, windowEnd)
	if err != nil {
		log.Error().
			Str("handler", "CapacityHandler.Check").
			Err(err).
			Str("segment_id", segmentID).
			Msg("reservation service availability check failed")
		h.writeError(w, http.StatusNotFound, err.Error(), traceID)
		return
	}

	log.Info().
		Str("handler", "CapacityHandler.Check").
		Str("segment_id", segmentID).
		Float64("available_slots", resp.AvailableSlots).
		Float64("reserved_slots", resp.ReservedSlots).
		Float64("max_capacity", resp.MaxCapacity).
		Bool("can_reserve", resp.CanReserve).
		Msg("check availability request completed")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *CapacityHandler) writeError(w http.ResponseWriter, status int, message, traceID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
