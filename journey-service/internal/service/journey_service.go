package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/event"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/repository"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/logger"
)

// CreateJourneyRequest is the input for creating a journey
type CreateJourneyRequest struct {
	Origin         model.Coordinates `json:"origin"`
	Destination    model.Coordinates `json:"destination"`
	DepartureTime  time.Time         `json:"departure_time"`
	VehicleType    string            `json:"vehicle_type"`
	IdempotencyKey string            `json:"-"`
	DriverID       string            `json:"-"`
}

// AdminListFilters holds filters for the admin list endpoint
type AdminListFilters struct {
	Status   string
	DriverID string
	Page     int
	Limit    int
}

// JourneyService handles all journey business logic
type JourneyService struct {
	repo            *repository.JourneyRepository
	idempRepo       *repository.IdempotencyRepository
	mapClient       *client.MapClient
	routeCache      *client.RedisRouteCache
	capacityClient  *client.CapacityClient
	minAdvanceMin   int
	minCancelMin    int
	activationGrace int
	log             *logger.Logger
}

// NewJourneyService creates a new journey service.
// Event publishing is now handled by the transactional outbox (F-03);
// the publisher parameter has been removed.
func NewJourneyService(
	repo *repository.JourneyRepository,
	idempRepo *repository.IdempotencyRepository,
	mapClient *client.MapClient,
	routeCache *client.RedisRouteCache,
	capacityClient *client.CapacityClient,
	minAdvanceMin, minCancelMin, activationGrace int,
	log *logger.Logger,
) *JourneyService {
	return &JourneyService{
		repo:            repo,
		idempRepo:       idempRepo,
		mapClient:       mapClient,
		routeCache:      routeCache,
		capacityClient:  capacityClient,
		minAdvanceMin:   minAdvanceMin,
		minCancelMin:    minCancelMin,
		activationGrace: activationGrace,
		log:             log,
	}
}

// generateJourneyID creates a "jrn_" prefixed ID
func generateJourneyID() string {
	id := uuid.New().String()
	clean := strings.ReplaceAll(id, "-", "")
	return "jrn_" + clean[:8]
}

// normalizeVehicleType normalizes vehicle type to lowercase and maps HGV -> truck
func normalizeVehicleType(vt string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(vt))
	switch lower {
	case "car", "van", "truck", "motorcycle":
		return lower, nil
	case "hgv":
		return "truck", nil
	default:
		return "", apperrors.BadRequest("vehicle_type must be one of: car, van, truck, motorcycle")
	}
}

