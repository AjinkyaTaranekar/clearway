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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "JourneyHandler.CreateJourney").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("create journey request received")

	var req createJourneyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().
			Str("handler", "JourneyHandler.CreateJourney").
			Err(err).
			Msg("failed to decode create journey request body")
		response.Error(w, apperrors.BadRequest("invalid request body"), traceID)
		return
	}

	if req.Origin.Lat == 0 && req.Origin.Lng == 0 {
		log.Warn().
			Str("handler", "JourneyHandler.CreateJourney").
			Msg("create journey validation failed: missing origin coordinates")
		response.Error(w, apperrors.BadRequest("origin coordinates required"), traceID)
		return
	}
	if req.Destination.Lat == 0 && req.Destination.Lng == 0 {
		log.Warn().
			Str("handler", "JourneyHandler.CreateJourney").
			Msg("create journey validation failed: missing destination coordinates")
		response.Error(w, apperrors.BadRequest("destination coordinates required"), traceID)
		return
	}
	if req.VehicleType == "" {
		log.Warn().
			Str("handler", "JourneyHandler.CreateJourney").
			Msg("create journey validation failed: missing vehicle type")
		response.Error(w, apperrors.BadRequest("vehicle_type required"), traceID)
		return
	}
	if req.DepartureTime.IsZero() {
		log.Warn().
			Str("handler", "JourneyHandler.CreateJourney").
			Msg("create journey validation failed: missing departure time")
		response.Error(w, apperrors.BadRequest("departure_time required"), traceID)
		return
	}

	driverID := middleware.GetDriverID(r.Context())
	idempKey := middleware.GetIdempotencyKey(r.Context())
	log.Info().
		Str("handler", "JourneyHandler.CreateJourney").
		Str("driver_id", driverID).
		Str("vehicle_type", req.VehicleType).
		Str("idempotency_key", idempKey).
		Time("departure_time", req.DepartureTime.Time).
		Msg("invoking journey service create flow")

	journey, err := h.svc.CreateJourney(r.Context(), service.CreateJourneyRequest{
		Origin:         req.Origin,
		Destination:    req.Destination,
		DepartureTime:  req.DepartureTime.Time,
		VehicleType:    req.VehicleType,
		IdempotencyKey: idempKey,
		DriverID:       driverID,
	})
	if err != nil {
		log.Error().
			Str("handler", "JourneyHandler.CreateJourney").
			Err(err).
			Str("driver_id", driverID).
			Msg("journey service create flow failed")
		response.Error(w, err, traceID)
		return
	}

	statusCode := http.StatusCreated
	if journey.Status == model.StatusRejected {
		statusCode = http.StatusOK
	}
	log.Info().
		Str("handler", "JourneyHandler.CreateJourney").
		Str("driver_id", driverID).
		Str("journey_id", journey.JourneyID).
		Str("journey_status", string(journey.Status)).
		Int("http_status", statusCode).
		Msg("create journey request completed")
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "JourneyHandler.GetJourney").
		Str("journey_id", journeyID).
		Str("driver_id", driverID).
		Msg("get journey request received")

	journey, err := h.svc.GetJourney(r.Context(), journeyID, driverID, false)
	if err != nil {
		log.Error().
			Str("handler", "JourneyHandler.GetJourney").
			Err(err).
			Str("journey_id", journeyID).
			Str("driver_id", driverID).
			Msg("journey service get journey failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "JourneyHandler.GetJourney").
		Str("journey_id", journey.JourneyID).
		Str("journey_status", string(journey.Status)).
		Msg("get journey request completed")
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "JourneyHandler.ListJourneys").
		Str("driver_id", driverID).
		Msg("list journeys request received")

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

	log.Info().
		Str("handler", "JourneyHandler.ListJourneys").
		Str("driver_id", driverID).
		Str("status_filter", statusFilter).
		Int("page", page).
		Int("limit", limit).
		Msg("invoking journey service list flow")

	journeys, total, err := h.svc.ListJourneys(r.Context(), driverID, statusFilter, page, limit)
	if err != nil {
		log.Error().
			Str("handler", "JourneyHandler.ListJourneys").
			Err(err).
			Str("driver_id", driverID).
			Str("status_filter", statusFilter).
			Msg("journey service list flow failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "JourneyHandler.ListJourneys").
		Int("result_count", len(journeys)).
		Int64("total", total).
		Msg("list journeys request completed")

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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "JourneyHandler.CancelJourney").
		Str("journey_id", journeyID).
		Str("driver_id", driverID).
		Msg("cancel journey request received")

	journey, err := h.svc.CancelJourney(r.Context(), journeyID, driverID)
	if err != nil {
		log.Error().
			Str("handler", "JourneyHandler.CancelJourney").
			Err(err).
			Str("journey_id", journeyID).
			Str("driver_id", driverID).
			Msg("journey service cancel flow failed")
		response.Error(w, err, traceID)
		return
	}
	log.Info().
		Str("handler", "JourneyHandler.CancelJourney").
		Str("journey_id", journey.JourneyID).
		Str("journey_status", string(journey.Status)).
		Msg("cancel journey request completed")
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "JourneyHandler.ActivateJourney").
		Str("journey_id", journeyID).
		Str("driver_id", driverID).
		Msg("activate journey request received")

	journey, err := h.svc.ActivateJourney(r.Context(), journeyID, driverID)
	if err != nil {
		log.Error().
			Str("handler", "JourneyHandler.ActivateJourney").
			Err(err).
			Str("journey_id", journeyID).
			Str("driver_id", driverID).
			Msg("journey service activate flow failed")
		response.Error(w, err, traceID)
		return
	}
	log.Info().
		Str("handler", "JourneyHandler.ActivateJourney").
		Str("journey_id", journey.JourneyID).
		Str("journey_status", string(journey.Status)).
		Msg("activate journey request completed")
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "JourneyHandler.CompleteJourney").
		Str("journey_id", journeyID).
		Str("driver_id", driverID).
		Msg("complete journey request received")

	journey, err := h.svc.CompleteJourney(r.Context(), journeyID, driverID)
	if err != nil {
		log.Error().
			Str("handler", "JourneyHandler.CompleteJourney").
			Err(err).
			Str("journey_id", journeyID).
			Str("driver_id", driverID).
			Msg("journey service complete flow failed")
		response.Error(w, err, traceID)
		return
	}
	log.Info().
		Str("handler", "JourneyHandler.CompleteJourney").
		Str("journey_id", journey.JourneyID).
		Str("journey_status", string(journey.Status)).
		Msg("complete journey request completed")
	response.Success(w, journey, traceID)
}
