package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
) // TODO: check import

// IdempotencyRepo handles database operations for the idempotency cache.
type IdempotencyRepo struct {
	db *sql.DB
}

// NewIdempotencyRepo creates a new IdempotencyRepo.
func NewIdempotencyRepo(db *sql.DB) *IdempotencyRepo {
	return &IdempotencyRepo{db: db}
}

// GetByKey looks up an existing idempotency cache entry.
// Returns nil, nil when no entry exists.
func (r *IdempotencyRepo) GetByKey(ctx context.Context, key string) (*model.IdempotencyCache, error) {
	const q = `
		SELECT idempotency_key, journey_id, reservation_id,
		       response_status, response_body, created_at, expires_at
		FROM capacity.idempotency_cache
		WHERE idempotency_key = $1 AND expires_at > NOW()`

	var entry model.IdempotencyCache
	err := r.db.QueryRowContext(ctx, q, key).Scan(
		&entry.IdempotencyKey,
		&entry.JourneyID,
		&entry.ReservationID,
		&entry.ResponseStatus,
		&entry.ResponseBody,
		&entry.CreatedAt,
		&entry.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("idempotency_repo.GetByKey: %w", err)
	}
	return &entry, nil
}

// InsertInTx inserts a new idempotency cache entry within an existing transaction.
// Returns the underlying error unchanged so the caller can detect unique violations.
func (r *IdempotencyRepo) InsertInTx(
	ctx context.Context,
	tx *sql.Tx,
	key, journeyID string,
	reservationID *string,
	responseStatus string,
	responseBody []byte,
) error {
	const q = `
		INSERT INTO capacity.idempotency_cache
			(idempotency_key, journey_id, reservation_id, response_status, response_body, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, NOW(), NOW() + INTERVAL '24 hours')`

	_, err := tx.ExecContext(ctx, q, key, journeyID, reservationID, responseStatus, string(responseBody))
	return err
}

// InsertOnConflictIgnore inserts a cache entry outside any transaction.
// Silently ignores unique constraint violations (the entry already exists from
// a concurrent request or a replicated write from another VM).
func (r *IdempotencyRepo) InsertOnConflictIgnore(
	ctx context.Context,
	key, journeyID string,
	reservationID *string,
	responseStatus string,
	responseBody []byte,
) error {
	const q = `
		INSERT INTO capacity.idempotency_cache
			(idempotency_key, journey_id, reservation_id, response_status, response_body, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, NOW(), NOW() + INTERVAL '24 hours')
		ON CONFLICT (idempotency_key) DO NOTHING`

	_, err := r.db.ExecContext(ctx, q, key, journeyID, reservationID, responseStatus, string(responseBody))
	if err != nil {
		return fmt.Errorf("idempotency_repo.InsertOnConflictIgnore: %w", err)
	}
	return nil
}

// DeleteExpired removes entries whose TTL has passed (called by the cleanup job).
func (r *IdempotencyRepo) DeleteExpired(ctx context.Context) (int64, error) {
	const q = `DELETE FROM capacity.idempotency_cache WHERE expires_at < NOW()`
	res, err := r.db.ExecContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("idempotency_repo.DeleteExpired: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