// CreateJourney orchestrates the full booking flow
func (s *JourneyService) CreateJourney(ctx context.Context, req CreateJourneyRequest) (*model.Journey, error) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "JourneyService.CreateJourney").
		Str("driver_id", req.DriverID).
		Str("vehicle_type", req.VehicleType).
		Str("idempotency_key", req.IdempotencyKey).
		Time("departure_time", req.DepartureTime).
		Msg("starting create journey flow")

	vehicleType, err := normalizeVehicleType(req.VehicleType)
	if err != nil {
		log.Warn().
			Str("service", "JourneyService.CreateJourney").
			Str("driver_id", req.DriverID).
			Err(err).
			Msg("invalid vehicle type")
		return nil, err
	}

	if req.IdempotencyKey != "" {
		log.Debug().
			Str("service", "JourneyService.CreateJourney").
			Str("idempotency_key", req.IdempotencyKey).
			Msg("checking idempotency cache")

		rec, err := s.idempRepo.Get(ctx, req.IdempotencyKey)
		if err != nil {
			log.Error().
				Str("service", "JourneyService.CreateJourney").
				Err(err).
				Str("idempotency_key", req.IdempotencyKey).
				Msg("idempotency lookup failed")
			return nil, err
		}
		if rec != nil {
			var cached model.Journey
			if err := json.Unmarshal(rec.ResponseBody, &cached); err == nil {
				log.Info().
					Str("service", "JourneyService.CreateJourney").
					Str("journey_id", cached.JourneyID).
					Msg("idempotency cache hit; returning cached response")
				return &cached, nil
			}
			log.Warn().
				Str("service", "JourneyService.CreateJourney").
				Str("idempotency_key", req.IdempotencyKey).
				Msg("failed to unmarshal idempotency payload; continuing with new booking")
		}
	}

	minDeparture := time.Now().Add(time.Duration(s.minAdvanceMin) * time.Minute)
	if req.DepartureTime.Before(minDeparture) {
		log.Warn().
			Str("service", "JourneyService.CreateJourney").
			Str("driver_id", req.DriverID).
			Time("departure_time", req.DepartureTime).
			Time("minimum_departure", minDeparture).
			Msg("departure too soon")
		return nil, &apperrors.AppError{
			Code:    "DEPARTURE_TOO_SOON",
			Message: "departure time must be at least 1 hour from now",
			Status:  422,
		}
	}

	hasActive, err := s.repo.HasActiveJourney(ctx, req.DriverID)
	if err != nil {
		log.Error().
			Str("service", "JourneyService.CreateJourney").
			Err(err).
			Str("driver_id", req.DriverID).
			Msg("failed to check active journey constraint")
		return nil, err
	}
	if hasActive {
		log.Warn().
			Str("service", "JourneyService.CreateJourney").
			Str("driver_id", req.DriverID).
			Msg("driver already has active or approved journey")
		return nil, apperrors.Conflict("driver already has an active or approved journey")
	}

	origin := client.MapCoordinates{Lat: req.Origin.Lat, Lng: req.Origin.Lng}
	dest := client.MapCoordinates{Lat: req.Destination.Lat, Lng: req.Destination.Lng}

	route, err := s.routeCache.Get(ctx, origin, dest)
	if err != nil || route == nil {
		if err != nil {
			log.Warn().Err(err).Str("service", "JourneyService.CreateJourney").Msg("route cache lookup failed")
		} else {
			log.Debug().Str("service", "JourneyService.CreateJourney").Msg("route cache miss")
		}

		route, err = s.mapClient.ComputeRoute(ctx, origin, dest)
		if err != nil {
			log.Error().
				Str("service", "JourneyService.CreateJourney").
				Err(err).
				Msg("map service unavailable")
			return nil, apperrors.ExternalAPIError("map service unavailable", err)
		}
		_ = s.routeCache.Set(ctx, origin, dest, route)
		log.Info().
			Str("service", "JourneyService.CreateJourney").
			Int("segment_count", len(route.Segments)).
			Msg("route fetched from map service")
	} else {
		log.Debug().
			Str("service", "JourneyService.CreateJourney").
			Int("segment_count", len(route.Segments)).
			Msg("route fetched from cache")
	}

	segments, estimatedArrival := ComputeTimeWindows(req.DepartureTime, route.Segments)
	journeyID := generateJourneyID()
	idempKey := req.IdempotencyKey
	if idempKey == "" {
		idempKey = uuid.New().String()
	}

	reservations := make([]client.Reservation, len(segments))
	for i, seg := range segments {
		reservations[i] = client.Reservation{
			SegmentID:       seg.SegmentID,
			TimeWindowStart: seg.TimeWindowStart,
			TimeWindowEnd:   seg.TimeWindowEnd,
		}
	}

	log.Info().
		Str("service", "JourneyService.CreateJourney").
		Str("journey_id", journeyID).
		Int("reservation_count", len(reservations)).
		Msg("calling capacity service reserve")

	reserveResp, err := s.capacityClient.Reserve(ctx, client.ReserveRequest{
		JourneyID:      journeyID,
		IdempotencyKey: idempKey,
		VehicleType:    vehicleType,
		Reservations:   reservations,
	})
	if err != nil {
		log.Error().
			Str("service", "JourneyService.CreateJourney").
			Err(err).
			Str("journey_id", journeyID).
			Msg("capacity service unavailable")
		return nil, apperrors.ExternalAPIError("capacity service unavailable", err)
	}

	now := time.Now()
	status := model.StatusApproved
	rejectionReason := ""
	reservationID := ""

	if reserveResp.Status == "failed" {
		status = model.StatusRejected
		if reserveResp.FailedSegment != nil {
			rejectionReason = "Segment " + reserveResp.FailedSegment.SegmentID + " is at capacity"
		} else {
			rejectionReason = "Capacity unavailable"
		}
		segments = nil
		log.Warn().
			Str("service", "JourneyService.CreateJourney").
			Str("journey_id", journeyID).
			Str("rejection_reason", rejectionReason).
			Msg("journey rejected by capacity constraints")
	} else {
		reservationID = reserveResp.ReservationID
		log.Info().
			Str("service", "JourneyService.CreateJourney").
			Str("journey_id", journeyID).
			Str("reservation_id", reservationID).
			Msg("journey approved by capacity service")
	}

	journey := &model.Journey{
		JourneyID:        journeyID,
		DriverID:         req.DriverID,
		IdempotencyKey:   idempKey,
		Origin:           req.Origin,
		Destination:      req.Destination,
		DepartureTime:    req.DepartureTime,
		EstimatedArrival: estimatedArrival,
		VehicleType:      vehicleType,
		Status:           status,
		RejectionReason:  rejectionReason,
		ReservationID:    reservationID,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// Build the outbox event before persisting the journey so the eventID is
	// known and can be written atomically in the same transaction (F-03).
	var evType string
	var evPayload interface{}
	if status == model.StatusApproved {
		evType = event.EventJourneyBooked
		evPayload = event.BookedPayload{
			JourneyID:        journeyID,
			DriverID:         req.DriverID,
			DepartureTime:    req.DepartureTime,
			EstimatedArrival: estimatedArrival,
			VehicleType:      vehicleType,
			Status:           string(status),
		}
	} else {
		evType = event.EventJourneyRejected
		evPayload = event.BookedPayload{
			JourneyID:       journeyID,
			DriverID:        req.DriverID,
			DepartureTime:   req.DepartureTime,
			VehicleType:     vehicleType,
			Status:          string(status),
			RejectionReason: rejectionReason,
		}
	}
	eventID, evData, err := event.MarshalEnvelope(evType, evPayload)
	if err != nil {
		log.Error().Err(err).Str("service", "JourneyService.CreateJourney").Msg("failed to marshal outbox event")
		return nil, err
	}

	if err := s.repo.Create(ctx, journey, segments, eventID, evType, evData); err != nil {
		log.Error().
			Str("service", "JourneyService.CreateJourney").
			Err(err).
			Str("journey_id", journeyID).
			Msg("failed to persist journey")
		return nil, err
	}

	journey.Segments = segments

	if req.IdempotencyKey != "" {
		if data, err := json.Marshal(journey); err == nil {
			_ = s.idempRepo.Save(ctx, req.IdempotencyKey, journeyID, data)
			log.Debug().
				Str("service", "JourneyService.CreateJourney").
				Str("idempotency_key", req.IdempotencyKey).
				Str("journey_id", journeyID).
				Msg("idempotency cache saved")
		}
	}

	log.Info().
		Str("service", "JourneyService.CreateJourney").
		Str("journey_id", journeyID).
		Str("status", string(status)).
		Msg("create journey flow completed")
	return journey, nil
}

