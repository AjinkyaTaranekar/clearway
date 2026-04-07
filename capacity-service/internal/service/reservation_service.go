package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// ReservationService handles the core capacity reservation logic.
type ReservationService struct {
	db          *sql.DB
	segmentRepo *repository.SegmentRepo
	reservRepo  *repository.ReservationRepo
	idempRepo   *repository.IdempotencyRepo
	redis       *redis.Client
	cacheTTL    time.Duration
	log         *logger.Logger
}

// NewReservationService creates a new ReservationService.
func NewReservationService(
	db *sql.DB,
	segmentRepo *repository.SegmentRepo,
	reservRepo *repository.ReservationRepo,
	idempRepo *repository.IdempotencyRepo,
	redisClient *redis.Client,
	cacheTTL time.Duration,
	log *logger.Logger,
) *ReservationService {
	return &ReservationService{
		db:          db,
		segmentRepo: segmentRepo,
		reservRepo:  reservRepo,
		idempRepo:   idempRepo,
		redis:       redisClient,
		cacheTTL:    cacheTTL,
		log:         log,
	}
}

// Reserve atomically reserves capacity across all requested segments.
// Returns the response body and the HTTP status code to send.
func (s *ReservationService) Reserve(ctx context.Context, req *model.ReserveRequest) (interface{}, int, error) {
	// --- Input validation ---
	if err := validateReserveRequest(req); err != nil {
		return nil, 400, err
	}

	slotsNeeded := req.VehicleType.SlotsNeeded()

	// Sort reservations by segment_id to guarantee a consistent lock order
	// across concurrent transactions and prevent deadlocks.
	reservations := make([]model.SegmentReservation, len(req.Reservations))
	copy(reservations, req.Reservations)
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].SegmentID < reservations[j].SegmentID
	})

	// --- Fast-path: check idempotency cache before acquiring any locks ---
	cached, err := s.idempRepo.GetByKey(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, 500, fmt.Errorf("idempotency lookup failed: %w", err)
	}
	if cached != nil {
		return s.replayFromCache(cached)
	}

	// --- Begin serialisable transaction ---
	// LevelSerializable prevents phantom reads: two concurrent transactions
	// that both read the same capacity sum (both below the limit) cannot both
	// commit — the second will be rolled back with a serialisation error.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, 500, fmt.Errorf("begin tx: %w", err)
	}
	// Deferred rollback is a no-op after a successful Commit.
	defer tx.Rollback()

	// --- Re-check idempotency cache inside the transaction ---
	// This prevents two concurrent requests with the same key from both
	// passing the first check and then both trying to insert.
	// (Rare but possible under high concurrency.)
	var recheck model.IdempotencyCache
	const recheckQ = `
		SELECT idempotency_key, journey_id, reservation_id, response_status, response_body, created_at, expires_at
		FROM capacity.idempotency_cache
		WHERE idempotency_key = $1 AND expires_at > NOW()`
	recheckErr := tx.QueryRowContext(ctx, recheckQ, req.IdempotencyKey).Scan(
		&recheck.IdempotencyKey,
		&recheck.JourneyID,
		&recheck.ReservationID,
		&recheck.ResponseStatus,
		&recheck.ResponseBody,
		&recheck.CreatedAt,
		&recheck.ExpiresAt,
	)
	if recheckErr != nil && recheckErr != sql.ErrNoRows {
		return nil, 500, fmt.Errorf("idempotency re-check: %w", recheckErr)
	}
	if recheckErr == nil {
		tx.Commit()
		return s.replayFromCache(&recheck)
	}

	// --- Lock each segment row (sorted order) and check capacity ---
	var failedSegment *model.FailedSegment

	for _, r := range reservations {
		// Validate time window
		if !r.TimeWindowEnd.After(r.TimeWindowStart) {
			failedSegment = &model.FailedSegment{
				SegmentID:       r.SegmentID,
				Reason:          "invalid_time_window",
				AvailableSlots:  0,
				RequestedSlots:  slotsNeeded,
				TimeWindowStart: r.TimeWindowStart,
				TimeWindowEnd:   r.TimeWindowEnd,
			}
			break
		}

		maxCapacity, err := s.segmentRepo.LockForUpdate(ctx, tx, r.SegmentID)
		if errors.Is(err, sql.ErrNoRows) {
			failedSegment = &model.FailedSegment{
				SegmentID:       r.SegmentID,
				Reason:          "unknown_segment",
				AvailableSlots:  0,
				RequestedSlots:  slotsNeeded,
				TimeWindowStart: r.TimeWindowStart,
				TimeWindowEnd:   r.TimeWindowEnd,
			}
			break
		}
		if err != nil {
			return nil, 500, fmt.Errorf("lock segment %s: %w", r.SegmentID, err)
		}

		currentlyReserved, err := s.reservRepo.SumActiveOverlapping(ctx, tx, r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd)
		if err != nil {
			return nil, 500, fmt.Errorf("sum overlapping %s: %w", r.SegmentID, err)
		}

		available := maxCapacity - currentlyReserved
		if available < slotsNeeded {
			if available < 0 {
				available = 0
			}
			failedSegment = &model.FailedSegment{
				SegmentID:       r.SegmentID,
				Reason:          "at_capacity",
				AvailableSlots:  available,
				RequestedSlots:  slotsNeeded,
				TimeWindowStart: r.TimeWindowStart,
				TimeWindowEnd:   r.TimeWindowEnd,
			}
			break
		}
	}

	// --- Capacity unavailable: rollback and record the failure ---
	if failedSegment != nil {
		tx.Rollback()

		failResp := &model.ReserveFailResponse{
			Status:        "failed",
			FailedSegment: failedSegment,
		}
		bodyBytes, _ := json.Marshal(failResp)

		// Record in idempotency_cache so retries return the same answer.
		if cErr := s.idempRepo.InsertOnConflictIgnore(
			ctx, req.IdempotencyKey, req.JourneyID, nil, "failed", bodyBytes,
		); cErr != nil {
			s.log.Warn().Err(cErr).Msg("failed to write failed idempotency cache entry")
		}
		return failResp, 200, nil
	}

	// --- All segments have capacity: insert reservations ---
	reservationID := generateID("rsv")

	for _, r := range reservations {
		if err := s.reservRepo.Insert(ctx, tx, &model.Reservation{
			ReservationID:   reservationID,
			JourneyID:       req.JourneyID,
			SegmentID:       r.SegmentID,
			TimeWindowStart: r.TimeWindowStart,
			TimeWindowEnd:   r.TimeWindowEnd,
			VehicleType:     req.VehicleType,
			SlotsUsed:       slotsNeeded,
		}); err != nil {
			return nil, 500, fmt.Errorf("insert reservation for segment %s: %w", r.SegmentID, err)
		}
	}

	// --- Insert idempotency cache entry (inside the same transaction) ---
	successResp := &model.ReserveSuccessResponse{
		Status:        "reserved",
		ReservationID: reservationID,
		JourneyID:     req.JourneyID,
	}
	bodyBytes, _ := json.Marshal(successResp)

	if err := s.idempRepo.InsertInTx(
		ctx, tx, req.IdempotencyKey, req.JourneyID, &reservationID, "reserved", bodyBytes,
	); err != nil {
		// Unique violation means another VM committed the same idempotency key first
		// (race condition during replication lag). Rollback and return the winner's entry.
		if isUniqueViolation(err) {
			tx.Rollback()
			// Retry lookup — may take a moment to replicate; return internal error if still absent.
			cached, lookupErr := s.idempRepo.GetByKey(ctx, req.IdempotencyKey)
			if lookupErr != nil || cached == nil {
				return nil, 500, fmt.Errorf("idempotency conflict — entry not yet visible: %w", err)
			}
			return s.replayFromCache(cached)
		}
		return nil, 500, fmt.Errorf("insert idempotency cache: %w", err)
	}

	// --- Commit ---
	if err := tx.Commit(); err != nil {
		return nil, 500, fmt.Errorf("commit reservation tx: %w", err)
	}

	// --- Invalidate availability cache for affected segments ---
	s.invalidateAvailabilityCache(ctx, reservations)

	return successResp, 201, nil
}

