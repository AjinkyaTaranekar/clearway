# Journey Service (S2) - Complete Specification

> **Owner:** Ajinkya Taranekar
> **Language:** Go 1.22+ (gorilla/mux)
> **Database:** PostgreSQL 16 (own schema: `journey`)
> **Status:** Planning Phase

---

## 1. Purpose

The Journey Service is the central orchestrator of the Distributed Traffic Service. It owns the entire booking lifecycle: receiving driver requests, coordinating with Map and Capacity services to validate and reserve road segments, managing journey state transitions, and publishing events for downstream consumers.

In a multi-VM deployment, all VMs run identical stacks behind a load balancer. Journey Service on each VM coordinates with its co-located Capacity and Map services — no cross-VM service calls are needed. Data consistency across VMs is maintained by PostgreSQL multi-master logical replication.

---

## 2. Responsibilities

The Journey Service is responsible for:

- Receiving and validating journey booking requests from drivers
- Computing cascading time windows per segment based on cumulative traversal time
- Orchestrating capacity reservation with the co-located Capacity Service (all-or-nothing, atomic)
- Managing the journey state machine (PENDING → APPROVED → ACTIVE → COMPLETED)
- Enforcing business rules (1-hour advance booking, 30-minute cancellation window, one active journey per driver)
- Caching route data from Map Service (24-hour TTL)
- Publishing journey lifecycle events to Redis Streams
- Exposing admin endpoints for force-cancel and journey listing
- Running background expiry for unused approved journeys

---

## 3. Architecture Context

### 3.1 Where Journey Service Sits

The system is deployed as **N identical VMs behind a load balancer**. Every VM runs all 5 services, its own PostgreSQL instance, and its own Redis instance. The load balancer distributes driver requests across VMs. PostgreSQL multi-master replication keeps all VM databases in sync.

```
                    ┌─────────────────────────────────────┐
                    │         Load Balancer               │
                    │    (Nginx / AWS ALB)                 │
                    └──────────────┬──────────────────────┘
                                   │ HTTP (round-robin or least-conn)
               ┌───────────────────┼───────────────────┐
               ▼                   ▼                   ▼
    ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
    │      VM A        │ │      VM B        │ │      VM C        │
    │  ─────────────── │ │  ─────────────── │ │  ─────────────── │
    │  IAM Service     │ │  IAM Service     │ │  IAM Service     │
    │  Journey Service │ │  Journey Service │ │  Journey Service │
    │  Capacity Service│ │  Capacity Service│ │  Capacity Service│
    │  Map Service     │ │  Map Service     │ │  Map Service     │
    │  Notification Svc│ │  Notification Svc│ │  Notification Svc│
    │  ─────────────── │ │  ─────────────── │ │  ─────────────── │
    │  PostgreSQL      │ │  PostgreSQL      │ │  PostgreSQL      │
    │  Redis           │ │  Redis           │ │  Redis           │
    └────────┬─────────┘ └────────┬─────────┘ └────────┬─────────┘
             │                    │                    │
             └─────────── Multi-Master PostgreSQL ─────┘
                          Logical Replication
                          (each VM publishes ↔ subscribes to all others)
```

Within a single VM, Journey Service calls Capacity Service and Map Service over the local Docker network (no cross-VM service calls needed):

```
  Driver ──► Load Balancer ──► (any VM) ──► Journey Service
                                                  │
                                  ┌───────────────┼──────────────────┐
                                  ▼               ▼                  ▼
                           Capacity Svc      Map Svc         Redis Streams
                           (same VM)        (same VM)        (same VM)
                                  │
                                  ▼
                           Notification Svc
                           (same VM, async)
```

### 3.2 Communication Pattern Summary

| From → To | Protocol | Sync/Async | Why |
|-----------|----------|------------|-----|
| Journey → IAM/Auth | Local JWT validation | Sync | Driver is waiting. Must validate before any work. No network call needed — uses cached JWKS public keys. |
| Journey → Map Service | REST (GET) | Sync | Journey Service needs the segment list before it can compute time windows or call Capacity. Driver is waiting. |
| Journey → Capacity Service | REST (POST) | Sync | Must confirm segment availability before responding to driver. All-or-nothing reservation. Both services run on the same VM. |
| Journey → Redis Streams | Redis XADD | Async | Publish event AFTER responding to driver. Notification delivery and analytics are not on the critical path. |
| Journey → PostgreSQL | SQL | Sync | Journey record persistence is on the critical path. |
| Journey → Redis Cache | GET/SET | Sync | Route cache lookup. Fast (sub-ms). |
| PostgreSQL VM-A ↔ VM-B ↔ VM-C | Logical Replication | Async | Multi-master sync. Writes on any VM propagate to all others within milliseconds. |

### 3.3 Why These Choices

**Auth validation is local, not a REST call.**
Journey Service holds the JWKS public keys in memory (refreshed every hour from IAM Service). JWT validation is a local cryptographic operation taking microseconds. No network hop. This removes IAM Service as a runtime dependency. If IAM is down, existing tokens still validate. Only new logins are affected.

**Map Service call is sync because it's blocking.**
Journey Service cannot compute time windows without knowing the segments. There's no useful work to do in parallel. Cache hit rate will be high for popular routes (Dublin→Cork is always the same segments), so most calls resolve from Redis cache in under 1ms.

**Capacity reservation is sync because the driver needs an answer.**
The driver is staring at a loading spinner. They need to know: approved or rejected. Making this async would mean "we'll notify you later," which is a worse user experience and adds complexity (pending state, callback mechanism, timeout handling).

**Notifications are async because the driver already has the answer.**
The booking response (200 OK with status: APPROVED) goes back in the HTTP response. The push notification via Firebase is a secondary channel. If it arrives 2 seconds later, nobody notices. If it fails entirely, the driver can still see their booking status in the app.

**Cancellation capacity release is async.**
When a driver cancels, Journey Service marks the journey as CANCELLED in PostgreSQL, responds 200 OK to the driver immediately, then publishes a journey.cancelled event. Capacity Service consumes this event and releases slots. The 1-hour advance booking requirement means this brief inconsistency window (seconds) is harmless. No other driver could book those slots and depart within seconds.

**Multi-master PostgreSQL replication is async but near-real-time.**
A write committed on VM A (e.g., a new journey booking) is replicated to VM B and VM C via PostgreSQL logical replication within milliseconds. All VMs converge to the same state. A driver whose request hits VM B immediately after VM A committed their booking will see the record within the replication lag window (typically < 100ms on a LAN). This is acceptable — the driver already received the booking confirmation in the HTTP response.

---

## 4. Journey State Machine

