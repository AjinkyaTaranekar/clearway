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
		return 0, fmt.Errorf("reservation_repo.SumActiveOverlapping(%s): %w", segmentID, err)
	}
	return total, nil
}

// Insert adds one reservation row inside an active transaction.
func (r *ReservationRepo) Insert(ctx context.Context, tx *sql.Tx, res *model.Reservation) error {
	const q = `
		INSERT INTO capacity.reservations
			(reservation_id, journey_id, segment_id, time_window_start, time_window_end,
			 vehicle_type, slots_used, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', NOW())`

	_, err := tx.ExecContext(ctx, q,
		res.ReservationID, res.JourneyID, res.SegmentID,
		res.TimeWindowStart, res.TimeWindowEnd,
		string(res.VehicleType), res.SlotsUsed,
	)
	if err != nil {
		return fmt.Errorf("reservation_repo.Insert: %w", err)
	}
	return nil
}

// ReleaseByJourneyID marks all active reservations for a journey as released
// and returns the affected (segmentID, windowStart, windowEnd) tuples for cache invalidation.
func (r *ReservationRepo) ReleaseByJourneyID(ctx context.Context, journeyID string) ([]model.SegmentReservation, error) {
	const q = `
		UPDATE capacity.reservations
		SET    status = 'released', released_at = NOW()
		WHERE  journey_id = $1 AND status = 'active'
		RETURNING segment_id, time_window_start, time_window_end`

	rows, err := r.master.QueryContext(ctx, q, journeyID)
	if err != nil {
		return nil, fmt.Errorf("reservation_repo.ReleaseByJourneyID: %w", err)
	}
	defer rows.Close()

	var affected []model.SegmentReservation
	for rows.Next() {
		var sr model.SegmentReservation
		if err := rows.Scan(&sr.SegmentID, &sr.TimeWindowStart, &sr.TimeWindowEnd); err != nil {
			return nil, fmt.Errorf("reservation_repo.ReleaseByJourneyID scan: %w", err)
		}
		affected = append(affected, sr)
	}
	return affected, rows.Err()
}

// ReleaseOrphans releases reservations whose time_window_end is older than the given threshold
// and returns the affected segments for cache invalidation.
func (r *ReservationRepo) ReleaseOrphans(ctx context.Context, olderThan time.Time) ([]model.SegmentReservation, error) {
	const q = `
		UPDATE capacity.reservations
		SET    status = 'released', released_at = NOW()
		WHERE  status = 'active' AND time_window_end < $1
		RETURNING segment_id, time_window_start, time_window_end`

	rows, err := r.master.QueryContext(ctx, q, olderThan)
	if err != nil {
		return nil, fmt.Errorf("reservation_repo.ReleaseOrphans: %w", err)
	}
	defer rows.Close()

	var affected []model.SegmentReservation
	for rows.Next() {
		var sr model.SegmentReservation
		if err := rows.Scan(&sr.SegmentID, &sr.TimeWindowStart, &sr.TimeWindowEnd); err != nil {
			return nil, fmt.Errorf("reservation_repo.ReleaseOrphans scan: %w", err)
		}
		affected = append(affected, sr)
	}
	return affected, rows.Err()
}

// SumActiveAtTime returns slot totals per segment at a specific point in time
// (used by the occupancy endpoint for trend calculation).
func (r *ReservationRepo) SumActiveAtTime(ctx context.Context, at time.Time) (map[string]float64, error) {
	const q = `
		SELECT segment_id, COALESCE(SUM(slots_used), 0.0)
		FROM capacity.reservations
		WHERE status = 'active'
		  AND time_window_start <= $1
		  AND time_window_end    > $1
		GROUP BY segment_id`

	rows, err := r.slave.QueryContext(ctx, q, at)
	if err != nil {
		return nil, fmt.Errorf("reservation_repo.SumActiveAtTime: %w", err)
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var segID string
		var total float64
		if err := rows.Scan(&segID, &total); err != nil {
			return nil, fmt.Errorf("reservation_repo.SumActiveAtTime scan: %w", err)
		}
		result[segID] = total
	}
	return result, rows.Err()
}

// CheckAvailability returns max_capacity and currently reserved slots for a single
// segment / time window without any locking (used by the GET /check endpoint).
func (r *ReservationRepo) CheckAvailability(
	ctx context.Context,
	segmentID string,
	windowStart, windowEnd time.Time,
) (maxCapacity, reservedSlots float64, err error) {
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
		return 0, 0, fmt.Errorf("reservation_repo.CheckAvailability(%s): %w", segmentID, err)
	}
	return maxCapacity, reservedSlots, nil
}
