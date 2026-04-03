# Capacity Service (S3) — Complete Specification

> **Owner:** Jai Nagle
> **Language:** Go 1.22+ (gorilla/mux)
> **Database:** PostgreSQL 16 (own schema: `capacity`)
> **Port:** 8081
> **Status:** Planning Phase

---

## 1. Purpose

The Capacity Service is the authoritative source of truth for road segment slot availability in the Distributed Traffic Service. It enforces a hard upper bound on the number of vehicles that may simultaneously occupy any road segment during any time window, ensuring no segment is ever over-subscribed.

In a multi-VM deployment, all VMs run identical stacks behind a load balancer. Each VM's Capacity Service independently manages its local PostgreSQL replica. PostgreSQL multi-master logical replication keeps all VMs' capacity data in sync within milliseconds. A reservation committed on VM B will be visible to VM A's Capacity Service before any subsequent booking request arrives — given the ~100ms replication lag and the minimum 1-hour advance booking requirement.

---

## 2. Responsibilities

The Capacity Service is responsible for:

- Maintaining a registry of road segments and their maximum concurrent slot capacity
- Accepting multi-segment reservation requests from Journey Service and atomically reserving all requested slots in a single PostgreSQL transaction (all-or-nothing)
- Supporting idempotency keys on reserve calls, stored durably in PostgreSQL, so that Journey Service retries never double-book
- Releasing reserved slots asynchronously by consuming `journey.cancelled`, `journey.completed`, and `journey.expired` events from the local Redis Streams instance
- Exposing a read-only availability check endpoint (no side effects) using a 30-second Redis cache for hot segments
- Exposing a segment occupancy endpoint consumed by Map Service for the admin traffic map
- Running a background job to clean up orphaned reservations (reservations with no corresponding journey, caused by a Journey Service crash mid-booking)
- Applying vehicle type slot weights: `car=1.0`, `van=1.5`, `motorcycle=0.5`, `truck=3.0`

---

## 3. Architecture Context

### 3.1 Where Capacity Service Sits

```
                    ┌─────────────────────────────────────┐
                    │         Load Balancer               │
                    │    (Nginx / AWS ALB)                 │
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
```

Within a single VM, Capacity Service is called by Journey Service over the local Docker network, and independently consumes events from the local Redis Streams instance:

```
  Journey Service (same VM)
       │  POST /api/v1/capacity/reserve  (sync, critical path)
       │  GET  /api/v1/capacity/check    (sync, read-only preview)
       ▼
  Capacity Service
       │  reads/writes capacity.segments, capacity.reservations
       ▼
  PostgreSQL (same VM)
       │  logical replication
       ▼
  PostgreSQL on VM A, VM C

  Redis Streams (same VM) ──► Capacity Service consumer goroutine
       journey.cancelled → release slots
       journey.completed → release slots
       journey.expired   → release slots

  Map Service (same VM)
       │  GET /api/v1/capacity/segments/occupancy  (sync, admin map)
       ▼
  Capacity Service
```

### 3.2 Communication Pattern Summary

| From → To | Protocol | Sync/Async | Why |
|-----------|----------|------------|-----|
| Journey Service → Capacity Service (reserve) | REST POST | **Sync** | The driver is waiting for a booking decision. The reservation must complete before Journey Service can respond. Async would require a pending state and callback mechanism — worse UX and more complexity. Both services are on the same VM so latency is sub-millisecond. |
| Journey Service → Capacity Service (check) | REST GET | **Sync** | Availability preview is a blocking UX interaction. Cache hit rate is high (30s TTL). |
| Redis Streams → Capacity Service (release) | Redis XREAD | **Async** | Releasing capacity is not on any driver's critical path. The driver already has their cancellation/completion confirmation in the HTTP response. A 2-second delay in capacity release is invisible. Async also decouples Journey Service from Capacity Service for terminal state transitions. |
| Map Service → Capacity Service (occupancy) | REST GET | **Sync** | Admin map poll. Not latency-sensitive (60s poll interval). Capacity Service has no push mechanism; Map Service needs to merge occupancy with topology before returning to the frontend. |
| PostgreSQL VM-A ↔ VM-B ↔ VM-C | Logical Replication | **Async** | Multi-master sync. Handled at the infrastructure layer. Services do not implement replication logic. |

### 3.3 Why These Choices

**Reserve is sync because the driver is waiting.**
Journey Service cannot respond to the driver until it knows whether capacity was successfully reserved. There is no useful work to do in parallel after the route is computed. Both services are on the same VM — the intra-VM REST call is effectively a function call with HTTP overhead, completing in under 5ms under normal conditions.

**Slot release is async because the driver already has the answer.**
When a driver cancels, Journey Service marks the journey CANCELLED, responds 200 OK to the driver, then publishes `journey.cancelled` to Redis Streams. Capacity Service processes this event and releases slots. The 1-hour advance booking requirement means the brief delay (seconds) has zero practical impact: no other driver could book and start a journey on those exact segment/windows in the seconds it takes for the event to be processed.

**Idempotency keys are stored in PostgreSQL, not Redis.**
PostgreSQL logical replication ensures idempotency records propagate to all VMs within ~100ms. If Journey Service retries a reserve call and the retry hits a different VM (due to load balancing), that VM will have the original idempotency record via replication and return the cached response. Redis is per-VM and not shared — a Redis-based idempotency store would fail for cross-VM retries. PostgreSQL's `UNIQUE` constraint on `idempotency_key` also provides a hard conflict guard if replication lag creates a brief race window.

