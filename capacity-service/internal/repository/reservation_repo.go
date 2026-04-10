package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// ReservationRepo handles database operations for reservations.
type ReservationRepo struct {
	master *sql.DB
	slave  *sql.DB
}

// NewReservationRepo creates a new ReservationRepo.
func NewReservationRepo(master, slave *sql.DB) *ReservationRepo {
	return &ReservationRepo{master: master, slave: slave}
}

// SumActiveOverlapping returns the total slots_used for active reservations on
// a segment whose time window overlaps [windowStart, windowEnd).
// Must be called inside a transaction (for consistency with the segment lock).
func (r *ReservationRepo) SumActiveOverlapping(
	ctx context.Context,
	tx *sql.Tx,
	segmentID string,
	windowStart, windowEnd time.Time,
) (float64, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ReservationRepo.SumActiveOverlapping").
		Str("segment_id", segmentID).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("summing active overlapping reservations")

	const q = `
		SELECT COALESCE(SUM(slots_used), 0.0)
		FROM capacity.reservations
		WHERE segment_id = $1
		  AND status = 'active'
		  AND time_window_start < $3
		  AND time_window_end   > $2`

	var total float64
	err := tx.QueryRowContext(ctx, q, segmentID, windowStart, windowEnd).Scan(&total)
	if err != nil {
		log.Error().
			Str("repository", "ReservationRepo.SumActiveOverlapping").
			Err(err).
			Str("segment_id", segmentID).
			Msg("failed to sum active overlapping reservations")
		return 0, fmt.Errorf("reservation_repo.SumActiveOverlapping(%s): %w", segmentID, err)
	}
	log.Debug().
		Str("repository", "ReservationRepo.SumActiveOverlapping").
		Str("segment_id", segmentID).
		Float64("reserved_slots", total).
		Msg("summed active overlapping reservations")
	return total, nil
}

// Insert adds one reservation row inside an active transaction.
func (r *ReservationRepo) Insert(ctx context.Context, tx *sql.Tx, res *model.Reservation) error {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ReservationRepo.Insert").
		Str("reservation_id", res.ReservationID).
		Str("journey_id", res.JourneyID).
		Str("segment_id", res.SegmentID).
		Float64("slots_used", res.SlotsUsed).
		Msg("inserting reservation row")

	const q = `
		INSERT INTO capacity.reservations
			(crdb_region, reservation_id, journey_id, segment_id, time_window_start, time_window_end,
			 vehicle_type, slots_used, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', NOW())`

	_, err := tx.ExecContext(ctx, q,
		res.CRDBRegion, res.ReservationID, res.JourneyID, res.SegmentID,
		res.TimeWindowStart, res.TimeWindowEnd,
		string(res.VehicleType), res.SlotsUsed,
	)
	if err != nil {
		log.Error().
			Str("repository", "ReservationRepo.Insert").
			Err(err).
			Str("reservation_id", res.ReservationID).
			Str("segment_id", res.SegmentID).
			Msg("failed to insert reservation row")
		return fmt.Errorf("reservation_repo.Insert: %w", err)
	}
	log.Debug().
		Str("repository", "ReservationRepo.Insert").
		Str("reservation_id", res.ReservationID).
		Str("segment_id", res.SegmentID).
		Msg("reservation row inserted")
	return nil
}

