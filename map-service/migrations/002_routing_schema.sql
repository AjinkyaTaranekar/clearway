-- Place cache and route-segment persistence for Nominatim + OSRM.
-- Allows the backend to proxy geocoding/routing and cache repeated O/D pairs.

CREATE TABLE IF NOT EXISTS map.places (
    place_id     TEXT        PRIMARY KEY,          -- OSM/Nominatim place identifier
    name         TEXT        NOT NULL,
    address      TEXT        NOT NULL DEFAULT '',
    lat          DOUBLE PRECISION NOT NULL,
    lng          DOUBLE PRECISION NOT NULL,
    country_code TEXT        NOT NULL DEFAULT 'IE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Intercity / motorway segments derived from routed road references.
-- Segment IDs follow the pattern {COUNTRY_CODE}-{ROAD_REF}, e.g. "IE-M50".
-- These are SEPARATE from the city-level graph segments in migration 001.
CREATE TABLE IF NOT EXISTS map.intercity_segments (
    segment_id   TEXT        PRIMARY KEY,          -- e.g. "IE-M7"
    segment_name TEXT        NOT NULL,
    road_ref     TEXT        NOT NULL,             -- e.g. "M7"
    country_code TEXT        NOT NULL DEFAULT 'IE',
    from_lat     DOUBLE PRECISION NOT NULL,
    from_lng     DOUBLE PRECISION NOT NULL,
    to_lat       DOUBLE PRECISION NOT NULL,
    to_lng       DOUBLE PRECISION NOT NULL,
    capacity     INT         NOT NULL DEFAULT 1800, -- vehicles/hour
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Cached routing results keyed by origin/destination place pair.
CREATE TABLE IF NOT EXISTS map.routes (
    route_id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    origin_place_id  TEXT        NOT NULL REFERENCES map.places(place_id),
    dest_place_id    TEXT        NOT NULL REFERENCES map.places(place_id),
    distance_m       INT         NOT NULL DEFAULT 0,
    duration_s       INT         NOT NULL DEFAULT 0,
    last_used_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (origin_place_id, dest_place_id)
);

-- Ordered list of intercity segments that make up a cached route.
CREATE TABLE IF NOT EXISTS map.route_segments (
    route_id      UUID        NOT NULL REFERENCES map.routes(route_id) ON DELETE CASCADE,
    sequence      INT         NOT NULL,
    segment_id    TEXT        NOT NULL,             -- may reference map.segments OR map.intercity_segments
    PRIMARY KEY (route_id, sequence)
);

-- ── Pre-seeded Irish places ─────────────────────────────────────────────────
-- These cover the main intercity O/D pairs used in demos / E2E tests.
INSERT INTO map.places (place_id, name, address, lat, lng, country_code) VALUES
    ('ie-dublin',    'Dublin',    'Dublin, Ireland',    53.3498, -6.2603, 'IE'),
    ('ie-cork',      'Cork',      'Cork, Ireland',      51.8985, -8.4756, 'IE'),
    ('ie-galway',    'Galway',    'Galway, Ireland',    53.2707, -9.0568, 'IE'),
    ('ie-limerick',  'Limerick',  'Limerick, Ireland',  52.6638, -8.6267, 'IE'),
    ('ie-waterford', 'Waterford', 'Waterford, Ireland', 52.2593, -7.1101, 'IE'),
    ('ie-belfast',   'Belfast',   'Belfast, N. Ireland',54.5973, -5.9301, 'GB'),
    ('ie-kilkenny',  'Kilkenny',  'Kilkenny, Ireland',  52.6541, -7.2448, 'IE'),
    ('ie-drogheda',  'Drogheda',  'Drogheda, Ireland',  53.7179, -6.3561, 'IE'),
    ('ie-sligo',     'Sligo',     'Sligo, Ireland',     54.2697, -8.4694, 'IE'),
    ('ie-athlone',   'Athlone',   'Athlone, Ireland',   53.4239, -7.9408, 'IE')
ON CONFLICT (place_id) DO UPDATE SET
    name         = EXCLUDED.name,
    address      = EXCLUDED.address,
    lat          = EXCLUDED.lat,
    lng          = EXCLUDED.lng,
    country_code = EXCLUDED.country_code;

-- ── Pre-seeded intercity segments (Irish motorway / national road network) ──
INSERT INTO map.intercity_segments
    (segment_id, segment_name, road_ref, country_code, from_lat, from_lng, to_lat, to_lng, capacity)
VALUES
    ('IE-M1',  'M1 Dublin–Belfast',        'M1',  'IE', 53.3498, -6.2603, 54.5973, -5.9301, 2000),
    ('IE-M7',  'M7 Dublin–Limerick',        'M7',  'IE', 53.3498, -6.2603, 52.6638, -8.6267, 1800),
    ('IE-M8',  'M8 Dublin–Cork',            'M8',  'IE', 53.3498, -6.2603, 51.8985, -8.4756, 1800),
    ('IE-M6',  'M6 Dublin–Galway',          'M6',  'IE', 53.3498, -6.2603, 53.2707, -9.0568, 1800),
    ('IE-M9',  'M9 Dublin–Waterford',       'M9',  'IE', 53.3498, -6.2603, 52.2593, -7.1101, 1600),
    ('IE-M11', 'M11 Dublin–Wexford',        'M11', 'IE', 53.3498, -6.2603, 52.3369, -6.4633, 1600),
    ('IE-N4',  'N4 Dublin–Sligo',           'N4',  'IE', 53.3498, -6.2603, 54.2697, -8.4694, 1400),
    ('IE-N6',  'N6 Athlone–Galway',         'N6',  'IE', 53.4239, -7.9408, 53.2707, -9.0568, 1200),
    ('IE-N7',  'N7 Limerick–Cork',          'N7',  'IE', 52.6638, -8.6267, 51.8985, -8.4756, 1400),
    ('IE-N25', 'N25 Cork–Waterford',        'N25', 'IE', 51.8985, -8.4756, 52.2593, -7.1101, 1200)
ON CONFLICT (segment_id) DO UPDATE SET
    segment_name = EXCLUDED.segment_name,
    road_ref     = EXCLUDED.road_ref,
    from_lat     = EXCLUDED.from_lat,
    from_lng     = EXCLUDED.from_lng,
    to_lat       = EXCLUDED.to_lat,
    to_lng       = EXCLUDED.to_lng,
    capacity     = EXCLUDED.capacity;