**`SELECT FOR UPDATE` is used over optimistic concurrency control for reservations.**
Optimistic concurrency control would require the application to retry on version conflict. Under high contention on a popular segment/window, this could cause many retries and unpredictable latency on the driver's critical path. `SELECT FOR UPDATE` serializes concurrent reservation attempts for the same segment rows, guaranteeing that the capacity check and slot insertion happen atomically. The lock is held for milliseconds. The trade-off is serialized throughput on hot segments, acceptable for a prototype.

**Deadlock prevention:** All `SELECT FOR UPDATE` locks are acquired on `capacity.segments` rows sorted by `segment_id` (alphabetically). Any transaction locking multiple segments always acquires locks in the same order, eliminating circular wait conditions.

---

## 4. API Contract

### 4.1 POST /api/v1/capacity/reserve

Reserve capacity for all segments in a journey. Called synchronously by Journey Service. All-or-nothing: either every segment is reserved or none are.

**Headers:** `Content-Type: application/json`

**Request Body:**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "idempotency_key": "idk_x1y2z3",
  "vehicle_type": "car",
  "reservations": [
    {
      "segment_id": "seg_m50",
      "time_window_start": "2026-04-15T08:00:00Z",
      "time_window_end": "2026-04-15T08:25:00Z"
    },
    {
      "segment_id": "seg_m7n",
      "time_window_start": "2026-04-15T08:25:00Z",
      "time_window_end": "2026-04-15T09:10:00Z"
    },
    {
      "segment_id": "seg_m7s",
      "time_window_start": "2026-04-15T09:10:00Z",
      "time_window_end": "2026-04-15T09:40:00Z"
    },
    {
      "segment_id": "seg_m8",
      "time_window_start": "2026-04-15T09:40:00Z",
      "time_window_end": "2026-04-15T10:15:00Z"
    }
  ]
}
```

**Field notes:**
- `vehicle_type`: one of `car`, `van`, `motorcycle`, `truck`. Determines `slots_used` weight.
- `time_window_start` / `time_window_end`: the exact traversal window for that segment, computed by Journey Service using the cascading time window algorithm. Capacity Service stores these as-is and uses overlap-based checking.
- `idempotency_key`: if this key already exists in `capacity.idempotency_cache` (and is not expired), return the stored response without re-processing.

**Response (201 Created — all segments reserved):**
```json
{
  "status": "reserved",
  "reservation_id": "rsv_abc123",
  "journey_id": "jrn_a1b2c3d4",
  "slots_reserved": [
    {
      "segment_id": "seg_m50",
      "slots_used": 1.0,
      "time_window_start": "2026-04-15T08:00:00Z",
      "time_window_end": "2026-04-15T08:25:00Z"
    },
    {
      "segment_id": "seg_m7n",
      "slots_used": 1.0,
      "time_window_start": "2026-04-15T08:25:00Z",
      "time_window_end": "2026-04-15T09:10:00Z"
    }
  ]
}
```

**Response (200 OK — capacity unavailable, nothing reserved):**
```json
{
  "status": "failed",
  "failed_segment": {
    "segment_id": "seg_m7n",
    "reason": "at_capacity",
    "available_slots": 0.5,
    "requested_slots": 1.0,
    "time_window_start": "2026-04-15T08:25:00Z",
    "time_window_end": "2026-04-15T09:10:00Z"
  }
}
```

**`reason` values:**

| Value | Meaning |
|-------|---------|
| `at_capacity` | `available_slots < requested_slots` for this segment/window |
| `unknown_segment` | `segment_id` not found in `capacity.segments` |
| `invalid_time_window` | `time_window_end <= time_window_start` or `time_window_end` is in the past |

**Error Responses:**
- `400` — Malformed request (missing fields, invalid `vehicle_type`, empty `reservations` array)
- `500` — Database error

> **Atomicity guarantee:** The entire reservation executes in a single PostgreSQL transaction with `SELECT FOR UPDATE` on all segment rows. If any segment check fails, the transaction rolls back before any inserts occur.

---

### 4.2 GET /api/v1/capacity/check

Preview availability without reserving. Used by Journey Service for optional pre-checks.

**Query Parameters:**
- `segments`: comma-separated segment IDs
- `time_start`: ISO 8601 datetime
- `time_end`: ISO 8601 datetime
- `vehicle_type`: `car`, `van`, `motorcycle`, or `truck`

**Example:**
```
GET /api/v1/capacity/check?segments=seg_m50,seg_m7n&time_start=2026-04-15T08:00:00Z&time_end=2026-04-15T09:10:00Z&vehicle_type=car
```

**Response (200 OK):**
```json
{
  "available": true,
  "vehicle_type": "car",
  "requested_slots": 1.0,
  "segments": [
    {
      "segment_id": "seg_m50",
      "segment_name": "M50 Dublin Ring",
      "max_capacity": 100.0,
      "reserved_slots": 42.5,
      "available_slots": 57.5,
      "is_available": true,
      "time_window_start": "2026-04-15T08:00:00Z",
      "time_window_end": "2026-04-15T09:10:00Z"
    },
    {
      "segment_id": "seg_m7n",
      "segment_name": "M7 Naas to Portlaoise",
      "max_capacity": 80.0,
      "reserved_slots": 80.0,
      "available_slots": 0.0,
      "is_available": false,
      "time_window_start": "2026-04-15T08:00:00Z",
      "time_window_end": "2026-04-15T09:10:00Z"
    }
  ]
}
```

Top-level `available` is `true` only if all segments return `is_available: true`.

**Caching:** Results are served from Redis per segment/window (key: `cap:avail:{segment_id}:{time_start_unix}:{time_end_unix}`, TTL: 30 seconds). Cache is populated on PostgreSQL fallback and invalidated after any reservation or release on the same segment/window.

**Error Responses:**
- `400` — Missing or invalid query parameters
- `404` — One or more `segment_id` values not found

---

### 4.3 GET /api/v1/capacity/segments/occupancy

Returns occupancy data for all (or filtered) segments. Consumed by Map Service to populate the admin traffic map. Not called by the browser directly.

**Query Parameters (all optional):**
- `from`: ISO 8601 datetime (inclusive). Default: start of current 15-minute window.
- `to`: ISO 8601 datetime (exclusive). Default: end of current 15-minute window.
- `region`: filter by region (`north`, `south`, `east`, `west`, `central`)

Default behaviour (no params): returns occupancy for the current 15-minute window (floor of `NOW()` to the nearest 15 minutes).

**Examples:**
```
GET /api/v1/capacity/segments/occupancy
GET /api/v1/capacity/segments/occupancy?from=2026-04-15T08:00:00Z&to=2026-04-15T09:00:00Z
GET /api/v1/capacity/segments/occupancy?region=north
```

**Response (200 OK):**
```json
{
  "window_start": "2026-04-15T08:00:00Z",
  "window_end": "2026-04-15T08:15:00Z",
  "generated_at": "2026-04-15T08:07:32Z",
  "segments": [
    {
      "segment_id": "seg_m50",
      "segment_name": "M50 Dublin Ring",
      "region": "north",
      "max_capacity": 100.0,
      "reserved_slots": 78.5,
      "available_slots": 21.5,
      "occupancy_pct": 78.5,
      "level": "high",
      "trend": "worsening"
    },
    {
      "segment_id": "seg_m7n",
      "segment_name": "M7 Naas to Portlaoise",
      "region": "south",
      "max_capacity": 80.0,
      "reserved_slots": 12.0,
      "available_slots": 68.0,
      "occupancy_pct": 15.0,
      "level": "low",
      "trend": "stable"
    }
  ]
}
```

**`level` classification:**

| Level | Occupancy % |
|-------|------------|
| `low` | < 50% |
| `moderate` | 50% – 75% |
| `high` | 75% – 90% |
| `critical` | > 90% |

**`trend` computation:** Compare `occupancy_pct` in the current window vs. 15 minutes ago. `worsening` if +5pp or more, `improving` if −5pp or more, otherwise `stable`.

**Auth:** Requires admin JWT (`role: "admin"`). Map Service calls this using a service account JWT configured at startup.

---

### 4.4 GET /health

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

### 4.5 GET /ready

Returns `200 OK` only when both the PostgreSQL connection pool and Redis connection are established. Returns `503` during startup or after connection failure. Used by Docker Swarm to gate traffic routing.

---

## 5. What Capacity Service Provides to Other Services

### To Journey Service (S2, Ajinkya)

| Endpoint | Purpose |
|----------|---------|
| `POST /api/v1/capacity/reserve` | Atomic multi-segment reservation. All-or-nothing. Idempotent on `idempotency_key`. |
| `GET /api/v1/capacity/check` | Read-only availability preview. Cached. |

**Contract guarantees Capacity Service must uphold:**
- The reserve endpoint MUST be all-or-nothing. Journey Service MUST NOT have to implement rollback.
- On duplicate `idempotency_key`, MUST return the original response without creating a new reservation.
- The `failed_segment` object on rejection MUST include `available_slots`, `requested_slots`, and `reason` so Journey Service can construct a meaningful driver-facing error.
- `reservation_id` in the success response is stored by Journey Service in `journey.journey_segments.reservation_id` for later reconciliation.

### To Map Service (S4, Xiaoxuan)

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/capacity/segments/occupancy` | Per-segment occupancy for the admin traffic map. Map Service merges this with its own node topology before returning `GET /api/v1/map/traffic` to the frontend. |

