package model

import "time"

// SagaStatus is the lifecycle state of a cross-regional reservation saga.
type SagaStatus string

const (
	SagaStatusPending     SagaStatus = "PENDING"
	SagaStatusCommitted   SagaStatus = "COMMITTED"
	SagaStatusCompensated SagaStatus = "COMPENSATED"
	SagaStatusFailed      SagaStatus = "FAILED"
)

// SagaRegionStep records the outcome of one geo-region's reservation transaction
// within a multi-region saga.
type SagaRegionStep struct {
	Region        string `json:"region"`
	Status        string `json:"status"`         // "RESERVED" | "COMPENSATED"
	ReservationID string `json:"reservation_id"` // shared reservation_id for this region's rows
}

// ReservationSaga is one row in capacity.reservation_sagas.
// It tracks the overall lifecycle of a cross-regional booking attempt and
// the per-region steps needed for crash recovery and compensation.
type ReservationSaga struct {
	SagaID         string           `json:"saga_id"`
	JourneyID      string           `json:"journey_id"`
	IdempotencyKey string           `json:"idempotency_key"`
	Status         SagaStatus       `json:"status"`
	RegionSteps    []SagaRegionStep `json:"region_steps"`
	ReservationID  *string          `json:"reservation_id,omitempty"`
	ErrorReason    *string          `json:"error_reason,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}