// CheckAvailability returns the current capacity info for one segment/window.
// Results are cached in Redis for cacheTTL seconds.
func (s *ReservationService) CheckAvailability(
	ctx context.Context,
	segmentID string,
	windowStart, windowEnd time.Time,
) (*model.CheckResponse, error) {
	cacheKey := availabilityCacheKey(segmentID, windowStart, windowEnd)

	// Try Redis cache first.
	if s.redis != nil {
		if val, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil {
			var cached model.CheckResponse
			if json.Unmarshal(val, &cached) == nil {
				return &cached, nil
			}
		}
	}

	maxCap, reserved, err := s.reservRepo.CheckAvailability(ctx, segmentID, windowStart, windowEnd)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("segment %s not found", segmentID)
		}
		return nil, err
	}

	available := maxCap - reserved
	if available < 0 {
		available = 0
	}

	resp := &model.CheckResponse{
		SegmentID:      segmentID,
		MaxCapacity:    maxCap,
		ReservedSlots:  reserved,
		AvailableSlots: available,
		CanReserve:     available >= 1.0, // minimum vehicle weight is 0.5 (motorcycle); use 1.0 as sensible default
	}

	// Populate cache asynchronously to not delay the response.
	if s.redis != nil {
		if data, err := json.Marshal(resp); err == nil {
			s.redis.Set(ctx, cacheKey, data, s.cacheTTL)
		}
	}

	return resp, nil
}