---

## 6. What Capacity Service Needs from Other Services

### From Journey Service (S2) — via Redis Streams (async)

Capacity Service is a **consumer** on the `journey.events` Redis Streams stream, consumer group `capacity-service`. It acts on three event types:

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
    "cancelled_by": "driver",
    "reservation_id": "rsv_abc123"
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
    "status": "COMPLETED",
    "reservation_id": "rsv_abc123"
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
    "status": "EXPIRED",
    "reservation_id": "rsv_abc123"
  }
}
```

**Processing logic for all three:**
1. Find all rows in `capacity.reservations` where `journey_id = $journey_id AND status = 'active'`
2. If none found: log warning (never reserved, or already released) — no-op, XACK
3. `UPDATE ... SET status = 'released', released_at = NOW()`
4. Delete Redis availability cache keys for all affected segment/window combinations
5. XACK the message

Capacity Service uses `journey_id` as the primary release key (not `reservation_id`), since `journey_id` is always present and unambiguous even if `reservation_id` has not yet replicated.

### From IAM Service (S1) — no runtime calls

Capacity Service fetches JWKS public keys from IAM on startup and refreshes every hour. JWT validation for the occupancy endpoint's admin check is performed locally. No runtime REST call to IAM per request.

---

## 7. Database Schema

```sql
CREATE SCHEMA IF NOT EXISTS capacity;

-- Road segment definitions
-- Seeded by migration (002_seed_segments.sql). No runtime admin API for prototype.
CREATE TABLE capacity.segments (
    segment_id      VARCHAR(30) PRIMARY KEY,          -- e.g. "seg_m50"
    segment_name    VARCHAR(100) NOT NULL,
    region          VARCHAR(20) NOT NULL,              -- north, south, east, west, central
    max_capacity    DECIMAL(10,2) NOT NULL,            -- max concurrent slots (fractional)
    version         INTEGER NOT NULL DEFAULT 1,        -- optimistic lock for future admin updates
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_max_capacity CHECK (max_capacity > 0),
    CONSTRAINT chk_region CHECK (region IN ('north','south','east','west','central'))
);

