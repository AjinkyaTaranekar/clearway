ALTER TABLE journey.journeys
    ADD COLUMN IF NOT EXISTS origin_place_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS dest_place_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS total_distance_km DECIMAL(10,2),
    ADD COLUMN IF NOT EXISTS total_duration_minutes INTEGER;

CREATE INDEX IF NOT EXISTS idx_journeys_origin_place ON journey.journeys (origin_place_id);
CREATE INDEX IF NOT EXISTS idx_journeys_dest_place ON journey.journeys (dest_place_id);

ALTER TABLE journey.journey_segments
    ADD COLUMN IF NOT EXISTS from_lat DECIMAL(9,6),
    ADD COLUMN IF NOT EXISTS from_lng DECIMAL(9,6),
    ADD COLUMN IF NOT EXISTS to_lat DECIMAL(9,6),
    ADD COLUMN IF NOT EXISTS to_lng DECIMAL(9,6);
