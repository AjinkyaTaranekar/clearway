# Notification Service (S5) — Complete Specification

> **Owner:** Ziwei Zhao  
> **Language:** Go 1.22+ (gorilla/mux)  
> **Database:** PostgreSQL 16 (own schema: `notification`)  
> **Port:** 8085  
> **Status:** Planning Phase

---

## 1. Purpose

The Notification Service is the asynchronous messaging component of the Distributed Traffic Service. It is responsible for turning journey lifecycle events into durable user-facing notifications, persisting them for in-app history, and delivering push notifications to drivers via Firebase Cloud Messaging (FCM).

In a multi-VM deployment, all VMs run identical stacks behind a load balancer. Each VM’s Notification Service independently consumes events from its local Redis Streams instance and persists notification state to its local PostgreSQL instance. PostgreSQL multi-master logical replication keeps notification data and device-token registrations in sync across VMs within milliseconds. A notification created on VM B will be visible on VM A and VM C shortly after commit, even if the next driver request is routed to a different VM.

---

## 2. Responsibilities

The Notification Service is responsible for:

- Consuming journey lifecycle events from the local Redis Streams instance using a consumer group
- Persisting all notifications in PostgreSQL for in-app notification history
- Delivering push notifications to drivers via Firebase Cloud Messaging (FCM)
- Managing FCM device token registration and updates
- Tracking delivery state for each notification (`pending`, `sent`, `failed`, `skipped`)
- Retrying transient FCM delivery failures with exponential backoff
- Exposing driver endpoints for listing notifications and marking them as read
- Exposing an admin endpoint for listing recent notifications across all users
- Handling duplicate event delivery safely using idempotent event processing keyed by `event_id`
- Running background jobs for retrying failed deliveries, reclaiming stuck Redis pending messages, and cleaning up stale device tokens

---

## 3. Architecture Context

### 3.1 Where Notification Service Sits

```text
                    ┌─────────────────────────────────────┐
                    │         Load Balancer               │
                    │    (Nginx / AWS ALB)                │
                    └──────────────┬──────────────────────┘
                                   │
               ┌───────────────────┼───────────────────┐
               ▼                   ▼                   ▼
    ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
    │      VM A        │ │      VM B        │ │      VM C        │
    │  ─────────────── │ │  ─────────────── │ │  ─────────────── │
    │  Journey Service │ │  Journey Service │ │  Journey Service │
    │  Capacity Service│ │  Capacity Service│ │  Capacity Service│
    │  Map Service     │ │  Map Service     │ │  Map Service     │
    │  IAM Service     │ │  IAM Service     │ │  IAM Service     │
    │  Notification Svc│ │  Notification Svc│ │  Notification Svc│
    │  ─────────────── │ │  ─────────────── │ │  ─────────────── │
    │  PostgreSQL      │ │  PostgreSQL      │ │  PostgreSQL      │
    │  Redis           │ │  Redis           │ │  Redis           │
    └────────┬─────────┘ └────────┬─────────┘ └────────┬─────────┘
             │                    │                    │
             └─────────── Multi-Master PostgreSQL ─────┘
                          Logical Replication
````

Within a single VM, Journey Service publishes events to the local Redis Streams instance, and Notification Service consumes them asynchronously. The browser also calls Notification Service directly for notification history and device token registration:

```text
  Journey Service (same VM)
       │  XADD journey.events  (async, after HTTP response)
       ▼
  Redis Streams (same VM)
       │  XREADGROUP journey.events
       ▼
  Notification Service
       │  INSERT notification rows
       │  UPSERT device tokens
       ▼
  PostgreSQL (same VM)
       │  logical replication
       ▼
  PostgreSQL on VM A, VM C

  Notification Service
       │  HTTPS POST /v1/projects/.../messages:send
       ▼
  Firebase Cloud Messaging (external)

  Browser
       │  GET /api/v1/notifications
       │  PUT /api/v1/notifications/:id/read
       │  PUT /api/v1/notifications/read-all
       │  POST /api/v1/notifications/device-token
       ▼
  Notification Service