-- Active and released reservations
-- One row per segment per journey. A 4-segment journey produces 4 rows,
-- all sharing the same reservation_id.
CREATE TABLE capacity.reservations (
    id                BIGSERIAL PRIMARY KEY,
    reservation_id    VARCHAR(30) NOT NULL,            -- "rsv_" + nanoid(10), same for all segments of a journey
    journey_id        VARCHAR(20) NOT NULL,
    segment_id        VARCHAR(30) NOT NULL REFERENCES capacity.segments(segment_id),
    time_window_start TIMESTAMPTZ NOT NULL,
    time_window_end   TIMESTAMPTZ NOT NULL,
    vehicle_type      VARCHAR(20) NOT NULL,
    slots_used        DECIMAL(10,2) NOT NULL,
    status            VARCHAR(10) NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at       TIMESTAMPTZ,

    CONSTRAINT chk_status      CHECK (status IN ('active', 'released')),
    CONSTRAINT chk_vehicle     CHECK (vehicle_type IN ('car','van','motorcycle','truck')),
    CONSTRAINT chk_slots_used  CHECK (slots_used > 0),
    CONSTRAINT chk_time_window CHECK (time_window_end > time_window_start)
);

-- Primary query: find overlapping active reservations for a segment/window
CREATE INDEX idx_reservations_segment_window
    ON capacity.reservations (segment_id, time_window_start, time_window_end)
    WHERE status = 'active';

-- Release lookup: find all active reservations for a journey
CREATE INDEX idx_reservations_journey
    ON capacity.reservations (journey_id)
    WHERE status = 'active';

-- Orphan cleanup: find stale active reservations by window end time
CREATE INDEX idx_reservations_orphan
    ON capacity.reservations (time_window_end)
    WHERE status = 'active';

-- Idempotency cache
-- Stores the full serialised response for replay on duplicate idempotency_key.
-- Replicates across VMs via PostgreSQL logical replication.
CREATE TABLE capacity.idempotency_cache (
    idempotency_key VARCHAR(64) PRIMARY KEY,
    journey_id      VARCHAR(20) NOT NULL,
    reservation_id  VARCHAR(30),
    response_status VARCHAR(10) NOT NULL,              -- "reserved" or "failed"
    response_body   JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours'
);

CREATE INDEX idx_idempotency_expires ON capacity.idempotency_cache (expires_at);
```

### Seed Data (002_seed_segments.sql)

```sql
INSERT INTO capacity.segments (segment_id, segment_name, region, max_capacity) VALUES
('seg_m50',     'M50 Dublin Ring Road',            'north',   100.00),
('seg_m1_n',    'M1 Northbound (City to Airport)', 'north',    80.00),
('seg_m1_s',    'M1 Southbound (Airport to City)', 'north',    80.00),
('seg_n11',     'N11 Stillorgan Road',              'south',    60.00),
('seg_m50_s',   'M50 South (Tallaght)',              'south',    90.00),
('seg_m7n',     'M7 Naas to Portlaoise',            'west',     70.00),
('seg_m7s',     'M7 Portlaoise to Cashel',          'south',    70.00),
('seg_m8',      'M8 Cashel to Cork',                'south',    65.00),
('seg_n4',      'N4 Lucan Bypass',                  'west',     75.00),
('seg_n3',      'N3 Navan Road',                    'north',    60.00),
('seg_m2',      'M2 Finglas Road',                  'north',    55.00),
('seg_n81',     'N81 Blessington Road',              'south',    45.00),
('seg_r132',    'R132 Dublin to Drogheda',           'north',    50.00),
('seg_port_n',  'Port Tunnel Northbound',             'central',  40.00),
('seg_port_s',  'Port Tunnel Southbound',             'central',  40.00),
('seg_n7',      'N7 Naas Road (City)',               'west',     85.00),
('seg_m4',      'M4 Leixlip to Kinnegad',            'west',     65.00),
('seg_n2',      'N2 Finglas to Ashbourne',           'north',    50.00),
('seg_quays_e', 'East Quays Corridor',               'central',  35.00),
('seg_quays_w', 'West Quays Corridor',               'central',  35.00);
```

---

## 8. Redis Usage

### 8.1 Availability Cache (Read Path Only)

Used exclusively by `GET /api/v1/capacity/check`. **The reserve endpoint always bypasses the cache and reads from PostgreSQL.**

**Key:** `cap:avail:{segment_id}:{time_start_unix}:{time_end_unix}`

**Example:** `cap:avail:seg_m50:1744704000:1744705500`

**Value (JSON string):**
```json
{
  "segment_id": "seg_m50",
  "max_capacity": 100.0,
  "reserved_slots": 42.5,
  "available_slots": 57.5,
  "computed_at": "2026-04-15T08:05:00Z"
}
```

**TTL:** 30 seconds

**Cache invalidation:** After any reservation commits or any slot release completes, `DEL` the cache keys for all affected segment/window combinations. This ensures the next check call sees fresh data within 30 seconds.

### 8.2 Redis Streams Consumer

**Stream:** `journey.events`
**Consumer group:** `capacity-service`
**Consumer name:** `${VM_ID}-capacity` (e.g., `vm-a-capacity`)

Consumer group creation at startup (idempotent):
```
XGROUP CREATE journey.events capacity-service $ MKSTREAM
```

`$` means start from new messages only. Unacknowledged messages from a previous crash are automatically re-delivered.

**Read loop:**
```
XREADGROUP GROUP capacity-service ${VM_ID}-capacity COUNT 10 BLOCK 5000 STREAMS journey.events >
```

**Processing per message:**
1. Decode event envelope; check `event_type`
2. If not `journey.cancelled`, `journey.completed`, or `journey.expired`: XACK and skip
3. Extract `journey_id` from payload
4. `UPDATE capacity.reservations SET status='released', released_at=NOW() WHERE journey_id=$1 AND status='active'`
5. If `rows_affected = 0`: no-op (never reserved or already released — log a warning)
6. DEL Redis cache keys for released segment/window combinations
7. XACK

**Retry on DB failure:**
- Do NOT XACK on failure
- Exponential backoff: 1s, 2s, 4s (max 3 retries)
- After 3 failures: XACK and log `level=error` with `event_id`, `journey_id`, `reason` — prevents a single bad message from blocking all subsequent ones
- Pending message recovery on restart: `XAUTOCLAIM journey.events capacity-service ${VM_ID}-capacity 60000 0-0 COUNT 100`

---

## 9. Reservation Logic (Core Algorithm)

### 9.1 Capacity Check for a Single Segment

```sql
-- Is there room for $slots_needed more slots on $segment_id
-- during the window [$window_start, $window_end)?
SELECT
    s.max_capacity,
    COALESCE(SUM(r.slots_used), 0.0) AS currently_reserved
