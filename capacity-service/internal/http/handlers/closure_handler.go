package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/tracing"
)

// ClosureHandler serves admin APIs for managing temporary segment closures.
type ClosureHandler struct {
	svc *service.ReservationService
	log *logger.Logger
}

// NewClosureHandler creates a new ClosureHandler.
func NewClosureHandler(svc *service.ReservationService, log *logger.Logger) *ClosureHandler {
	return &ClosureHandler{svc: svc, log: log}
}

type createClosureRequest struct {
	SegmentID       string `json:"segment_id"`
	DurationMinutes int    `json:"duration_minutes"`
	Reason          string `json:"reason"`
}

// Create godoc
// @Summary      Create a segment closure
// @Description  Admin-only endpoint. Creates an immediate closure for a segment using duration_minutes and reason.
// @Tags         Capacity
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body createClosureRequest true "Create closure request"
// @Success      201 {object} model.SegmentClosure
// @Failure      400 {object} map[string]string
// @Failure      401 {object} map[string]string
// @Failure      403 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Failure      409 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/closures [post]
func (h *ClosureHandler) Create(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	var req createClosureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", traceID)
		return
	}

	adminID := strings.TrimSpace(r.Header.Get("X-Auth-Subject"))
	var closure *model.SegmentClosure
	closure, err := h.svc.CreateClosure(r.Context(), service.CreateClosureInput{
		SegmentID:       req.SegmentID,
		DurationMinutes: req.DurationMinutes,
		Reason:          req.Reason,
		AdminID:         adminID,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrClosureOverlap):
			h.writeError(w, http.StatusConflict, err.Error(), traceID)
		case errors.Is(err, service.ErrUnknownSegment):
			h.writeError(w, http.StatusNotFound, err.Error(), traceID)
		default:
			if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "duration_minutes") {
				h.writeError(w, http.StatusBadRequest, err.Error(), traceID)
				return
			}
			h.writeError(w, http.StatusInternalServerError, "internal error", traceID)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(closure)
}

// List godoc
// @Summary      List active segment closures
// @Description  Admin-only endpoint. Returns active closures whose end_time is in the future.
// @Tags         Capacity
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} model.SegmentClosure
// @Failure      401 {object} map[string]string
// @Failure      403 {object} map[string]string
// @Failure      500 {object} map[string]string
// @Router       /api/v1/capacity/closures [get]
func (h *ClosureHandler) List(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	closures, err := h.svc.ListActiveClosures(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list closures", traceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(closures)
}

func (h *ClosureHandler) writeError(w http.ResponseWriter, status int, message, traceID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
