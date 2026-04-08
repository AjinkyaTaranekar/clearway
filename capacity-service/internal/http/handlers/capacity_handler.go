package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/tracing"
	"github.com/gorilla/mux"
)

type registerSegmentPayload struct {
	SegmentID   string `json:"segment_id"`
	SegmentName string `json:"segment_name"`
	Region      string `json:"region"`
}

type registerSegmentsRequest struct {
	Segments []registerSegmentPayload `json:"segments"`
}

type updateCapacityRequest struct {
	MaxCapacity float64 `json:"max_capacity"`
}

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
// @Description  Atomically reserves slots on all requested segments. Returns 201 on success or 200 with failed_segment when capacity is unavailable or a segment is closed.
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
		Str("priority_level", req.PriorityLevel).
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
// @Description  Returns current availability for a single road segment. Includes closure metadata when the segment is closed for the requested window.
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
	priorityLevel := r.URL.Query().Get("priority_level")

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
		Str("priority_level", priorityLevel).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("invoking reservation service availability check")

	resp, err := h.svc.CheckAvailability(r.Context(), segmentID, windowStart, windowEnd, priorityLevel)
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

// RegisterSegments godoc
// @Summary      Register dynamic segments
// @Description  Internal endpoint used by Map Service to ensure OSRM-derived segment IDs exist in capacity storage.
// @Tags         Capacity
// @Accept       json
// @Produce      json
// @Param        body body registerSegmentsRequest true "Segments to register"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/segments/register [post]
func (h *CapacityHandler) RegisterSegments(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	var req registerSegmentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", traceID)
		return
	}
	if len(req.Segments) == 0 {
		h.writeError(w, http.StatusBadRequest, "segments list must not be empty", traceID)
		return
	}

	segments := make([]service.SegmentRegistration, 0, len(req.Segments))
	for _, segment := range req.Segments {
		segmentID := strings.TrimSpace(segment.SegmentID)
		if segmentID == "" {
			h.writeError(w, http.StatusBadRequest, "segment_id is required for every segment", traceID)
			return
		}

		segments = append(segments, service.SegmentRegistration{
			SegmentID:   segmentID,
			SegmentName: strings.TrimSpace(segment.SegmentName),
			Region:      strings.TrimSpace(segment.Region),
		})
	}

	defaultCapacity, registeredCount, err := h.svc.RegisterSegments(r.Context(), segments)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to register segments", traceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"registered_count":        registeredCount,
		"default_max_capacity":    defaultCapacity,
		"requested_segment_count": len(req.Segments),
	})
}

// GetDefaultCapacity godoc
// @Summary      Get default segment max capacity
// @Description  Admin-only endpoint returning the default max capacity used for newly discovered segments.
// @Tags         Capacity
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} map[string]number
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/default-capacity [get]
func (h *CapacityHandler) GetDefaultCapacity(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	defaultCapacity, err := h.svc.GetDefaultSegmentCapacity(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to load default capacity", traceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]float64{
		"default_max_capacity": defaultCapacity,
	})
}

// UpdateDefaultCapacity godoc
// @Summary      Update default segment max capacity
// @Description  Admin-only endpoint to change default max capacity for newly discovered segments.
// @Tags         Capacity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body updateCapacityRequest true "Default capacity payload"
// @Success      200 {object} map[string]number
// @Failure      400 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/default-capacity [put]
func (h *CapacityHandler) UpdateDefaultCapacity(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	var req updateCapacityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", traceID)
		return
	}
	if req.MaxCapacity <= 0 {
		h.writeError(w, http.StatusBadRequest, "max_capacity must be greater than 0", traceID)
		return
	}

	adminID := strings.TrimSpace(r.Header.Get("X-Auth-Subject"))
	updated, err := h.svc.UpdateDefaultSegmentCapacity(r.Context(), req.MaxCapacity, adminID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update default capacity", traceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]float64{
		"default_max_capacity": updated,
	})
}

// UpdateSegmentCapacity godoc
// @Summary      Update one segment max capacity
// @Description  Admin-only endpoint to change max capacity for an existing segment.
// @Tags         Capacity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        segment_id path string true "Segment ID"
// @Param        body body updateCapacityRequest true "Segment capacity payload"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/segments/{segment_id}/capacity [put]
func (h *CapacityHandler) UpdateSegmentCapacity(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	segmentID := strings.TrimSpace(mux.Vars(r)["segment_id"])
	if segmentID == "" {
		h.writeError(w, http.StatusBadRequest, "segment_id is required", traceID)
		return
	}

	var req updateCapacityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", traceID)
		return
	}
	if req.MaxCapacity <= 0 {
		h.writeError(w, http.StatusBadRequest, "max_capacity must be greater than 0", traceID)
		return
	}

	err := h.svc.UpdateSegmentCapacity(r.Context(), segmentID, req.MaxCapacity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.writeError(w, http.StatusNotFound, "segment not found", traceID)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to update segment capacity", traceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"segment_id":   segmentID,
		"max_capacity": req.MaxCapacity,
	})
}

func (h *CapacityHandler) writeError(w http.ResponseWriter, status int, message, traceID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