FROM capacity.segments s
LEFT JOIN capacity.reservations r
    ON  r.segment_id = s.segment_id
    AND r.status = 'active'
    AND r.time_window_start < $window_end    -- overlap: existing starts before new ends
    AND r.time_window_end   > $window_start  -- overlap: existing ends after new starts
WHERE s.segment_id = $segment_id
FOR UPDATE                                   -- lock the segment row
GROUP BY s.max_capacity;

-- available = max_capacity - currently_reserved
-- Can reserve if: available >= $slots_needed
```

### 9.2 Atomic Multi-Segment Reserve Transaction

```
BEGIN;

1. Look up $idempotency_key in capacity.idempotency_cache.
   If found and not expired: ROLLBACK; return cached response_body.

2. Determine slots_needed from vehicle_type:
   car=1.0, van=1.5, motorcycle=0.5, truck=3.0

3. Validate all segment_ids exist in capacity.segments (single IN query, no lock yet).
   If any missing: ROLLBACK; return failed_segment with reason="unknown_segment".

4. SELECT segment_id, max_capacity FROM capacity.segments
   WHERE segment_id IN (all requested segment_ids)
   ORDER BY segment_id ASC   -- consistent sort order → prevents deadlock
   FOR UPDATE;

5. For each segment (iterate in sorted order):
   a. Execute the overlap SUM query (§9.1)
   b. If currently_reserved + slots_needed > max_capacity:
      → ROLLBACK
      → INSERT idempotency_cache with response_status='failed' and the failed_segment body
      → Return 200 with {status:"failed", failed_segment:{...}}

6. All segments pass. Generate reservation_id = "rsv_" + nanoid(10).

7. INSERT INTO capacity.reservations (one row per segment):
   (reservation_id, journey_id, segment_id, time_window_start, time_window_end,
    vehicle_type, slots_used, status='active', created_at=NOW())

8. INSERT INTO capacity.idempotency_cache:
   (idempotency_key, journey_id, reservation_id, response_status='reserved',
    response_body=<JSON success response>, expires_at=NOW()+24h)

COMMIT;

