package handler

import (
	"net/http"
	"strconv"

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
