package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// ClosureRepo handles persistence for segment closures.
type ClosureRepo struct {
	db *sql.DB
}

// NewClosureRepo creates a new ClosureRepo.
func NewClosureRepo(db *sql.DB) *ClosureRepo {
	return &ClosureRepo{db: db}
}

// FindActiveOverlapTx returns an active closure overlapping the provided window.
// Returns nil, nil when no overlap exists.
func (r *ClosureRepo) FindActiveOverlapTx(
	ctx context.Context,
	tx *sql.Tx,
	segmentID string,
	windowStart, windowEnd time.Time,
) (*model.SegmentClosure, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ClosureRepo.FindActiveOverlapTx").
		Str("segment_id", segmentID).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("checking active closure overlap in transaction")

	const q = `
		SELECT id, closure_id, segment_id, start_time, end_time, reason, admin_id, status, created_at, updated_at
		FROM capacity.segment_closures
		WHERE segment_id = $1
		  AND status = 'active'
		  AND start_time < $3
		  AND end_time > $2
		ORDER BY start_time ASC
		LIMIT 1`

	var closure model.SegmentClosure
	err := tx.QueryRowContext(ctx, q, segmentID, windowStart, windowEnd).Scan(
		&closure.ID,
		&closure.ClosureID,
		&closure.SegmentID,
		&closure.StartTime,
		&closure.EndTime,
		&closure.Reason,
		&closure.AdminID,
		&closure.Status,
		&closure.CreatedAt,
		&closure.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("closure_repo.FindActiveOverlapTx(%s): %w", segmentID, err)
	}
	return &closure, nil
}

// FindActiveOverlap returns an active closure overlapping the provided window.
// Returns nil, nil when no overlap exists.
func (r *ClosureRepo) FindActiveOverlap(
	ctx context.Context,
	segmentID string,
	windowStart, windowEnd time.Time,
) (*model.SegmentClosure, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ClosureRepo.FindActiveOverlap").
		Str("segment_id", segmentID).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("checking active closure overlap")

	const q = `
		SELECT id, closure_id, segment_id, start_time, end_time, reason, admin_id, status, created_at, updated_at
		FROM capacity.segment_closures
		WHERE segment_id = $1
		  AND status = 'active'
		  AND start_time < $3
		  AND end_time > $2
		ORDER BY start_time ASC
		LIMIT 1`

	var closure model.SegmentClosure
	err := r.db.QueryRowContext(ctx, q, segmentID, windowStart, windowEnd).Scan(
		&closure.ID,
		&closure.ClosureID,
		&closure.SegmentID,
		&closure.StartTime,
		&closure.EndTime,
		&closure.Reason,
		&closure.AdminID,
		&closure.Status,
		&closure.CreatedAt,
		&closure.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("closure_repo.FindActiveOverlap(%s): %w", segmentID, err)
	}
	return &closure, nil
}

// InsertTx inserts a new closure in an existing transaction.
func (r *ClosureRepo) InsertTx(ctx context.Context, tx *sql.Tx, closure *model.SegmentClosure) error {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ClosureRepo.InsertTx").
		Str("closure_id", closure.ClosureID).
		Str("segment_id", closure.SegmentID).
		Msg("inserting segment closure")

	const q = `
		INSERT INTO capacity.segment_closures
			(closure_id, segment_id, start_time, end_time, reason, admin_id, status, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		RETURNING id, created_at, updated_at`

	if err := tx.QueryRowContext(
		ctx,
		q,
		closure.ClosureID,
		closure.SegmentID,
		closure.StartTime,
		closure.EndTime,
		closure.Reason,
		closure.AdminID,
		closure.Status,
	).Scan(&closure.ID, &closure.CreatedAt, &closure.UpdatedAt); err != nil {
		return fmt.Errorf("closure_repo.InsertTx(%s): %w", closure.ClosureID, err)
	}

	return nil
}

// ListActive returns all active closures that have not ended yet.
func (r *ClosureRepo) ListActive(ctx context.Context) ([]model.SegmentClosure, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "ClosureRepo.ListActive").
		Msg("listing active segment closures")

	const q = `
		SELECT id, closure_id, segment_id, start_time, end_time, reason, admin_id, status, created_at, updated_at
		FROM capacity.segment_closures
		WHERE status = 'active'
		  AND end_time > NOW()
		ORDER BY start_time ASC`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("closure_repo.ListActive: %w", err)
	}
	defer rows.Close()

	closures := make([]model.SegmentClosure, 0)
	for rows.Next() {
		var closure model.SegmentClosure
		if err := rows.Scan(
			&closure.ID,
			&closure.ClosureID,
			&closure.SegmentID,
			&closure.StartTime,
			&closure.EndTime,
			&closure.Reason,
			&closure.AdminID,
			&closure.Status,
			&closure.CreatedAt,
			&closure.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("closure_repo.ListActive scan: %w", err)
		}
		closures = append(closures, closure)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return closures, nil
}
