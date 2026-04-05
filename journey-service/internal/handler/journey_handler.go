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

// CreateJourney godoc
// @Summary Book a new journey
// @Description Creates a journey booking. The capacity service reserves all route segments atomically. Returns APPROVED (201) or REJECTED (200).
// @Tags Journeys
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string false "Client-generated idempotency key (UUID) for safe retries"
// @Param body body createJourneyRequest true "Journey booking payload"
// @Success 201 {object} model.Journey "Journey approved"
// @Success 200 {object} model.Journey "Journey rejected (capacity unavailable)"
// @Failure 400 {object} response.Response "Invalid request body or missing fields"
// @Failure 401 {object} response.Response "Missing or invalid JWT"
// @Failure 409 {object} response.Response "Driver already has an APPROVED or ACTIVE journey"
// @Failure 422 {object} response.Response "Departure time less than 1 hour from now"
// @Failure 502 {object} response.Response "Map or Capacity service unavailable"
// @Router /api/v1/journeys [post]
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

// GetJourney godoc
// @Summary Get journey details
// @Description Returns a journey by ID. Drivers can only retrieve their own journeys.
// @Tags Journeys
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journey ID (e.g. jrn_a1b2c3d4)"
// @Success 200 {object} model.Journey
// @Failure 401 {object} response.Response "Missing or invalid JWT"
// @Failure 404 {object} response.Response "Journey not found or not owned by this driver"
// @Router /api/v1/journeys/{id} [get]
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

// ListJourneys godoc
// @Summary List my journeys
// @Description Returns a paginated list of journeys for the authenticated driver.
// @Tags Journeys
// @Produce json
// @Security BearerAuth
// @Param status query string false "Filter by status (APPROVED, ACTIVE, COMPLETED, CANCELLED, REJECTED, EXPIRED)"
// @Param page query int false "Page number (default 1)"
// @Param limit query int false "Page size, max 100 (default 20)"
// @Success 200 {object} map[string]interface{} "journeys array, total count, page, limit"
// @Failure 401 {object} response.Response "Missing or invalid JWT"
// @Router /api/v1/journeys [get]
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

// CancelJourney godoc
// @Summary Cancel a journey
// @Description Cancels an APPROVED journey. Only allowed more than 30 minutes before departure.
// @Tags Journeys
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journey ID"
// @Success 200 {object} model.Journey
// @Failure 400 {object} response.Response "Journey not in APPROVED state"
// @Failure 403 {object} response.Response "Less than 30 minutes before departure"
// @Failure 404 {object} response.Response "Journey not found or not owned by this driver"
// @Router /api/v1/journeys/{id}/cancel [put]
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

// ActivateJourney godoc
// @Summary Activate a journey
// @Description Transitions an APPROVED journey to ACTIVE. Only allowed from departure_time up to departure_time + 30 minutes.
// @Tags Journeys
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journey ID"
// @Success 200 {object} model.Journey
// @Failure 400 {object} response.Response "Journey not in APPROVED state"
// @Failure 403 {object} response.Response "Too early (before departure_time) or too late (30+ min after departure_time)"
// @Failure 404 {object} response.Response "Journey not found or not owned by this driver"
// @Router /api/v1/journeys/{id}/activate [put]
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

// CompleteJourney godoc
// @Summary Complete a journey
// @Description Transitions an ACTIVE journey to COMPLETED. Call when the driver reaches the destination.
// @Tags Journeys
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journey ID"
// @Success 200 {object} model.Journey
// @Failure 400 {object} response.Response "Journey not in ACTIVE state"
// @Failure 404 {object} response.Response "Journey not found or not owned by this driver"
// @Router /api/v1/journeys/{id}/complete [put]
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
