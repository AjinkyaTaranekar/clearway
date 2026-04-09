package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// ClosureRepo manages segment closure persistence.
type ClosureRepo struct {
	master *sql.DB
	slave  *sql.DB
}

// NewClosureRepo creates a new ClosureRepo.
func NewClosureRepo(master, slave *sql.DB) *ClosureRepo {
	return &ClosureRepo{master: master, slave: slave}
}

// ListAll returns all closures ordered by active-first then newest-first, with segment_name joined.
func (r *ClosureRepo) ListAll(ctx context.Context) ([]model.Closure, error) {
	const q = `
		SELECT c.closure_id, c.segment_id, s.segment_name, c.reason,
		       c.starts_at, c.ends_at, c.is_active, c.created_at, c.created_by
		FROM capacity.closures c
		JOIN capacity.segments s ON s.segment_id = c.segment_id
		ORDER BY c.is_active DESC, c.created_at DESC`

	rows, err := r.slave.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var closures []model.Closure
	for rows.Next() {
		var c model.Closure
		if err := rows.Scan(
			&c.ClosureID, &c.SegmentID, &c.SegmentName, &c.Reason,
			&c.StartsAt, &c.EndsAt, &c.IsActive, &c.CreatedAt, &c.CreatedBy,
		); err != nil {
			return nil, err
		}
		closures = append(closures, c)
	}
	return closures, rows.Err()
}

// Insert creates a new closure row.
func (r *ClosureRepo) Insert(ctx context.Context, c *model.Closure) error {
	const q = `
		INSERT INTO capacity.closures (closure_id, segment_id, reason, starts_at, ends_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.master.ExecContext(ctx, q,
		c.ClosureID, c.SegmentID, c.Reason, c.StartsAt, c.EndsAt, c.CreatedBy,
	)
	return err
}

// Deactivate marks a closure as resolved (is_active = false).
func (r *ClosureRepo) Deactivate(ctx context.Context, closureID string) error {
	const q = `UPDATE capacity.closures SET is_active = FALSE WHERE closure_id = $1`
	_, err := r.master.ExecContext(ctx, q, closureID)
	return err
}

// IsSegmentClosed returns true if the segment has an active closure overlapping the given time window.
// A closure with NULL ends_at is treated as indefinite.
func (r *ClosureRepo) IsSegmentClosed(ctx context.Context, segmentID string, windowStart, windowEnd time.Time) (bool, error) {
	const q = `
		SELECT COUNT(*) FROM capacity.closures
		WHERE segment_id = $1
		  AND is_active = TRUE
		  AND starts_at < $3
		  AND (ends_at IS NULL OR ends_at > $2)`

	var count int
	err := r.slave.QueryRowContext(ctx, q, segmentID, windowStart, windowEnd).Scan(&count)
	return count > 0, err
}