```
    ┌──────────────────────────────────────────────────────────────────┐
    │                                                                  │
    │   ┌─────────┐    all segments    ┌──────────┐    driver taps    │
    │   │         │    reserved        │          │    "start"        │
    │   │ PENDING ├───────────────────►│ APPROVED ├──────────────────►│
    │   │         │                    │          │                    │
    │   └────┬────┘                    └────┬─────┘                   │
    │        │                              │                          │
    │        │ capacity                     │ driver cancels           │
    │        │ unavailable                  │ (30min+ before dept)     │
    │        │                              │ OR admin force-cancel    │
    │        ▼                              ▼                          │
    │   ┌──────────┐                  ┌───────────┐                   │
    │   │          │                  │           │                    │
    │   │ REJECTED │                  │ CANCELLED │                    │
    │   │          │                  │           │                    │
    │   └──────────┘                  └───────────┘                   │
    │                                                                  │
    │                                                                  │
    │   ┌──────────┐    reaches        ┌───────────┐                  │
    │   │          │    destination     │           │                  │
    │   │  ACTIVE  ├──────────────────►│ COMPLETED │                  │
    │   │          │                    │           │                  │
    │   └──────────┘                    └───────────┘                  │
    │                                                                  │
    │                                                                  │
    │   ┌──────────┐                                                   │
    │   │          │  departure time passed, driver never activated     │
    │   │ EXPIRED  │  (background job, async)                          │
    │   │          │                                                    │
    │   └──────────┘                                                   │
    │                                                                  │
    └──────────────────────────────────────────────────────────────────┘
```

### State Transition Rules

| From | To | Trigger | Validation |
|------|----|---------|------------|
| (new) | PENDING | Driver submits booking | Departure >= now + 1hr. No existing APPROVED/ACTIVE journey for this driver. Valid JWT. |
| PENDING | APPROVED | All segments reserved | Capacity Service confirms all segments (local + remote) reserved successfully. |
| PENDING | REJECTED | Capacity unavailable | One or more segments at capacity. All previously reserved segments rolled back. |
| APPROVED | ACTIVE | Driver taps "Start Journey" | Current time >= departure_time. Current time <= departure_time + 30min (grace window). |
| APPROVED | CANCELLED | Driver cancels | departure_time - now > 30 minutes. |
| APPROVED | CANCELLED | Admin force-cancels | No time restriction. Admin role required. |
| APPROVED | EXPIRED | Background job | Current time > departure_time + 30min. Driver never activated. |
| ACTIVE | COMPLETED | Driver taps "Complete" OR estimated arrival time reached | Journey must be in ACTIVE state. |
| REJECTED | (terminal) | - | No further transitions. |
| CANCELLED | (terminal) | - | No further transitions. |
| COMPLETED | (terminal) | - | No further transitions. |
| EXPIRED | (terminal) | - | No further transitions. |

---

## 5. API Contract

### 5.1 Driver Endpoints

#### POST /api/v1/journeys
Create a new journey booking.

**Headers:** `Authorization: Bearer <jwt>`, `Idempotency-Key: <uuid>`

**Request Body:**
```json
{
  "origin": { "lat": 53.3498, "lng": -6.2603 },
  "destination": { "lat": 51.8985, "lng": -8.4756 },
  "departure_time": "2026-04-15T08:00:00Z",
  "vehicle_type": "car"
}
```

**Response (201 Created):**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "status": "APPROVED",
  "departure_time": "2026-04-15T08:00:00Z",
  "estimated_arrival": "2026-04-15T10:15:00Z",
  "vehicle_type": "car",
  "segments": [
    {
      "segment_id": "seg_m50",
      "segment_name": "M50 Dublin Ring",
      "time_window_start": "2026-04-15T08:00:00Z",
      "time_window_end": "2026-04-15T08:25:00Z",
      "region": "ireland"
    },
    {
      "segment_id": "seg_m7n",
      "segment_name": "M7 Naas to Portlaoise",
      "time_window_start": "2026-04-15T08:25:00Z",
      "time_window_end": "2026-04-15T09:10:00Z",
      "region": "ireland"
    }
  ]
}
```

**Response (200 OK, rejected):**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "status": "REJECTED",
  "reason": "Segment M7 Naas to Portlaoise is at capacity between 08:25 and 09:10",
  "failed_segment": {
    "segment_id": "seg_m7n",
    "time_window_start": "2026-04-15T08:25:00Z",
    "time_window_end": "2026-04-15T09:10:00Z"
  }
}
```

**Error Responses:**
- `400` - Invalid request (missing fields, invalid coordinates, vehicle_type not recognized)
- `401` - Invalid or expired JWT
- `409` - Driver already has an APPROVED or ACTIVE journey
- `422` - Departure time less than 1 hour from now
- `503` - Map Service or Capacity Service unavailable (Retry-After header included)

---

#### GET /api/v1/journeys/:id
Get journey details and current status.

**Response (200 OK):**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "status": "APPROVED",
  "origin": { "lat": 53.3498, "lng": -6.2603 },
  "destination": { "lat": 51.8985, "lng": -8.4756 },
  "departure_time": "2026-04-15T08:00:00Z",
  "estimated_arrival": "2026-04-15T10:15:00Z",
  "vehicle_type": "car",
  "segments": [...],
  "created_at": "2026-04-14T20:30:00Z",
  "updated_at": "2026-04-14T20:30:01Z"
}
```

---

#### GET /api/v1/journeys
List driver's journeys (paginated).

**Query Params:** `status` (filter), `page` (default 1), `limit` (default 20, max 100)

**Response (200 OK):**
```json
{
  "journeys": [...],
  "total": 45,
  "page": 1,
  "limit": 20
}
```

---

#### PUT /api/v1/journeys/:id/cancel
Cancel an approved journey.

**Response (200 OK):**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "status": "CANCELLED",
  "cancelled_at": "2026-04-14T21:00:00Z"
}
```

**Error Responses:**
- `400` - Journey not in APPROVED state
- `403` - Less than 30 minutes before departure
- `404` - Journey not found or not owned by this driver

---

#### PUT /api/v1/journeys/:id/activate
Start the journey (APPROVED → ACTIVE).

**Response (200 OK):**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "status": "ACTIVE",
  "activated_at": "2026-04-15T08:02:00Z"
}
```

**Error Responses:**
- `400` - Journey not in APPROVED state
- `403` - Too early (before departure_time) or too late (30min+ after departure_time)

---

#### PUT /api/v1/journeys/:id/complete
Complete the journey (ACTIVE → COMPLETED).

**Response (200 OK):**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "status": "COMPLETED",
  "completed_at": "2026-04-15T10:20:00Z"
}
```

---

### 5.2 Admin Endpoints (consumed by S5 Frontend+Admin Service)

#### GET /api/v1/admin/journeys
List all journeys across all drivers. Filterable by status, driver_id, region, date range.

**Query Params:** `status`, `driver_id`, `region`, `from_date`, `to_date`, `page`, `limit`

---

#### PUT /api/v1/admin/journeys/:id/cancel
Force-cancel any journey. No 30-minute restriction. Requires admin role in JWT.

---

### 5.3 Internal Endpoints (health + metrics)

#### GET /health
Returns service health status. Used by Docker Swarm health checks.

```json
{
  "status": "healthy",
  "db": "connected",
  "redis": "connected",
  "uptime_seconds": 86400
}
```

#### GET /ready
Readiness probe. Returns 200 only when DB and Redis connections are established.

---

## 6. Dependencies on Other Services

### 6.1 From IAM/Auth Service (S1, Deepika)

**No runtime REST calls.** Journey Service fetches JWKS public keys from IAM on startup and refreshes every hour.

```
GET /api/v1/auth/.well-known/jwks.json
Response: { "keys": [ { "kty": "RSA", "kid": "...", "n": "...", "e": "..." } ] }
```

JWT validation is performed locally using these cached keys. This means IAM Service downtime does not affect Journey Service for existing tokens.

**What Deepika needs to provide:**
- JWKS endpoint serving RSA public keys
- JWT claims must include: `sub` (driver_id), `role` ("driver" or "admin"), `exp` (expiry)

