CREATE SCHEMA IF NOT EXISTS journey;

CREATE TABLE IF NOT EXISTS journey.journeys (
    journey_id        VARCHAR(20) PRIMARY KEY,
    driver_id         VARCHAR(50) NOT NULL,
    idempotency_key   VARCHAR(64) UNIQUE,

    origin_lat        DECIMAL(9,6) NOT NULL,
    origin_lng        DECIMAL(9,6) NOT NULL,
    dest_lat          DECIMAL(9,6) NOT NULL,
    dest_lng          DECIMAL(9,6) NOT NULL,

    departure_time    TIMESTAMPTZ NOT NULL,
    estimated_arrival TIMESTAMPTZ NOT NULL,
    vehicle_type      VARCHAR(20) NOT NULL,

    status            VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    rejection_reason  TEXT,
    reservation_id    VARCHAR(30),

    version           INTEGER NOT NULL DEFAULT 1,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at      TIMESTAMPTZ,
    activated_at      TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    expired_at        TIMESTAMPTZ,

    CONSTRAINT chk_status CHECK (status IN ('PENDING','APPROVED','REJECTED','CANCELLED','ACTIVE','COMPLETED','EXPIRED')),
    CONSTRAINT chk_vehicle CHECK (vehicle_type IN ('car','van','truck','motorcycle'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_one_active_per_driver
    ON journey.journeys (driver_id)
    WHERE status IN ('APPROVED', 'ACTIVE');

CREATE INDEX IF NOT EXISTS idx_journeys_driver    ON journey.journeys (driver_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_journeys_status    ON journey.journeys (status);
CREATE INDEX IF NOT EXISTS idx_journeys_departure ON journey.journeys (departure_time)
    WHERE status = 'APPROVED';

CREATE TABLE IF NOT EXISTS journey.journey_segments (
    id                SERIAL PRIMARY KEY,
    journey_id        VARCHAR(20) NOT NULL REFERENCES journey.journeys(journey_id),
    segment_id        VARCHAR(30) NOT NULL,
    segment_name      VARCHAR(100) NOT NULL,
    sequence_order    INTEGER NOT NULL,
    time_window_start TIMESTAMPTZ NOT NULL,
    time_window_end   TIMESTAMPTZ NOT NULL,
    traversal_minutes INTEGER NOT NULL,
    region            VARCHAR(50) NOT NULL DEFAULT '',

    CONSTRAINT uq_journey_segment UNIQUE (journey_id, segment_id)
);

CREATE INDEX IF NOT EXISTS idx_segments_journey ON journey.journey_segments (journey_id);

CREATE TABLE IF NOT EXISTS journey.idempotency_cache (
    idempotency_key VARCHAR(64) PRIMARY KEY,
    journey_id      VARCHAR(20) NOT NULL,
    response_body   JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours'
);
