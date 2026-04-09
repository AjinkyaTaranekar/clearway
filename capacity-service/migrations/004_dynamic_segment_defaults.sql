-- Migration 004: Dynamic segment defaults + intercity support

CREATE TABLE IF NOT EXISTS capacity.settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE,
    default_segment_max_capacity DECIMAL(10, 2) NOT NULL,
    updated_by VARCHAR(64) NOT NULL DEFAULT 'system',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_default_segment_max_capacity CHECK (default_segment_max_capacity > 0)
);

INSERT INTO capacity.settings (singleton, default_segment_max_capacity, updated_by)
VALUES (TRUE, 100.00, 'system')
ON CONFLICT (singleton) DO NOTHING;

DO $$
BEGIN
    ALTER TABLE capacity.segments DROP CONSTRAINT IF EXISTS chk_region;
    ALTER TABLE capacity.segments ADD CONSTRAINT chk_region
        CHECK (region IN ('north', 'south', 'east', 'west', 'central', 'intercity'));
END $$;
