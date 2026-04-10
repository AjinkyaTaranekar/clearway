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
	capacitypostgres "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/postgres"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// ReservationService handles the core capacity reservation logic.
type ReservationService struct {
	db            *sql.DB
	segmentRepo   *repository.SegmentRepo
	reservRepo    *repository.ReservationRepo
	closureRepo   *repository.ClosureRepo
	idempRepo     *repository.IdempotencyRepo
	sagaRepo      *repository.SagaRepo
	regionalPools *capacitypostgres.RegionalPools // per-region DB pools for saga coordinator
	redis         *redis.Client
	cacheTTL      time.Duration
	log           *logger.Logger
}

const (
	priorityLevelNormal         = "normal"
	priorityLevelMax            = "max"
	emergencyCapacityBufferSlot = 1.0
)

// SegmentRegistration represents a segment that should exist in capacity.segments.
// The map-service uses this to register OSRM-derived segments before booking.
type SegmentRegistration struct {
	SegmentID   string `json:"segment_id"`
	SegmentName string `json:"segment_name"`
	Region      string `json:"region"`
}

// NewReservationService creates a new ReservationService.
// regionalPools provides per-region CockroachDB master connections for the saga
// coordinator; if nil a default single-pool setup using db is used.
func NewReservationService(
	db *sql.DB,
	segmentRepo *repository.SegmentRepo,
	reservRepo *repository.ReservationRepo,
	closureRepo *repository.ClosureRepo,
	idempRepo *repository.IdempotencyRepo,
	sagaRepo *repository.SagaRepo,
	regionalPools *capacitypostgres.RegionalPools,
	redisClient *redis.Client,
	cacheTTL time.Duration,
	log *logger.Logger,
) *ReservationService {
	if regionalPools == nil {
		// Single-cell fallback: all regions use the same master pool.
		regionalPools = &capacitypostgres.RegionalPools{EU: db, US: db, APAC: db}
	}
	return &ReservationService{
		db:            db,
		segmentRepo:   segmentRepo,
		reservRepo:    reservRepo,
		closureRepo:   closureRepo,
		idempRepo:     idempRepo,
		sagaRepo:      sagaRepo,
		regionalPools: regionalPools,
		redis:         redisClient,
		cacheTTL:      cacheTTL,
		log:           log,
	}
}

// Reserve atomically reserves capacity across all requested segments.
// Returns the response body and the HTTP status code to send.
func (s *ReservationService) Reserve(ctx context.Context, req *model.ReserveRequest) (interface{}, int, error) {
	log := s.logWithTrace(ctx)
	if req != nil {
		log.Info().
			Str("service", "ReservationService.Reserve").
			Str("journey_id", req.JourneyID).
			Str("idempotency_key", req.IdempotencyKey).
			Str("vehicle_type", string(req.VehicleType)).
			Str("priority_level", normalizePriorityLevel(req.PriorityLevel)).
			Int("segment_count", len(req.Reservations)).
			Msg("starting capacity reservation flow")
	} else {
		log.Info().
			Str("service", "ReservationService.Reserve").
			Msg("starting capacity reservation flow with nil request")
	}

	// --- Input validation ---
	if err := validateReserveRequest(req); err != nil {
		log.Warn().
			Str("service", "ReservationService.Reserve").
			Err(err).
			Msg("reserve request validation failed")
		return nil, 400, err
	}

	priorityLevel := normalizePriorityLevel(req.PriorityLevel)
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
		log.Error().
			Str("service", "ReservationService.Reserve").
			Err(err).
			Str("idempotency_key", req.IdempotencyKey).
			Msg("idempotency lookup failed")
		return nil, 500, fmt.Errorf("idempotency lookup failed: %w", err)
	}
	if cached != nil {
		log.Info().
			Str("service", "ReservationService.Reserve").
			Str("idempotency_key", req.IdempotencyKey).
			Str("response_status", cached.ResponseStatus).
			Msg("idempotency cache hit")
		return s.replayFromCache(cached)
	}

	// --- Route: single-region fast path vs. multi-region saga ---
	// Group segments by crdb_region.  If all segments are in the same region,
	// use the existing single serialisable transaction (fast path).
	// If segments span multiple regions, delegate to the saga coordinator.
	groups, regionOrder := groupByRegion(reservations)

	if len(groups) > 1 {
		log.Info().
			Str("service", "ReservationService.Reserve").
			Str("journey_id", req.JourneyID).
			Strs("regions", regionOrder).
			Msg("cross-regional journey detected; routing to saga coordinator")
		return s.executeSaga(ctx, req, groups, regionOrder, slotsNeeded, priorityLevel)
	}

	// Single-region fast path: determine the crdb_region for the INSERT.
	crdbRegion := regionOrder[0]

	// --- Transaction with CRDB serialization-error retry ---
	// CockroachDB may return "restart transaction" (SQLSTATE 40001) when a
	// serializable transaction hits a read/write conflict. The correct response
	// is to roll back and retry the entire transaction from scratch.
	const maxTxRetries = 5
	var (
		txResult interface{}
		txStatus int
		txErr    error
	)
	for attempt := 0; attempt <= maxTxRetries; attempt++ {
		txResult, txStatus, txErr = s.doReserveTx(ctx, req, reservations, crdbRegion, slotsNeeded, priorityLevel)
		if txErr == nil || !isCRDBRetryError(txErr) {
			break
		}
		log.Warn().
			Str("service", "ReservationService.Reserve").
			Str("journey_id", req.JourneyID).
			Int("attempt", attempt+1).
			Err(txErr).
			Msg("retrying reservation transaction due to serialization conflict")
	}
	return txResult, txStatus, txErr
}