```

### 3.2 Communication Pattern Summary

| From → To                            | Protocol                | Sync/Async | Why                                                                                                                                                                                  |
| ------------------------------------ | ----------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Journey Service → Redis Streams      | Redis XADD              | **Async**  | Notifications are not on the booking critical path. Journey Service should respond to the driver immediately after the journey state is committed, without waiting for FCM delivery. |
| Redis Streams → Notification Service | Redis XREADGROUP / XACK | **Async**  | Notification processing is event-driven and decoupled from the HTTP request lifecycle. Consumer groups provide at-least-once delivery and crash recovery.                            |
| Browser → Notification Service       | REST HTTP               | **Sync**   | The driver/admin is waiting for notification history, read-state updates, or device token registration to complete.                                                                  |
| Notification Service → PostgreSQL    | SQL                     | **Sync**   | Notification records and token state must be durably persisted before the event is acknowledged.                                                                                     |
| Notification Service → Firebase FCM  | HTTPS                   | **Async**  | Push delivery is external, retryable, and non-critical to correctness. The app still works if the push is delayed or fails.                                                          |
| Notification Service → IAM/Auth      | Local JWT validation    | **Sync**   | Driver/admin endpoints require auth, but validation is local using cached JWKS public keys. No runtime REST call is needed.                                                          |
| PostgreSQL VM-A ↔ VM-B ↔ VM-C        | Logical Replication     | **Async**  | Multi-master sync. Handled at the infrastructure layer. Services do not implement replication logic.                                                                                 |

### 3.3 Why These Choices

**Event consumption is async because push delivery is not on the critical path.**
When a driver books, cancels, activates, completes, or expires a journey, they already receive the result in the Journey Service HTTP response. The notification is a secondary delivery channel. If it arrives 2 seconds later, the system is still correct.

**Notification persistence happens before event acknowledgement.**
The notification history page must show notifications even if FCM delivery fails or the user has not granted browser push permission. Therefore Notification Service always writes the notification row to PostgreSQL first, then attempts push delivery, and only acknowledges the Redis message after the durable write has succeeded.

**Device tokens are stored in PostgreSQL, not Redis.**
Redis is per-VM and not shared. A token registered on VM A must be visible to VM B if the next event is processed there after replication converges. PostgreSQL logical replication makes the token registration visible across VMs. A Redis-only store would break cross-VM consistency.

**Idempotency is based on `event_id`, not a separate request key.**
Notification Service does not receive reserve-like write requests from another service. Its primary input is an event stream. The stable unique key for deduplication is the `event_id` included in the Journey Service envelope. A `UNIQUE` constraint on `event_id` makes duplicate event delivery harmless.

**FCM retries are asynchronous because provider failures are outside our control.**
Firebase may fail transiently due to rate limiting, timeout, or upstream issues. Retrying in the background improves delivery while keeping the consumer pipeline moving. Permanent token failures are handled by deactivating the token rather than retrying forever.

**Read/unread state is managed separately from delivery state.**
Whether a notification has been delivered to FCM is independent of whether the user has viewed it in the app. A notification can be `failed` for push delivery but still appear unread in the in-app notification list.

---

## 4. API Contract

### 4.1 POST /api/v1/notifications/device-token

Register or update the driver’s FCM device token. Called by the frontend after login when browser notification permission has been granted.

**Headers:** `Authorization: Bearer <jwt>`, `Content-Type: application/json`

**Request Body:**

```json
{
  "driver_id": "usr_x1y2z3",
  "fcm_token": "fcm_token_string_from_firebase",
  "platform": "web"
}
```

**Field notes:**

* `driver_id` must match the JWT `sub` claim unless the caller is an admin
* `platform` is one of `web`, `android`, `ios`
* If the same `fcm_token` already exists for another row, the service deactivates the old active row and reassigns the token to the current driver

**Response (200 OK):**

```json
{
  "status": "registered",
  "driver_id": "usr_x1y2z3",
  "device_token_id": "dvt_a1b2c3d4",
  "updated_at": "2026-04-15T08:00:01Z"
}
```

**Error Responses:**

* `400` — malformed request (missing `driver_id`, missing `fcm_token`, invalid `platform`)
* `401` — invalid or expired JWT
* `403` — `driver_id` does not match JWT subject and caller is not admin
* `500` — database error

---

### 4.2 GET /api/v1/notifications

List notifications for the authenticated driver.

**Headers:** `Authorization: Bearer <jwt>`

**Query Parameters:**

* `page`: default `1`
* `limit`: default `20`, max `100`
* `read`: optional filter (`true` or `false`)
* `type`: optional filter (`info`, `success`, `warning`, `error`)

**Example:**

```http
GET /api/v1/notifications?page=1&limit=20&read=false
```

**Response (200 OK):**

```json
{
  "notifications": [
    {
      "id": "ntf_a1b2c3",
      "title": "Journey Approved",
      "message": "Your journey from City Centre to Airport has been approved.",
      "type": "success",
      "read": false,
      "timestamp": "2026-04-15T08:00:01Z",
      "journey_id": "jrn_a1b2c3d4",
      "delivery_status": "sent"
    },
    {
      "id": "ntf_d4e5f6",
      "title": "Journey Completed",
      "message": "Your journey has been completed successfully.",
      "type": "success",
      "read": true,
      "timestamp": "2026-04-15T10:20:00Z",
      "journey_id": "jrn_a1b2c3d4",
      "delivery_status": "skipped"
    }
  ],
  "unread_count": 3,
  "total": 42,
  "page": 1,
  "limit": 20
}
```

**Error Responses:**

* `401` — invalid or expired JWT
* `500` — database error

---

### 4.3 PUT /api/v1/notifications/:id/read

Mark a single notification as read.

**Headers:** `Authorization: Bearer <jwt>`

**Response (200 OK):**

```json
{
  "notification_id": "ntf_a1b2c3",
  "read": true,
  "read_at": "2026-04-15T10:21:00Z"
}
```

**Error Responses:**

* `401` — invalid or expired JWT
* `404` — notification not found or not owned by this driver
* `500` — database error

---

### 4.4 PUT /api/v1/notifications/read-all

Mark all notifications as read for the authenticated driver.

**Headers:** `Authorization: Bearer <jwt>`

**Response (200 OK):**

```json
{
  "status": "ok",
  "updated_count": 12,
  "read_at": "2026-04-15T10:22:00Z"
}
```

**Error Responses:**

* `401` — invalid or expired JWT
* `500` — database error

---

### 4.5 GET /api/v1/admin/notifications

Returns recent notifications across all drivers. Used by the admin notifications page. Not called by regular drivers.

**Headers:** `Authorization: Bearer <jwt>`

**Query Parameters (all optional):**

* `page`: default `1`
* `limit`: default `50`, max `100`
* `type`: filter by `info`, `success`, `warning`, `error`
* `delivery_status`: filter by `pending`, `sent`, `failed`, `skipped`
* `driver_id`: filter by a specific driver
* `from_date`: ISO 8601 datetime (inclusive)
* `to_date`: ISO 8601 datetime (exclusive)

**Response (200 OK):**

```json
{
  "notifications": [
    {
      "id": "ntf_a1b2c3",
      "driver_id": "usr_x1y2z3",
      "journey_id": "jrn_a1b2c3d4",
      "title": "Journey Rejected",
      "message": "Your journey was rejected due to capacity constraints.",
      "type": "error",
      "read": false,
      "timestamp": "2026-04-15T08:00:01Z",
      "delivery_status": "sent",
      "event_type": "journey.rejected"
    }
  ],
  "total": 248,
  "page": 1,
  "limit": 50
}
```

**Auth:** Requires admin JWT (`role: "admin"`)

**Error Responses:**

* `401` — invalid or expired JWT
* `403` — caller is not an admin
* `500` — database error

---

### 4.6 GET /health

```json
{
  "status": "healthy",
  "db": "connected",
  "redis": "connected",
  "uptime_seconds": 3600
}
```

Returns `200` when healthy, `503` if DB or Redis is unreachable.

---

### 4.7 GET /ready

Returns `200 OK` only when the PostgreSQL connection pool, Redis connection, and Redis consumer initialisation have completed successfully. Returns `503` during startup or after connection failure. Used by Docker Swarm to gate traffic routing.

---

## 5. What Notification Service Provides to Other Services

### To the Frontend

| Endpoint                                  | Purpose                          |
| ----------------------------------------- | -------------------------------- |
| `POST /api/v1/notifications/device-token` | Register/update FCM device token |
| `GET /api/v1/notifications`               | Driver notification history      |
| `PUT /api/v1/notifications/:id/read`      | Mark one notification as read    |
| `PUT /api/v1/notifications/read-all`      | Mark all notifications as read   |
| `GET /api/v1/admin/notifications`         | Admin notification feed          |

**Contract guarantees Notification Service must uphold:**

* `GET /api/v1/notifications` must always return the fields expected by the frontend: `id`, `title`, `message`, `type`, `read`, `timestamp`, and optional `journey_id`
* Notification `type` values must exactly match the frontend union: `info`, `success`, `warning`, `error`
* `read-all` must only affect rows visible to the authenticated driver
* Device token registration must be idempotent for repeated submissions of the same token by the same user

### To Admin UI Consumers

| Endpoint                          | Purpose                                                        |
| --------------------------------- | -------------------------------------------------------------- |
| `GET /api/v1/admin/notifications` | Paginated admin view of recent notifications across the system |

No other backend service depends on Notification Service’s API for correctness. Notification Service is a leaf node in the backend dependency graph.

---

## 6. What Notification Service Needs from Other Services

### From Journey Service (S2) — via Redis Streams (async)

Notification Service is a **consumer** on the `journey.events` Redis Streams stream, consumer group `notification-service`. It acts on six event types:

#### journey.booked

```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.booked",
  "timestamp": "2026-04-15T08:00:01Z",
  "source_vm": "vm-b",
  "payload": {
    "journey_id": "jrn_a1b2c3d4",
    "driver_id": "usr_x1y2z3",
    "origin_label": "City Centre",
    "destination_label": "Airport",
    "status": "APPROVED"
  }
}
```

#### journey.rejected

```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.rejected",
  "timestamp": "2026-04-15T08:00:02Z",
  "source_vm": "vm-b",
  "payload": {
    "journey_id": "jrn_a1b2c3d4",
    "driver_id": "usr_x1y2z3",
    "status": "REJECTED",
    "reason": "Segment M7 Naas to Portlaoise is at capacity"
  }
}
```

#### journey.cancelled

```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.cancelled",
  "timestamp": "2026-04-15T09:30:00Z",
  "source_vm": "vm-b",
  "payload": {
    "journey_id": "jrn_a1b2c3d4",
    "driver_id": "usr_x1y2z3",
    "status": "CANCELLED",
    "cancelled_by": "driver"
  }
}
```

#### journey.activated

```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.activated",
  "timestamp": "2026-04-15T08:02:00Z",
  "source_vm": "vm-b",
  "payload": {
    "journey_id": "jrn_a1b2c3d4",
    "driver_id": "usr_x1y2z3",
    "status": "ACTIVE"
  }
}
```

#### journey.completed

```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.completed",
  "timestamp": "2026-04-15T10:20:00Z",
  "source_vm": "vm-b",
  "payload": {
    "journey_id": "jrn_a1b2c3d4",
    "driver_id": "usr_x1y2z3",
    "status": "COMPLETED"
  }
}
```

#### journey.expired

```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.expired",
  "timestamp": "2026-04-15T08:35:00Z",
  "source_vm": "vm-b",
  "payload": {
    "journey_id": "jrn_a1b2c3d4",
    "driver_id": "usr_x1y2z3",
    "status": "EXPIRED"
  }
}
```

**Processing logic for all six:**

1. Check whether `event_id` already exists in `notification.notifications`
2. If already processed: XACK and return
3. Derive notification `title`, `message`, and `type` from `event_type` + payload
4. Insert notification row with `delivery_status='pending'`
5. Look up active device token(s) for `driver_id`
6. If no token found: update row to `delivery_status='skipped'`
7. If token found: attempt FCM send
8. On success: update row to `delivery_status='sent'`
9. On retryable failure: update row to `delivery_status='failed'`, increment retry count
10. On terminal token failure: update row to `delivery_status='failed'`, deactivate token
11. XACK the message after the durable DB write path completes

**Event → notification mapping:**

| Event               | Title             | Type      | Example Message                                                        |
| ------------------- | ----------------- | --------- | ---------------------------------------------------------------------- |
| `journey.booked`    | Journey Approved  | `success` | Your journey from City Centre to Airport has been approved.            |
| `journey.rejected`  | Journey Rejected  | `error`   | Your journey was rejected due to capacity constraints.                 |
| `journey.cancelled` | Journey Cancelled | `warning` | Your journey has been cancelled.                                       |
| `journey.activated` | Journey Started   | `info`    | Journey started. Drive safe!                                           |
| `journey.completed` | Journey Completed | `success` | Your journey has been completed successfully.                          |
| `journey.expired`   | Journey Expired   | `warning` | Your journey booking has expired because it was not activated on time. |

**What Journey Service must provide:**

* Stable event envelope with unique `event_id`
* `payload.driver_id` on every event
* `payload.journey_id` on every event
* Enough contextual fields to render human-readable messages (e.g. rejection reason, origin/destination labels where available)
* Event publication only after the journey state is committed in PostgreSQL

### From IAM Service (S1) — no runtime calls

Notification Service fetches JWKS public keys from IAM on startup and refreshes every hour. JWT validation for driver and admin HTTP endpoints is performed locally. No runtime REST call to IAM is made per request.

### From Firebase Cloud Messaging (External)

Notification Service uses Firebase Cloud Messaging for push delivery.

**Failure classification:**

* **Retryable:** timeout, temporary upstream error, rate limit
* **Terminal:** invalid token, unregistered device, malformed request

On terminal token errors, the token row is marked inactive so future notifications are not retried against the same invalid token.

---

## 7. Database Schema

```sql
CREATE SCHEMA IF NOT EXISTS notification;