---

### 6.2 From Map/Route Service (S4, Xiaoxuan)

```
POST /api/v1/routes/compute
```

**Request:**
```json
{
  "origin": { "lat": 53.3498, "lng": -6.2603 },
  "destination": { "lat": 51.8985, "lng": -8.4756 }
}
```

**Response:**
```json
{
  "route_id": "rte_dublin_cork",
  "total_distance_km": 256,
  "total_duration_minutes": 135,
  "segments": [
    {
      "segment_id": "seg_m50",
      "segment_name": "M50 Dublin Ring",
      "traversal_time_minutes": 25,
      "sequence_order": 1,
      "region": "ireland"
    },
    {
      "segment_id": "seg_m7n",
      "segment_name": "M7 Naas to Portlaoise",
      "traversal_time_minutes": 45,
      "sequence_order": 2,
      "region": "ireland"
    },
    {
      "segment_id": "seg_m7s",
      "segment_name": "M7 Portlaoise to Cashel",
      "traversal_time_minutes": 30,
      "sequence_order": 3,
      "region": "ireland"
    },
    {
      "segment_id": "seg_m8",
      "segment_name": "M8 Cashel to Cork",
      "traversal_time_minutes": 35,
      "sequence_order": 4,
      "region": "ireland"
    }
  ]
}
```

**What Xiaoxuan needs to provide:**
- Stable `segment_id` values that match what Capacity Service uses
- Stable segment_ids that match what Capacity Service uses
- Traversal time in whole minutes

**Caching strategy:**
Journey Service caches this response in Redis with key `route:{origin_lat_3dp}:{origin_lng_3dp}:{dest_lat_3dp}:{dest_lng_3dp}` and 24-hour TTL. Cache is invalidated if Capacity Service returns "unknown segment" (indicates road graph changed).

---

### 6.3 From Capacity Service (S3, Jai)

#### Reserve (sync, on booking)
```
POST /api/v1/capacity/reserve
```

**Request:**
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
    }
  ]
}
```

**Response (success):**
```json
{
  "status": "reserved",
  "reservation_id": "rsv_abc123",
  "journey_id": "jrn_a1b2c3d4"
}
```

**Response (failure):**
```json
{
  "status": "failed",
  "failed_segment": {
    "segment_id": "seg_m7n",
    "reason": "at_capacity",
    "available_slots": 0,
    "requested_slots": 1,
    "time_window_start": "2026-04-15T08:25:00Z",
    "time_window_end": "2026-04-15T09:10:00Z"
  }
}
```

**Critical requirement for Jai:** The reserve endpoint MUST be atomic. Either ALL segments in the request are reserved, or NONE are. This should be a single database transaction. Journey Service should NOT have to call reserve per-segment and handle rollback. The all-or-nothing guarantee lives inside Capacity Service.

#### Release (async, consumed from Redis Streams)
Capacity Service subscribes to `journey.events` stream and listens for `journey.cancelled`, `journey.completed`, and `journey.expired` events. On receiving these, it releases all slots for the given journey_id.

**Why async for release?** Releasing capacity is not on the driver's critical path. The driver already has their cancellation/completion confirmation. A 2-second delay in capacity release is invisible.

#### Check Availability (sync, optional preview)
```
GET /api/v1/capacity/check?segments=seg_m50,seg_m7n&time_start=2026-04-15T08:00:00Z&vehicle_type=car
```

Returns availability without reserving. Used for "preview" in the UI before the driver commits to booking.

**What Jai needs to provide:**
- Atomic multi-segment reservation in a single API call
- Idempotency key support (same key returns same result, no double booking)
- The `failed_segment` detail on rejection (Journey Service uses this for the driver-facing error message)
- Redis Streams consumer for release events

---

## 7. Cascading Time Window Computation

This is the core algorithm in Journey Service. Given a departure time and a list of segments with traversal times, compute the exact time window each segment is occupied.

### Algorithm

```
Input:
  departure_time = 2026-04-15T08:00:00Z
  segments = [
    { id: "seg_m50", traversal: 25min, order: 1 },
    { id: "seg_m7n", traversal: 45min, order: 2 },
    { id: "seg_m7s", traversal: 30min, order: 3 },
    { id: "seg_m8",  traversal: 35min, order: 4 },
  ]

Processing:
  cumulative = departure_time

  for each segment in order:
    window_start = cumulative
    window_end   = cumulative + segment.traversal
    cumulative   = window_end

Output:
  seg_m50: 08:00 → 08:25
  seg_m7n: 08:25 → 09:10
  seg_m7s: 09:10 → 09:40
  seg_m8:  09:40 → 10:15

  estimated_arrival = 10:15
```

### Edge Case: Midnight Crossover

If departure is 23:30 and traversal totals 2 hours:
- seg_1: 23:30 → 23:55 (same day)
- seg_2: 23:55 → 00:40 (crosses midnight, next day)
- seg_3: 00:40 → 01:15 (next day)

Use full datetime (not just time-of-day) for all window computations. Never strip the date component.

---

## 8. Multi-VM Distribution & Replication

The system runs N identical VMs behind a load balancer. Every service, database, and cache is replicated across VMs. This section describes how data stays consistent across them.

### 8.1 Request Flow

Any VM can handle any request. The load balancer distributes using round-robin or least-connections. There are no "primary" or "secondary" VMs from an application perspective.

```
1. Driver submits: City Centre → Airport, depart 08:00

2. Load balancer routes request to VM B (arbitrary)

3. VM B's Journey Service:
   a. Validates JWT locally (cached JWKS)
   b. Calls VM B's Map Service — GET route segments
   c. Computes cascading time windows
   d. Calls VM B's Capacity Service — atomically reserve all segments
   e. Persists journey to VM B's PostgreSQL

4. VM B responds to driver: APPROVED

5. PostgreSQL logical replication propagates the new journey record
   from VM B → VM A and VM B → VM C (async, < 100ms on LAN)

6. VM B publishes journey.booked to VM B's Redis Streams (async)
   VM B's Notification Service consumes and sends Firebase push
```

### 8.2 Multi-Master PostgreSQL Replication

Each VM runs a full PostgreSQL instance. All instances participate in **logical replication** — every VM is both a publisher (sending its writes) and a subscriber (receiving writes from all other VMs).

**Setup (PostgreSQL 16 logical replication):**

```sql
-- On each VM, create a publication for all tables
CREATE PUBLICATION vcs_pub FOR ALL TABLES;

-- On each VM, subscribe to every other VM's publication
CREATE SUBSCRIPTION vcs_sub_vm_a
  CONNECTION 'host=vm-a port=5432 dbname=trafficservice user=replicator password=<secret>'
  PUBLICATION vcs_pub;

CREATE SUBSCRIPTION vcs_sub_vm_b
  CONNECTION 'host=vm-b port=5432 dbname=trafficservice user=replicator password=<secret>'
  PUBLICATION vcs_pub;