// doReserveTx executes the reservation inside a single serializable transaction.
// It returns the response body, HTTP status, and any error. Callers should retry
// if isCRDBRetryError(err) is true.
func (s *ReservationService) doReserveTx(
	ctx context.Context,
	req *model.ReserveRequest,
	reservations []model.SegmentReservation,
	crdbRegion string,
	slotsNeeded float64,
	priorityLevel string,
) (interface{}, int, error) {
	log := s.logWithTrace(ctx)

	// --- Begin serialisable transaction ---
	// LevelSerializable prevents phantom reads: two concurrent transactions
	// that both read the same capacity sum (both below the limit) cannot both
	// commit - the second will be rolled back with a serialisation error.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Error().
			Str("service", "ReservationService.Reserve").
			Err(err).
			Msg("failed to begin serializable transaction")
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
		log.Error().
			Str("service", "ReservationService.Reserve").
			Err(recheckErr).
			Str("idempotency_key", req.IdempotencyKey).
			Msg("idempotency re-check failed")
		return nil, 500, fmt.Errorf("idempotency re-check: %w", recheckErr)
	}
	if recheckErr == nil {
		log.Info().
			Str("service", "ReservationService.Reserve").
			Str("idempotency_key", req.IdempotencyKey).
			Str("response_status", recheck.ResponseStatus).
			Msg("idempotency cache found inside transaction")
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
			log.Warn().
				Str("service", "ReservationService.Reserve").
				Str("segment_id", r.SegmentID).
				Msg("reserve failed: unknown segment")
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
			log.Error().
				Str("service", "ReservationService.Reserve").
				Err(err).
				Str("segment_id", r.SegmentID).
				Msg("failed to lock segment for update")
			return nil, 500, fmt.Errorf("lock segment %s: %w", r.SegmentID, err)
		}

		if s.closureRepo != nil {
			activeClosure, err := s.closureRepo.FindActiveOverlapTx(ctx, tx, r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd)
			if err != nil {
				log.Error().
					Str("service", "ReservationService.Reserve").
					Err(err).
					Str("segment_id", r.SegmentID).
					Msg("failed to check segment closure overlap")
				return nil, 500, fmt.Errorf("check closure overlap %s: %w", r.SegmentID, err)
			}
			if activeClosure != nil {
				closureStart := activeClosure.StartTime
				closureEnd := activeClosure.EndTime
				failedSegment = &model.FailedSegment{
					SegmentID:       r.SegmentID,
					Reason:          "segment_closed",
					AvailableSlots:  0,
					RequestedSlots:  slotsNeeded,
					TimeWindowStart: r.TimeWindowStart,
					TimeWindowEnd:   r.TimeWindowEnd,
					ClosureReason:   activeClosure.Reason,
					ClosureStart:    &closureStart,
					ClosureEnd:      &closureEnd,
				}
				break
			}
		}

		currentlyReserved, err := s.reservRepo.SumActiveOverlapping(ctx, tx, r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd)
		if err != nil {
			log.Error().
				Str("service", "ReservationService.Reserve").
				Err(err).
				Str("segment_id", r.SegmentID).
				Msg("failed to calculate overlapping reservations")
			return nil, 500, fmt.Errorf("sum overlapping %s: %w", r.SegmentID, err)
		}

		effectiveCapacity := effectiveCapacityForPriority(maxCapacity, priorityLevel)
		available := effectiveCapacity - currentlyReserved
		if available < slotsNeeded {
			if available < 0 {
				available = 0
			}
			log.Warn().
				Str("service", "ReservationService.Reserve").
				Str("segment_id", r.SegmentID).
				Str("priority_level", priorityLevel).
				Float64("effective_capacity", effectiveCapacity).
				Float64("available_slots", available).
				Float64("requested_slots", slotsNeeded).
				Msg("reserve failed: segment at capacity")
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
		log.Warn().
			Str("service", "ReservationService.Reserve").
			Str("segment_id", failedSegment.SegmentID).
			Str("reason", failedSegment.Reason).
			Msg("reservation flow completed with capacity failure")

		failResp := &model.ReserveFailResponse{
			Status:        "failed",
			FailedSegment: failedSegment,
		}
		bodyBytes, _ := json.Marshal(failResp)

		// Record in idempotency_cache so retries return the same answer.
		if cErr := s.idempRepo.InsertOnConflictIgnore(
			ctx, req.IdempotencyKey, req.JourneyID, nil, "failed", bodyBytes,
		); cErr != nil {
			log.Warn().Err(cErr).Msg("failed to write failed idempotency cache entry")
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
			CRDBRegion:      crdbRegion,
			TimeWindowStart: r.TimeWindowStart,
			TimeWindowEnd:   r.TimeWindowEnd,
			VehicleType:     req.VehicleType,
			SlotsUsed:       slotsNeeded,
		}); err != nil {
			log.Error().
				Str("service", "ReservationService.Reserve").
				Err(err).
				Str("segment_id", r.SegmentID).
				Str("reservation_id", reservationID).
				Msg("failed to insert reservation row")
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
			log.Warn().
				Str("service", "ReservationService.Reserve").
				Err(err).
				Str("idempotency_key", req.IdempotencyKey).
				Msg("idempotency unique conflict; replaying winner")
			tx.Rollback()
			// Retry lookup - may take a moment to replicate; return internal error if still absent.
			cached, lookupErr := s.idempRepo.GetByKey(ctx, req.IdempotencyKey)
			if lookupErr != nil || cached == nil {
				log.Error().
					Str("service", "ReservationService.Reserve").
					Err(err).
					Str("idempotency_key", req.IdempotencyKey).
					Msg("idempotency conflict unresolved after lookup")
				return nil, 500, fmt.Errorf("idempotency conflict - entry not yet visible: %w", err)
			}
			return s.replayFromCache(cached)
		}
		log.Error().
			Str("service", "ReservationService.Reserve").
			Err(err).
			Str("idempotency_key", req.IdempotencyKey).
			Msg("failed to insert idempotency record in transaction")
		return nil, 500, fmt.Errorf("insert idempotency cache: %w", err)
	}

	// --- Commit ---
	if err := tx.Commit(); err != nil {
		log.Error().
			Str("service", "ReservationService.Reserve").
			Err(err).
			Str("reservation_id", reservationID).
			Msg("failed to commit reservation transaction")
		return nil, 500, fmt.Errorf("commit reservation tx: %w", err)
	}

	// --- Invalidate availability cache for affected segments ---
	s.invalidateAvailabilityCache(ctx, reservations)
	log.Info().
		Str("service", "ReservationService.Reserve").
		Str("journey_id", req.JourneyID).
		Str("reservation_id", reservationID).
		Int("segment_count", len(reservations)).
		Msg("capacity reservation flow completed successfully")

	return successResp, 201, nil
}