// GetJourney returns a journey by ID, checking ownership
func (s *JourneyService) GetJourney(ctx context.Context, journeyID, driverID string, isAdmin bool) (*model.Journey, error) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "JourneyService.GetJourney").
		Str("journey_id", journeyID).
		Str("driver_id", driverID).
		Bool("is_admin", isAdmin).
		Msg("loading journey")

	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		log.Error().Str("service", "JourneyService.GetJourney").Err(err).Str("journey_id", journeyID).Msg("failed to load journey")
		return nil, err
	}
	if !isAdmin && j.DriverID != driverID {
		log.Warn().
			Str("service", "JourneyService.GetJourney").
			Str("journey_id", journeyID).
			Str("requested_driver_id", driverID).
			Str("owner_driver_id", j.DriverID).
			Msg("journey access denied")
		return nil, apperrors.NotFound("journey not found")
	}

	log.Info().Str("service", "JourneyService.GetJourney").Str("journey_id", j.JourneyID).Str("status", string(j.Status)).Msg("journey loaded")
	return j, nil
}

// ListJourneys returns paginated journeys for a driver
func (s *JourneyService) ListJourneys(ctx context.Context, driverID, statusFilter string, page, limit int) ([]model.Journey, int64, error) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "JourneyService.ListJourneys").
		Str("driver_id", driverID).
		Str("status_filter", statusFilter).
		Int("page", page).
		Int("limit", limit).
		Msg("listing driver journeys")

	journeys, total, err := s.repo.ListByDriverID(ctx, driverID, statusFilter, page, limit)
	if err != nil {
		log.Error().Str("service", "JourneyService.ListJourneys").Err(err).Str("driver_id", driverID).Msg("failed to list journeys")
		return nil, 0, err
	}
	if journeys == nil {
		journeys = []model.Journey{}
	}

	log.Info().Str("service", "JourneyService.ListJourneys").Int("result_count", len(journeys)).Int64("total", total).Msg("listed driver journeys")
	return journeys, total, nil
}

