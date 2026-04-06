package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/service"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/tracing"
)

// AdminHandler handles admin-only journey endpoints
type AdminHandler struct {
	svc *service.JourneyService
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(svc *service.JourneyService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// ListJourneys godoc
// @Summary List all journeys (admin)
// @Description Returns paginated journeys across all drivers with optional filters. Requires admin role.
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param status query string false "Filter by status"
// @Param driver_id query string false "Filter by driver ID"
// @Param page query int false "Page number (default 1)"
// @Param limit query int false "Page size, max 100 (default 20)"
// @Success 200 {object} map[string]interface{} "journeys array, total count, page, limit"
// @Failure 401 {object} response.Response "Missing or invalid JWT"
// @Failure 403 {object} response.Response "Admin role required"
// @Router /api/v1/admin/journeys [get]
// ListJourneys handles GET /api/v1/admin/journeys
func (h *AdminHandler) ListJourneys(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	journeys, total, err := h.svc.AdminListJourneys(r.Context(), service.AdminListFilters{
		Status:   q.Get("status"),
		DriverID: q.Get("driver_id"),
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	response.Success(w, map[string]interface{}{
		"journeys": journeys,
		"total":    total,
		"page":     page,
		"limit":    limit,
	}, traceID)
}

// EnforcementVerify godoc
// @Summary Verify vehicle segment authorization
// @Description Checks whether an ACTIVE journey covers the given road segment at the specified timestamp. Returns authorized=true with journey details if found. Requires enforcement or admin role.
// @Tags Enforcement
// @Produce json
// @Security BearerAuth
// @Param segment_id query string true "Road segment ID to check (e.g. seg_m50)"
// @Param vehicle_plate query string false "Vehicle registration plate (hint from enforcement client; not verified server-side)"
// @Param timestamp query string false "ISO 8601 timestamp to check (defaults to now)"
// @Success 200 {object} service.EnforcementVerifyResult
// @Failure 400 {object} response.Response "Missing segment_id or invalid timestamp format"
// @Failure 401 {object} response.Response "Missing or invalid JWT"
// @Failure 403 {object} response.Response "Role is not enforcement or admin"
// @Router /api/v1/enforcement/verify [get]
// EnforcementVerify handles GET /api/v1/enforcement/verify
func (h *AdminHandler) EnforcementVerify(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	segmentID := r.URL.Query().Get("segment_id")
	if segmentID == "" {
		response.Error(w, apperrors.BadRequest("segment_id query parameter required"), traceID)
		return
	}

	ts := time.Now().UTC()
	if tsStr := r.URL.Query().Get("timestamp"); tsStr != "" {
		parsed, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			response.Error(w, apperrors.BadRequest("timestamp must be ISO 8601 (RFC3339)"), traceID)
			return
		}
		ts = parsed
	}

	result, err := h.svc.EnforcementVerify(r.Context(), segmentID, ts)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, result, traceID)
}

// CancelJourney godoc
// @Summary Force-cancel a journey (admin)
// @Description Force-cancels any APPROVED or ACTIVE journey. No 30-minute time restriction. Requires admin role.
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journey ID"
// @Success 200 {object} model.Journey
// @Failure 400 {object} response.Response "Journey is not in a cancellable state"
// @Failure 401 {object} response.Response "Missing or invalid JWT"
// @Failure 403 {object} response.Response "Admin role required"
// @Failure 404 {object} response.Response "Journey not found"
// @Router /api/v1/admin/journeys/{id}/cancel [put]
// CancelJourney handles PUT /api/v1/admin/journeys/{id}/cancel
func (h *AdminHandler) CancelJourney(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	journeyID := mux.Vars(r)["id"]

	if journeyID == "" {
		response.Error(w, apperrors.BadRequest("journey ID required"), traceID)
		return
	}

	journey, err := h.svc.AdminCancelJourney(r.Context(), journeyID)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, journey, traceID)
}