// CheckAvailability returns the current capacity info for one segment/window.
// Results are cached in Redis for cacheTTL seconds.
func (s *ReservationService) CheckAvailability(
	ctx context.Context,
	segmentID string,
	windowStart, windowEnd time.Time,
	priorityLevel string,
) (*model.CheckResponse, error) {
	normalizedPriority := normalizePriorityLevel(priorityLevel)
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "ReservationService.CheckAvailability").
		Str("segment_id", segmentID).
		Str("priority_level", normalizedPriority).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("checking segment availability")

	cacheKey := availabilityCacheKey(segmentID, windowStart, windowEnd, normalizedPriority)

	// Try Redis cache first.
	if s.redis != nil {
		if val, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil {
			var cached model.CheckResponse
			if json.Unmarshal(val, &cached) == nil {
				if s.closureRepo != nil {
					activeClosure, cErr := s.closureRepo.FindActiveOverlap(ctx, segmentID, windowStart, windowEnd)
					if cErr != nil {
						log.Warn().
							Str("service", "ReservationService.CheckAvailability").
							Err(cErr).
							Str("segment_id", segmentID).
							Msg("failed to validate closure state for cached availability")
					} else if activeClosure != nil {
						closureStart := activeClosure.StartTime
						closureEnd := activeClosure.EndTime
						cached.AvailableSlots = 0
						cached.CanReserve = false
						cached.IsClosed = true
						cached.ClosureReason = activeClosure.Reason
						cached.ClosureStart = &closureStart
						cached.ClosureEnd = &closureEnd
					}
				}
				log.Debug().
					Str("service", "ReservationService.CheckAvailability").
					Str("segment_id", segmentID).
					Msg("availability cache hit")
				return &cached, nil
			}
		}
	}

	maxCap, reserved, err := s.reservRepo.CheckAvailability(ctx, segmentID, windowStart, windowEnd)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Warn().
				Str("service", "ReservationService.CheckAvailability").
				Str("segment_id", segmentID).
				Msg("availability check failed: segment not found")
			return nil, fmt.Errorf("segment %s not found", segmentID)
		}
		log.Error().
			Str("service", "ReservationService.CheckAvailability").
			Err(err).
			Str("segment_id", segmentID).
			Msg("availability query failed")
		return nil, err
	}

	effectiveCapacity := effectiveCapacityForPriority(maxCap, normalizedPriority)
	available := effectiveCapacity - reserved
	if available < 0 {
		available = 0
	}

	var activeClosure *model.SegmentClosure
	if s.closureRepo != nil {
		activeClosure, err = s.closureRepo.FindActiveOverlap(ctx, segmentID, windowStart, windowEnd)
		if err != nil {
			log.Error().
				Str("service", "ReservationService.CheckAvailability").
				Err(err).
				Str("segment_id", segmentID).
				Msg("closure overlap query failed")
			return nil, err
		}
	}
	if activeClosure != nil {
		available = 0
	}

	resp := &model.CheckResponse{
		SegmentID:      segmentID,
		MaxCapacity:    maxCap,
		ReservedSlots:  reserved,
		AvailableSlots: available,
		CanReserve:     available >= 1.0, // minimum vehicle weight is 0.5 (motorcycle); use 1.0 as sensible default
	}
	if activeClosure != nil {
		closureStart := activeClosure.StartTime
		closureEnd := activeClosure.EndTime
		resp.IsClosed = true
		resp.CanReserve = false
		resp.ClosureReason = activeClosure.Reason
		resp.ClosureStart = &closureStart
		resp.ClosureEnd = &closureEnd
	}

	// Populate cache asynchronously to not delay the response.
	if s.redis != nil {
		if data, err := json.Marshal(resp); err == nil {
			s.redis.Set(ctx, cacheKey, data, s.cacheTTL)
		}
	}

	log.Info().
		Str("service", "ReservationService.CheckAvailability").
		Str("segment_id", segmentID).
		Str("priority_level", normalizedPriority).
		Float64("effective_capacity", effectiveCapacity).
		Float64("available_slots", resp.AvailableSlots).
		Float64("reserved_slots", resp.ReservedSlots).
		Float64("max_capacity", resp.MaxCapacity).
		Bool("can_reserve", resp.CanReserve).
		Msg("availability check completed")

	return resp, nil
}