-- Stored notifications
-- One row per consumed journey event
CREATE TABLE notification.notifications (
    notification_id     VARCHAR(20) PRIMARY KEY,         -- "ntf_" + nanoid(10)
    event_id            VARCHAR(40) NOT NULL UNIQUE,     -- dedupe key for at-least-once event delivery
    driver_id           VARCHAR(20) NOT NULL,
    journey_id          VARCHAR(20),
    event_type          VARCHAR(40) NOT NULL,

    title               VARCHAR(120) NOT NULL,
    message             TEXT NOT NULL,
    type                VARCHAR(20) NOT NULL,            -- info, success, warning, error

    delivery_status     VARCHAR(20) NOT NULL DEFAULT 'pending',
    retry_count         INTEGER NOT NULL DEFAULT 0,
    last_error          TEXT,

    is_read             BOOLEAN NOT NULL DEFAULT FALSE,
    read_at             TIMESTAMPTZ,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at        TIMESTAMPTZ,
    failed_at           TIMESTAMPTZ,

    CONSTRAINT chk_notification_type
      CHECK (type IN ('info','success','warning','error')),

    CONSTRAINT chk_delivery_status
      CHECK (delivery_status IN ('pending','sent','failed','skipped'))
);

-- Primary query: driver's notification list
CREATE INDEX idx_notifications_driver_created
    ON notification.notifications (driver_id, created_at DESC);

