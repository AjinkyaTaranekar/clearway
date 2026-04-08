ALTER TABLE auth.users
    ADD COLUMN IF NOT EXISTS is_emergency_vehicle BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS auth.user_secondary_vehicles (
    id                   VARCHAR(40) PRIMARY KEY,
    user_id              VARCHAR(40) NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    vehicle_type         VARCHAR(15) NOT NULL,
    license_info         JSONB       NOT NULL DEFAULT '{}',
    is_emergency_vehicle BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_secondary_vehicle_type CHECK (vehicle_type IN ('car', 'van', 'motorcycle', 'truck'))
);

CREATE INDEX IF NOT EXISTS idx_user_secondary_vehicles_user_id
    ON auth.user_secondary_vehicles (user_id);
