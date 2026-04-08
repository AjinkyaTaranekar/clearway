-- Migration 003: Segment closures for admin-managed temporary road blocks

CREATE TABLE IF NOT EXISTS capacity.segment_closures (
    id          BIGSERIAL      PRIMARY KEY,
    closure_id  VARCHAR(30)    NOT NULL UNIQUE,
    segment_id  VARCHAR(30)    NOT NULL REFERENCES capacity.segments(segment_id),
    start_time  TIMESTAMPTZ    NOT NULL,
    end_time    TIMESTAMPTZ    NOT NULL,
    reason      TEXT           NOT NULL,
    admin_id    VARCHAR(50)    NOT NULL,
    status      VARCHAR(20)    NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_segment_closure_window CHECK (end_time > start_time),
    CONSTRAINT chk_segment_closure_status CHECK (status IN ('active', 'cancelled', 'completed'))
);

CREATE INDEX IF NOT EXISTS idx_segment_closures_active_overlap
    ON capacity.segment_closures (segment_id, start_time, end_time)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_segment_closures_status_end
    ON capacity.segment_closures (status, end_time);