// CancelJourney cancels an APPROVED journey (30+ min before departure)
func (s *JourneyService) CancelJourney(ctx context.Context, journeyID, driverID string) (*model.Journey, error) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "JourneyService.CancelJourney").Str("journey_id", journeyID).Str("driver_id", driverID).Msg("starting cancel journey flow")

	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		log.Error().Str("service", "JourneyService.CancelJourney").Err(err).Str("journey_id", journeyID).Msg("failed to load journey")
		return nil, err
	}
	if j.DriverID != driverID {
		log.Warn().Str("service", "JourneyService.CancelJourney").Str("journey_id", journeyID).Str("owner_driver_id", j.DriverID).Msg("cancel denied due to ownership")
		return nil, apperrors.NotFound("journey not found")
	}
	if j.Status != model.StatusApproved {
		log.Warn().Str("service", "JourneyService.CancelJourney").Str("journey_id", journeyID).Str("status", string(j.Status)).Msg("cancel denied due to status")
		return nil, apperrors.BadRequest("only APPROVED journeys can be cancelled")
	}

	timeUntilDeparture := time.Until(j.DepartureTime)
	if timeUntilDeparture <= time.Duration(s.minCancelMin)*time.Minute {
		log.Warn().Str("service", "JourneyService.CancelJourney").Str("journey_id", journeyID).Dur("time_until_departure", timeUntilDeparture).Msg("cancel denied due to time window")
		return nil, apperrors.Forbidden("cannot cancel within 30 minutes of departure")
	}

	now := time.Now()
	evID, evData, err := event.MarshalEnvelope(event.EventJourneyCancelled, event.CancelledPayload{
		JourneyID:     journeyID,
		DriverID:      driverID,
		Status:        string(model.StatusCancelled),
		CancelledBy:   "driver",
		ReservationID: j.ReservationID,
	})
	if err != nil {
		log.Error().Err(err).Str("service", "JourneyService.CancelJourney").Msg("failed to marshal cancel event")
		return nil, err
	}
	if err := s.repo.UpdateStatusWithEvent(ctx, journeyID, model.StatusCancelled, j.Version,
		map[string]interface{}{"cancelled_at": now},
		evID, event.EventJourneyCancelled, evData,
	); err != nil {
		log.Error().Str("service", "JourneyService.CancelJourney").Err(err).Str("journey_id", journeyID).Msg("failed to persist cancelled status")
		return nil, err
	}

	j.Status = model.StatusCancelled
	j.CancelledAt = &now

	log.Info().Str("service", "JourneyService.CancelJourney").Str("journey_id", journeyID).Msg("cancel journey flow completed")
	return j, nil
}

