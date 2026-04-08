-- Migration 003: persist traversal time per cached route segment

ALTER TABLE map.route_segments
    ADD COLUMN IF NOT EXISTS traversal_time_minutes INT NOT NULL DEFAULT 1;