-- repeat for each VM
```

**Conflict Resolution:**

When two VMs write to the same row simultaneously (e.g., two drivers try to activate the same journey from different VMs), the database will raise a replication conflict. Resolution strategy:

| Conflict type | Resolution |
|---------------|-----------|
| UPDATE conflicts (same row, different VMs) | Last-write-wins using `updated_at` timestamp. The replica applies the change only if the incoming `updated_at` is newer than the local value. |
| INSERT conflicts (duplicate primary key) | The `idempotency_key` UNIQUE constraint catches duplicate bookings. The second insert is rejected at the DB level on whichever VM receives it second. |
| Journey state transition conflicts | Optimistic locking via `version` field (existing). UPDATE fails if `version` has changed. The application retries or returns a conflict error. |

**Replication lag:**
- Target: < 100ms within the same data centre or VPC
- Acceptable: up to 500ms under peak load
- Monitoring: `pg_stat_replication` on each VM. Alert if lag exceeds 1 second.

### 8.3 Redis Configuration

Each VM has its own Redis instance used for:
- Route cache (Map Service responses, 24h TTL)
- Idempotency cache
- Redis Streams (journey events for Notification Service + Capacity slot release)

Since each VM's Redis is independent, the Notification Service on each VM only consumes events published by Journey Service on that same VM. A driver whose booking is handled by VM A receives their Firebase push notification from VM A's Notification Service. This is correct — no cross-VM Redis coordination needed.

If a VM goes down mid-request, the load balancer stops routing to it. Events already in that VM's Redis Streams will be delivered once the VM recovers (Redis Streams with consumer groups guarantee at-least-once delivery).

### 8.4 Load Balancer Configuration

```nginx
upstream vcs_backend {
    least_conn;                    # route to VM with fewest active connections
    server vm-a:80 weight=1;
    server vm-b:80 weight=1;
    server vm-c:80 weight=1;
}