9. DEL Redis cache keys for all reserved segment/windows.
10. Return 201 {status:"reserved", reservation_id:..., journey_id:...}
```

---

## 10. Edge Cases

| # | Scenario | Risk | Mitigation |
|---|----------|------|------------|
| E1 | Two concurrent reserve calls on the same VM for the last available slot on `seg_m50` | Both read `available > 0`, both insert → over-capacity | `SELECT FOR UPDATE` on `capacity.segments` serializes both transactions. The second blocks until the first commits, then re-reads the true `reserved_slots` and correctly sees `available = 0`. Returns `at_capacity`. |
| E2 | Two VMs simultaneously reserve the last slot on the same segment | Both VMs hold `SELECT FOR UPDATE` on their local copy of the segment row at the same time | PostgreSQL row locking is per-instance. Both VMs can acquire the lock locally and commit. After replication converges, combined reservations may exceed `max_capacity` by one booking. Probability is negligible: replication lag is ~100ms and bookings require 1-hour advance. Post-replication, the orphan cleanup job and normal slot release will restore consistency. |
| E3 | Journey Service retries `POST /capacity/reserve` with the same `idempotency_key` after a timeout | Double-booking | Step 1 of the transaction checks `capacity.idempotency_cache`. If found: return stored response, no new inserts. The PostgreSQL `UNIQUE` constraint on `idempotency_key` catches any race between the check and a concurrent first-time insert. |
| E4 | Retry hits a different VM before replication propagates the idempotency record | ~100ms window where VM B hasn't seen VM A's idempotency insert yet | VM B's transaction tries to INSERT the same `idempotency_key`. The `UNIQUE` constraint raises a conflict. Application catches it, queries for the existing record by key, and returns the cached response. |
| E5 | `journey.cancelled` event consumed twice (duplicate delivery after consumer crash) | Double-release attempt | `UPDATE WHERE status='active'` is idempotent. Second update matches 0 rows — no-op. Log a warning. |
| E6 | Release event for a journey that was REJECTED (never had slots reserved) | Lookup returns 0 rows | No-op. XACK. Log warning with `journey_id`. |
| E7 | Redis consumer crashes after DB commit but before XACK | Message re-delivered on restart | Release is idempotent (see E5). Re-processing is safe. |
| E8 | Two reservations with partially overlapping windows on the same segment (08:00–08:25 and 08:10–08:35) | Both occupy the segment concurrently in the 08:10–08:25 overlap | The overlap condition `existing_start < new_end AND existing_end > new_start` correctly detects any overlap. Both reservations count against concurrent capacity. Correct behaviour. |
| E9 | Journey Service crashes after `/capacity/reserve` returns 201, before writing journey to PostgreSQL | Slots reserved but no journey record exists. Release event never published. | Orphan cleanup job (§11.1) runs every 30 minutes. Finds reservations where `status='active'` and `time_window_end < NOW() - 1 hour`. Releases them. |
| E10 | Segment `max_capacity` is 0 (hypothetical seeding error) | All reservations fail with `at_capacity` | `0.0 + slots_needed > 0.0` is always true. Correct behaviour. The `CHECK (max_capacity > 0)` constraint prevents this from being inserted via migration. |
| E11 | All 20 segments needed in one reserve call | Long-held transaction locking 20 rows | 20 row locks held for ~5ms. Acceptable. Consistent sort order prevents deadlock with other concurrent transactions. |
| E12 | `idempotency_key` reused with a different `journey_id` | Caller gets a cached response for the wrong journey | Return the original cached response. Log a warning with both the cached `journey_id` and the requested `journey_id`. Idempotency contract: a key is permanently tied to the first operation it identified. |
| E13 | `time_window_end` is in the past (departure time slipped past the booking validation window) | Stale reservation for a time slot that has already elapsed | Capacity Service validates `time_window_end > NOW()` on reserve. Returns `400 Bad Request`. Defence-in-depth against Journey Service bugs — the 1-hour advance booking rule should already prevent this. |
| E14 | Admin force-cancel of an ACTIVE journey (driver is physically on the road) | Slots released while journey is in progress | Correct behaviour per spec. `journey.cancelled` event is published; Capacity Service releases slots. The driver's physical journey is unaffected; only the system record is updated. |
| E15 | Very high load: hundreds of concurrent bookings for the same segment/window | All transactions queue on `SELECT FOR UPDATE` → latency spike | Acceptable for a prototype. Production mitigation: segment-level sharding or atomic slot counter via Redis Lua script. Out of scope for this iteration. |

---

## 11. Background Jobs

### 11.1 Orphaned Reservation Cleanup

**Frequency:** Every 30 minutes (configurable: `ORPHAN_CLEANUP_INTERVAL_MINUTES`)

**Purpose:** Release reservations whose traversal windows have fully elapsed and no release event was received. Caused by Journey Service crash between the Capacity reserve call and the journey DB commit, or by missed Redis Streams events.

```sql
UPDATE capacity.reservations
SET status = 'released', released_at = NOW()
WHERE status = 'active'
  AND time_window_end < NOW() - INTERVAL '1 hour'
RETURNING segment_id, time_window_start, time_window_end;
```

The 1-hour threshold ensures in-flight journeys are not prematurely released.

Post-update: delete Redis cache keys for all returned `segment_id`/window combinations.

### 11.2 Idempotency Cache Cleanup

**Frequency:** Every hour

```sql
DELETE FROM capacity.idempotency_cache WHERE expires_at < NOW();
```

---

## 12. Multi-VM Behavior

### 12.1 Request Flow (Booking)

```
1. Driver books City Centre → Airport, departing 08:00

2. Load balancer routes request to VM B

3. VM B's Journey Service:
   a. Validates JWT locally (cached JWKS)
   b. Gets route from VM B's Map Service
   c. Computes cascading time windows
   d. Calls VM B's Capacity Service: POST /api/v1/capacity/reserve

4. VM B's Capacity Service:
   a. Checks idempotency_cache (miss — first attempt)
   b. Validates all segment_ids exist
   c. SELECT … FOR UPDATE on segment rows (sorted order)
   d. Checks available capacity for each segment/window (overlap queries)
   e. All pass → INSERT reservations → INSERT idempotency_cache → COMMIT
   f. Returns {status: "reserved"} to Journey Service

5. VM B's Journey Service writes journey to VM B's PostgreSQL, returns 201 to driver

6. PostgreSQL logical replication propagates within ~100ms:
   - capacity.reservations rows → VM A, VM C
   - capacity.idempotency_cache row → VM A, VM C
   - journey.journeys row → VM A, VM C

7. VM B publishes journey.booked to VM B's Redis Streams (async)
```

### 12.2 Request Flow (Cancellation on a Different VM)

```
1. Driver cancels from a new session routed to VM A

2. VM A's Journey Service handles cancellation:
   - Marks journey CANCELLED in VM A's PostgreSQL
   - Publishes journey.cancelled to VM A's Redis Streams

3. VM A's Capacity Service consumer goroutine:
   - Consumes journey.cancelled event
   - Looks up reservations by journey_id
     (rows present on VM A via replication from booking on VM B)
   - Releases slots on VM A's PostgreSQL
   - DELs Redis cache keys

4. Replication propagates the release to VM B and VM C
```

**Key insight:** Reservation rows are present on ALL VMs via replication. Whichever VM handles the cancellation releases slots on its local PostgreSQL; replication propagates the release everywhere.

### 12.3 Multi-Master Conflict Resolution

| Conflict type | Resolution |
|---------------|-----------|
| Concurrent INSERT of same `idempotency_key` from two VMs | `UNIQUE` constraint rejects the second insert. Application catches the violation, queries for the existing record, returns the cached response. |
| Concurrent release of the same reservation from two consumers | `UPDATE WHERE status='active'` is idempotent. Second update matches 0 rows. No-op. |
| Over-capacity during network partition | Both VMs commit overlapping reservations. On partition heal, replication inserts both rows. Temporary over-subscription. Self-corrects via normal slot release and orphan cleanup. Probability is extremely low given SELECT FOR UPDATE and 1-hour advance booking window. |

### 12.4 Replication Monitoring

```sql
-- On each VM — check WAL lag to all subscribers
SELECT
    application_name,
    state,
    (sent_lsn - replay_lsn) AS replication_lag_bytes