// GetOccupancy returns current occupancy info for all segments plus a trend indicator.
func (s *ReservationService) GetOccupancy(ctx context.Context) ([]model.OccupancyInfo, error) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "ReservationService.GetOccupancy").Msg("building occupancy snapshot")

	now := time.Now().UTC()
	prev := now.Add(-15 * time.Minute)

	segments, err := s.segmentRepo.GetAll(ctx)
	if err != nil {
		log.Error().Str("service", "ReservationService.GetOccupancy").Err(err).Msg("failed to load segments")
		return nil, err
	}

	currentMap, err := s.reservRepo.SumActiveAtTime(ctx, now)
	if err != nil {
		log.Error().Str("service", "ReservationService.GetOccupancy").Err(err).Msg("failed to load current occupancy")
		return nil, err
	}
	prevMap, err := s.reservRepo.SumActiveAtTime(ctx, prev)
	if err != nil {
		log.Error().Str("service", "ReservationService.GetOccupancy").Err(err).Msg("failed to load previous occupancy")
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
	log.Info().
		Str("service", "ReservationService.GetOccupancy").
		Int("segment_count", len(infos)).
		Msg("occupancy snapshot built")
	return infos, nil
}

// GetAllSegments returns the full list of registered road segments and their
// static metadata (no occupancy calculation).
// This is the canonical segment-ID source of truth consumed by Map Service on
// startup for XC-02 alignment validation.
func (s *ReservationService) GetAllSegments(ctx context.Context) ([]model.Segment, error) {
	log := s.logWithTrace(ctx)
	log.Info().Str("service", "ReservationService.GetAllSegments").Msg("listing all segments")

	segments, err := s.segmentRepo.GetAll(ctx)
	if err != nil {
		log.Error().Str("service", "ReservationService.GetAllSegments").Err(err).Msg("failed to list segments")
		return nil, err
	}
	log.Info().Str("service", "ReservationService.GetAllSegments").Int("segment_count", len(segments)).Msg("listed all segments")
	return segments, nil
}