server {
    listen 443 ssl;
    location /api/ {
        proxy_pass http://vcs_backend;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

**Sticky sessions are NOT required.** Because PostgreSQL multi-master replication propagates writes to all VMs within milliseconds, a driver whose booking was created on VM A can query their journey list from VM B and see the record. The replication lag (< 100ms) is imperceptible to users.

The one exception: immediately after a write, the same client may want to read their own write from the same VM. If this is critical, pass a `X-Prefer-VM` hint header or use a short sticky TTL (5s) scoped to write operations only.

### 8.5 Failure Handling

| Scenario | Behaviour |
|----------|-----------|
| VM B goes down mid-request | Load balancer detects via health check (GET /health). Stops routing to VM B. In-flight requests fail with 502; clients retry. Replication from VM A/C continues as usual. |
| VM B PostgreSQL replication lag spikes | Reads from VM B may return slightly stale data. Writes still succeed. Monitoring alerts at 1s lag. Journeys are eventually consistent. |
| Split brain: network partition isolates VM B | VM B continues serving requests with its local DB. Writes diverge. On partition heal, PostgreSQL logical replication conflict resolution (last-write-wins) merges changes. The optimistic lock on `version` prevents invalid state transitions from winning. |
| Capacity reservation race across VMs | Two drivers book the last slot for the same segment/time-window from different VMs simultaneously. Capacity Service uses a DB-level transaction with SELECT FOR UPDATE. Whichever VM commits first wins; the other gets a serialization failure and returns REJECTED to the driver. |
| Idempotency key sent to different VMs | Client sends same `Idempotency-Key` to VM A then VM B (e.g., after timeout retry). VM A commits and replicates to VM B. VM B checks its idempotency_cache (now populated via replication) and returns the cached response without re-processing. Replication lag means there is a small window (~100ms) where VM B might re-process before the record arrives — the UNIQUE constraint on `idempotency_key` will reject the duplicate insert. |

---

## 9. Events Published to Redis Streams

### Stream: `journey.events`

All events include a consistent envelope:

```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.booked",
  "timestamp": "2026-04-15T08:00:01Z",
  "source_vm": "vm-a",
  "payload": { ... }
}
```

### Event Types

| Event | Trigger | Consumers | Purpose |
|-------|---------|-----------|---------|
| `journey.booked` | Journey approved | Notification Service | Send push notification. |
| `journey.rejected` | Journey rejected | Notification Service | Send rejection notification with reason. |
| `journey.cancelled` | Driver or admin cancels | Notification Service, Capacity Service | Send cancellation notification. Release reserved capacity slots. |
| `journey.activated` | Driver starts journey | Notification Service | Confirmation push. |
| `journey.completed` | Driver finishes journey | Capacity Service | Release reserved capacity slots. |
| `journey.expired` | Background job detects unused booking | Capacity Service | Release reserved capacity slots. |

### Event Payload Examples

**journey.booked:**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "driver_id": "usr_x1y2z3",
  "origin": { "lat": 53.3498, "lng": -6.2603 },
  "destination": { "lat": 51.8985, "lng": -8.4756 },
  "departure_time": "2026-04-15T08:00:00Z",
  "estimated_arrival": "2026-04-15T10:15:00Z",
  "vehicle_type": "car",
  "status": "APPROVED",
  "segments": [...]
}
```

**journey.cancelled:**
```json
{
  "journey_id": "jrn_a1b2c3d4",
  "driver_id": "usr_x1y2z3",
  "status": "CANCELLED",
  "cancelled_by": "driver",
  "reservation_id": "rsv_abc123"
}
```

The `reservation_id` field allows Capacity Service (on the same VM) to locate and release the reservation for this journey.

---

## 10. Database Schema (journey schema)

```sql
CREATE SCHEMA IF NOT EXISTS journey;

-- Main journey table
CREATE TABLE journey.journeys (
    journey_id      VARCHAR(20) PRIMARY KEY,         -- "jrn_" + nanoid
    driver_id       VARCHAR(20) NOT NULL,
    idempotency_key VARCHAR(64) UNIQUE,              -- prevents duplicate bookings on retry

    origin_lat      DECIMAL(9,6) NOT NULL,
    origin_lng      DECIMAL(9,6) NOT NULL,
    dest_lat        DECIMAL(9,6) NOT NULL,
    dest_lng        DECIMAL(9,6) NOT NULL,

    departure_time  TIMESTAMPTZ NOT NULL,
    estimated_arrival TIMESTAMPTZ NOT NULL,
    vehicle_type    VARCHAR(20) NOT NULL,             -- car, truck, motorcycle

    status          VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    rejection_reason TEXT,

    version         INTEGER NOT NULL DEFAULT 1,       -- optimistic lock for state transitions

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at    TIMESTAMPTZ,
    activated_at    TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    expired_at      TIMESTAMPTZ,

    -- Enforce one active journey per driver
    -- Partial unique index: only applies when status is APPROVED or ACTIVE
    CONSTRAINT chk_status CHECK (status IN ('PENDING','APPROVED','REJECTED','CANCELLED','ACTIVE','COMPLETED','EXPIRED')),
    CONSTRAINT chk_vehicle CHECK (vehicle_type IN ('car','van','truck','motorcycle'))
);

-- Partial unique index: one APPROVED or ACTIVE journey per driver at a time
CREATE UNIQUE INDEX idx_one_active_per_driver
    ON journey.journeys (driver_id)
    WHERE status IN ('APPROVED', 'ACTIVE');

-- Query indexes
CREATE INDEX idx_journeys_driver     ON journey.journeys (driver_id, created_at DESC);
CREATE INDEX idx_journeys_status     ON journey.journeys (status);
CREATE INDEX idx_journeys_departure  ON journey.journeys (departure_time)
    WHERE status = 'APPROVED';  -- for expiry background job

-- Journey segments (denormalized from Map Service response)
CREATE TABLE journey.journey_segments (
    id              SERIAL PRIMARY KEY,
    journey_id      VARCHAR(20) NOT NULL REFERENCES journey.journeys(journey_id),
    segment_id      VARCHAR(30) NOT NULL,
    segment_name    VARCHAR(100) NOT NULL,
    sequence_order  INTEGER NOT NULL,
    time_window_start TIMESTAMPTZ NOT NULL,
    time_window_end   TIMESTAMPTZ NOT NULL,
    traversal_minutes INTEGER NOT NULL,
    reservation_id  VARCHAR(30),                      -- from Capacity Service response

    CONSTRAINT uq_journey_segment UNIQUE (journey_id, segment_id)
);

CREATE INDEX idx_segments_journey ON journey.journey_segments (journey_id);

-- Idempotency tracking (optional, can also use the UNIQUE on idempotency_key)
-- Stores the full response for idempotent replay
CREATE TABLE journey.idempotency_cache (
    idempotency_key VARCHAR(64) PRIMARY KEY,
    journey_id      VARCHAR(20) NOT NULL,
    response_body   JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours'
);
```

---

## 11. Edge Cases

| # | Scenario | Risk | Mitigation |
|---|----------|------|------------|
| E1 | Two simultaneous bookings by same driver | Both pass "no active booking" check, both get approved | Partial unique index `idx_one_active_per_driver` causes second INSERT to fail with unique violation. Caught and returned as 409 Conflict. |
| E2 | Capacity Service timeout mid-reservation | Unknown state: did reservation succeed? | Idempotency key on reserve call. On retry, Capacity returns original result. If still fails after 2 retries, reject journey. |
| E3 | Cascading time window crosses midnight | Time arithmetic breaks if using time-of-day only | Use full TIMESTAMPTZ everywhere. Never strip date component. |
| E4 | Stale Map Service cache | Road segment removed/modified, Capacity returns "unknown segment" | Invalidate cache for this route. Retry with fresh Map Service call. Max 1 retry. |
| E5 | Cancellation at exactly 30-minute boundary | Ambiguous: is exactly 30min "allowed" or "too late"? | Strict inequality: `departure_time - now > 30 minutes`. Exactly 30 minutes is rejected. |
| E6 | Expiry job races with activation | Background job marks EXPIRED while driver taps "Activate" | Optimistic lock via `version` field. UPDATE ... WHERE version = @read_version. Whoever commits first wins. |
| E7 | Redis Streams publish fails after booking | Journey is APPROVED in DB but notification never sent | Journey is committed regardless. Notification is best-effort. Driver sees status on next app refresh. Retry publish 3 times with 1s backoff. |
| E8 | Map Service completely down, no cache | Cannot compute route at all | Return 503 with Retry-After: 30 header. Do not attempt partial booking. |
| E9 | Capacity reservation succeeds but DB write fails | Slots reserved in Capacity Service but no journey record exists | Orphaned reservation cleanup job (§12.2) runs every 15 minutes. Capacity Service releases reservations with no matching journey_id after 1 hour. |
| E10 | Two VMs simultaneously approve the last capacity slot | Race condition: both VMs' Capacity Services might both approve before replication catches up | Capacity Service uses `SELECT FOR UPDATE` on the slot row within a transaction. Only one transaction wins; the other gets a serialization error and returns REJECTED. |
| E11 | Departure time in the past | Invalid booking attempt | Reject immediately with 422. Check: `departure_time > now() + 1 hour`. |
| E12 | Replication lag causes stale read after write | Driver books on VM A, immediately queries from VM B before replication arrives | Journeys are not polled immediately after booking — the result is in the POST /journeys response. Subsequent list queries will return the record once replication propagates (< 100ms). Acceptable for this use case. |
| E13 | Driver books, journey approved, then same route booked by 1000 other drivers exhausting capacity, first driver cancels and re-books | Re-booking may now be rejected even though they "had" the slot | Expected behavior. Cancellation releases capacity. Re-booking is a new request subject to current availability. No "hold" mechanism. |
| E14 | Admin force-cancels a journey that is ACTIVE | Driver is currently on the road | Allowed. Journey moves to CANCELLED. Capacity released. In a real system this might trigger a notification to pull over; for prototype, just update state. |
| E15 | Idempotency key reused with different payload | Client sends same key but different origin/destination | Return the original response for that key. Do NOT process the new payload. Log a warning. The idempotency_cache table stores the original response. |

---

## 12. Background Jobs

### 12.1 Journey Expiry

**Frequency:** Every 5 minutes
**Query:** `SELECT journey_id FROM journey.journeys WHERE status = 'APPROVED' AND departure_time < NOW() - INTERVAL '30 minutes'`
**Action:** For each result, update status to EXPIRED (with version check), publish `journey.expired` event to Redis Streams. Capacity Service consumes event and releases slots.

### 12.2 Orphaned Reservation Cleanup

**Frequency:** Every 15 minutes
**Purpose:** Detect reservations that exist in Capacity Service but have no matching journey record (due to crash between Capacity reservation and DB commit in Journey Service).
**Approach:** Journey Service publishes a `journey.reconcile` heartbeat event containing all active `journey_id` values. Capacity Service compares this list against its reservations and releases any reservation older than 1 hour with no matching journey_id. Both services run on the same VM so this event goes through the local Redis Streams instance.

### 12.3 Idempotency Cache Cleanup

**Frequency:** Every hour
**Query:** `DELETE FROM journey.idempotency_cache WHERE expires_at < NOW()`

### 12.4 Route Cache Refresh

**Mechanism:** Redis key expiry (24-hour TTL). No explicit background job needed. Cache is populated on-demand when a route is first requested.

---

## 13. Configuration (Environment Variables)

```bash
# Server
PORT=8083
ENV=production
LOG_LEVEL=info
VM_ID=vm-a                         # identifies this VM in logs and metrics

# Database (local PostgreSQL instance on this VM)
DB_HOST=localhost
DB_PORT=5432
DB_NAME=trafficservice
DB_SCHEMA=journey
DB_USER=journey_svc
DB_PASSWORD=<secret>
DB_MAX_CONNS=20
DB_IDLE_CONNS=5

# Redis (local Redis instance on this VM)
REDIS_HOST=localhost:6379
REDIS_PASSWORD=<secret>
REDIS_DB=0

# Co-located services (same VM, internal Docker network)
CAPACITY_SERVICE_URL=http://capacity-service:8081
MAP_SERVICE_URL=http://map-service:8084
IAM_SERVICE_URL=http://iam-service:8082

# IAM JWKS
JWKS_URL=http://iam-service:8082/api/v1/auth/.well-known/jwks.json
JWKS_REFRESH_INTERVAL=3600

# Timeouts (all intra-VM calls — should be fast)
CAPACITY_TIMEOUT_MS=3000
MAP_SERVICE_TIMEOUT_MS=2000

# Business Rules
MIN_ADVANCE_BOOKING_MINUTES=60
MIN_CANCELLATION_WINDOW_MINUTES=30
ACTIVATION_GRACE_WINDOW_MINUTES=30
ROUTE_CACHE_TTL_HOURS=24
MAX_CAPACITY_RETRIES=2

# Background Jobs
EXPIRY_JOB_INTERVAL_MINUTES=5
RECONCILIATION_JOB_INTERVAL_MINUTES=15
```

---

## 14. Project Structure

```
journey-service/
├── cmd/
│   └── server/
│       └── main.go                 # Entry point, wires everything
├── internal/
│   ├── config/
│   │   └── config.go               # Loads env vars, validates
│   ├── handler/
│   │   ├── journey_handler.go      # HTTP handlers (create, cancel, activate, complete)
│   │   ├── admin_handler.go        # Admin endpoints
│   │   └── health_handler.go       # /health, /ready
│   ├── middleware/
│   │   ├── auth.go                 # JWT validation middleware (cached JWKS)
│   │   ├── idempotency.go          # Idempotency key middleware
│   │   └── logging.go              # Request logging
│   ├── model/
│   │   ├── journey.go              # Journey struct, status constants
│   │   └── segment.go              # Segment struct, time window
│   ├── service/
│   │   ├── journey_service.go      # Core business logic, orchestration
│   │   ├── time_window.go          # Cascading time window computation
│   │   └── expiry.go               # Background expiry job
│   ├── repository/
│   │   ├── journey_repo.go         # PostgreSQL queries for journeys
│   │   └── idempotency_repo.go     # Idempotency cache queries
│   ├── client/
│   │   ├── capacity_client.go      # REST client for Capacity Service (local + remote)
│   │   ├── map_client.go           # REST client for Map Service
│   │   └── route_cache.go          # Redis route cache (24h TTL)
│   └── event/
│       ├── publisher.go            # Redis Streams XADD wrapper
│       ├── consumer.go             # Consumes replicated journey events from other cells
│       └── events.go               # Event type definitions, payload structs
├── migrations/
│   └── 001_create_schema.sql       # Database migrations
├── proto/                          # (future) if switching to gRPC
├── Dockerfile                      # Multi-stage build
├── docker-compose.yml              # Local dev
├── go.mod
├── go.sum
└── README.md
```

---

## 15. Sequence Diagram: Complete Booking Flow

All actors below run on the **same VM**. The load balancer has already routed the request to this VM before step 1.

```
Driver     Load Balancer  Journey Svc    Map Svc     Capacity Svc   Redis Streams   Notification Svc
  │              │              │            │              │                │               │
  │  POST /journeys             │            │              │                │               │
  ├─────────────►├─────────────►│            │              │                │               │
  │              │              │            │              │                │               │
  │              │     1. Validate JWT (local, cached JWKS)  │                │               │
  │              │              │            │              │                │               │
  │              │     2. Check one-active-journey rule (DB) │                │               │
  │              │              │            │              │                │               │
  │              │     3. Check idempotency key (DB)         │                │               │
  │              │              │            │              │                │               │
  │              │     4. GET route (or Redis cache hit)      │                │               │
  │              │              ├───────────►│              │                │               │
  │              │              │◄───────────┤              │                │               │
  │              │              │  segments  │              │                │               │
  │              │              │            │              │                │               │
  │              │     5. Compute cascading time windows     │                │               │
  │              │              │            │              │                │               │
  │              │     6. Reserve all segments (atomic, single TX)            │               │
  │              │              ├────────────────────────────►│                │               │
  │              │              │◄────────────────────────────┤                │               │
  │              │              │  reserved / failed         │                │               │
  │              │              │            │              │                │               │
  │              │     7. Persist journey (APPROVED) to DB   │                │               │
  │              │              │            │              │                │               │
  │              │     8. Respond to driver                  │                │               │
  │◄─────────────┤◄─────────────┤            │              │                │               │
  │  201 APPROVED│              │            │              │                │               │
  │              │              │            │              │                │               │
  │              │     9. Publish event (async, after response)               │               │
  │              │              ├─────────────────────────────────────────────►│               │
  │              │              │            │              │                │               │
  │              │              │            │              │                │  10. Consume  │
  │              │              │            │              │                ├──────────────►│
  │              │              │            │              │                │  send Firebase│
  │              │              │            │              │                │  push (async) │
  │              │              │            │              │                │               │
  │              │     -- Meanwhile, PostgreSQL replicates  │                │               │
  │              │     -- new journey row to VM B and VM C  │                │               │
  │              │     -- (async, < 100ms)                  │                │               │
```

---

## 16. Frontend Integration

The frontend is a React 18 PWA already built and running at `frontend/`. It is currently **fully mocked** — all data comes from `AppContext` and `mockData.ts`. Wiring it to real backend APIs is a second phase. This section describes the full integration contract between frontend and backend so that both can be built in parallel without surprises.

### 16.1 Tech Stack

| Concern | Library |
|---------|---------|
| Framework | React 18.3.1 + TypeScript, Vite 6.3.5 |
| Routing | React Router 7.13.0 (`createBrowserRouter`, nested layouts) |
| Styling | Tailwind CSS 4.x, Radix UI, shadcn/ui patterns |
| Forms | React Hook Form 7.x |
| Charts | Recharts 2.15.2 (admin analytics) |
| Toasts | Sonner 2.x |
| State | React Context (`AppContext`) — mock today, real API calls tomorrow |
| HTTP | Not yet implemented — see §16.6 for the API client blueprint |

### 16.2 Application Routes

```
/auth                         → LoginPage (driver or admin)

/driver                       → Driver Dashboard
/driver/book                  → Book Journey (2-step form)
/driver/booking-result        → Booking result (approved / rejected)
/driver/journeys              → My Journeys (paginated list)
/driver/journeys/:id          → Journey Detail (activate / complete / cancel)
/driver/notifications         → Notifications
/driver/settings              → Profile & settings

/admin                        → Admin Dashboard
/admin/journeys               → All Journeys (filterable)
/admin/journeys/:id           → Journey Detail (force-cancel)
/admin/analytics              → Analytics (booking trends, approval rates)
/admin/map                    → Traffic Map (live segment occupancy)
/admin/notifications          → Admin Notifications
/admin/settings               → Admin Settings
```

### 16.3 Page → API Mapping

Each page and the real backend calls it needs once mocking is removed:

#### Driver pages

| Page | Backend Call | Service | Notes |
|------|-------------|---------|-------|
| `/auth` | `POST /api/v1/auth/login` | IAM (8082) | Returns JWT + user profile |
| `/driver` (dashboard) | `GET /api/v1/journeys?limit=3&status=approved,active` | Journey (8083) | Recent journeys summary |
| `/driver/book` | `GET /api/v1/map/nodes` | Map (8084) | Resolve node names → lat/lng for origin/destination dropdowns |
| | `POST /api/v1/journeys` | Journey (8083) | Submit booking with Idempotency-Key header |
| `/driver/booking-result` | — | — | Reads result from state set by the booking call; no additional API call |
| `/driver/journeys` | `GET /api/v1/journeys?page=1&limit=20&status=…` | Journey (8083) | Paginated, filterable by status |
| `/driver/journeys/:id` | `GET /api/v1/journeys/:id` | Journey (8083) | Full journey detail + segments + timeline |
| | `PUT /api/v1/journeys/:id/activate` | Journey (8083) | "Start journey" button |
| | `PUT /api/v1/journeys/:id/complete` | Journey (8083) | "Complete journey" button |
| | `PUT /api/v1/journeys/:id/cancel` | Journey (8083) | "Cancel booking" button |
| `/driver/notifications` | `GET /api/v1/notifications?driver_id=…` | Notification (8085) | Notification list |
| | `PUT /api/v1/notifications/:id/read` | Notification (8085) | Mark single read |
| | `PUT /api/v1/notifications/read-all` | Notification (8085) | Mark all read |
| `/driver/settings` | `GET /api/v1/auth/profile` | IAM (8082) | Driver profile |
| | `PUT /api/v1/auth/profile` | IAM (8082) | Update name, phone |
| | `POST /api/v1/notifications/device-token` | Notification (8085) | FCM token registration |

#### Admin pages

| Page | Backend Call | Service | Notes |
|------|-------------|---------|-------|
| `/admin` | `GET /api/v1/admin/journeys?limit=10` | Journey (8083) | Recent journeys feed |
| | `GET /api/v1/admin/analytics/summary` | Journey (8083) | KPI cards (total, approval rate, active count) |
| `/admin/journeys` | `GET /api/v1/admin/journeys?status=…&region=…&page=…` | Journey (8083) | Full filterable list |
| `/admin/journeys/:id` | `GET /api/v1/admin/journeys/:id` | Journey (8083) | Journey detail |
| | `PUT /api/v1/admin/journeys/:id/cancel` | Journey (8083) | Force-cancel (no 30-min restriction) |
| `/admin/analytics` | `GET /api/v1/admin/analytics?from=…&to=…` | Journey (8083) | Booking trend + regional stats (feeds Recharts) |
| `/admin/map` | `GET /api/v1/map/traffic` | Map (8084) | Segment occupancy + node topology for SVG map |
| `/admin/notifications` | `GET /api/v1/admin/notifications` | Notification (8085) | All recent notifications across drivers |
| `/admin/settings` | `GET /api/v1/auth/profile` | IAM (8082) | Admin profile |

### 16.4 Authentication Flow

```
1. User hits /auth → selects role (driver | admin), enters email + password

2. POST /api/v1/auth/login  (IAM Service)
   Body:     { "email": "...", "password": "..." }
   Response: { "access_token": "...", "refresh_token": "...",
               "user": { "id", "name", "email", "role", "vehicle_type" } }

3. Frontend stores:
     localStorage["cw_token"] = access_token
     localStorage["cw_user"]  = JSON.stringify(user)

4. Every subsequent request sends:
     Authorization: Bearer <access_token>

5. On 401 response from any service:
     POST /api/v1/auth/refresh  { "refresh_token": "..." }
     Replace stored access_token, retry original request once.
     If refresh also fails → clear localStorage, redirect to /auth.

6. Logout: DELETE localStorage keys, redirect to /auth.
```

JWT must contain claims: `sub` (user id), `role` (`"driver"` or `"admin"`), `exp`.
Journey Service (and all other services) validate this token **locally** using JWKS public keys fetched from IAM on startup — no runtime IAM call per request.

### 16.5 Data Model Alignment

#### Vehicle Types

The frontend uses `'Car' | 'Van' | 'Motorcycle' | 'HGV'`. The backend spec currently uses `car | truck | motorcycle`. Alignment needed:

| Frontend | Backend API | Action |
|----------|------------|--------|
| `"Car"` | `"car"` | Lowercase on submit |
| `"Van"` | `"van"` | **Add `van` to backend vehicle_type enum** (weight = 1.5 slots). Update Journey Service and Capacity Service specs. |
| `"Motorcycle"` | `"motorcycle"` | Lowercase on submit |
| `"HGV"` | `"truck"` | Map `HGV → truck` on submit |

#### Origin / Destination

The frontend `BookJourneyPage` currently uses human-readable location names (e.g., `"City Centre"`). The Journey Service API expects `{ lat, lng }` coordinates.

**Resolution strategy (Option A — recommended):**
Map Service exposes `GET /api/v1/map/nodes` returning all graph nodes with coordinates. Frontend fetches this list on `BookJourneyPage` mount, builds a `name → {lat,lng}` lookup, and converts before calling `POST /api/v1/journeys`. The hardcoded `ORIGINS` / `DESTINATIONS` arrays in `BookJourneyPage.tsx` become dynamic from this endpoint.

The frontend node names already match `mockData.ts` map nodes:
```
"City Centre"    → { lat: 53.3498, lng: -6.2603 }
"Airport"        → { lat: 53.4264, lng: -6.2499 }
"North Gate"     → { lat: 53.4200, lng: -6.2603 }
... (all 10 nodes defined in mockData.ts mapNodes)
```

#### Journey Status

| Frontend (lowercase) | Backend (uppercase) |
|---------------------|---------------------|
| `'pending'` | `PENDING` |
| `'approved'` | `APPROVED` |
| `'rejected'` | `REJECTED` |
| `'active'` | `ACTIVE` |
| `'completed'` | `COMPLETED` |
| `'cancelled'` | `CANCELLED` |
| `'expired'` *(not yet in frontend types)* | `EXPIRED` — **add to frontend `JourneyStatus` type** |

The frontend should lowercase status values when displaying. The backend should accept and return uppercase. No status translation layer needed in the API; handle in the frontend display layer.

### 16.6 API Client Blueprint (Frontend)

When replacing mock data, add a thin API client module:

```typescript
// src/app/lib/api.ts
const BASE = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost';

const SERVICES = {
  iam:          `${BASE}:8082/api/v1`,
  journey:      `${BASE}:8083/api/v1`,
  capacity:     `${BASE}:8081/api/v1`,
  map:          `${BASE}:8084/api/v1`,
  notification: `${BASE}:8085/api/v1`,
};

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const token = localStorage.getItem('cw_token');
  const res = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...options?.headers,
    },
  });
  if (res.status === 401) {
    // attempt token refresh then retry — omitted for brevity
    localStorage.removeItem('cw_token');
    window.location.href = '/auth';
    throw new Error('Unauthenticated');
  }
  if (!res.ok) throw await res.json();
  return res.json();
}

