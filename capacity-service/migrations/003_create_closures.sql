-- Migration 003: Create segment closures table

CREATE TABLE IF NOT EXISTS capacity.closures (
    closure_id   VARCHAR(30)     PRIMARY KEY,
    segment_id   VARCHAR(30)     NOT NULL REFERENCES capacity.segments(segment_id),
    reason       TEXT            NOT NULL,
    starts_at    TIMESTAMPTZ     NOT NULL,
    ends_at      TIMESTAMPTZ,
    is_active    BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(100)    NOT NULL DEFAULT 'admin'
);

-- Index for active closure overlap queries (hot path in Reserve())
CREATE INDEX IF NOT EXISTS idx_closures_segment_active
    ON capacity.closures (segment_id, starts_at, ends_at)
    WHERE is_active = TRUE;