// RegisterSegments ensures all provided segments exist in capacity.segments.
// Missing rows are inserted using the current default max capacity.
func (s *ReservationService) RegisterSegments(ctx context.Context, segments []SegmentRegistration) (float64, int, error) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "ReservationService.RegisterSegments").
		Int("requested_count", len(segments)).
		Msg("registering dynamic segments")

	if len(segments) == 0 {
		return 0, 0, fmt.Errorf("segments list must not be empty")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	defaultCapacity, err := s.segmentRepo.GetDefaultMaxCapacityTx(ctx, tx)
	if err != nil {
		return 0, 0, fmt.Errorf("load default segment capacity: %w", err)
	}

	registeredCount := 0
	seen := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		segmentID := strings.TrimSpace(segment.SegmentID)
		if segmentID == "" {
			return 0, 0, fmt.Errorf("segment_id is required")
		}
		if _, ok := seen[segmentID]; ok {
			continue
		}
		seen[segmentID] = struct{}{}

		segmentName := strings.TrimSpace(segment.SegmentName)
		if segmentName == "" {
			segmentName = segmentID
		}

		if err := s.segmentRepo.InsertIfMissingTx(
			ctx,
			tx,
			segmentID,
			segmentName,
			normalizeSegmentRegion(segment.Region),
			defaultCapacity,
		); err != nil {
			return 0, 0, err
		}
		registeredCount++
	}

	if registeredCount == 0 {
		return 0, 0, fmt.Errorf("no valid segments provided")
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit segment registration tx: %w", err)
	}

	log.Info().
		Str("service", "ReservationService.RegisterSegments").
		Float64("default_max_capacity", defaultCapacity).
		Int("registered_count", registeredCount).
		Msg("dynamic segment registration completed")

	return defaultCapacity, registeredCount, nil
}

// GetDefaultSegmentCapacity returns the default max capacity used for newly
// discovered segments.
func (s *ReservationService) GetDefaultSegmentCapacity(ctx context.Context) (float64, error) {
	return s.segmentRepo.GetDefaultMaxCapacity(ctx)
}

// UpdateDefaultSegmentCapacity updates the default max capacity used for newly
// discovered segments.
func (s *ReservationService) UpdateDefaultSegmentCapacity(ctx context.Context, maxCapacity float64, adminID string) (float64, error) {
	if maxCapacity <= 0 {
		return 0, fmt.Errorf("max_capacity must be greater than 0")
	}

	updatedBy := strings.TrimSpace(adminID)
	if updatedBy == "" {
		updatedBy = "admin"
	}

	persisted, err := s.segmentRepo.SetDefaultMaxCapacity(ctx, maxCapacity, updatedBy)
	if err != nil {
		return 0, err
	}

	return persisted, nil
}

// UpdateSegmentCapacity updates max capacity for one segment.
func (s *ReservationService) UpdateSegmentCapacity(ctx context.Context, segmentID string, maxCapacity float64) error {
	segmentID = strings.TrimSpace(segmentID)
	if segmentID == "" {
		return fmt.Errorf("segment_id is required")
	}
	if maxCapacity <= 0 {
		return fmt.Errorf("max_capacity must be greater than 0")
	}

	if err := s.segmentRepo.UpdateMaxCapacity(ctx, segmentID, maxCapacity); err != nil {
		return err
	}

	s.invalidateAvailabilityCacheBySegment(ctx, segmentID)
	return nil
}