-- Driver unread count / read filter
CREATE INDEX idx_notifications_driver_read
    ON notification.notifications (driver_id, is_read, created_at DESC);

-- Admin feed
CREATE INDEX idx_notifications_admin_created
    ON notification.notifications (created_at DESC);

-- Retry worker
CREATE INDEX idx_notifications_delivery_retry
    ON notification.notifications (delivery_status, retry_count, created_at)
    WHERE delivery_status = 'failed';

-- Journey-specific notification lookup
CREATE INDEX idx_notifications_journey
    ON notification.notifications (journey_id);

-- Registered device tokens
-- A driver may have multiple tokens (e.g. multiple browsers/devices),
-- but each token value is unique among active rows.
CREATE TABLE notification.device_tokens (
    device_token_id      VARCHAR(20) PRIMARY KEY,        -- "dvt_" + nanoid(10)
    driver_id            VARCHAR(20) NOT NULL,
    fcm_token            TEXT NOT NULL,
    platform             VARCHAR(20) NOT NULL DEFAULT 'web',
    is_active            BOOLEAN NOT NULL DEFAULT TRUE,
    last_seen_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    invalidated_at       TIMESTAMPTZ,
    invalidation_reason  TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_platform
      CHECK (platform IN ('web','android','ios'))
);

-- Prevent two active rows with the same token
CREATE UNIQUE INDEX uq_active_fcm_token
    ON notification.device_tokens (fcm_token)
    WHERE is_active = TRUE;

-- Lookup all active tokens for a driver
CREATE INDEX idx_device_tokens_driver
    ON notification.device_tokens (driver_id, is_active);