export const iamAPI = {
  login:   (body: { email: string; password: string }) =>
    request(`${SERVICES.iam}/auth/login`, { method: 'POST', body: JSON.stringify(body) }),
  profile: () => request(`${SERVICES.iam}/auth/profile`),
  refresh: (refreshToken: string) =>
    request(`${SERVICES.iam}/auth/refresh`, { method: 'POST', body: JSON.stringify({ refresh_token: refreshToken }) }),
};

export const journeyAPI = {
  book:     (body: BookJourneyRequest, idempotencyKey: string) =>
    request(`${SERVICES.journey}/journeys`, {
      method: 'POST',
      body: JSON.stringify(body),
      headers: { 'Idempotency-Key': idempotencyKey },
    }),
  list:     (params?: URLSearchParams) => request(`${SERVICES.journey}/journeys?${params ?? ''}`),
  get:      (id: string) => request(`${SERVICES.journey}/journeys/${id}`),
  activate: (id: string) => request(`${SERVICES.journey}/journeys/${id}/activate`, { method: 'PUT' }),
  complete: (id: string) => request(`${SERVICES.journey}/journeys/${id}/complete`, { method: 'PUT' }),
  cancel:   (id: string) => request(`${SERVICES.journey}/journeys/${id}/cancel`,   { method: 'PUT' }),
  adminList: (params?: URLSearchParams) => request(`${SERVICES.journey}/admin/journeys?${params ?? ''}`),
  adminGet:  (id: string) => request(`${SERVICES.journey}/admin/journeys/${id}`),
  adminCancel: (id: string) => request(`${SERVICES.journey}/admin/journeys/${id}/cancel`, { method: 'PUT' }),
  analytics:   (params?: URLSearchParams) => request(`${SERVICES.journey}/admin/analytics?${params ?? ''}`),
};