// InvalidateCacheForJourney removes availability cache entries for the segments
// that were just released. Called by the event consumer after a release.
func (s *ReservationService) InvalidateCacheForJourney(ctx context.Context, affected []model.SegmentReservation) {
	log := s.logWithTrace(ctx)
	log.Debug().
		Str("service", "ReservationService.InvalidateCacheForJourney").
		Int("segment_count", len(affected)).
		Msg("invalidating availability cache for journey")
	s.invalidateAvailabilityCache(ctx, affected)
}

// --- helpers ---

func (s *ReservationService) replayFromCache(entry *model.IdempotencyCache) (interface{}, int, error) {
	log := s.logWithTrace(context.Background())
	log.Debug().
		Str("service", "ReservationService.replayFromCache").
		Str("idempotency_key", entry.IdempotencyKey).
		Str("response_status", entry.ResponseStatus).
		Msg("replaying cached reservation response")

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
	log := s.logWithTrace(ctx)
	if s.redis == nil {
		log.Debug().
			Str("service", "ReservationService.invalidateAvailabilityCache").
			Msg("cache invalidation skipped: redis disabled")
		return
	}
	keys := make([]string, 0, len(reservations))
	for _, r := range reservations {
		keys = append(keys,
			availabilityCacheKey(r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd, priorityLevelNormal),
			availabilityCacheKey(r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd, priorityLevelMax),
		)
	}
	if len(keys) == 0 {
		log.Debug().
			Str("service", "ReservationService.invalidateAvailabilityCache").
			Msg("cache invalidation skipped: no keys")
		return
	}
	if err := s.redis.Del(ctx, keys...).Err(); err != nil {
		log.Warn().Err(err).Strs("keys", keys).Msg("failed to invalidate availability cache")
		return
	}
	log.Debug().
		Str("service", "ReservationService.invalidateAvailabilityCache").
		Int("key_count", len(keys)).
		Msg("availability cache invalidated")
}

func (s *ReservationService) invalidateAvailabilityCacheBySegment(ctx context.Context, segmentID string) {
	log := s.logWithTrace(ctx)
	if s.redis == nil {
		return
	}

	pattern := fmt.Sprintf("cap:avail:%s:*", segmentID)
	keys := make([]string, 0, 16)
	iter := s.redis.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		log.Warn().
			Str("service", "ReservationService.invalidateAvailabilityCacheBySegment").
			Err(err).
			Str("segment_id", segmentID).
			Msg("failed while scanning availability cache keys")
		return
	}
	if len(keys) == 0 {
		return
	}
	if err := s.redis.Del(ctx, keys...).Err(); err != nil {
		log.Warn().
			Str("service", "ReservationService.invalidateAvailabilityCacheBySegment").
			Err(err).
			Str("segment_id", segmentID).
			Msg("failed to invalidate segment availability cache keys")
	}
}

func availabilityCacheKey(segmentID string, start, end time.Time, priorityLevel string) string {
	return fmt.Sprintf("cap:avail:%s:%d:%d:%s", segmentID, start.Unix(), end.Unix(), normalizePriorityLevel(priorityLevel))
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

// isCRDBRetryError reports whether err is a CockroachDB serialization error
// that the application must handle by retrying the entire transaction from scratch.
// CRDB signals these as SQLSTATE 40001 with a message prefix of "restart transaction".
func isCRDBRetryError(err error) bool {
	if err == nil {
		return false
	}
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "40001" && strings.HasPrefix(pqErr.Message, "restart transaction")
}

func normalizePriorityLevel(level string) string {
	lower := strings.ToLower(strings.TrimSpace(level))
	if lower == priorityLevelMax {
		return priorityLevelMax
	}
	return priorityLevelNormal
}

func normalizeSegmentRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "north", "south", "east", "west", "central", "intercity":
		return strings.ToLower(strings.TrimSpace(region))
	default:
		return "intercity"
	}
}

func isPriorityLevelValid(level string) bool {
	lower := strings.ToLower(strings.TrimSpace(level))
	return lower == "" || lower == priorityLevelNormal || lower == priorityLevelMax
}

func effectiveCapacityForPriority(maxCapacity float64, priorityLevel string) float64 {
	if normalizePriorityLevel(priorityLevel) == priorityLevelMax {
		return maxCapacity
	}

	buffer := emergencyCapacityBufferSlot
	if buffer > maxCapacity {
		buffer = maxCapacity
	}

	effectiveCapacity := maxCapacity - buffer
	if effectiveCapacity < 0 {
		return 0
	}
	return effectiveCapacity
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
	if !isPriorityLevelValid(req.PriorityLevel) {
		return fmt.Errorf("priority_level must be either normal or max")
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