-- Optional audit log of delivery attempts
CREATE TABLE notification.delivery_attempts (
    attempt_id           BIGSERIAL PRIMARY KEY,
    notification_id      VARCHAR(20) NOT NULL REFERENCES notification.notifications(notification_id),
    attempt_number       INTEGER NOT NULL,
    status               VARCHAR(20) NOT NULL,           -- success, failed
    error_message        TEXT,
    attempted_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_delivery_attempts_notification
    ON notification.delivery_attempts (notification_id, attempted_at DESC);
```

---

## 8. Redis Usage

### 8.1 Redis Streams Consumer

**Stream:** `journey.events`
**Consumer group:** `notification-service`
**Consumer name:** `${VM_ID}-notification` (e.g. `vm-a-notification`)

Consumer group creation at startup (idempotent):

```text
XGROUP CREATE journey.events notification-service $ MKSTREAM
```

`$` means start from new messages only. Unacknowledged messages from a previous crash are reclaimed via `XAUTOCLAIM`.

**Read loop:**

```text
XREADGROUP GROUP notification-service ${VM_ID}-notification COUNT 10 BLOCK 5000 STREAMS journey.events >
```

**Processing per message:**

1. Decode event envelope
2. Ignore and XACK unknown event types
3. Check `event_id` dedupe against PostgreSQL
4. Insert notification row
5. Attempt FCM send if token exists
6. Update `delivery_status`
7. Write `delivery_attempts` row where applicable
8. XACK

### 8.2 Why Redis Is Not Used for Notification Storage

Notification rows and token registrations must survive VM restarts and replicate across VMs. Redis is per-VM and not shared. Therefore Redis is used only for the event stream transport layer, not as the system of record for notification history or device token storage.

### 8.3 Retry on DB Failure

* Do **not** XACK the Redis message if the initial notification insert fails
* Exponential retry for consumer-side transient DB errors: 1s, 2s, 4s (max 3 attempts)
* After 3 failures: leave the message pending for reclaim, or log and continue depending on operational policy
* Pending recovery on restart:

```text
XAUTOCLAIM journey.events notification-service ${VM_ID}-notification 60000 0-0 COUNT 100
```

---

## 9. Notification Processing Logic (Core Algorithm)

### 9.1 Event Mapping Logic

```text
Input:
  event_type = "journey.rejected"
  payload = {
    driver_id: "usr_x1y2z3",
    journey_id: "jrn_a1b2c3d4",
    reason: "Segment M7 Naas to Portlaoise is at capacity"
  }

Processing:
  title   = "Journey Rejected"
  type    = "error"
  message = payload.reason if present
            else "Your journey was rejected. Please try a different departure time."

Output:
  {
    title: "Journey Rejected",
    type: "error",
    message: "Segment M7 Naas to Portlaoise is at capacity"
  }
```

### 9.2 Atomic Notification Insert + Delivery Status Update

```text
1. Read Redis message
2. Extract event_id, event_type, driver_id, journey_id
3. Query notification.notifications by event_id
   If found: XACK and return

4. Insert notification row:
   - delivery_status = 'pending'
   - is_read = false

5. Query active device token(s) by driver_id
   If none:
      UPDATE notification row SET delivery_status='skipped', updated_at=NOW()
      XACK and return

6. Attempt FCM send for each active token
   a. If at least one succeeds:
      UPDATE notification row SET delivery_status='sent', delivered_at=NOW()
   b. If all fail transiently:
      UPDATE notification row SET delivery_status='failed', retry_count=retry_count+1, last_error=...
   c. If token invalid/unregistered:
      UPDATE device_tokens SET is_active=false, invalidated_at=NOW(), invalidation_reason=...
      UPDATE notification row SET delivery_status='failed', last_error=...

7. INSERT delivery_attempts audit row(s)

8. XACK the Redis message
```

### 9.3 Retry Worker Logic

```text
SELECT notification_id, driver_id
FROM notification.notifications
WHERE delivery_status = 'failed'
  AND retry_count < 3
ORDER BY created_at ASC
LIMIT 100;

For each row:
  1. Look up active device token(s)
  2. If none remain: set delivery_status='skipped'
  3. Retry FCM send
  4. On success: set delivery_status='sent', delivered_at=NOW()
  5. On failure: increment retry_count, update last_error
  6. Insert delivery_attempts audit row
```

---

## 10. Edge Cases

| #   | Scenario                                                                  | Risk                                                  | Mitigation                                                                                                                 |
| --- | ------------------------------------------------------------------------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| E1  | Same Redis event delivered twice                                          | Duplicate notification rows and duplicate pushes      | `event_id` has a `UNIQUE` constraint. Second processing attempt becomes a no-op.                                           |
| E2  | Redis consumer crashes after DB insert but before XACK                    | Event is replayed                                     | Replay is safe because the second insert hits the `event_id` uniqueness guard.                                             |
| E3  | Driver has no registered device token                                     | No push can be sent                                   | Persist notification anyway, mark `delivery_status='skipped'`. The user still sees it in-app.                              |
| E4  | FCM token is invalid or unregistered                                      | Endless failed retries                                | Mark token inactive and store failure reason; stop retrying that token.                                                    |
| E5  | FCM transient outage                                                      | Push delayed                                          | Mark notification `failed` and retry in background with exponential backoff.                                               |
| E6  | Journey event missing optional fields (e.g. rejection reason)             | Broken message rendering                              | Use fallback generic message templates. Notification creation must never fail solely due to missing optional fields.       |
| E7  | Driver registers the same token multiple times                            | Duplicate token rows                                  | Use `UNIQUE` constraint on active token value and UPSERT/deactivate-old-row logic.                                         |
| E8  | Driver marks another user’s notification as read                          | Unauthorized mutation                                 | `UPDATE ... WHERE notification_id=$1 AND driver_id=$jwt_sub`. If 0 rows affected, return `404`.                            |
| E9  | read-all races with new notification insert                               | Some rows remain unread                               | Accept eventual consistency. The operation applies to rows visible at transaction start.                                   |
| E10 | Replication lag after token registration                                  | Event handled on another VM may not yet see the token | Notification is still persisted. Push may be skipped for that one event; future events succeed once replication converges. |
| E11 | Admin requests very large notification history                            | Heavy DB load                                         | Paginate results, cap `limit` at 100, index by `created_at DESC`.                                                          |
| E12 | Retry loop on permanent FCM error                                         | Wasted compute and noisy logs                         | Distinguish retryable vs terminal errors explicitly.                                                                       |
| E13 | Split brain duplicates event handling on two VMs                          | Double inserts after partition heal                   | `event_id` uniqueness across replicated PostgreSQL prevents duplicate rows from surviving.                                 |
| E14 | Event order inversion under load                                          | “Completed” appears before “Activated”                | Sort notifications by `created_at DESC`. Minor ordering drift is acceptable for prototype scope.                           |
| E15 | Driver has multiple valid devices                                         | Multiple pushes for same event                        | This is correct behaviour. One notification row, multiple push deliveries.                                                 |
| E16 | Device token reused by another driver after logout/login on shared device | Push sent to wrong user                               | Re-register token on login, deactivate previous active row for that token value.                                           |

---

## 11. Background Jobs

### 11.1 Retry Failed Deliveries

**Frequency:** Every 60 seconds

**Purpose:** Retry notifications whose initial FCM send failed due to a transient error.

```sql
SELECT notification_id
FROM notification.notifications
WHERE delivery_status = 'failed'
  AND retry_count < 3
ORDER BY created_at ASC
LIMIT 100;
```

For each row:

* retry FCM send
* update `delivery_status`, `retry_count`, `last_error`
* insert `delivery_attempts` row

### 11.2 Reclaim Stuck Pending Redis Messages

**Frequency:** Every 60 seconds

**Purpose:** Recover events that were delivered to a crashed consumer but never acknowledged.

Uses:

```text
XAUTOCLAIM journey.events notification-service ${VM_ID}-notification 60000 0-0 COUNT 100
```

### 11.3 Device Token Cleanup

**Frequency:** Every 24 hours

**Purpose:** Deactivate tokens already known to be invalid, or optionally prune very old inactive rows.

Example cleanup:

```sql
DELETE FROM notification.device_tokens
WHERE is_active = FALSE
  AND invalidated_at < NOW() - INTERVAL '30 days';
```

### 11.4 Delivery Attempt Cleanup

**Frequency:** Every 24 hours

**Purpose:** Delete old audit rows beyond retention window (default: 30 days).

```sql
DELETE FROM notification.delivery_attempts
WHERE attempted_at < NOW() - INTERVAL '30 days';
```

---

## 12. Multi-VM Behavior

### 12.1 Request Flow (Journey Approved)

```text
1. Driver books a journey on VM B

2. VM B's Journey Service:
   a. Persists journey state
   b. Returns 201/200 to the driver
   c. Publishes journey.booked to VM B's Redis Streams

3. VM B's Notification Service:
   a. Consumes journey.booked from VM B's Redis
   b. Inserts notification row into VM B's PostgreSQL
   c. Sends push via Firebase FCM
   d. Updates delivery status

4. PostgreSQL logical replication propagates:
   - notification.notifications row → VM A, VM C
   - notification.device_tokens changes (if any) → VM A, VM C

5. Driver later opens /driver/notifications from a new request
   routed to VM A, VM B, or VM C
```

### 12.2 Request Flow (Driver Reads Notifications on Different VM)

```text
1. Device token was originally registered on VM A

2. A later journey.completed event is published on VM B

3. VM B's Notification Service:
   - sees the token row if replication has already converged
   - otherwise still inserts the notification row and may mark push as skipped/failed

4. Driver opens /driver/notifications routed to VM C
   - notification row is visible once PostgreSQL replication converges
```

**Key insight:** Notification rows and device token rows are present on all VMs via PostgreSQL replication. Redis is local-only, but the durable state converges across all VMs.

### 12.3 Multi-Master Conflict Resolution

| Conflict type                                                                 | Resolution                                                           |
| ----------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Duplicate insert of same `event_id`                                           | `UNIQUE` constraint rejects duplicate row                            |
| Duplicate active `fcm_token`                                                  | Partial unique index on active token value rejects second active row |
| Concurrent read-state updates on same notification                            | Last-write-wins using `read_at` / `updated_at`                       |
| Notification insert on one VM while read occurs on another before replication | Temporary stale read; converges after replication delay              |

### 12.4 Replication Monitoring

```sql
SELECT
    application_name,
    state,
    (sent_lsn - replay_lsn) AS replication_lag_bytes
FROM pg_stat_replication;
```

Alert threshold: lag > 1 second. Notification history is eventually consistent across VMs; the frontend remains usable even during short lag spikes.

---

## 13. Configuration (Environment Variables)

```bash
# Server
PORT=8085
ENV=production
LOG_LEVEL=info
VM_ID=vm-a                              # Used in logs and Redis consumer name

# PostgreSQL (local instance on this VM)
DB_HOST=localhost
DB_PORT=5432
DB_NAME=trafficservice
DB_SCHEMA=notification
DB_USER=notification_svc
DB_PASSWORD=<secret>
DB_MAX_CONNS=20
DB_IDLE_CONNS=5
DB_CONN_TIMEOUT_SECONDS=5

# Redis (local instance on this VM)
REDIS_HOST=localhost:6379
REDIS_PASSWORD=<secret>
REDIS_DB=0
REDIS_CONNECT_TIMEOUT_SECONDS=5

# IAM JWKS (for auth on HTTP endpoints — no runtime calls per request)
JWKS_URL=http://iam-service:8082/api/v1/auth/.well-known/jwks.json
JWKS_REFRESH_INTERVAL_SECONDS=3600

# Redis Streams
STREAM_NAME=journey.events
CONSUMER_GROUP=notification-service
CONSUMER_NAME=${VM_ID}-notification
STREAM_READ_COUNT=10
STREAM_BLOCK_MS=5000
STREAM_RECLAIM_IDLE_MS=60000

# FCM
FCM_PROJECT_ID=<project-id>
FCM_CREDENTIALS_JSON_BASE64=<base64-service-account-json>
FCM_SEND_TIMEOUT_SECONDS=5

# Retry policy
MAX_DELIVERY_RETRIES=3
DELIVERY_RETRY_BACKOFF_SECONDS=1,4,16

# Background jobs
DELIVERY_RETRY_INTERVAL_SECONDS=60
PENDING_RECLAIM_INTERVAL_SECONDS=60
TOKEN_CLEANUP_INTERVAL_HOURS=24
DELIVERY_ATTEMPT_RETENTION_DAYS=30

# API
DEFAULT_PAGE_SIZE=20
MAX_PAGE_SIZE=100
```

---

## 14. Project Structure

```text
notification-service/
├── cmd/
│   └── server/
│       └── main.go                      # Entry point: wires all components, starts HTTP + consumer goroutine
│
├── internal/
│   ├── handler/
│   │   ├── notification_handler.go      # GET /notifications, PUT /:id/read, PUT /read-all
│   │   ├── device_token_handler.go      # POST /device-token
│   │   ├── admin_handler.go             # GET /admin/notifications
│   │   └── health_handler.go            # GET /health, GET /ready
│   │
│   ├── middleware/
│   │   ├── auth.go                      # JWT validation (cached JWKS)
│   │   └── logging.go                   # Request logging with X-Trace-ID
│   │
│   ├── model/
│   │   ├── notification.go              # Notification struct, type/status constants
│   │   ├── device_token.go              # DeviceToken struct
│   │   └── delivery_attempt.go          # DeliveryAttempt struct
│   │
│   ├── service/
│   │   ├── notification_service.go      # Core: CreateFromEvent(), List(), MarkRead(), MarkAllRead()
│   │   ├── delivery_service.go          # SendPush(), classify FCM failures, retry logic
│   │   ├── token_service.go             # RegisterOrUpdateToken()
│   │   └── cleanup.go                   # Background jobs: retry, token cleanup, audit cleanup
│   │
│   ├── repository/
│   │   ├── notification_repo.go         # Insert(), GetByEventID(), ListByDriver(), MarkRead(), MarkAllRead()
│   │   ├── token_repo.go                # Upsert(), GetActiveByDriver(), DeactivateToken()
│   │   └── delivery_attempt_repo.go     # Insert(), DeleteOld()
│   │
│   └── event/
│       ├── consumer.go                  # Redis Streams XREADGROUP loop + event handling
│       └── mapper.go                    # Event → title/message/type mapping
│
├── migrations/
│   └── 001_create_schema.sql            # CREATE SCHEMA notification; all tables and indexes
│
├── pkg/                                 # Shared packages (already exist in skeleton)
│   ├── config/config.go
│   ├── errors/errors.go
│   ├── logger/logger.go
│   ├── postgres/connection.go
│   ├── response/response.go
│   └── tracing/middleware.go
│
├── docs/
│   ├── docs.go
│   ├── swagger.json
│   └── swagger.yaml
│
├── Dockerfile
├── Makefile
├── config.yaml
└── go.mod
```

---

## 15. Sequence Diagrams

### 15.1 Notification Creation + Push Delivery

All actors run on the **same VM** except Firebase FCM.

```text
Journey Svc      Redis Streams    Notification Svc    PostgreSQL      Firebase FCM
    │                  │                │                │                 │
    │  XADD event       │                │                │                 │
    ├──────────────────►│                │                │                 │
    │                  │                │                │                 │
    │                  │  1. XREADGROUP │                │                 │
    │                  ├───────────────►│                │                 │
    │                  │                │                │                 │
    │                  │                │  2. Check event_id dedupe       │
    │                  │                ├───────────────►│                 │
    │                  │                │◄───────────────┤                 │
    │                  │                │  miss          │                 │
    │                  │                │                │                 │
    │                  │                │  3. INSERT notification row     │
    │                  │                ├───────────────►│                 │
    │                  │                │◄───────────────┤                 │
    │                  │                │                │                 │
    │                  │                │  4. Lookup active device tokens │
    │                  │                ├───────────────►│                 │
    │                  │                │◄───────────────┤                 │
    │                  │                │                │                 │
    │                  │                │  5. Send push                  ├──────────────►
    │                  │                │                                │◄──────────────┤
    │                  │                │                │                 │
    │                  │                │  6. UPDATE delivery_status      │
    │                  │                ├───────────────►│                 │
    │                  │                │◄───────────────┤                 │
    │                  │                │                │                 │
    │                  │  7. XACK       │                │                 │
    │                  │◄───────────────┤                │                 │
```

### 15.2 Device Token Registration

```text
Browser                Notification Svc         PostgreSQL
   │                           │                      │
   │  POST /device-token       │                      │
   │  { driver_id, fcm_token } │                      │
   ├──────────────────────────►│                      │
   │                           │ validate JWT locally │
   │                           │ verify sub == driver │
   │                           │                      │
   │                           │ UPSERT token row ───►│
   │                           │◄─────────────────────│
   │◄──────────────────────────┤                      │
   │  200 registered           │                      │
```

### 15.3 Mark Notification Read

```text
Browser                Notification Svc         PostgreSQL
   │                           │                      │
   │  PUT /notifications/:id/read                      │
   ├──────────────────────────►│                      │
   │                           │ validate JWT locally │
   │                           │                      │
   │                           │ UPDATE row ─────────►│
   │                           │ WHERE id=$1          │
   │                           │ AND driver_id=$2     │
   │                           │◄─────────────────────│
   │                           │ if 0 rows → 404      │
   │◄──────────────────────────┤                      │
   │  200 read=true            │                      │
```

---

## 16. Frontend Integration

**Notification Service is called directly by the browser.** Unlike Capacity Service, the frontend talks to port 8085 for notification history, read-state changes, and FCM token registration.

### 16.1 Routes That Call Notification Service

```text
/driver/notifications         → Driver notification history
/driver/settings              → Driver profile/settings, FCM token registration
/admin/notifications          → Admin notification feed
```

### 16.2 Page → API Mapping

| Page                    | Backend Call       | Endpoint                                          | Notes                                   |
| ----------------------- | ------------------ | ------------------------------------------------- | --------------------------------------- |
| `/driver/notifications` | List notifications | `GET /api/v1/notifications?page=1&limit=20`       | Driver notification history             |
| `/driver/notifications` | Mark one as read   | `PUT /api/v1/notifications/:id/read`              | Single notification read toggle         |
| `/driver/notifications` | Mark all as read   | `PUT /api/v1/notifications/read-all`              | Bulk mark read                          |
| `/driver/settings`      | Register FCM token | `POST /api/v1/notifications/device-token`         | Called after browser permission granted |
| `/admin/notifications`  | List notifications | `GET /api/v1/admin/notifications?page=1&limit=50` | Admin feed across all drivers           |

### 16.3 Notification Type Alignment

Notification `type` values must match frontend expectations exactly:

| Backend   | Frontend  |
| --------- | --------- |
| `info`    | `info`    |
| `success` | `success` |
| `warning` | `warning` |
| `error`   | `error`   |

### 16.4 Notification Response Shape

Frontend expects:

```json
{
  "notifications": [
    {
      "id": "ntf_a1b2c3",
      "title": "Journey Approved",
      "message": "Your journey from City Centre to Airport has been approved.",
      "type": "success",
      "read": false,
      "timestamp": "2026-04-15T08:00:01Z",
      "journey_id": "jrn_a1b2c3d4"
    }
  ],
  "unread_count": 3
}
```

Backend may include extra fields such as `delivery_status`, but the fields above must always be present in the driver-facing API.

### 16.5 FCM Token Registration Flow

```typescript
const permission = await Notification.requestPermission();

if (permission === 'granted') {
  const fcmToken = await getToken(messaging, {
    vapidKey: import.meta.env.VITE_FCM_VAPID_KEY,
  });

  await notificationAPI.registerDeviceToken({
    driver_id: user.id,
    fcm_token: fcmToken,
    platform: 'web',
  });
}
```

Notification Service stores the `driver_id → fcm_token` mapping in PostgreSQL and uses it when processing future journey lifecycle events.

### 16.6 CORS

Notification Service **does** need browser-origin CORS headers because it is called directly by the frontend.

Required development origin:

* `http://localhost:5173`

Recommended allowed headers:

* `Content-Type`
* `Authorization`
* `X-Trace-ID`

---

## 17. Interface Contract Summary

Notification Service is a **leaf node** in the backend dependency graph. No other service depends on its API for correctness.

**Consumes from Journey Service:**

* `journey.booked`
* `journey.rejected`
* `journey.cancelled`
* `journey.activated`
* `journey.completed`
* `journey.expired`

**Provides to frontend/admin:**

* `POST /api/v1/notifications/device-token`
* `GET /api/v1/notifications`
* `PUT /api/v1/notifications/:id/read`
* `PUT /api/v1/notifications/read-all`
* `GET /api/v1/admin/notifications`

**Auth dependency:**

* IAM JWKS endpoint for local JWT validation

**External dependency:**

* Firebase Cloud Messaging for push delivery

---

*Last updated: 2026-04-05*
*Service version: 0.1.0 (planning)*

