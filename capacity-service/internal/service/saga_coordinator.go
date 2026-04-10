package service

// saga_coordinator.go — cross-regional capacity reservation saga.
//
// When a journey touches road segments in more than one geo-region (eu/us/apac),
// the Saga coordinator replaces the single serialisable transaction with a sequence
// of regional sub-transactions plus compensation (rollback) on partial failure.
//
// Flow:
//   Reserve() groups segments by crdb_region.
//   If 1 region  → fast path (doReserveTx, unchanged behaviour).
//   If >1 regions → executeSaga():
//     1. Persist saga record (PENDING).
//     2. For each region in deterministic order:
//          a. doReserveSegmentsTx(regionalPool.Get(region), ...)
//          b. On success → record step as RESERVED, persist saga.
//          c. On capacity failure → compensate all RESERVED regions,
//             mark saga COMPENSATED, return failure response.
//     3. All regions RESERVED → mark saga COMMITTED.
//     4. Write idempotency cache entry.
//     5. Return success response.
//
// Crash recovery (PENDING saga found on retry):
//   All steps recorded as RESERVED are compensated before retrying fresh.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// SegmentCRDBRegion maps a segment ID to its geo-region.
// Convention (segment ID prefix):
//   "US-*"                      → us
//   "JP-*", "AP-*", "SG-*", "CN-*" → apac
//   everything else              → eu   (IE-*, seg_*, coord-*)
func SegmentCRDBRegion(segmentID string) string {
	if len(segmentID) >= 3 {
		prefix := segmentID[:3]
		switch prefix {
		case "US-", "us-":
			return "us"
		case "JP-", "jp-", "AP-", "ap-", "SG-", "sg-", "CN-", "cn-":
			return "apac"
		}
	}
	return "eu"
}

// groupByRegion partitions the reservation list into per-region slices.
// Returns a map of crdb_region → segments and a sorted slice of region keys
// (sorted for deterministic lock ordering across concurrent sagas).
func groupByRegion(reservations []model.SegmentReservation) (map[string][]model.SegmentReservation, []string) {
	groups := make(map[string][]model.SegmentReservation)
	for _, r := range reservations {
		region := SegmentCRDBRegion(r.SegmentID)
		groups[region] = append(groups[region], r)
	}
	regions := make([]string, 0, len(groups))
	for r := range groups {
		regions = append(regions, r)
	}
	sort.Strings(regions) // deterministic order: apac < eu < us
	return groups, regions
}