// GetOccupancy returns current occupancy info for all segments plus a trend indicator.
func (s *ReservationService) GetOccupancy(ctx context.Context) ([]model.OccupancyInfo, error) {
	now := time.Now().UTC()
	prev := now.Add(-15 * time.Minute)

	segments, err := s.segmentRepo.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	currentMap, err := s.reservRepo.SumActiveAtTime(ctx, now)
	if err != nil {
		return nil, err
	}
	prevMap, err := s.reservRepo.SumActiveAtTime(ctx, prev)
	if err != nil {
		return nil, err
	}

	infos := make([]model.OccupancyInfo, 0, len(segments))
	for _, seg := range segments {
		current := currentMap[seg.SegmentID]
		previous := prevMap[seg.SegmentID]

		pct := 0.0
		if seg.MaxCapacity > 0 {
			pct = (current / seg.MaxCapacity) * 100.0
		}

		trend := "stable"
		diff := current - previous
		if diff > 0.5 {
			trend = "increasing"
		} else if diff < -0.5 {
			trend = "decreasing"
		}

		infos = append(infos, model.OccupancyInfo{
			SegmentID:       seg.SegmentID,
			OccupancyPct:    roundTwo(pct),
			Level:           model.OccupancyLevel(pct),
			CurrentVehicles: current,
			MaxCapacity:     seg.MaxCapacity,
			Trend:           trend,
		})
	}
	return infos, nil
}

// InvalidateCacheForJourney removes availability cache entries for the segments
// that were just released. Called by the event consumer after a release.
func (s *ReservationService) InvalidateCacheForJourney(ctx context.Context, affected []model.SegmentReservation) {
	s.invalidateAvailabilityCache(ctx, affected)
}

// --- helpers ---

func (s *ReservationService) replayFromCache(entry *model.IdempotencyCache) (interface{}, int, error) {
	if entry.ResponseStatus == "reserved" {
		var resp model.ReserveSuccessResponse
		if err := json.Unmarshal(entry.ResponseBody, &resp); err != nil {
			return nil, 500, fmt.Errorf("unmarshal cached success response: %w", err)
		}
		return &resp, 201, nil
	}
	var resp model.ReserveFailResponse
	if err := json.Unmarshal(entry.ResponseBody, &resp); err != nil {
		return nil, 500, fmt.Errorf("unmarshal cached fail response: %w", err)
	}
	return &resp, 200, nil
}

func (s *ReservationService) invalidateAvailabilityCache(ctx context.Context, reservations []model.SegmentReservation) {
	if s.redis == nil {
		return
	}
	keys := make([]string, 0, len(reservations))
	for _, r := range reservations {
		keys = append(keys, availabilityCacheKey(r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd))
	}
	if len(keys) == 0 {
		return
	}
	if err := s.redis.Del(ctx, keys...).Err(); err != nil {
		s.log.Warn().Err(err).Strs("keys", keys).Msg("failed to invalidate availability cache")
	}
}

func availabilityCacheKey(segmentID string, start, end time.Time) string {
	return fmt.Sprintf("cap:avail:%s:%d:%d", segmentID, start.Unix(), end.Unix())
}

func generateID(prefix string) string {
	id := uuid.New().String()
	// Take first 8 hex chars from UUID for a short readable ID
	compact := strings.ReplaceAll(id, "-", "")[:8]
	return prefix + "_" + compact
}

func roundTwo(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// lib/pq returns structured SQLSTATE codes; 23505 = unique_violation.
	// Using errors.As avoids false positives from error messages that happen
	// to contain the word "unique" (e.g. column names or driver messages).
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func validateReserveRequest(req *model.ReserveRequest) error {
	if req.JourneyID == "" {
		return fmt.Errorf("journey_id is required")
	}
	if req.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key is required")
	}
	if !req.VehicleType.IsValid() {
		return fmt.Errorf("invalid vehicle_type %q: must be car, van, motorcycle, or truck", req.VehicleType)
	}
	if len(req.Reservations) == 0 {
		return fmt.Errorf("reservations list must not be empty")
	}
	for i, r := range req.Reservations {
		if r.SegmentID == "" {
			return fmt.Errorf("reservations[%d].segment_id is required", i)
		}
		if r.TimeWindowStart.IsZero() || r.TimeWindowEnd.IsZero() {
			return fmt.Errorf("reservations[%d]: time_window_start and time_window_end are required", i)
		}
	}
	return nil
}
