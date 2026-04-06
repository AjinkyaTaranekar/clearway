
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
	publisher       *event.Publisher
	minAdvanceMin   int
	minCancelMin    int
	activationGrace int
	log             *logger.Logger
}

// NewJourneyService creates a new journey service
func NewJourneyService(
	repo *repository.JourneyRepository,
	idempRepo *repository.IdempotencyRepository,
	mapClient *client.MapClient,
	routeCache *client.RedisRouteCache,
	capacityClient *client.CapacityClient,
	publisher *event.Publisher,
	minAdvanceMin, minCancelMin, activationGrace int,
	log *logger.Logger,
) *JourneyService {
	return &JourneyService{
		repo:            repo,
		idempRepo:       idempRepo,
		mapClient:       mapClient,
		routeCache:      routeCache,
		capacityClient:  capacityClient,
		publisher:       publisher,
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

// normalizeVehicleType normalizes vehicle type to lowercase and maps HGV → truck
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
	// Validate vehicle type
	vehicleType, err := normalizeVehicleType(req.VehicleType)
	if err != nil {
		return nil, err
	}

	// Check idempotency
	if req.IdempotencyKey != "" {
		rec, err := s.idempRepo.Get(ctx, req.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			var j model.Journey
			if err := json.Unmarshal(rec.ResponseBody, &j); err == nil {
				return &j, nil
			}
		}
	}

	// Validate departure time >= now + minAdvanceMin
	minDeparture := time.Now().Add(time.Duration(s.minAdvanceMin) * time.Minute)
	if req.DepartureTime.Before(minDeparture) {
		return nil, &apperrors.AppError{
			Code:    "DEPARTURE_TOO_SOON",
			Message: "departure time must be at least 1 hour from now",
			Status:  422,
		}
	}

	// Check one active journey per driver
	hasActive, err := s.repo.HasActiveJourney(ctx, req.DriverID)
	if err != nil {
		return nil, err
	}
	if hasActive {
		return nil, apperrors.Conflict("driver already has an active or approved journey")
	}

	// Get route (cache → Map Service)
	origin := client.MapCoordinates{Lat: req.Origin.Lat, Lng: req.Origin.Lng}
	dest := client.MapCoordinates{Lat: req.Destination.Lat, Lng: req.Destination.Lng}

	route, err := s.routeCache.Get(ctx, origin, dest)
	if err != nil || route == nil {
		route, err = s.mapClient.ComputeRoute(ctx, origin, dest)
		if err != nil {
			return nil, apperrors.ExternalAPIError("map service unavailable", err)
		}
		_ = s.routeCache.Set(ctx, origin, dest, route)
	}

	// Compute cascading time windows
	segments, estimatedArrival := ComputeTimeWindows(req.DepartureTime, route.Segments)

	// Build capacity reservations
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

	// Call Capacity Service
	reserveResp, err := s.capacityClient.Reserve(ctx, client.ReserveRequest{
		JourneyID:      journeyID,
		IdempotencyKey: idempKey,
		VehicleType:    vehicleType,
		Reservations:   reservations,
	})
	if err != nil {
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
		segments = nil // don't store segments for rejected journeys
	} else {
		reservationID = reserveResp.ReservationID
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

	if err := s.repo.Create(ctx, journey, segments); err != nil {
		return nil, err
	}

	journey.Segments = segments

	// Cache idempotency key
	if req.IdempotencyKey != "" {
		if data, err := json.Marshal(journey); err == nil {
			_ = s.idempRepo.Save(ctx, req.IdempotencyKey, journeyID, data)
		}
	}

	// Publish event (best-effort, after response)
	if status == model.StatusApproved {
		s.publisher.Publish(ctx, event.EventJourneyBooked, event.BookedPayload{
			JourneyID:        journeyID,
			DriverID:         req.DriverID,
			DepartureTime:    req.DepartureTime,
			EstimatedArrival: estimatedArrival,
			VehicleType:      vehicleType,
			Status:           string(status),
		})
	} else {
		s.publisher.Publish(ctx, event.EventJourneyRejected, event.BookedPayload{
			JourneyID:       journeyID,
			DriverID:        req.DriverID,
			DepartureTime:   req.DepartureTime,
			VehicleType:     vehicleType,
			Status:          string(status),
			RejectionReason: rejectionReason,
		})
	}

	return journey, nil
}

// GetJourney returns a journey by ID, checking ownership
func (s *JourneyService) GetJourney(ctx context.Context, journeyID, driverID string, isAdmin bool) (*model.Journey, error) {
	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		return nil, err
	}
	if !isAdmin && j.DriverID != driverID {
		return nil, apperrors.NotFound("journey not found")
	}
	return j, nil
}

// ListJourneys returns paginated journeys for a driver
func (s *JourneyService) ListJourneys(ctx context.Context, driverID, statusFilter string, page, limit int) ([]model.Journey, int64, error) {
	journeys, total, err := s.repo.ListByDriverID(ctx, driverID, statusFilter, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if journeys == nil {
		journeys = []model.Journey{}
	}
	return journeys, total, nil
}

// CancelJourney cancels an APPROVED journey (30+ min before departure)
func (s *JourneyService) CancelJourney(ctx context.Context, journeyID, driverID string) (*model.Journey, error) {
	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		return nil, err
	}
	if j.DriverID != driverID {
		return nil, apperrors.NotFound("journey not found")
	}
	if j.Status != model.StatusApproved {
		return nil, apperrors.BadRequest("only APPROVED journeys can be cancelled")
	}

	timeUntilDeparture := time.Until(j.DepartureTime)
	if timeUntilDeparture <= time.Duration(s.minCancelMin)*time.Minute {
		return nil, apperrors.Forbidden("cannot cancel within 30 minutes of departure")
	}

	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, journeyID, model.StatusCancelled, j.Version, map[string]interface{}{
		"cancelled_at": now,
	}); err != nil {
		return nil, err
	}

	j.Status = model.StatusCancelled
	j.CancelledAt = &now

	s.publisher.Publish(ctx, event.EventJourneyCancelled, event.CancelledPayload{
		JourneyID:     journeyID,
		DriverID:      driverID,
		Status:        string(model.StatusCancelled),
		CancelledBy:   "driver",
		ReservationID: j.ReservationID,
	})

	return j, nil
}