// executeSaga orchestrates a multi-region reservation saga.
// Called by Reserve() when segments span more than one crdb_region.
func (s *ReservationService) executeSaga(
	ctx context.Context,
	req *model.ReserveRequest,
	groups map[string][]model.SegmentReservation,
	regionOrder []string,
	slotsNeeded float64,
	priorityLevel string,
) (interface{}, int, error) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "SagaCoordinator.executeSaga").
		Str("journey_id", req.JourneyID).
		Strs("regions", regionOrder).
		Msg("starting multi-region reservation saga")

	// --- Idempotency: check for an existing saga with this key ---
	existing, err := s.sagaRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, 500, fmt.Errorf("saga: idempotency lookup: %w", err)
	}
	if existing != nil {
		switch existing.Status {
		case model.SagaStatusCommitted:
			// Already succeeded — replay from the idempotency cache written at commit time.
			log.Info().
				Str("service", "SagaCoordinator.executeSaga").
				Str("saga_id", existing.SagaID).
				Msg("saga already committed; replaying from idempotency cache")
			cached, err := s.idempRepo.GetByKey(ctx, req.IdempotencyKey)
			if err != nil || cached == nil {
				return nil, 500, fmt.Errorf("saga: committed but idempotency cache missing")
			}
			return s.replayFromCache(cached)

		case model.SagaStatusCompensated:
			// Previous attempt failed after partial success; all regions were released.
			// Fall through to retry a fresh saga.
			log.Info().
				Str("service", "SagaCoordinator.executeSaga").
				Str("saga_id", existing.SagaID).
				Msg("saga previously compensated; retrying fresh")

		case model.SagaStatusPending:
			// Crash mid-saga. Compensate any steps that were recorded as RESERVED.
			log.Warn().
				Str("service", "SagaCoordinator.executeSaga").
				Str("saga_id", existing.SagaID).
				Msg("found PENDING saga from prior attempt; compensating before retry")
			s.compensateSaga(ctx, existing)
		}
	}

	// --- Create a new saga record ---
	sagaID := generateID("saga")
	reservationID := generateID("rsv") // shared across all regional sub-txs

	saga := &model.ReservationSaga{
		SagaID:         sagaID,
		JourneyID:      req.JourneyID,
		IdempotencyKey: req.IdempotencyKey,
		Status:         model.SagaStatusPending,
		RegionSteps:    []model.SagaRegionStep{},
		ReservationID:  &reservationID,
	}
	if err := s.sagaRepo.Create(ctx, saga); err != nil {
		return nil, 500, fmt.Errorf("saga: create record: %w", err)
	}

	// --- Execute one regional sub-transaction per region ---
	var reservedSteps []model.SagaRegionStep

	for _, region := range regionOrder {
		segments := groups[region]

		// Sort segments within the region by segment_id for consistent lock order.
		sort.Slice(segments, func(i, j int) bool {
			return segments[i].SegmentID < segments[j].SegmentID
		})

		pool := s.regionalPools.Get(region)

		log.Info().
			Str("service", "SagaCoordinator.executeSaga").
			Str("saga_id", sagaID).
			Str("region", region).
			Int("segment_count", len(segments)).
			Msg("executing regional sub-transaction")

		var failedSeg *model.FailedSegment
		const maxRetries = 5
		for attempt := 0; attempt <= maxRetries; attempt++ {
			failedSeg, err = s.doReserveSegmentsTx(
				ctx, pool,
				req.JourneyID, reservationID, req.VehicleType,
				segments, region, slotsNeeded, priorityLevel,
			)
			if err == nil || !isCRDBRetryError(err) {
				break
			}
			log.Warn().
				Str("service", "SagaCoordinator.executeSaga").
				Str("region", region).
				Int("attempt", attempt+1).
				Err(err).
				Msg("retrying regional sub-tx due to serialisation conflict")
		}

		if err != nil {
			// System-level error — compensate what we have and propagate.
			log.Error().
				Str("service", "SagaCoordinator.executeSaga").
				Str("saga_id", sagaID).
				Str("region", region).
				Err(err).
				Msg("regional sub-tx system error; compensating saga")
			errMsg := err.Error()
			_ = s.sagaRepo.UpdateStepsAndStatus(ctx, sagaID, reservedSteps,
				model.SagaStatusFailed, &reservationID, &errMsg)
			s.compensateSteps(ctx, reservedSteps)
			return nil, 500, fmt.Errorf("saga: region %s system error: %w", region, err)
		}

		if failedSeg != nil {
			// Capacity unavailable in this region — compensate all prior reserved regions.
			log.Warn().
				Str("service", "SagaCoordinator.executeSaga").
				Str("saga_id", sagaID).
				Str("region", region).
				Str("failed_segment", failedSeg.SegmentID).
				Msg("capacity failure in region; compensating saga")

			reason := fmt.Sprintf("region %s: segment %s: %s", region, failedSeg.SegmentID, failedSeg.Reason)
			_ = s.sagaRepo.UpdateStepsAndStatus(ctx, sagaID, reservedSteps,
				model.SagaStatusCompensated, &reservationID, &reason)
			s.compensateSteps(ctx, reservedSteps)

			failResp := &model.ReserveFailResponse{
				Status:        "failed",
				FailedSegment: failedSeg,
			}
			bodyBytes, _ := json.Marshal(failResp)
			_ = s.idempRepo.InsertOnConflictIgnore(
				ctx, req.IdempotencyKey, req.JourneyID, nil, "failed", bodyBytes,
			)
			return failResp, 200, nil
		}

		// Region succeeded — record the step.
		step := model.SagaRegionStep{
			Region:        region,
			Status:        "RESERVED",
			ReservationID: reservationID,
		}
		reservedSteps = append(reservedSteps, step)
		if err := s.sagaRepo.UpdateStepsAndStatus(ctx, sagaID, reservedSteps,
			model.SagaStatusPending, &reservationID, nil); err != nil {
			log.Warn().Err(err).Str("saga_id", sagaID).Msg("failed to persist saga step (non-fatal)")
		}

		log.Info().
			Str("service", "SagaCoordinator.executeSaga").
			Str("saga_id", sagaID).
			Str("region", region).
			Msg("regional sub-tx committed")
	}

	// --- All regions reserved: commit saga and write idempotency cache ---
	if err := s.sagaRepo.UpdateStepsAndStatus(ctx, sagaID, reservedSteps,
		model.SagaStatusCommitted, &reservationID, nil); err != nil {
		log.Warn().Err(err).Str("saga_id", sagaID).Msg("failed to mark saga COMMITTED (non-fatal)")
	}

	successResp := &model.ReserveSuccessResponse{
		Status:        "reserved",
		ReservationID: reservationID,
		JourneyID:     req.JourneyID,
	}
	bodyBytes, _ := json.Marshal(successResp)
	if err := s.idempRepo.InsertOnConflictIgnore(
		ctx, req.IdempotencyKey, req.JourneyID, &reservationID, "reserved", bodyBytes,
	); err != nil {
		log.Warn().Err(err).Str("saga_id", sagaID).Msg("failed to write idempotency cache after saga commit (non-fatal)")
	}

	log.Info().
		Str("service", "SagaCoordinator.executeSaga").
		Str("saga_id", sagaID).
		Str("reservation_id", reservationID).
		Int("region_count", len(regionOrder)).
		Msg("multi-region saga committed successfully")

	return successResp, 201, nil
}