// ActivateJourney transitions APPROVED -> ACTIVE
func (s *JourneyService) ActivateJourney(ctx context.Context, journeyID, driverID string) (*model.Journey, error) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "JourneyService.ActivateJourney").Str("journey_id", journeyID).Str("driver_id", driverID).Msg("starting activate journey flow")

	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		log.Error().Str("service", "JourneyService.ActivateJourney").Err(err).Str("journey_id", journeyID).Msg("failed to load journey")
		return nil, err
	}
	if j.DriverID != driverID {
		log.Warn().Str("service", "JourneyService.ActivateJourney").Str("journey_id", journeyID).Str("owner_driver_id", j.DriverID).Msg("activation denied due to ownership")
		return nil, apperrors.NotFound("journey not found")
	}
	if j.Status != model.StatusApproved {
		log.Warn().Str("service", "JourneyService.ActivateJourney").Str("journey_id", journeyID).Str("status", string(j.Status)).Msg("activation denied due to status")
		return nil, apperrors.BadRequest("only APPROVED journeys can be activated")
	}

	now := time.Now()
	graceEnd := j.DepartureTime.Add(time.Duration(s.activationGrace) * time.Minute)
	if now.Before(j.DepartureTime) {
		log.Warn().Str("service", "JourneyService.ActivateJourney").Str("journey_id", journeyID).Msg("activation denied: too early")
		return nil, apperrors.Forbidden("cannot activate before departure time")
	}
	if now.After(graceEnd) {
		log.Warn().Str("service", "JourneyService.ActivateJourney").Str("journey_id", journeyID).Time("grace_end", graceEnd).Msg("activation denied: grace expired")
		return nil, apperrors.Forbidden("activation window has expired (30 minutes after departure)")
	}

	evID, evData, err := event.MarshalEnvelope(event.EventJourneyActivated, event.SimplePayload{
		JourneyID: journeyID,
		DriverID:  driverID,
		Status:    string(model.StatusActive),
	})
	if err != nil {
		log.Error().Err(err).Str("service", "JourneyService.ActivateJourney").Msg("failed to marshal activate event")
		return nil, err
	}
	if err := s.repo.UpdateStatusWithEvent(ctx, journeyID, model.StatusActive, j.Version,
		map[string]interface{}{"activated_at": now},
		evID, event.EventJourneyActivated, evData,
	); err != nil {
		log.Error().Str("service", "JourneyService.ActivateJourney").Err(err).Str("journey_id", journeyID).Msg("failed to persist active status")
		return nil, err
	}

	j.Status = model.StatusActive
	j.ActivatedAt = &now

	log.Info().Str("service", "JourneyService.ActivateJourney").Str("journey_id", journeyID).Msg("activate journey flow completed")
	return j, nil
}

// CompleteJourney transitions ACTIVE -> COMPLETED
func (s *JourneyService) CompleteJourney(ctx context.Context, journeyID, driverID string) (*model.Journey, error) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "JourneyService.CompleteJourney").Str("journey_id", journeyID).Str("driver_id", driverID).Msg("starting complete journey flow")

	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		log.Error().Str("service", "JourneyService.CompleteJourney").Err(err).Str("journey_id", journeyID).Msg("failed to load journey")
		return nil, err
	}
	if j.DriverID != driverID {
		log.Warn().Str("service", "JourneyService.CompleteJourney").Str("journey_id", journeyID).Str("owner_driver_id", j.DriverID).Msg("completion denied due to ownership")
		return nil, apperrors.NotFound("journey not found")
	}
	if j.Status != model.StatusActive {
		log.Warn().Str("service", "JourneyService.CompleteJourney").Str("journey_id", journeyID).Str("status", string(j.Status)).Msg("completion denied due to status")
		return nil, apperrors.BadRequest("only ACTIVE journeys can be completed")
	}

	now := time.Now()
	evID, evData, err := event.MarshalEnvelope(event.EventJourneyCompleted, event.SimplePayload{
		JourneyID:     journeyID,
		DriverID:      driverID,
		Status:        string(model.StatusCompleted),
		ReservationID: j.ReservationID,
	})
	if err != nil {
		log.Error().Err(err).Str("service", "JourneyService.CompleteJourney").Msg("failed to marshal complete event")
		return nil, err
	}
	if err := s.repo.UpdateStatusWithEvent(ctx, journeyID, model.StatusCompleted, j.Version,
		map[string]interface{}{"completed_at": now},
		evID, event.EventJourneyCompleted, evData,
	); err != nil {
		log.Error().Str("service", "JourneyService.CompleteJourney").Err(err).Str("journey_id", journeyID).Msg("failed to persist completed status")
		return nil, err
	}

	j.Status = model.StatusCompleted
	j.CompletedAt = &now

	log.Info().Str("service", "JourneyService.CompleteJourney").Str("journey_id", journeyID).Msg("complete journey flow completed")
	return j, nil
}

