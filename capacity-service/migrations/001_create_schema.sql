-- Migration 001: Create capacity schema and tables

CREATE SCHEMA IF NOT EXISTS capacity;

-- Road segments: authoritative list of road segments with max capacity
CREATE TABLE IF NOT EXISTS capacity.segments (
    segment_id   VARCHAR(30)     PRIMARY KEY,
    segment_name VARCHAR(100)    NOT NULL,
    region       VARCHAR(20)     NOT NULL,
    max_capacity DECIMAL(10, 2)  NOT NULL,
    version      INTEGER         NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_max_capacity CHECK (max_capacity > 0)
);

-- Reservations: one row per segment per journey
CREATE TABLE IF NOT EXISTS capacity.reservations (
    id                BIGSERIAL       PRIMARY KEY,
    reservation_id    VARCHAR(30)     NOT NULL,
    journey_id        VARCHAR(20)     NOT NULL,
    segment_id        VARCHAR(30)     NOT NULL REFERENCES capacity.segments(segment_id),
    time_window_start TIMESTAMPTZ     NOT NULL,
    time_window_end   TIMESTAMPTZ     NOT NULL,
    vehicle_type      VARCHAR(20)     NOT NULL,
    slots_used        DECIMAL(10, 2)  NOT NULL,
    status            VARCHAR(10)     NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    released_at       TIMESTAMPTZ,
    CONSTRAINT chk_status CHECK (status IN ('active', 'released')),
    CONSTRAINT chk_vehicle_type CHECK (vehicle_type IN ('car', 'van', 'motorcycle', 'truck')),
    CONSTRAINT chk_slots_used CHECK (slots_used > 0),
    CONSTRAINT chk_time_window CHECK (time_window_end > time_window_start)
);

-- Idempotency cache: durable store for reserve responses (prevents double-booking on retries)
CREATE TABLE IF NOT EXISTS capacity.idempotency_cache (
    idempotency_key VARCHAR(64)  PRIMARY KEY,
    journey_id      VARCHAR(20)  NOT NULL,
    reservation_id  VARCHAR(30),
    response_status VARCHAR(10)  NOT NULL,
    response_body   JSONB        NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW() + INTERVAL '24 hours',
    CONSTRAINT chk_response_status CHECK (response_status IN ('reserved', 'failed'))
);

-- Indexes for reservation overlap queries (hot path)
CREATE INDEX IF NOT EXISTS idx_reservations_segment_window
    ON capacity.reservations (segment_id, time_window_start, time_window_end)
    WHERE status = 'active';

-- Index for releasing reservations by journey_id
CREATE INDEX IF NOT EXISTS idx_reservations_journey
    ON capacity.reservations (journey_id)
    WHERE status = 'active';

-- Index for orphan cleanup job
CREATE INDEX IF NOT EXISTS idx_reservations_orphan
    ON capacity.reservations (time_window_end)
    WHERE status = 'active';

-- Index for idempotency cache TTL cleanup
CREATE INDEX IF NOT EXISTS idx_idempotency_expires
    ON capacity.idempotency_cache (expires_at);
