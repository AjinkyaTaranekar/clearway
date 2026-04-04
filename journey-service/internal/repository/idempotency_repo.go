package repository

import (
	"context"
	"database/sql"
	"time"

	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
)

// IdempotencyRecord stores a cached API response for replay
type IdempotencyRecord struct {
	IdempotencyKey string
	JourneyID      string
	ResponseBody   []byte
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

// IdempotencyRepository handles idempotency cache DB operations
type IdempotencyRepository struct {
	db *sql.DB
}

// NewIdempotencyRepository creates a new idempotency repository
func NewIdempotencyRepository(db *sql.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// Get retrieves a cached response by idempotency key (nil if not found or expired)
func (r *IdempotencyRepository) Get(ctx context.Context, key string) (*IdempotencyRecord, error) {
	rec := &IdempotencyRecord{}
	err := r.db.QueryRowContext(ctx, `
		SELECT idempotency_key, journey_id, response_body, created_at, expires_at
		FROM journey.idempotency_cache
		WHERE idempotency_key = $1 AND expires_at > NOW()`, key).
		Scan(&rec.IdempotencyKey, &rec.JourneyID, &rec.ResponseBody, &rec.CreatedAt, &rec.ExpiresAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.DatabaseError("failed to get idempotency record", err)
	}
	return rec, nil
}

// Save stores a response body for future idempotent replay
func (r *IdempotencyRepository) Save(ctx context.Context, key, journeyID string, body []byte) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO journey.idempotency_cache (idempotency_key, journey_id, response_body)
		VALUES ($1, $2, $3)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		key, journeyID, body)
	if err != nil {
		return apperrors.DatabaseError("failed to save idempotency record", err)
	}
	return nil
}

// Cleanup deletes expired idempotency records
func (r *IdempotencyRepository) Cleanup(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM journey.idempotency_cache WHERE expires_at < NOW()")
	if err != nil {
		return apperrors.DatabaseError("failed to cleanup idempotency cache", err)
	}
	return nil
}
