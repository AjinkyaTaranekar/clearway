-- Notification Service Schema
-- Run against the target PostgreSQL database before starting the service.

CREATE SCHEMA IF NOT EXISTS notification;

-- ============================================================
-- Notifications table
-- ============================================================
CREATE TABLE IF NOT EXISTS notification.notifications (
    notification_id     VARCHAR(20)  PRIMARY KEY,
    event_id            VARCHAR(40)  NOT NULL UNIQUE,
    driver_id           VARCHAR(40)  NOT NULL,
    journey_id          VARCHAR(20),
    event_type          VARCHAR(40)  NOT NULL,
    title               VARCHAR(120) NOT NULL,
    message             TEXT         NOT NULL,
    type                VARCHAR(20)  NOT NULL,
    delivery_status     VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    retry_count         INTEGER      NOT NULL DEFAULT 0,
    last_error          TEXT,
    is_read             BOOLEAN      NOT NULL DEFAULT FALSE,
    read_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    sent_at             TIMESTAMPTZ,
    failed_at           TIMESTAMPTZ,
    CONSTRAINT chk_notification_type  CHECK (type IN ('info','success','warning','error')),
    CONSTRAINT chk_delivery_status    CHECK (delivery_status IN ('PENDING','SENT','FAILED','SKIPPED','RETRYING'))
);

CREATE INDEX IF NOT EXISTS idx_notifications_driver_created
    ON notification.notifications (driver_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_driver_read
    ON notification.notifications (driver_id, is_read, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_admin_feed
    ON notification.notifications (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_delivery_status
    ON notification.notifications (delivery_status, created_at);

CREATE INDEX IF NOT EXISTS idx_notifications_journey
    ON notification.notifications (journey_id);


-- ============================================================
-- Device tokens table
-- ============================================================
CREATE TABLE IF NOT EXISTS notification.device_tokens (
    device_token_id      VARCHAR(20)  PRIMARY KEY,
    driver_id            VARCHAR(40)  NOT NULL,
    fcm_token            TEXT         NOT NULL,
    platform             VARCHAR(20)  NOT NULL DEFAULT 'web',
    is_active            BOOLEAN      NOT NULL DEFAULT TRUE,
    last_seen_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    invalidated_at       TIMESTAMPTZ,
    invalidation_reason  TEXT,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_platform CHECK (platform IN ('web','android','ios'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_active_token_value
    ON notification.device_tokens (fcm_token) WHERE is_active = TRUE;

CREATE INDEX IF NOT EXISTS idx_device_tokens_driver
    ON notification.device_tokens (driver_id, is_active);


-- ============================================================
-- Delivery attempts audit log
-- ============================================================
CREATE TABLE IF NOT EXISTS notification.delivery_attempts (
    attempt_id       BIGSERIAL    PRIMARY KEY,
    notification_id  VARCHAR(20)  NOT NULL REFERENCES notification.notifications(notification_id),
    attempt_number   INTEGER      NOT NULL,
    status           VARCHAR(20)  NOT NULL,
    error_message    TEXT,
    attempted_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_delivery_attempts_notification
    ON notification.delivery_attempts (notification_id, attempted_at DESC);
