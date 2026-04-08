package repository

import (
	"context"
	"database/sql"
	"errors"
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

// GetDefaultMaxCapacity returns the current default capacity used for newly
// discovered segments.
func (r *SegmentRepo) GetDefaultMaxCapacity(ctx context.Context) (float64, error) {
	const q = `
		SELECT default_segment_max_capacity
		FROM capacity.settings
		WHERE singleton = TRUE`

	var maxCapacity float64
	err := r.db.QueryRowContext(ctx, q).Scan(&maxCapacity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("segment_repo.GetDefaultMaxCapacity: defaults row missing")
		}
		return 0, fmt.Errorf("segment_repo.GetDefaultMaxCapacity: %w", err)
	}

	return maxCapacity, nil
}

// GetDefaultMaxCapacityTx returns the default segment capacity while inside an
// active transaction.
func (r *SegmentRepo) GetDefaultMaxCapacityTx(ctx context.Context, tx *sql.Tx) (float64, error) {
	const q = `
		SELECT default_segment_max_capacity
		FROM capacity.settings
		WHERE singleton = TRUE
		FOR UPDATE`

	var maxCapacity float64
	err := tx.QueryRowContext(ctx, q).Scan(&maxCapacity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("segment_repo.GetDefaultMaxCapacityTx: defaults row missing")
		}
		return 0, fmt.Errorf("segment_repo.GetDefaultMaxCapacityTx: %w", err)
	}

	return maxCapacity, nil
}

// SetDefaultMaxCapacity updates the default capacity value used for newly
// registered segments and returns the stored value.
func (r *SegmentRepo) SetDefaultMaxCapacity(ctx context.Context, maxCapacity float64, updatedBy string) (float64, error) {
	const q = `
		INSERT INTO capacity.settings (singleton, default_segment_max_capacity, updated_by, updated_at)
		VALUES (TRUE, $1, $2, NOW())
		ON CONFLICT (singleton)
		DO UPDATE SET
			default_segment_max_capacity = EXCLUDED.default_segment_max_capacity,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING default_segment_max_capacity`

	var persisted float64
	err := r.db.QueryRowContext(ctx, q, maxCapacity, updatedBy).Scan(&persisted)
	if err != nil {
		return 0, fmt.Errorf("segment_repo.SetDefaultMaxCapacity: %w", err)
	}

	return persisted, nil
}

// InsertIfMissingTx inserts a segment using the provided default max capacity.
// If the segment already exists, the call is a no-op.
func (r *SegmentRepo) InsertIfMissingTx(ctx context.Context, tx *sql.Tx, segmentID, segmentName, region string, maxCapacity float64) error {
	const q = `
		INSERT INTO capacity.segments (
			segment_id, segment_name, region, max_capacity, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 1, NOW(), NOW())
		ON CONFLICT (segment_id) DO NOTHING`

	if _, err := tx.ExecContext(ctx, q, segmentID, segmentName, region, maxCapacity); err != nil {
		return fmt.Errorf("segment_repo.InsertIfMissingTx(%s): %w", segmentID, err)
	}

	return nil
}

// UpdateMaxCapacity updates an existing segment capacity and bumps its version.
// Returns sql.ErrNoRows when the target segment does not exist.
func (r *SegmentRepo) UpdateMaxCapacity(ctx context.Context, segmentID string, maxCapacity float64) error {
	const q = `
		UPDATE capacity.segments
		SET max_capacity = $2,
			version = version + 1,
			updated_at = NOW()
		WHERE segment_id = $1`

	res, err := r.db.ExecContext(ctx, q, segmentID, maxCapacity)
	if err != nil {
		return fmt.Errorf("segment_repo.UpdateMaxCapacity(%s): %w", segmentID, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("segment_repo.UpdateMaxCapacity(%s): rows_affected: %w", segmentID, err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}