FROM pg_stat_replication;
```

Alert threshold: lag > 1 second. The 30-second availability cache TTL provides a buffer — stale capacity data is visible for at most 30 seconds even at high replication lag.

---

## 13. Configuration (Environment Variables)

```bash
# Server
PORT=8081
ENV=production
LOG_LEVEL=info
VM_ID=vm-a                              # Used in logs and consumer name

# PostgreSQL (local instance on this VM)
DB_HOST=localhost
DB_PORT=5432
DB_NAME=trafficservice
DB_SCHEMA=capacity
DB_USER=capacity_svc
DB_PASSWORD=<secret>
DB_MAX_CONNS=20
DB_IDLE_CONNS=5
DB_CONN_TIMEOUT_SECONDS=5

# Redis (local instance on this VM)
REDIS_HOST=localhost:6379
REDIS_PASSWORD=<secret>
REDIS_DB=0
REDIS_CONNECT_TIMEOUT_SECONDS=5

# IAM JWKS (for admin endpoint auth — no runtime calls per request)
JWKS_URL=http://iam-service:8082/api/v1/auth/.well-known/jwks.json
JWKS_REFRESH_INTERVAL_SECONDS=3600

# Capacity logic
AVAILABILITY_CACHE_TTL_SECONDS=30       # Redis TTL for GET /capacity/check cache
IDEMPOTENCY_TTL_HOURS=24                # How long to keep idempotency records

# Redis Streams
STREAM_NAME=journey.events
CONSUMER_GROUP=capacity-service
CONSUMER_NAME=${VM_ID}-capacity         # e.g. "vm-a-capacity" — unique per VM
STREAM_READ_COUNT=10                    # Messages per XREADGROUP call
STREAM_BLOCK_MS=5000                    # XREADGROUP BLOCK wait time

# Background jobs
ORPHAN_CLEANUP_INTERVAL_MINUTES=30
ORPHAN_THRESHOLD_HOURS=1
IDEMPOTENCY_CLEANUP_INTERVAL_HOURS=1
```

---

## 14. Project Structure

```
capacity-service/
├── cmd/
│   └── server/
│       └── main.go                      # Entry point: wires all components, starts HTTP + consumer goroutine
│
├── internal/
│   ├── handler/
│   │   ├── capacity_handler.go          # POST /reserve, GET /check
│   │   ├── occupancy_handler.go         # GET /segments/occupancy
│   │   └── health_handler.go            # GET /health, GET /ready (already exists)
│   │
│   ├── middleware/
│   │   ├── auth.go                      # JWT validation (cached JWKS, admin role check for occupancy)
│   │   └── logging.go                   # Request logging with X-Trace-ID
│   │
│   ├── model/
│   │   ├── segment.go                   # Segment struct, level constants
│   │   ├── reservation.go               # Reservation struct, status constants
│   │   └── vehicle.go                   # VehicleType enum + SlotWeights map
│   │
│   ├── service/
│   │   ├── reservation_service.go       # Core: Reserve(), CheckAvailability(), Release()
│   │   └── cleanup.go                   # Background jobs: orphan cleanup, idempotency cleanup
│   │
│   ├── repository/
│   │   ├── segment_repo.go              # GetSegmentsForUpdate(), GetAll()
│   │   ├── reservation_repo.go          # Insert(), Release(), GetOverlappingSlots(), GetOrphaned()
│   │   └── idempotency_repo.go          # GetByKey(), Insert(), DeleteExpired()
│   │
│   └── event/
│       └── consumer.go                  # Redis Streams XREADGROUP loop + release logic
│
├── migrations/
│   ├── 001_create_schema.sql            # CREATE SCHEMA capacity; all tables and indexes
│   └── 002_seed_segments.sql            # INSERT 20 Dublin road segments
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

**Vehicle weight lookup (`internal/model/vehicle.go`):**
```go
type VehicleType string

const (
    VehicleCar        VehicleType = "car"
    VehicleVan        VehicleType = "van"
    VehicleMotorcycle VehicleType = "motorcycle"
    VehicleTruck      VehicleType = "truck"
)

var SlotWeights = map[VehicleType]float64{
    VehicleCar:        1.0,
    VehicleVan:        1.5,
    VehicleMotorcycle: 0.5,
    VehicleTruck:      3.0,
}
```

`slots_used` is resolved once in `reservation_service.go` and passed to all repository calls. The repository layer only works with `float64` slot counts — it has no knowledge of vehicle types.

---

## 15. Sequence Diagrams

### 15.1 Successful Atomic Reservation

All actors run on the **same VM**.

```
Journey Svc      Capacity Svc     PostgreSQL        Redis
    │                  │               │               │
    │  POST /reserve   │               │               │
    ├─────────────────►│               │               │
    │                  │               │               │
    │                  │  1. Check idempotency_cache   │
    │                  ├──────────────►│               │
    │                  │◄──────────────┤               │
    │                  │  miss         │               │
    │                  │               │               │
    │                  │  2. Validate segment_ids exist │
    │                  ├──────────────►│               │
    │                  │◄──────────────┤               │
    │                  │  all found    │               │
    │                  │               │               │
    │                  │  3. BEGIN TRANSACTION          │
    │                  │               │               │
    │                  │  4. SELECT … FOR UPDATE (sorted segment_ids)
    │                  ├──────────────►│               │
    │                  │◄──────────────┤               │
    │                  │  rows locked  │               │
    │                  │               │               │
    │                  │  5. SUM overlap slots per segment (all pass)
    │                  ├──────────────►│               │
    │                  │◄──────────────┤               │
    │                  │               │               │
    │                  │  6. INSERT reservations (one row per segment)
    │                  │  7. INSERT idempotency_cache   │
    │                  │  8. COMMIT    │               │
    │                  ├──────────────►│               │
    │                  │◄──────────────┤               │
    │                  │               │               │
    │                  │  9. DEL availability cache keys
    │                  ├───────────────────────────────►│
    │                  │                               │
    │  201 reserved    │               │               │
    │◄─────────────────┤               │               │
```