// ReleaseByJourneyID marks all active reservations for a journey as released
// and returns the affected (segmentID, windowStart, windowEnd) tuples for cache invalidation.
func (r *ReservationRepo) ReleaseByJourneyID(ctx context.Context, journeyID string) ([]model.SegmentReservation, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "ReservationRepo.ReleaseByJourneyID").
		Str("journey_id", journeyID).
		Msg("releasing reservations by journey id")

	const q = `
		UPDATE capacity.reservations
		SET    status = 'released', released_at = NOW()
		WHERE  journey_id = $1 AND status = 'active'
		RETURNING segment_id, time_window_start, time_window_end`

	rows, err := r.master.QueryContext(ctx, q, journeyID)
	if err != nil {
		log.Error().
			Str("repository", "ReservationRepo.ReleaseByJourneyID").
			Err(err).
			Str("journey_id", journeyID).
			Msg("failed to release reservations by journey id")
		return nil, fmt.Errorf("reservation_repo.ReleaseByJourneyID: %w", err)
	}
	defer rows.Close()

	var affected []model.SegmentReservation
	for rows.Next() {
		var sr model.SegmentReservation
		if err := rows.Scan(&sr.SegmentID, &sr.TimeWindowStart, &sr.TimeWindowEnd); err != nil {
			log.Error().
				Str("repository", "ReservationRepo.ReleaseByJourneyID").
				Err(err).
				Str("journey_id", journeyID).
				Msg("failed to scan released reservation row")
			return nil, fmt.Errorf("reservation_repo.ReleaseByJourneyID scan: %w", err)
		}
		affected = append(affected, sr)
	}
	if err := rows.Err(); err != nil {
		log.Error().
			Str("repository", "ReservationRepo.ReleaseByJourneyID").
			Err(err).
			Str("journey_id", journeyID).
			Msg("row iteration failed for released reservations")
		return nil, err
	}

	log.Info().
		Str("repository", "ReservationRepo.ReleaseByJourneyID").
		Str("journey_id", journeyID).
		Int("released_count", len(affected)).
		Msg("released reservations by journey id")
	return affected, nil
}

// ReleaseOrphans releases reservations whose time_window_end is older than the given threshold
// and returns the affected segments for cache invalidation.
func (r *ReservationRepo) ReleaseOrphans(ctx context.Context, olderThan time.Time) ([]model.SegmentReservation, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "ReservationRepo.ReleaseOrphans").
		Time("older_than", olderThan).
		Msg("releasing orphan reservations")

	const q = `
		UPDATE capacity.reservations
		SET    status = 'released', released_at = NOW()
		WHERE  status = 'active' AND time_window_end < $1
		RETURNING segment_id, time_window_start, time_window_end`

	rows, err := r.master.QueryContext(ctx, q, olderThan)
	if err != nil {
		log.Error().
			Str("repository", "ReservationRepo.ReleaseOrphans").
			Err(err).
			Time("older_than", olderThan).
			Msg("failed to release orphan reservations")
		return nil, fmt.Errorf("reservation_repo.ReleaseOrphans: %w", err)
	}
	defer rows.Close()

	var affected []model.SegmentReservation
	for rows.Next() {
		var sr model.SegmentReservation
		if err := rows.Scan(&sr.SegmentID, &sr.TimeWindowStart, &sr.TimeWindowEnd); err != nil {
			log.Error().
				Str("repository", "ReservationRepo.ReleaseOrphans").
				Err(err).
				Msg("failed to scan orphan release row")
			return nil, fmt.Errorf("reservation_repo.ReleaseOrphans scan: %w", err)
		}
		affected = append(affected, sr)
	}
	if err := rows.Err(); err != nil {
		log.Error().
			Str("repository", "ReservationRepo.ReleaseOrphans").
			Err(err).
			Msg("row iteration failed for orphan releases")
		return nil, err
	}

	log.Info().
		Str("repository", "ReservationRepo.ReleaseOrphans").
		Int("released_count", len(affected)).
		Msg("orphan reservations released")
	return affected, nil
}

// SumActiveAtTime returns slot totals per segment at a specific point in time
// (used by the occupancy endpoint for trend calculation).
func (r *ReservationRepo) SumActiveAtTime(ctx context.Context, at time.Time) (map[string]float64, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ReservationRepo.SumActiveAtTime").
		Time("at", at).
		Msg("summing active reservations at point in time")

	const q = `
		SELECT segment_id, COALESCE(SUM(slots_used), 0.0)
		FROM capacity.reservations
		WHERE status = 'active'
		  AND time_window_start <= $1
		  AND time_window_end    > $1
		GROUP BY segment_id`

	rows, err := r.slave.QueryContext(ctx, q, at)
	if err != nil {
		log.Error().
			Str("repository", "ReservationRepo.SumActiveAtTime").
			Err(err).
			Time("at", at).
			Msg("failed to query active reservations at time")
		return nil, fmt.Errorf("reservation_repo.SumActiveAtTime: %w", err)
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var segID string
		var total float64
		if err := rows.Scan(&segID, &total); err != nil {
			log.Error().
				Str("repository", "ReservationRepo.SumActiveAtTime").
				Err(err).
				Msg("failed to scan active reservation aggregate row")
			return nil, fmt.Errorf("reservation_repo.SumActiveAtTime scan: %w", err)
		}
		result[segID] = total
	}
	if err := rows.Err(); err != nil {
		log.Error().
			Str("repository", "ReservationRepo.SumActiveAtTime").
			Err(err).
			Msg("row iteration failed for active reservations at time")
		return nil, err
	}

	log.Debug().
		Str("repository", "ReservationRepo.SumActiveAtTime").
		Int("segment_count", len(result)).
		Msg("summed active reservations at point in time")
	return result, nil
}

