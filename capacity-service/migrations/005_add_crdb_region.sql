-- Migration 005: Add crdb_region column to segments and reservations.
-- crdb_region drives geo-partitioning (eu / us / apac) and is the key
-- used by the Saga coordinator to route each region's transaction to its
-- local CockroachDB leaseholder.
--
-- All current segments are Irish, so they default to 'eu'.
-- Future US or APAC segments will carry 'us' or 'apac' at INSERT time.

ALTER TABLE capacity.segments
    ADD COLUMN IF NOT EXISTS crdb_region VARCHAR(10) NOT NULL DEFAULT 'eu';

ALTER TABLE capacity.reservations
    ADD COLUMN IF NOT EXISTS crdb_region VARCHAR(10) NOT NULL DEFAULT 'eu';

-- No explicit UPDATE needed: ADD COLUMN ... DEFAULT 'eu' backfills existing rows.
-- Mixing DDL + DML in the same CockroachDB transaction causes "column does not exist".

-- Index used by the Saga compensation query (release by reservation_id)
CREATE INDEX IF NOT EXISTS idx_reservations_reservation_id
    ON capacity.reservations (reservation_id)
    WHERE status = 'active';