// ActivateJourney transitions APPROVED → ACTIVE
func (s *JourneyService) ActivateJourney(ctx context.Context, journeyID, driverID string) (*model.Journey, error) {
	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		return nil, err
	}
	if j.DriverID != driverID {
		return nil, apperrors.NotFound("journey not found")
	}
	if j.Status != model.StatusApproved {
		return nil, apperrors.BadRequest("only APPROVED journeys can be activated")
	}

	now := time.Now()
	graceEnd := j.DepartureTime.Add(time.Duration(s.activationGrace) * time.Minute)
	if now.Before(j.DepartureTime) {
		return nil, apperrors.Forbidden("cannot activate before departure time")
	}
	if now.After(graceEnd) {
		return nil, apperrors.Forbidden("activation window has expired (30 minutes after departure)")
	}

	if err := s.repo.UpdateStatus(ctx, journeyID, model.StatusActive, j.Version, map[string]interface{}{
		"activated_at": now,
	}); err != nil {
		return nil, err
	}

	j.Status = model.StatusActive
	j.ActivatedAt = &now

	s.publisher.Publish(ctx, event.EventJourneyActivated, event.SimplePayload{
		JourneyID: journeyID,
		DriverID:  driverID,
		Status:    string(model.StatusActive),
	})

	return j, nil
}

// CompleteJourney transitions ACTIVE → COMPLETED
func (s *JourneyService) CompleteJourney(ctx context.Context, journeyID, driverID string) (*model.Journey, error) {
	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		return nil, err
	}
	if j.DriverID != driverID {
		return nil, apperrors.NotFound("journey not found")
	}
	if j.Status != model.StatusActive {
		return nil, apperrors.BadRequest("only ACTIVE journeys can be completed")
	}

	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, journeyID, model.StatusCompleted, j.Version, map[string]interface{}{
		"completed_at": now,
	}); err != nil {
		return nil, err
	}

	j.Status = model.StatusCompleted
	j.CompletedAt = &now

	s.publisher.Publish(ctx, event.EventJourneyCompleted, event.SimplePayload{
		JourneyID:     journeyID,
		DriverID:      driverID,
		Status:        string(model.StatusCompleted),
		ReservationID: j.ReservationID,
	})

	return j, nil
}

// AdminCancelJourney force-cancels any APPROVED or ACTIVE journey
func (s *JourneyService) AdminCancelJourney(ctx context.Context, journeyID string) (*model.Journey, error) {
	j, err := s.repo.GetByID(ctx, journeyID)
	if err != nil {
		return nil, err
	}
	if j.Status != model.StatusApproved && j.Status != model.StatusActive {
		return nil, apperrors.BadRequest("only APPROVED or ACTIVE journeys can be cancelled")
	}

	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, journeyID, model.StatusCancelled, j.Version, map[string]interface{}{
		"cancelled_at": now,
	}); err != nil {
		return nil, err
	}

	j.Status = model.StatusCancelled
	j.CancelledAt = &now

	s.publisher.Publish(ctx, event.EventJourneyCancelled, event.CancelledPayload{
		JourneyID:     journeyID,
		DriverID:      j.DriverID,
		Status:        string(model.StatusCancelled),
		CancelledBy:   "admin",
		ReservationID: j.ReservationID,
	})

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
	rec, err := s.repo.FindActiveJourneyForSegment(ctx, segmentID, ts)
	if err != nil {
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
	}
	return result, nil
}

// AdminListJourneys returns all journeys with filters
func (s *JourneyService) AdminListJourneys(ctx context.Context, filters AdminListFilters) ([]model.Journey, int64, error) {
	journeys, total, err := s.repo.AdminList(ctx, filters.Status, filters.DriverID, filters.Page, filters.Limit)
	if err != nil {
		return nil, 0, err
	}
	if journeys == nil {
		journeys = []model.Journey{}
	}
	return journeys, total, nil
}

// RunExpiryJob periodically expires approved journeys past their grace window
func (s *JourneyService) RunExpiryJob(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.expireJourneys(ctx)
		}
	}
}

func (s *JourneyService) expireJourneys(ctx context.Context) {
	journeys, err := s.repo.GetExpiredJourneys(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("expiry job: failed to get expired journeys")
		return
	}

	now := time.Now()
	for _, j := range journeys {
		if err := s.repo.UpdateStatus(ctx, j.JourneyID, model.StatusExpired, j.Version, map[string]interface{}{
			"expired_at": now,
		}); err != nil {
			s.log.Warn().Err(err).Str("journey_id", j.JourneyID).Msg("expiry job: failed to expire journey")
			continue
		}
		s.publisher.Publish(ctx, event.EventJourneyExpired, event.SimplePayload{
			JourneyID:     j.JourneyID,
			DriverID:      j.DriverID,
			Status:        string(model.StatusExpired),
			ReservationID: j.ReservationID,
		})
		s.log.Info().Str("journey_id", j.JourneyID).Msg("expiry job: journey expired")
	}
}