// CheckAvailability returns max_capacity and currently reserved slots for a single
// segment / time window without any locking (used by the GET /check endpoint).
func (r *ReservationRepo) CheckAvailability(
	ctx context.Context,
	segmentID string,
	windowStart, windowEnd time.Time,
) (maxCapacity, reservedSlots float64, err error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ReservationRepo.CheckAvailability").
		Str("segment_id", segmentID).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("checking availability aggregates")

	const q = `
		SELECT s.max_capacity, COALESCE(SUM(res.slots_used), 0.0) AS reserved_slots
		FROM capacity.segments s
		LEFT JOIN capacity.reservations res
		       ON res.segment_id = s.segment_id
		      AND res.status = 'active'
		      AND res.time_window_start < $3
		      AND res.time_window_end   > $2
		WHERE s.segment_id = $1
		GROUP BY s.max_capacity`

	err = r.slave.QueryRowContext(ctx, q, segmentID, windowStart, windowEnd).
		Scan(&maxCapacity, &reservedSlots)
	if err != nil {
		log.Error().
			Str("repository", "ReservationRepo.CheckAvailability").
			Err(err).
			Str("segment_id", segmentID).
			Msg("failed to check availability aggregates")
		return 0, 0, fmt.Errorf("reservation_repo.CheckAvailability(%s): %w", segmentID, err)
	}
	log.Debug().
		Str("repository", "ReservationRepo.CheckAvailability").
		Str("segment_id", segmentID).
		Float64("max_capacity", maxCapacity).
		Float64("reserved_slots", reservedSlots).
		Msg("checked availability aggregates")
	return maxCapacity, reservedSlots, nil
}

// ReleaseByReservationID marks all active rows with the given reservation_id as
// released and returns the affected segment windows for cache invalidation.
// Used by the Saga coordinator to compensate a previously-committed regional
// sub-transaction when a later region fails.
func (r *ReservationRepo) ReleaseByReservationID(ctx context.Context, reservationID string) ([]model.SegmentReservation, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "ReservationRepo.ReleaseByReservationID").
		Str("reservation_id", reservationID).
		Msg("compensating saga: releasing reservations by reservation_id")

	const q = `
		UPDATE capacity.reservations
		SET    status = 'released', released_at = NOW()
		WHERE  reservation_id = $1 AND status = 'active'
		RETURNING segment_id, time_window_start, time_window_end`

	rows, err := r.master.QueryContext(ctx, q, reservationID)
	if err != nil {
		log.Error().
			Str("repository", "ReservationRepo.ReleaseByReservationID").
			Err(err).
			Str("reservation_id", reservationID).
			Msg("failed to release reservations by reservation_id")
		return nil, fmt.Errorf("reservation_repo.ReleaseByReservationID: %w", err)
	}
	defer rows.Close()

	var affected []model.SegmentReservation
	for rows.Next() {
		var sr model.SegmentReservation
		if err := rows.Scan(&sr.SegmentID, &sr.TimeWindowStart, &sr.TimeWindowEnd); err != nil {
			return nil, fmt.Errorf("reservation_repo.ReleaseByReservationID scan: %w", err)
		}
		affected = append(affected, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	log.Info().
		Str("repository", "ReservationRepo.ReleaseByReservationID").
		Str("reservation_id", reservationID).
		Int("released_count", len(affected)).
		Msg("saga compensation complete")
	return affected, nil
}