// AdminCancelJourney force-cancels any APPROVED or ACTIVE journey.
// adminID is the user ID of the admin performing the cancellation, extracted
// from the JWT by the handler and stored in the event for audit purposes (F-10/F-19).
func (s *JourneyService) AdminCancelJourney(ctx context.Context, journeyID, adminID string) (*model.Journey, error) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "JourneyService.AdminCancelJourney").Str("journey_id", journeyID).Msg("starting admin cancel journey flow")

	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		log.Error().Str("service", "JourneyService.AdminCancelJourney").Err(err).Str("journey_id", journeyID).Msg("failed to load journey")
		return nil, err
	}
	if j.Status != model.StatusApproved && j.Status != model.StatusActive {
		log.Warn().Str("service", "JourneyService.AdminCancelJourney").Str("journey_id", journeyID).Str("status", string(j.Status)).Msg("admin cancellation denied due to status")
		return nil, apperrors.BadRequest("only APPROVED or ACTIVE journeys can be cancelled")
	}

	now := time.Now()
	// Use the actual admin user ID for audit trail (F-10/F-19).
	cancelledBy := adminID
	if cancelledBy == "" {
		cancelledBy = "admin"
	}
	evID, evData, err := event.MarshalEnvelope(event.EventJourneyCancelled, event.CancelledPayload{
		JourneyID:     journeyID,
		DriverID:      j.DriverID,
		Status:        string(model.StatusCancelled),
		CancelledBy:   cancelledBy,
		ReservationID: j.ReservationID,
	})
	if err != nil {
		log.Error().Err(err).Str("service", "JourneyService.AdminCancelJourney").Msg("failed to marshal admin cancel event")
		return nil, err
	}
	if err := s.repo.UpdateStatusWithEvent(ctx, journeyID, model.StatusCancelled, j.Version,
		map[string]interface{}{"cancelled_at": now},
		evID, event.EventJourneyCancelled, evData,
	); err != nil {
		log.Error().Str("service", "JourneyService.AdminCancelJourney").Err(err).Str("journey_id", journeyID).Msg("failed to persist cancelled status")
		return nil, err
	}

	j.Status = model.StatusCancelled
	j.CancelledAt = &now

	log.Info().Str("service", "JourneyService.AdminCancelJourney").
		Str("journey_id", journeyID).
		Str("cancelled_by", cancelledBy).
		Msg("admin cancel journey flow completed")
	return j, nil
}

// EnforcementVerifyResult is the response for the enforcement verify endpoint
type EnforcementVerifyResult struct {
	Authorized      bool       `json:"authorized"`
	JourneyID       string     `json:"journey_id,omitempty"`
	DriverID        string     `json:"driver_id,omitempty"`
	Status          string     `json:"status,omitempty"`
	SegmentID       string     `json:"segment_id"`
	TimeWindowStart *time.Time `json:"time_window_start,omitempty"`
	TimeWindowEnd   *time.Time `json:"time_window_end,omitempty"`
	Timestamp       time.Time  `json:"timestamp"`
}

// EnforcementVerify checks whether an ACTIVE journey covers segmentID at the given timestamp.
func (s *JourneyService) EnforcementVerify(ctx context.Context, segmentID string, ts time.Time) (*EnforcementVerifyResult, error) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "JourneyService.EnforcementVerify").Str("segment_id", segmentID).Time("timestamp", ts).Msg("running enforcement verification")

	rec, err := s.repo.FindActiveJourneyForSegment(ctx, segmentID, ts)
	if err != nil {
		log.Error().Str("service", "JourneyService.EnforcementVerify").Err(err).Str("segment_id", segmentID).Msg("failed to query enforcement record")
		return nil, err
	}

	result := &EnforcementVerifyResult{
		SegmentID: segmentID,
		Timestamp: ts,
	}
	if rec != nil {
		result.Authorized = true
		result.JourneyID = rec.JourneyID
		result.DriverID = rec.DriverID
		result.Status = rec.Status
		result.TimeWindowStart = &rec.TimeWindowStart
		result.TimeWindowEnd = &rec.TimeWindowEnd
		log.Info().Str("service", "JourneyService.EnforcementVerify").Str("segment_id", segmentID).Str("journey_id", rec.JourneyID).Msg("enforcement authorized")
	} else {
		log.Info().Str("service", "JourneyService.EnforcementVerify").Str("segment_id", segmentID).Msg("enforcement unauthorized")
	}

	return result, nil
}

// AdminListJourneys returns all journeys with filters
func (s *JourneyService) AdminListJourneys(ctx context.Context, filters AdminListFilters) ([]model.Journey, int64, error) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "JourneyService.AdminListJourneys").
		Str("status_filter", filters.Status).
		Str("driver_id_filter", filters.DriverID).
		Int("page", filters.Page).
		Int("limit", filters.Limit).
		Msg("listing journeys for admin")

	journeys, total, err := s.repo.AdminList(ctx, filters.Status, filters.DriverID, filters.Page, filters.Limit)
	if err != nil {
		log.Error().Str("service", "JourneyService.AdminListJourneys").Err(err).Msg("failed to list admin journeys")
		return nil, 0, err
	}
	if journeys == nil {
		journeys = []model.Journey{}
	}

	log.Info().Str("service", "JourneyService.AdminListJourneys").Int("result_count", len(journeys)).Int64("total", total).Msg("admin journeys listed")
	return journeys, total, nil
}

