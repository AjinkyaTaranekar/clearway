-- Migration 006: Saga state table for cross-regional capacity reservations.
--
-- When a journey spans multiple geo-regions (e.g. EU segments + US segments),
-- the capacity service runs one serialisable transaction per region instead of
-- one distributed transaction spanning all regions.  The saga table durably
-- tracks which regions have been reserved and drives compensation (rollback)
-- if any later region fails.
--
-- region_steps is a JSONB array of {region, status, reservation_id} objects,
-- one element per geo-region touched by this saga.

CREATE TABLE IF NOT EXISTS capacity.reservation_sagas (
    saga_id         VARCHAR(30)  PRIMARY KEY,
    journey_id      VARCHAR(20)  NOT NULL,
    idempotency_key VARCHAR(64)  NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    -- PENDING  → saga started, some regions may already be RESERVED
    -- COMMITTED → all regions reserved successfully
    -- COMPENSATED → partial failure; all reserved regions have been released
    -- FAILED → unrecoverable error (system failure after compensation)
    region_steps    JSONB        NOT NULL DEFAULT '[]'::JSONB,
    reservation_id  VARCHAR(30),          -- shared across all region sub-txs
    error_reason    TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_saga_status CHECK (
        status IN ('PENDING', 'COMMITTED', 'COMPENSATED', 'FAILED')
    )
);

CREATE INDEX IF NOT EXISTS idx_sagas_journey ON capacity.reservation_sagas (journey_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sagas_idem ON capacity.reservation_sagas (idempotency_key);

-- Only PENDING sagas need to be found during crash recovery.
CREATE INDEX IF NOT EXISTS idx_sagas_pending
    ON capacity.reservation_sagas (updated_at)
    WHERE status = 'PENDING';
