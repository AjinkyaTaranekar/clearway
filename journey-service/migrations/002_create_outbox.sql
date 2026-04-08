-- Transactional outbox table for Journey Service.
-- Events are written here in the same DB transaction as the state change,
-- then relayed to Redis Streams by the OutboxRelay goroutine.
-- This guarantees at-least-once delivery even if the process crashes between
-- the DB commit and the Redis XAdd. Fixes F-03 (fire-and-forget event loss).

CREATE TABLE IF NOT EXISTS journey.outbox (
    id           BIGSERIAL    PRIMARY KEY,
    event_id     VARCHAR(64)  NOT NULL UNIQUE,
    event_type   VARCHAR(50)  NOT NULL,
    payload      JSONB        NOT NULL,
    published    BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

-- Partial index: only un-published rows need to be scanned by the relay.
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished
    ON journey.outbox (created_at)
    WHERE published = FALSE;