// doReserveSegmentsTx runs one serialisable transaction that locks all segments
// in the given slice and inserts reservation rows — without touching the
// idempotency cache.  Used exclusively by the saga coordinator.
//
// Returns:
//   - (*FailedSegment, nil) if a segment had no capacity (soft failure).
//   - (nil, nil) on success.
//   - (nil, err) on a system/DB error.
func (s *ReservationService) doReserveSegmentsTx(
	ctx context.Context,
	db *sql.DB,
	journeyID string,
	reservationID string,
	vehicleType model.VehicleType,
	segments []model.SegmentReservation,
	crdbRegion string,
	slotsNeeded float64,
	priorityLevel string,
) (*model.FailedSegment, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("doReserveSegmentsTx begin: %w", err)
	}
	defer tx.Rollback()

	var failedSeg *model.FailedSegment

	for _, seg := range segments {
		if !seg.TimeWindowEnd.After(seg.TimeWindowStart) {
			failedSeg = &model.FailedSegment{
				SegmentID:       seg.SegmentID,
				Reason:          "invalid_time_window",
				RequestedSlots:  slotsNeeded,
				TimeWindowStart: seg.TimeWindowStart,
				TimeWindowEnd:   seg.TimeWindowEnd,
			}
			break
		}

		maxCapacity, err := s.segmentRepo.LockForUpdate(ctx, tx, seg.SegmentID)
		if errors.Is(err, sql.ErrNoRows) {
			failedSeg = &model.FailedSegment{
				SegmentID:       seg.SegmentID,
				Reason:          "unknown_segment",
				RequestedSlots:  slotsNeeded,
				TimeWindowStart: seg.TimeWindowStart,
				TimeWindowEnd:   seg.TimeWindowEnd,
			}
			break
		}
		if err != nil {
			return nil, fmt.Errorf("doReserveSegmentsTx lock %s: %w", seg.SegmentID, err)
		}

		if s.closureRepo != nil {
			activeClosure, err := s.closureRepo.FindActiveOverlapTx(ctx, tx, seg.SegmentID, seg.TimeWindowStart, seg.TimeWindowEnd)
			if err != nil {
				return nil, fmt.Errorf("doReserveSegmentsTx closure %s: %w", seg.SegmentID, err)
			}
			if activeClosure != nil {
				closureStart := activeClosure.StartTime
				closureEnd := activeClosure.EndTime
				failedSeg = &model.FailedSegment{
					SegmentID:       seg.SegmentID,
					Reason:          "segment_closed",
					RequestedSlots:  slotsNeeded,
					TimeWindowStart: seg.TimeWindowStart,
					TimeWindowEnd:   seg.TimeWindowEnd,
					ClosureReason:   activeClosure.Reason,
					ClosureStart:    &closureStart,
					ClosureEnd:      &closureEnd,
				}
				break
			}
		}

		reserved, err := s.reservRepo.SumActiveOverlapping(ctx, tx, seg.SegmentID, seg.TimeWindowStart, seg.TimeWindowEnd)
		if err != nil {
			return nil, fmt.Errorf("doReserveSegmentsTx sum %s: %w", seg.SegmentID, err)
		}

		effective := effectiveCapacityForPriority(maxCapacity, priorityLevel)
		available := effective - reserved
		if available < slotsNeeded {
			if available < 0 {
				available = 0
			}
			failedSeg = &model.FailedSegment{
				SegmentID:       seg.SegmentID,
				Reason:          "at_capacity",
				AvailableSlots:  available,
				RequestedSlots:  slotsNeeded,
				TimeWindowStart: seg.TimeWindowStart,
				TimeWindowEnd:   seg.TimeWindowEnd,
			}
			break
		}
	}

	if failedSeg != nil {
		// Soft failure — rollback this region's tx and signal caller to compensate.
		return failedSeg, nil
	}

	// All segments in this region have capacity — insert reservation rows.
	for _, seg := range segments {
		if err := s.reservRepo.Insert(ctx, tx, &model.Reservation{
			ReservationID:   reservationID,
			JourneyID:       journeyID,
			SegmentID:       seg.SegmentID,
			CRDBRegion:      crdbRegion,
			TimeWindowStart: seg.TimeWindowStart,
			TimeWindowEnd:   seg.TimeWindowEnd,
			VehicleType:     vehicleType,
			SlotsUsed:       slotsNeeded,
		}); err != nil {
			return nil, fmt.Errorf("doReserveSegmentsTx insert %s: %w", seg.SegmentID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("doReserveSegmentsTx commit: %w", err)
	}

	// Invalidate availability cache for the segments we just reserved.
	s.invalidateAvailabilityCache(ctx, segments)
	return nil, nil
}

// compensateSaga releases reservations for all RESERVED steps in a saga.
// Called during crash recovery when a PENDING saga is found on retry.
func (s *ReservationService) compensateSaga(ctx context.Context, saga *model.ReservationSaga) {
	s.compensateSteps(ctx, saga.RegionSteps)
	_ = s.sagaRepo.UpdateStepsAndStatus(
		ctx, saga.SagaID, saga.RegionSteps,
		model.SagaStatusCompensated, saga.ReservationID, nil,
	)
}

// compensateSteps releases all reservations for each RESERVED region step
// and invalidates the availability cache for the affected segments.
func (s *ReservationService) compensateSteps(ctx context.Context, steps []model.SagaRegionStep) {
	log := s.logWithTrace(ctx)
	for _, step := range steps {
		if step.Status != "RESERVED" {
			continue
		}
		affected, err := s.reservRepo.ReleaseByReservationID(ctx, step.ReservationID)
		if err != nil {
			log.Error().
				Err(err).
				Str("region", step.Region).
				Str("reservation_id", step.ReservationID).
				Msg("saga compensation: failed to release reservations")
			continue
		}
		s.InvalidateCacheForJourney(ctx, affected)
		log.Info().
			Str("region", step.Region).
			Str("reservation_id", step.ReservationID).
			Int("released", len(affected)).
			Msg("saga compensation: reservations released")
	}
}
