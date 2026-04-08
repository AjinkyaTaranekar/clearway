package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/middleware"
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.ListJourneys").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("admin list journeys request received")

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	filters := service.AdminListFilters{
		Status:   q.Get("status"),
		DriverID: q.Get("driver_id"),
		Page:     page,
		Limit:    limit,
	}
	log.Info().
		Str("handler", "AdminHandler.ListJourneys").
		Str("status_filter", filters.Status).
		Str("driver_id_filter", filters.DriverID).
		Int("page", page).
		Int("limit", limit).
		Msg("invoking admin list journeys service")

	journeys, total, err := h.svc.AdminListJourneys(r.Context(), filters)
	if err != nil {
		log.Error().
			Str("handler", "AdminHandler.ListJourneys").
			Err(err).
			Msg("admin list journeys service failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AdminHandler.ListJourneys").
		Int("result_count", len(journeys)).
		Int64("total", total).
		Msg("admin list journeys request completed")

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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.EnforcementVerify").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("enforcement verify request received")

	segmentID := r.URL.Query().Get("segment_id")
	if segmentID == "" {
		log.Warn().
			Str("handler", "AdminHandler.EnforcementVerify").
			Msg("enforcement verify validation failed: segment_id missing")
		response.Error(w, apperrors.BadRequest("segment_id query parameter required"), traceID)
		return
	}

	ts := time.Now().UTC()
	if tsStr := r.URL.Query().Get("timestamp"); tsStr != "" {
		parsed, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			log.Warn().
				Str("handler", "AdminHandler.EnforcementVerify").
				Str("segment_id", segmentID).
				Str("timestamp", tsStr).
				Err(err).
				Msg("enforcement verify validation failed: invalid timestamp")
			response.Error(w, apperrors.BadRequest("timestamp must be ISO 8601 (RFC3339)"), traceID)
			return
		}
		ts = parsed
	}

	log.Info().
		Str("handler", "AdminHandler.EnforcementVerify").
		Str("segment_id", segmentID).
		Time("timestamp", ts).
		Msg("invoking enforcement verify service")

	result, err := h.svc.EnforcementVerify(r.Context(), segmentID, ts)
	if err != nil {
		log.Error().
			Str("handler", "AdminHandler.EnforcementVerify").
			Err(err).
			Str("segment_id", segmentID).
			Msg("enforcement verify service failed")
		response.Error(w, err, traceID)
		return
	}
	log.Info().
		Str("handler", "AdminHandler.EnforcementVerify").
		Str("segment_id", segmentID).
		Bool("authorized", result.Authorized).
		Str("journey_id", result.JourneyID).
		Msg("enforcement verify request completed")
	response.Success(w, result, traceID)
}

// Analytics godoc
// @Summary Admin analytics
// @Description Returns aggregated journey counts for the last 1h, 24h, or 7d. Fixes U-14 (admin dashboard mock data).
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param window query string false "Time window: 1h | 24h | 7d (default 24h)"
// @Success 200 {object} service.AnalyticsResult
// @Failure 401 {object} response.Response "Missing or invalid JWT"
// @Failure 403 {object} response.Response "Admin role required"
// @Router /api/v1/admin/analytics [get]
func (h *AdminHandler) Analytics(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "24h"
	}
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.Analytics").
		Str("window", window).
		Msg("admin analytics request received")

	result, err := h.svc.AdminAnalytics(r.Context(), window)
	if err != nil {
		log.Error().
			Str("handler", "AdminHandler.Analytics").
			Err(err).
			Msg("admin analytics service failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AdminHandler.Analytics").
		Str("window", window).
		Int64("total_journeys", result.TotalJourneys).
		Msg("admin analytics request completed")
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.CancelJourney").
		Str("journey_id", journeyID).
		Msg("admin cancel journey request received")

	if journeyID == "" {
		log.Warn().
			Str("handler", "AdminHandler.CancelJourney").
			Msg("admin cancel journey validation failed: journey id missing")
		response.Error(w, apperrors.BadRequest("journey ID required"), traceID)
		return
	}

	// Extract the admin's own user ID from the JWT for audit trail (F-10/F-19).
	adminID := middleware.GetDriverID(r.Context())
	journey, err := h.svc.AdminCancelJourney(r.Context(), journeyID, adminID)
	if err != nil {
		log.Error().
			Str("handler", "AdminHandler.CancelJourney").
			Err(err).
			Str("journey_id", journeyID).
			Str("admin_id", adminID).
			Msg("admin cancel journey service failed")
		response.Error(w, err, traceID)
		return
	}
	log.Info().
		Str("handler", "AdminHandler.CancelJourney").
		Str("journey_id", journey.JourneyID).
		Str("journey_status", string(journey.Status)).
		Msg("admin cancel journey request completed")
	response.Success(w, journey, traceID)
}
