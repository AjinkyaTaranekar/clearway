ALTER TABLE map.routes
    ADD COLUMN IF NOT EXISTS path_geojson JSONB;
