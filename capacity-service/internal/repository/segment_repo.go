package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// SegmentRepo handles database operations for road segments.
type SegmentRepo struct {
	db *sql.DB
}

// NewSegmentRepo creates a new SegmentRepo using the given connection pool.
func NewSegmentRepo(db *sql.DB) *SegmentRepo {
	return &SegmentRepo{db: db}
}

// GetAll returns every segment in the capacity schema (used by occupancy endpoint).
func (r *SegmentRepo) GetAll(ctx context.Context) ([]model.Segment, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "SegmentRepo.GetAll").
		Msg("querying all capacity segments")

	const q = `
		SELECT segment_id, segment_name, region, max_capacity, version, created_at, updated_at
		FROM capacity.segments
		ORDER BY segment_id`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		log.Error().
			Str("repository", "SegmentRepo.GetAll").
			Err(err).
			Msg("failed to query capacity segments")
		return nil, fmt.Errorf("segment_repo.GetAll: %w", err)
	}
	defer rows.Close()

	var segments []model.Segment
	for rows.Next() {
		var s model.Segment
		if err := rows.Scan(
			&s.SegmentID, &s.SegmentName, &s.Region,
			&s.MaxCapacity, &s.Version, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			log.Error().
				Str("repository", "SegmentRepo.GetAll").
				Err(err).
				Msg("failed to scan capacity segment row")
			return nil, fmt.Errorf("segment_repo.GetAll scan: %w", err)
		}
		segments = append(segments, s)
	}
	if err := rows.Err(); err != nil {
		log.Error().
			Str("repository", "SegmentRepo.GetAll").
			Err(err).
			Msg("row iteration failed for capacity segments")
		return nil, err
	}

	log.Debug().
		Str("repository", "SegmentRepo.GetAll").
		Int("segment_count", len(segments)).
		Msg("loaded capacity segments")
	return segments, nil
}

// LockForUpdate locks the segment row and returns its max_capacity.
// Returns sql.ErrNoRows if the segment does not exist.
// Must be called inside an active transaction.
func (r *SegmentRepo) LockForUpdate(ctx context.Context, tx *sql.Tx, segmentID string) (float64, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "SegmentRepo.LockForUpdate").
		Str("segment_id", segmentID).
		Msg("locking segment for update")

	const q = `
		SELECT max_capacity
		FROM capacity.segments
		WHERE segment_id = $1
		FOR UPDATE`

	var maxCapacity float64
	err := tx.QueryRowContext(ctx, q, segmentID).Scan(&maxCapacity)
	if err != nil {
		log.Error().
			Str("repository", "SegmentRepo.LockForUpdate").
			Err(err).
			Str("segment_id", segmentID).
			Msg("failed to lock segment")
		return 0, fmt.Errorf("segment_repo.LockForUpdate(%s): %w", segmentID, err)
	}
	log.Debug().
		Str("repository", "SegmentRepo.LockForUpdate").
		Str("segment_id", segmentID).
		Float64("max_capacity", maxCapacity).
		Msg("segment locked for update")
	return maxCapacity, nil
}