### 15.2 Failed Reservation (At Capacity)

```
Journey Svc      Capacity Svc     PostgreSQL
    │                  │               │
    │  POST /reserve   │               │
    ├─────────────────►│               │
    │                  │               │
    │                  │  1. Check idempotency (miss)
    │                  ├──────────────►│
    │                  │◄──────────────┤
    │                  │               │
    │                  │  2. BEGIN TRANSACTION
    │                  │  3. SELECT … FOR UPDATE (sorted)
    │                  ├──────────────►│
    │                  │◄──────────────┤
    │                  │               │
    │                  │  4. SUM overlap slots for seg_m50
    │                  │     100.0 + 1.0 > 100.0 → FAIL
    │                  │               │
    │                  │  5. ROLLBACK (no reservation inserts)
    │                  ├──────────────►│
    │                  │               │
    │                  │  6. INSERT idempotency_cache (status="failed")
    │                  ├──────────────►│
    │                  │◄──────────────┤
    │  200 failed      │               │
    │◄─────────────────┤               │
    │  {failed_segment:│               │
    │   seg_m50,       │               │
    │   at_capacity,   │               │
    │   available:0.0} │               │
```

### 15.3 Async Slot Release via Redis Streams

```
Redis Streams    Capacity Svc     PostgreSQL        Redis Cache
    │                  │               │               │
    │  XREADGROUP      │               │               │
    │  journey.cancelled               │               │
    ├─────────────────►│               │               │
    │                  │               │               │
    │                  │  1. Extract journey_id from payload
    │                  │               │               │
    │                  │  2. UPDATE reservations        │
    │                  │     SET status='released'      │
    │                  │     WHERE journey_id=...       │
    │                  │     AND status='active'        │
    │                  ├──────────────►│               │
    │                  │◄──────────────┤               │
    │                  │  N rows updated               │
    │                  │               │               │
    │                  │  3. DEL cache keys for released windows
    │                  ├───────────────────────────────►│
    │                  │◄───────────────────────────────┤
    │                  │               │               │
    │  XACK            │               │               │
    │◄─────────────────┤               │               │
```

---

## 16. Frontend Integration

**Capacity Service has NO direct frontend API calls.** The browser never contacts port 8081. All driver interactions go through Journey Service; admin map data flows through Map Service.

### 16.1 Indirect Data Flow to Admin Traffic Map

```
Browser (admin /admin/map)
  │  GET /api/v1/map/traffic
  ▼
Map Service :8084  (same VM)
  │  GET /api/v1/capacity/segments/occupancy
  ▼
Capacity Service :8081  (same VM)
  │  queries capacity.reservations for current window
  ▼
PostgreSQL
```

Map Service merges Capacity Service's `occupancy_pct`, `level`, and `trend` with its own node topology (`from_node`, `to_node`, `x`, `y` for SVG positions) before returning `GET /api/v1/map/traffic` to the browser.

**The frontend's `TrafficMapPage.tsx` colour-codes segments using:**
- `level: "low"` → green
- `level: "moderate"` → orange
- `level: "high"` → red
- `level: "critical"` → dark red

Capacity Service's level thresholds (§4.3) must match these four values exactly. The admin alert bar in `TrafficMapPage` fires when any segment is `high` or `critical`.

### 16.2 Vehicle Type Alignment

The frontend sends vehicle types in title case. Journey Service normalises them before calling Capacity Service. **Capacity Service only ever receives lowercase.**

| Frontend | Journey Service normalises | Capacity Service `vehicle_type` | `slots_used` |
|----------|---------------------------|----------------------------------|-------------|
| `"Car"` | `"car"` | `"car"` | 1.0 |
| `"Van"` | `"van"` | `"van"` | 1.5 |
| `"Motorcycle"` | `"motorcycle"` | `"motorcycle"` | 0.5 |
| `"HGV"` | `"truck"` | `"truck"` | 3.0 |

### 16.3 CORS

Capacity Service does not need browser-origin CORS headers. It is never called cross-origin from the browser. The existing `CORSMiddleware` in the skeleton (`Access-Control-Allow-Origin: *`) can remain as-is.

---

## 17. Interface Contract Summary (for Journey Service)

Journey Service (already specced) expects exactly these shapes. Capacity Service must not deviate:

**Reserve — success:**
```
HTTP 201
{ "status": "reserved", "reservation_id": "rsv_...", "journey_id": "jrn_..." }
```

**Reserve — failure:**
```
HTTP 200   ← NOT 4xx. Journey Service reads the body to determine outcome.
{
  "status": "failed",
  "failed_segment": {
    "segment_id": "...",
    "reason": "at_capacity" | "unknown_segment" | "invalid_time_window",
    "available_slots": <number>,
    "requested_slots": <number>,
    "time_window_start": "...",
    "time_window_end": "..."
  }
}
```

Journey Service stores the `reservation_id` from the success response in `journey.journey_segments.reservation_id`. It passes this back in event payloads for convenience, but Capacity Service uses `journey_id` as the primary release key.

---

*Last updated: 2026-04-03*
