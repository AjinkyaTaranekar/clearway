CREATE SCHEMA IF NOT EXISTS auth;

CREATE TABLE IF NOT EXISTS auth.users (
    id              VARCHAR(40)  PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    email           VARCHAR(254) NOT NULL,
    email_lower     VARCHAR(254) NOT NULL,
    password_hash   VARCHAR(255) NOT NULL,
    role            VARCHAR(10)  NOT NULL DEFAULT 'driver',
    vehicle_type    VARCHAR(15)  NOT NULL DEFAULT 'car',
    license_info    JSONB        NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_role         CHECK (role IN ('driver', 'admin')),
    CONSTRAINT chk_vehicle_type CHECK (vehicle_type IN ('car', 'van', 'motorcycle', 'truck'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower ON auth.users (email_lower);
CREATE INDEX        IF NOT EXISTS idx_users_role        ON auth.users (role);
