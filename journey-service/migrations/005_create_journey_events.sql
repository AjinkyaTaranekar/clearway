-- Durable journey lifecycle/audit events exposed to driver/admin timelines.
-- Records who performed each step, human-readable notes, and structured metadata.

CREATE TABLE IF NOT EXISTS journey.journey_events (
    id         BIGSERIAL    PRIMARY KEY,
    event_id   VARCHAR(64)  NOT NULL UNIQUE,
    journey_id VARCHAR(20)  NOT NULL REFERENCES journey.journeys(journey_id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    actor_type VARCHAR(20)  NOT NULL,
    actor_id   VARCHAR(50),
    note       TEXT,
    metadata   JSONB        NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_journey_events_journey_created
    ON journey.journey_events (journey_id, created_at, id);
