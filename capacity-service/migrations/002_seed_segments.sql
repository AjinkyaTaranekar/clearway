-- Migration 002: Seed road segments matching the map service graph.
-- Segment IDs align exactly with the edge IDs in
-- map-service/internal/http/handlers/map_handler.go so that capacity
-- reservations created by the journey service can be looked up by the
-- same segment_id the map service returns in its route response.
-- Each segment represents a physical road section; because the map service
-- models roads as bidirectional (same segment_id for both directions), a
-- single capacity row covers vehicles travelling either way.

INSERT INTO capacity.segments (segment_id, segment_name, region, max_capacity) VALUES
    ('seg_city_north',      'City Centre to North Gate',       'central',  80.0),
    ('seg_north_airport',   'North Gate to Airport',           'north',   100.0),
    ('seg_city_east',       'City Centre to East Quay',        'central',  90.0),
    ('seg_east_airport',    'East Quay to Airport',            'east',     85.0),
    ('seg_city_riverside',  'City Centre to Riverside',        'central', 120.0),
    ('seg_riverside_south', 'Riverside to South Terminal',     'south',    75.0),
    ('seg_south_industrial','South Terminal to Industrial Park','south',   70.0),
    ('seg_industrial_east', 'Industrial Park to East Quay',    'east',     65.0),
    ('seg_city_west',       'City Centre to West Depot',       'west',     95.0),
    ('seg_west_port',       'West Depot to Port Terminal',     'west',     80.0),
    ('seg_port_south',      'Port Terminal to South Terminal', 'south',    75.0),
    ('seg_west_northfield', 'West Depot to Northfield',        'west',     60.0),
    ('seg_northfield_north','Northfield to North Gate',        'north',    55.0)
ON CONFLICT (segment_id) DO NOTHING;
