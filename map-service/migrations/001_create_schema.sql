CREATE SCHEMA IF NOT EXISTS map;

CREATE TABLE IF NOT EXISTS map.nodes (
    node_id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    lat DOUBLE PRECISION NOT NULL,
    lng DOUBLE PRECISION NOT NULL,
    sort_order INT NOT NULL
);

CREATE TABLE IF NOT EXISTS map.segments (
    segment_id TEXT PRIMARY KEY,
    segment_name TEXT NOT NULL,
    region TEXT NOT NULL,
    from_node_id TEXT NOT NULL REFERENCES map.nodes(node_id),
    to_node_id TEXT NOT NULL REFERENCES map.nodes(node_id),
    traversal_time_minutes INT NOT NULL,
    sort_order INT NOT NULL
);

INSERT INTO map.nodes (node_id, label, lat, lng, sort_order) VALUES
    ('city', 'City Centre', 53.3498, -6.2603, 1),
    ('north', 'North Gate', 53.4200, -6.2603, 2),
    ('airport', 'Airport', 53.4264, -6.2499, 3),
    ('east', 'East Quay', 53.3498, -6.2100, 4),
    ('south', 'South Terminal', 53.3100, -6.2603, 5),
    ('industrial', 'Industrial Park', 53.3150, -6.2100, 6),
    ('west', 'West Depot', 53.3498, -6.3200, 7),
    ('port', 'Port Terminal', 53.3100, -6.3200, 8),
    ('northfield', 'Northfield', 53.4000, -6.3000, 9),
    ('riverside', 'Riverside', 53.3350, -6.2700, 10)
ON CONFLICT (node_id) DO UPDATE SET
    label = EXCLUDED.label,
    lat = EXCLUDED.lat,
    lng = EXCLUDED.lng,
    sort_order = EXCLUDED.sort_order;

INSERT INTO map.segments (
    segment_id,
    segment_name,
    region,
    from_node_id,
    to_node_id,
    traversal_time_minutes,
    sort_order
) VALUES
    ('seg_city_north', 'City Centre - North Gate', 'central', 'city', 'north', 8, 1),
    ('seg_north_airport', 'North Gate - Airport', 'north', 'north', 'airport', 16, 2),
    ('seg_city_east', 'City Centre - East Quay', 'central', 'city', 'east', 6, 3),
    ('seg_east_airport', 'East Quay - Airport', 'east', 'east', 'airport', 20, 4),
    ('seg_city_riverside', 'City Centre - Riverside', 'central', 'city', 'riverside', 5, 5),
    ('seg_riverside_south', 'Riverside - South Terminal', 'south', 'riverside', 'south', 7, 6),
    ('seg_south_industrial', 'South Terminal - Industrial Park', 'south', 'south', 'industrial', 6, 7),
    ('seg_industrial_east', 'Industrial Park - East Quay', 'east', 'industrial', 'east', 6, 8),
    ('seg_city_west', 'City Centre - West Depot', 'west', 'city', 'west', 9, 9),
    ('seg_west_port', 'West Depot - Port Terminal', 'west', 'west', 'port', 8, 10),
    ('seg_port_south', 'Port Terminal - South Terminal', 'south', 'port', 'south', 7, 11),
    ('seg_west_northfield', 'West Depot - Northfield', 'north', 'west', 'northfield', 9, 12),
    ('seg_northfield_north', 'Northfield - North Gate', 'north', 'northfield', 'north', 7, 13)
ON CONFLICT (segment_id) DO UPDATE SET
    segment_name = EXCLUDED.segment_name,
    region = EXCLUDED.region,
    from_node_id = EXCLUDED.from_node_id,
    to_node_id = EXCLUDED.to_node_id,
    traversal_time_minutes = EXCLUDED.traversal_time_minutes,
    sort_order = EXCLUDED.sort_order;