export const mapAPI = {
  nodes:   () => request(`${SERVICES.map}/map/nodes`),
  traffic: () => request(`${SERVICES.map}/map/traffic`),
};

export const notificationAPI = {
  list:              (params?: URLSearchParams) => request(`${SERVICES.notification}/notifications?${params ?? ''}`),
  markRead:          (id: string) => request(`${SERVICES.notification}/notifications/${id}/read`, { method: 'PUT' }),
  markAllRead:       () => request(`${SERVICES.notification}/notifications/read-all`, { method: 'PUT' }),
  registerDeviceToken: (body: { driver_id: string; fcm_token: string }) =>
    request(`${SERVICES.notification}/notifications/device-token`, { method: 'POST', body: JSON.stringify(body) }),
};
```

### 16.7 Additional Endpoints Needed

The following endpoints are required by the frontend but not yet in the current service specs. They must be added before the frontend can be fully wired:

#### `GET /api/v1/map/nodes` — Map Service

Returns all graph nodes for the origin/destination dropdowns in `BookJourneyPage`.

```json
{
  "nodes": [
    { "node_id": "city",       "label": "City Centre",    "lat": 53.3498, "lng": -6.2603 },
    { "node_id": "north",      "label": "North Gate",     "lat": 53.4200, "lng": -6.2603 },
    { "node_id": "airport",    "label": "Airport",        "lat": 53.4264, "lng": -6.2499 },
    { "node_id": "east",       "label": "East Quay",      "lat": 53.3498, "lng": -6.2100 },
    { "node_id": "south",      "label": "South Terminal", "lat": 53.3100, "lng": -6.2603 },
    { "node_id": "industrial", "label": "Industrial Park","lat": 53.3150, "lng": -6.2100 },
    { "node_id": "west",       "label": "West Depot",     "lat": 53.3498, "lng": -6.3200 },
    { "node_id": "port",       "label": "Port Terminal",  "lat": 53.3100, "lng": -6.3200 },
    { "node_id": "northfield", "label": "Northfield",     "lat": 53.4000, "lng": -6.3000 },
    { "node_id": "riverside",  "label": "Riverside",      "lat": 53.3350, "lng": -6.2700 }
  ]
}
```

#### `GET /api/v1/map/traffic` — Map Service (aggregated with Capacity data)

Returns segment topology + live occupancy for the admin traffic map SVG.

```json
{
  "segments": [
    {
      "segment_id": "TS01",
      "name": "North Ring Road",
      "region": "north",
      "level": "high",
      "occupancy_pct": 78,
      "vehicles": 234,
      "capacity": 300,
      "trend": "worsening",
      "from_node": "north",
      "to_node": "city"
    }
  ],
  "nodes": [
    { "node_id": "city", "label": "City Centre", "x": 300, "y": 250 }
  ]
}
```

**Owner decision:** Map Service owns the graph topology and node positions. Capacity Service owns the live occupancy. Two approaches:
- Map Service calls Capacity Service internally and returns the merged response (simpler for frontend, more coupling between services).
- Frontend calls Map Service for topology and Capacity Service for occupancy separately and merges in the client (more frontend complexity, better service independence).

For a prototype, the first approach is recommended.

#### `GET /api/v1/admin/analytics` — Journey Service

Feeds the Recharts components in `AnalyticsPage.tsx`.

```json
{
  "booking_trend": [
    { "day": "Mon", "bookings": 45, "approved": 38, "rejected": 7 }
  ],
  "regional_stats": [
    { "region": "North", "bookings": 98, "approved": 82, "rejected": 16 }
  ],
  "kpis": {
    "total_bookings": 346,
    "approval_rate": 87.3,
    "rejection_rate": 12.7,
    "active_journeys": 14,
    "avg_duration_minutes": 38,
    "cancellations": 23
  }
}
```

Query params: `from_date` (ISO 8601), `to_date` (ISO 8601), `region` (optional filter).

#### `GET /api/v1/admin/analytics/summary` — Journey Service

For the admin dashboard KPI cards (smaller, faster than full analytics).

```json
{
  "total_bookings_today": 62,
  "approval_rate_today": 89.0,
  "active_journeys": 14,
  "pending_journeys": 3
}
```

### 16.8 CORS Configuration

All backend services (via Nginx in production, directly in development) must respond with:

```
Access-Control-Allow-Origin: http://localhost:5173    (Vite dev, port is configurable)
Access-Control-Allow-Origin: https://<production-domain>
Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, Idempotency-Key, X-Trace-ID
Access-Control-Expose-Headers: X-Trace-ID, Retry-After
Access-Control-Max-Age: 86400
```

The boilerplate `CORSMiddleware` in each service already sets `Access-Control-Allow-Origin: *` — tighten this for production and ensure `Idempotency-Key` is in the allow-headers list.

### 16.9 FCM Device Token Registration

After login the frontend requests browser notification permission and registers with Notification Service:

```typescript
// In AppContext login() after storing token:
const permission = await Notification.requestPermission();
if (permission === 'granted') {
  const fcmToken = await getToken(messaging, {
    vapidKey: import.meta.env.VITE_FCM_VAPID_KEY,
  });
  await notificationAPI.registerDeviceToken({ driver_id: user.id, fcm_token: fcmToken });
}
```

**Backend:** `POST /api/v1/notifications/device-token` (Notification Service)
```json
{ "driver_id": "usr_x1y2z3", "fcm_token": "fcm_token_string_from_firebase" }
```

This is the bridge between IAM-authenticated users and Notification Service's FCM delivery. The Notification Service stores `driver_id → fcm_token` and uses it when it consumes `journey.booked` / `journey.rejected` / etc. events from Redis Streams.

### 16.10 End-to-End Implementation Order

Build and wire in this sequence so each step is independently testable:

| Step | What to build | Who | Frontend effect |
|------|--------------|-----|----------------|
| 1 | IAM: `POST /auth/login`, `POST /auth/refresh`, `GET /auth/profile` | Deepika | Login page works. JWT stored. All subsequent steps authenticated. |
| 2 | Map: `GET /map/nodes` | Xiaoxuan | `BookJourneyPage` origin/destination dropdowns populated from API instead of hardcoded. |
| 3 | Journey: `POST /journeys` | Ajinkya | Book journey end-to-end. Booking result page shows real APPROVED/REJECTED. |
| 4 | Journey: `GET /journeys`, `GET /journeys/:id` | Ajinkya | My Journeys list and detail pages show real data. |
| 5 | Journey: activate/complete/cancel | Ajinkya | Journey lifecycle buttons work on detail page. |
| 6 | Capacity: wired to Journey Service | Jai | Booking approval/rejection is based on real capacity, not frontend mock logic. |
| 7 | Notification: `POST /device-token`, `GET /notifications`, `PUT /read` | Ziwei | Notifications page real. Push notifications arrive via FCM. |
| 8 | Journey: admin list, detail, force-cancel | Ajinkya | Admin All Journeys and detail pages work. |
| 9 | Journey: `GET /admin/analytics`, `GET /admin/analytics/summary` | Ajinkya | Admin analytics Recharts powered by real data. |
| 10 | Map: `GET /map/traffic` | Xiaoxuan | Admin traffic map shows real segment occupancy. |

---

*Last updated: March 2026*
*Service version: 0.1.0 (planning)*
