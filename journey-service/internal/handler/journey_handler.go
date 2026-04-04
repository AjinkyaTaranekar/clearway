package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/service"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/tracing"
)

// JourneyHandler handles driver-facing journey endpoints
type JourneyHandler struct {
	svc *service.JourneyService
}

// NewJourneyHandler creates a new journey handler
func NewJourneyHandler(svc *service.JourneyService) *JourneyHandler {
	return &JourneyHandler{svc: svc}
}

// createJourneyRequest is the request body for POST /api/v1/journeys
type createJourneyRequest struct {
	Origin        model.Coordinates `json:"origin"`
	Destination   model.Coordinates `json:"destination"`
	DepartureTime jsonTime          `json:"departure_time"`
	VehicleType   string            `json:"vehicle_type"`
}

// CreateJourney handles POST /api/v1/journeys
func (h *JourneyHandler) CreateJourney(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	var req createJourneyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperrors.BadRequest("invalid request body"), traceID)
		return
	}

	if req.Origin.Lat == 0 && req.Origin.Lng == 0 {
		response.Error(w, apperrors.BadRequest("origin coordinates required"), traceID)
		return
	}
	if req.Destination.Lat == 0 && req.Destination.Lng == 0 {
		response.Error(w, apperrors.BadRequest("destination coordinates required"), traceID)
		return
	}
	if req.VehicleType == "" {
		response.Error(w, apperrors.BadRequest("vehicle_type required"), traceID)
		return
	}
	if req.DepartureTime.IsZero() {
		response.Error(w, apperrors.BadRequest("departure_time required"), traceID)
		return
	}

	driverID := middleware.GetDriverID(r.Context())
	idempKey := middleware.GetIdempotencyKey(r.Context())

	journey, err := h.svc.CreateJourney(r.Context(), service.CreateJourneyRequest{
		Origin:         req.Origin,
		Destination:    req.Destination,
		DepartureTime:  req.DepartureTime.Time,
		VehicleType:    req.VehicleType,
		IdempotencyKey: idempKey,
		DriverID:       driverID,
	})
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	statusCode := http.StatusCreated
	if journey.Status == model.StatusRejected {
		statusCode = http.StatusOK
	}
	response.JSON(w, statusCode, journey, traceID)
}

// GetJourney handles GET /api/v1/journeys/{id}
func (h *JourneyHandler) GetJourney(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	journeyID := mux.Vars(r)["id"]
	driverID := middleware.GetDriverID(r.Context())

	journey, err := h.svc.GetJourney(r.Context(), journeyID, driverID, false)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, journey, traceID)
}

// ListJourneys handles GET /api/v1/journeys
func (h *JourneyHandler) ListJourneys(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	driverID := middleware.GetDriverID(r.Context())

	q := r.URL.Query()
	statusFilter := q.Get("status")
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	journeys, total, err := h.svc.ListJourneys(r.Context(), driverID, statusFilter, page, limit)
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

// CancelJourney handles PUT /api/v1/journeys/{id}/cancel
func (h *JourneyHandler) CancelJourney(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	journeyID := mux.Vars(r)["id"]
	driverID := middleware.GetDriverID(r.Context())

	journey, err := h.svc.CancelJourney(r.Context(), journeyID, driverID)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, journey, traceID)
}

// ActivateJourney handles PUT /api/v1/journeys/{id}/activate
func (h *JourneyHandler) ActivateJourney(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	journeyID := mux.Vars(r)["id"]
	driverID := middleware.GetDriverID(r.Context())

	journey, err := h.svc.ActivateJourney(r.Context(), journeyID, driverID)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, journey, traceID)
}

// CompleteJourney handles PUT /api/v1/journeys/{id}/complete
func (h *JourneyHandler) CompleteJourney(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	journeyID := mux.Vars(r)["id"]
	driverID := middleware.GetDriverID(r.Context())

	journey, err := h.svc.CompleteJourney(r.Context(), journeyID, driverID)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, journey, traceID)
}