// AnalyticsResult is the response shape for GET /api/v1/admin/analytics (U-14).
type AnalyticsResult struct {
	TotalJourneys int64   `json:"total_journeys"`
	Approved      int64   `json:"approved"`
	Rejected      int64   `json:"rejected"`
	Active        int64   `json:"active"`
	Completed     int64   `json:"completed"`
	Cancelled     int64   `json:"cancelled"`
	Expired       int64   `json:"expired"`
	ApprovalRate  float64 `json:"approval_rate"`
	RejectionRate float64 `json:"rejection_rate"`
	Window        string  `json:"window"`
}

// AdminAnalytics returns aggregated journey counts for the requested time window.
// window must be one of "1h", "24h", "7d". Defaults to "24h" on unrecognised input.
func (s *JourneyService) AdminAnalytics(ctx context.Context, window string) (*AnalyticsResult, error) {
	windowHours := 24
	switch window {
	case "1h":
		windowHours = 1
	case "24h":
		windowHours = 24
	case "7d":
		windowHours = 24 * 7
	}

	counts, err := s.repo.AdminAnalytics(ctx, windowHours)
	if err != nil {
		return nil, err
	}

	var approvalRate, rejectionRate float64
	decided := counts.Approved + counts.Rejected
	if decided > 0 {
		approvalRate = float64(counts.Approved) / float64(decided) * 100
		rejectionRate = float64(counts.Rejected) / float64(decided) * 100
	}

	return &AnalyticsResult{
		TotalJourneys: counts.Total,
		Approved:      counts.Approved,
		Rejected:      counts.Rejected,
		Active:        counts.Active,
		Completed:     counts.Completed,
		Cancelled:     counts.Cancelled,
		Expired:       counts.Expired,
		ApprovalRate:  approvalRate,
		RejectionRate: rejectionRate,
		Window:        window,
	}, nil
}

// RunExpiryJob periodically expires approved journeys past their grace window
func (s *JourneyService) RunExpiryJob(ctx context.Context) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "JourneyService.RunExpiryJob").Dur("interval", 5*time.Minute).Msg("expiry job started")

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("service", "JourneyService.RunExpiryJob").Msg("expiry job stopped")
			return
		case <-ticker.C:
			log.Debug().Str("service", "JourneyService.RunExpiryJob").Msg("expiry tick triggered")
			s.expireJourneys(ctx)
		}
	}
}

func (s *JourneyService) expireJourneys(ctx context.Context) {
	log := s.logWithTrace(ctx)
	journeys, err := s.repo.GetExpiredJourneys(ctx)
	if err != nil {
		log.Error().Err(err).Str("service", "JourneyService.expireJourneys").Msg("failed to get expired journeys")
		return
	}

	log.Debug().Str("service", "JourneyService.expireJourneys").Int("expired_count", len(journeys)).Msg("loaded expired journeys")

	now := time.Now()
	for _, j := range journeys {
		evID, evData, err := event.MarshalEnvelope(event.EventJourneyExpired, event.SimplePayload{
			JourneyID:     j.JourneyID,
			DriverID:      j.DriverID,
			Status:        string(model.StatusExpired),
			ReservationID: j.ReservationID,
		})
		if err != nil {
			log.Warn().Err(err).Str("service", "JourneyService.expireJourneys").Str("journey_id", j.JourneyID).Msg("failed to marshal expire event")
			continue
		}
		if err := s.repo.UpdateStatusWithEvent(ctx, j.JourneyID, model.StatusExpired, j.Version,
			map[string]interface{}{"expired_at": now},
			evID, event.EventJourneyExpired, evData,
		); err != nil {
			log.Warn().Err(err).Str("service", "JourneyService.expireJourneys").Str("journey_id", j.JourneyID).Msg("failed to expire journey")
			continue
		}
		log.Info().Str("service", "JourneyService.expireJourneys").Str("journey_id", j.JourneyID).Msg("journey expired")
	}
}
