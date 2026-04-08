-- Durable audit log for privileged journey actions.
-- Records which admin performed each force-cancel operation.

CREATE TABLE IF NOT EXISTS journey.admin_actions (
    id         BIGSERIAL    PRIMARY KEY,
    journey_id VARCHAR(20)  NOT NULL REFERENCES journey.journeys(journey_id),
    action     VARCHAR(50)  NOT NULL,
    admin_id   VARCHAR(50)  NOT NULL,
    timestamp  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_actions_journey_timestamp
    ON journey.admin_actions (journey_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_admin_actions_admin_timestamp
    ON journey.admin_actions (admin_id, timestamp DESC);