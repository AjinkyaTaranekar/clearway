-- Migration 002: Seed Dublin road segments

INSERT INTO capacity.segments (segment_id, segment_name, region, max_capacity) VALUES
    ('seg_m50',     'M50 Motorway (North-South)',          'central', 120.0),
    ('seg_m1_n',    'M1 Motorway Northbound',              'north',   100.0),
    ('seg_m1_s',    'M1 Motorway Southbound',              'north',   100.0),
    ('seg_n11',     'N11 Stillorgan Road',                 'south',    80.0),
    ('seg_m50_s',   'M50 South (Sandyford Junction)',      'south',   110.0),
    ('seg_m7n',     'M7 Naas Road Northbound',             'west',     90.0),
    ('seg_m7s',     'M7 Naas Road Southbound',             'west',     90.0),
    ('seg_m8',      'M8 Cork Road',                        'south',    85.0),
    ('seg_n4',      'N4 Galway Road',                      'west',     75.0),
    ('seg_n3',      'N3 Navan Road',                       'north',    70.0),
    ('seg_m2',      'M2 Motorway',                         'north',    80.0),
    ('seg_n81',     'N81 Tallaght Road',                   'south',    60.0),
    ('seg_r132',    'R132 Swords Road',                    'north',    50.0),
    ('seg_port_n',  'Port Tunnel Northbound',              'east',     65.0),
    ('seg_port_s',  'Port Tunnel Southbound',              'east',     65.0),
    ('seg_n7',      'N7 Naas Dual Carriageway',            'west',     95.0),
    ('seg_m4',      'M4 Westlink Motorway',                'west',     85.0),
    ('seg_n2',      'N2 Finglas Road',                     'north',    55.0),
    ('seg_quays_e', 'Dublin Quays Eastbound',              'central',  45.0),
    ('seg_quays_w', 'Dublin Quays Westbound',              'central',  45.0)
ON CONFLICT (segment_id) DO NOTHING;
