package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// SagaRepo persists cross-regional reservation saga state.
// All writes go to the master pool; the saga table is small and read
// only during idempotency checks and crash recovery.
type SagaRepo struct {
	db *sql.DB
}

// NewSagaRepo creates a new SagaRepo backed by the given master pool.
func NewSagaRepo(db *sql.DB) *SagaRepo {
	return &SagaRepo{db: db}
}

// Create inserts a new saga record in PENDING state with no region steps yet.
func (r *SagaRepo) Create(ctx context.Context, saga *model.ReservationSaga) error {
	stepsJSON, err := json.Marshal(saga.RegionSteps)
	if err != nil {
		return fmt.Errorf("saga_repo.Create marshal: %w", err)
	}
	const q = `
		INSERT INTO capacity.reservation_sagas
			(saga_id, journey_id, idempotency_key, status, region_steps, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`
	if _, err := r.db.ExecContext(ctx, q,
		saga.SagaID, saga.JourneyID, saga.IdempotencyKey, string(saga.Status), stepsJSON,
	); err != nil {
		return fmt.Errorf("saga_repo.Create: %w", err)
	}
	return nil
}

// UpdateStepsAndStatus atomically persists the current region_steps array
// alongside the new saga status, reservation_id, and optional error_reason.
// Called after each regional sub-transaction (step added) and at final
// COMMITTED / COMPENSATED transitions.
func (r *SagaRepo) UpdateStepsAndStatus(
	ctx context.Context,
	sagaID string,
	steps []model.SagaRegionStep,
	status model.SagaStatus,
	reservationID *string,
	errorReason *string,
) error {
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("saga_repo.UpdateStepsAndStatus marshal: %w", err)
	}
	const q = `
		UPDATE capacity.reservation_sagas
		SET    region_steps   = $2,
		       status         = $3,
		       reservation_id = $4,
		       error_reason   = $5,
		       updated_at     = NOW()
		WHERE  saga_id = $1`
	if _, err := r.db.ExecContext(ctx, q,
		sagaID, stepsJSON, string(status), reservationID, errorReason,
	); err != nil {
		return fmt.Errorf("saga_repo.UpdateStepsAndStatus: %w", err)
	}
	return nil
}

// GetByIdempotencyKey returns the saga record for the given idempotency key
// (only within the 24-hour TTL window), or nil if not found.
func (r *SagaRepo) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*model.ReservationSaga, error) {
	const q = `
		SELECT saga_id, journey_id, idempotency_key, status,
		       region_steps, reservation_id, error_reason, created_at, updated_at
		FROM   capacity.reservation_sagas
		WHERE  idempotency_key = $1
		  AND  created_at > NOW() - INTERVAL '24 hours'`
	return scanSaga(r.db.QueryRowContext(ctx, q, idempotencyKey))
}

func scanSaga(row *sql.Row) (*model.ReservationSaga, error) {
	var s model.ReservationSaga
	var stepsJSON []byte
	var statusStr string

	err := row.Scan(
		&s.SagaID, &s.JourneyID, &s.IdempotencyKey, &statusStr,
		&stepsJSON, &s.ReservationID, &s.ErrorReason,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("saga_repo scan: %w", err)
	}
	s.Status = model.SagaStatus(statusStr)
	if err := json.Unmarshal(stepsJSON, &s.RegionSteps); err != nil {
		return nil, fmt.Errorf("saga_repo unmarshal steps: %w", err)
	}
	return &s, nil
}
