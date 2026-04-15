# Clearway — Distributed Vehicle Capacity System: The Bible

> **Course:** CS7NS6 Distributed Systems — Exercise 2, TCD  
> **Group:** Group C  
> **GCP Project:** `distributed-capacity-system`  
> **Repo:** [AjinkyaTaranekar/distributed-vehicle-capacity-system](https://github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system)  
> **Live EU endpoint:** https://35.244.162.92.nip.io  
> **Last updated:** April 2026

---

## Table of Contents

1. [What Is This System?](#1-what-is-this-system)
2. [High-Level Architecture](#2-high-level-architecture)
3. [Infrastructure & Deployment](#3-infrastructure--deployment)
4. [Services Deep Dive](#4-services-deep-dive)
   - [IAM Service](#41-iam-service-port-8082)
   - [Journey Service](#42-journey-service-port-8083)
   - [Capacity Service](#43-capacity-service-port-8081)
   - [Map Service](#44-map-service-port-8084)
   - [Notification Service](#45-notification-service-port-8085)
5. [Data Layer](#5-data-layer)
6. [Event System (Outbox + Redis Streams)](#6-event-system-outbox--redis-streams)
7. [API Gateway (nginx)](#7-api-gateway-nginx)
8. [Frontend](#8-frontend)
9. [Key Design Decisions & Why](#9-key-design-decisions--why)
10. [Distributed Systems Concepts Implemented](#10-distributed-systems-concepts-implemented)
11. [CI/CD Pipeline](#11-cicd-pipeline)
12. [Observability Stack](#12-observability-stack)
13. [Chaos Engineering](#13-chaos-engineering)
14. [Load Testing](#14-load-testing)
15. [Known Limitations & Tech Debt](#15-known-limitations--tech-debt)
16. [Live System Verification Commands](#16-live-system-verification-commands)
17. [Quick Reference](#17-quick-reference)

---

## 1. What Is This System?

**Clearway** is a distributed system that manages vehicle journey bookings across a road network by enforcing **per-segment capacity limits**. Think of it as a traffic management system: roads have a maximum number of vehicles they can carry at any given time window. Drivers book journeys in advance; the system either approves or rejects each booking based on whether sufficient capacity remains on all road segments that the journey traverses.

**The core problem it solves:**  
Without coordination, two drivers could book the same road segment at the same time when only one slot remains. The system prevents this using pessimistic locking and serializable transactions — exactly one booking wins, the other is deterministically rejected.

**Geographic scope:** Built on a Dublin road network model — 10 nodes (City Centre, Airport, North Gate, etc.) with 13 bidirectional road segments connecting them. The system is architecturally designed to scale to any road network.

**Academic goals demonstrated:**
- Multi-region active-active writes (no single primary DB)
- Geographic distribution with replication lag visibility
- Fault tolerance via circuit breakers and chaos testing
- Distributed consistency via CRDB's Raft consensus
- Event-driven architecture via transactional outbox + Redis Streams
- Saga pattern for cross-regional transactions

---

## 2. High-Level Architecture

```
                        ┌─────────────────────────────┐
                        │         Browser / App        │
                        │   React SPA + PWA + FCM      │
                        └──────────────┬──────────────┘
                                       │ HTTPS
                         ┌─────────────▼──────────────┐
                         │   GCP Global HTTPS LB       │
                         │ TLS terminated here         │
                         │ HTTP→HTTPS 301 redirect     │
                         └─────────────┬──────────────┘
                                       │ HTTP (internal)
                         ┌─────────────▼──────────────┐
                         │   nginx :80 (API Gateway)   │
                         │ Rate limiting · Path routing │
                         │ Static SPA · Swagger proxy  │
                         └──┬──┬──┬──┬──┬─────────────┘
                            │  │  │  │  │
              ┌─────────────┘  │  │  │  └──────────────┐
              │    ┌───────────┘  │  └──────┐           │
              ▼    ▼              ▼         ▼           ▼
         IAM :8082  Journey :8083  Capacity :8081  Map :8084
                         │            │
                         │            │
                    ┌────▼────────────▼────┐
                    │   Notification :8085  │
                    └──────────────────────┘
                                │
              ┌─────────────────┼──────────────────┐
              ▼                 ▼                  ▼
        CockroachDB        Redis :6379        External APIs
        :26257            (Stream + Cache)   Nominatim / OSRM
        (Raft cluster)                       Firebase FCM
```

**Three independent regional cells run the same stack:**

```
┌──────────────────┐   ┌─────────────┐   ┌─────────────┐
│    EU Cell        │   │   US Cell   │   │  APAC Cell  │
│  europe-west1-b   │   │ us-east1-d  │   │asia-east1-b │
│                   │   │             │   │             │
│ vcs-vm-eu1 (mgr)  │   │ vcs-vm-us1  │   │ vcs-vm-ap1  │
│ vcs-vm-eu2 (wkr)  │   │             │   │             │
│                   │   │             │   │             │
│ Docker Swarm A    │   │  Swarm B    │   │  Swarm C    │
│ (2-node cluster)  │   │ (1-node)    │   │  (1-node)   │
└────────┬──────────┘   └──────┬──────┘   └──────┬──────┘
         │                     │                  │
         │    CockroachDB Raft Replication         │
         └─────────────────────┴──────────────────┘
              Every write → Raft quorum (2/3 nodes)
              All 3 nodes hold all data at all times
```

**Swarms are independent** — they never join each other. Cross-cell coordination happens exclusively through CockroachDB's Raft replication. Redis is local per cell.

---

## 3. Infrastructure & Deployment

### 3.1 GCP Resources

| Resource | Detail |
|---|---|
| Project | `distributed-capacity-system` |
| Network | Custom VPC `vcs-vpc` |
| EU subnet | `10.0.1.0/24` — `europe-west1` |
| US subnet | `10.0.2.0/24` — `us-east1` |
| APAC subnet | `10.0.3.0/24` — `asia-east1` |
| VM type | `e2-medium` (2 vCPU, 4 GB RAM) |
| EU VMs | `vcs-vm-eu1` (10.0.1.11) + `vcs-vm-eu2` (10.0.1.12) |
| US VM | `vcs-vm-us1` |
| APAC VM | `vcs-vm-ap1` |
| VM schedule | Stop 20:00, Start 08:00 weekdays, Europe/Dublin — cost saving |

### 3.2 HTTPS / TLS Architecture

Firebase web push requires HTTPS. No real domain was available, so **nip.io** is used — a wildcard DNS service that maps `<IP>.nip.io → <IP>`. GCP managed TLS certs are issued per-region and auto-renew.

```
Browser (HTTPS :443)
    │
    ▼
GCP Global HTTPS LB           ← TLS terminates here
    │  X-Forwarded-Proto: https
    ▼
nginx (HTTP :80 internal)     ← reads X-Forwarded-Proto, passes $real_proto upstream
    │
    ▼
Backend services              ← see correct scheme for Swagger, redirects, etc.
```

| Region | Global Static IP | HTTPS URL |
|---|---|---|
| EU | 35.244.162.92 | https://35.244.162.92.nip.io |
| US | 35.227.198.68 | https://35.227.198.68.nip.io |
| APAC | 34.8.134.246 | https://34.8.134.246.nip.io |

HTTP requests on :80 get a `301 Moved Permanently` to HTTPS.

**Old regional LB (HTTP only, still running):**  
EU: `34.78.55.96` — left untouched alongside the new HTTPS LB.

### 3.3 Docker Swarm Stack

Every cell runs a Docker Swarm stack named `vcs`. Every service is deployed in `mode: global` — one container per Swarm node. For EU (2 nodes), that means 2 containers of every service. For US and APAC (1 node each), it's 1 container per service.

**Services in the stack:**

| Service | Image | Port | Role |
|---|---|---|---|
| `nginx` | `ghcr.io/.../nginx` | 80 (ingress) | API gateway + SPA |
| `iam-service` | `ghcr.io/.../iam-service` | 8082 (internal) | Auth & identity |
| `journey-service` | `ghcr.io/.../journey-service` | 8083 (internal) | Booking orchestrator |
| `capacity-service` | `ghcr.io/.../capacity-service` | 8081 (internal) | Slot reservation |
| `map-service` | `ghcr.io/.../map-service` | 8084 (internal) | Routing & geocoding |
| `notification-service` | `ghcr.io/.../notification-service` | 8085 (internal) | Push notifications |
| `db` | `cockroachdb/cockroach:v24.1.0` | 26257 (Raft), 8080 (UI) | Distributed SQL |
| `redis` | `redis:7-alpine` | 6379 (internal) | Event stream + cache |
| `prometheus` | `prom/prometheus:v2.51.0` | 9090 (manager only) | Metrics scrape |
| `grafana` | `grafana/grafana:10.4.0` | 3000 (manager only) | Dashboards |
| `loki` | `grafana/loki:2.9.4` | 3100 (manager only) | Log aggregation |
| `promtail` | `grafana/promtail:2.9.4` | — (global) | Log shipping |
| `cadvisor` | `gcr.io/cadvisor/cadvisor` | — (global) | Container metrics |
| `redis-exporter` | `oliver006/redis_exporter` | — (global) | Redis metrics |

**Docker Swarm secrets (created manually on each manager):**

```bash
echo "your-jwt-secret" | docker secret create jwt_secret -
cat firebase-credentials.json | docker secret create firebase_credentials -
cat iam_private_key.pem | docker secret create iam_private_key -
```

### 3.4 CockroachDB Cluster Bootstrap

CockroachDB nodes find their own external IP at container startup via GCP metadata API — no manual IP config needed:

```bash
# First deploy: bootstrap the cluster (run once on EU manager)
docker exec -it <crdb_container> /cockroach/cockroach init \
  --insecure --host=localhost:26257

# Verify all nodes joined
docker exec -it <crdb_container> /cockroach/cockroach node status \
  --insecure --host=localhost:26257
```

Deploy command (run from GCP Cloud Shell):

```bash
CRDB_JOIN="35.187.121.12:26257,34.76.63.61:26257,<us_ip>:26257,<ap_ip>:26257" \
GITHUB_REPOSITORY="ajinkyataranekar/distributed-vehicle-capacity-system" \
IMAGE_TAG="latest" \
SWAGGER_PUBLIC_BASE_URL="https://35.244.162.92.nip.io" \
REGION="EU" \
docker stack deploy --with-registry-auth -c docker-stack.yml vcs
```

### 3.5 Cost-Saving VM Schedule

GCP Instance Schedules (resource policies) automatically stop VMs at 20:00 and start them at 08:00 weekdays (Europe/Dublin timezone). This prevents unexpected spend during nights and weekends.

---

## 4. Services Deep Dive

All five backend services are written in **Go 1.24+** and follow the same internal structure:

```
<service>/
├── cmd/server/main.go        # entry point, wires dependencies
├── config.yaml               # local dev config
├── Dockerfile                # multi-stage build
├── internal/
│   ├── model/                # domain types
│   ├── service/              # business logic
│   ├── repository/           # database access (master/slave pools)
│   ├── handler/ or http/     # HTTP handlers
│   └── event/                # Redis stream producer/consumer
├── migrations/               # SQL migration files (applied by CI)
└── pkg/
    ├── config/               # Viper config loader
    ├── postgres/             # DB connection + migrations runner
    ├── logger/               # zerolog structured logging
    ├── metrics/              # Prometheus metrics
    └── errors/               # typed error codes
```

Each service connects to CockroachDB via two pools:
- **Master pool** — used for writes and transactions
- **Slave pool** — used for read queries (points to same CockroachDB since it's multi-master; this abstraction was inherited from the prior Postgres setup and remains for architectural clarity)

### 4.1 IAM Service (port 8082)

**Responsibility:** Everything to do with identity — who you are, proving it, and what you're allowed to do.

**Key capabilities:**
- User registration with bcrypt password hashing
- Login → issues an access token (JWT RS256, short TTL) + refresh token (opaque, stored in DB)
- Token refresh — rotate refresh token, issue new access token
- Logout + force-logout (invalidates all sessions for a user)
- JWKS endpoint (`/.well-known/jwks.json`) — exposes the RSA public key so other services can verify RS256 tokens without calling IAM
- User profile management (name, email, phone)
- Vehicle registration (car, van, truck, motorcycle) with license info
- Admin: list all users, force-logout any user

**Token design:**
- Access token: JWT, RS256, short-lived (minutes). Claims include `user_id`, `role` (`driver`/`admin`), `email`.
- Refresh token: 64-byte crypto-random hex, stored hashed in DB, long-lived (days).
- JWT signing key: RSA private key injected as Docker secret at `/run/secrets/iam_private_key`.

**How other services validate tokens:**  
At startup, each service fetches the JWKS from `http://iam-service:8082/.well-known/jwks.json` and caches the RSA public key. All incoming requests are validated locally — no IAM call per request.

**Database schema (schema: `auth`):**

```
auth.users          — user accounts (id, email, password_hash, role, name, phone)
auth.refresh_tokens — active sessions (token_hash, user_id, expires_at, user_agent, ip)
auth.vehicles       — registered vehicles per user
```

**Key files:**
- `iam-service/internal/service/auth_service.go` — register, login, refresh, logout
- `iam-service/internal/service/jwks_service.go` — RSA key loading + JWKS endpoint
- `iam-service/internal/repository/token_repo.go` — refresh token CRUD

### 4.2 Journey Service (port 8083)

**Responsibility:** The central orchestrator. Accepts booking requests from drivers, coordinates with Map and Capacity services, manages the full journey lifecycle.

**Journey State Machine:**

```
                    ┌──────────┐
        book()      │  PENDING │
      ┌────────────►│          │
      │             └────┬─────┘
      │                  │ capacity reserved OK
      │                  ▼
      │             ┌──────────┐
      │             │ APPROVED │◄──────── driver can activate here
      │             └────┬──┬──┘
      │                  │  └─── cancel() ──────────────────────┐
      │    capacity full  │                                      │
      │    or error       ▼                                      │
      │             ┌──────────┐   activate()   ┌────────┐      │
      │             │ REJECTED │                │ ACTIVE │      │
      │             └──────────┘   ────────────►│        │      │
      │                                         └───┬────┘      │
      │                                             │ complete() │
      │                                             ▼            ▼
      │                                       ┌──────────┐  ┌──────────┐
      │                                       │COMPLETED │  │CANCELLED │
      │                                       └──────────┘  └──────────┘
      │
      │  departure_time + 30min passes, expiry job runs every 5min
      └──────────────────────────────────────────────────────────►
                                                           ┌──────────┐
                                                           │ EXPIRED  │
                                                           └──────────┘
```

**Booking flow (what happens when a driver books a journey):**

1. Driver submits: origin coords, destination coords, vehicle type, departure time.
2. Journey service calls **Map service** → `POST /api/v1/routes/compute` → gets ordered list of road segments with traversal times.
3. Journey service computes per-segment time windows (segment[i] starts when segment[i-1] ends).
4. Journey service calls **Capacity service** → `POST /api/v1/capacity/reserve` with all segments + time windows.
5. If capacity reserved → journey status becomes `APPROVED`, reservation_id stored.
6. If any segment is at capacity → journey status becomes `REJECTED` with reason.
7. Outbox event written: `journey.approved` or `journey.rejected` → published to Redis stream → Notification service consumes it.

**Idempotency:** Every booking request carries an `Idempotency-Key` header. Journey service stores the response against this key in `journey.idempotency_cache`. Duplicate requests with the same key return the cached response without re-running the booking logic.

**Optimistic locking on status transitions:**  
The `journeys` table has a `version` column. Status updates use `WHERE version = $expected AND journey_id = $id`. If another process changed the status concurrently, the update affects 0 rows → retry or conflict error. This prevents lost-update bugs when two requests try to transition the same journey simultaneously.

**Admin capabilities:** Approve/reject journeys manually, view all journeys with filtering, enforcement endpoint (verify a driver has an active journey for a given road segment).

**Expiry job:** Background goroutine runs every 5 minutes. Any journey in `APPROVED` status where `departure_time + 30 minutes < now` is transitioned to `EXPIRED`. This releases the held capacity slots.

**Route cache:** Computed routes (origin → destination → segments) are cached in Redis with a 24-hour TTL. Same O/D pair on the same day → instant response without calling Map service again.

**Key files:**
- `journey-service/internal/service/journey_service.go` — core booking logic
- `journey-service/internal/model/journey.go` — state machine types
- `journey-service/internal/client/map_client.go` — map service HTTP client + circuit breaker
- `journey-service/internal/client/capacity_client.go` — capacity service HTTP client + circuit breaker
- `journey-service/internal/event/outbox_relay.go` — polls DB outbox, publishes to Redis stream
- `journey-service/internal/client/route_cache.go` — Redis route caching

### 4.3 Capacity Service (port 8081)

**Responsibility:** The enforcer. Manages how many vehicles can be on each road segment at any given time. Every booking goes through here. Rejects instantly if full.

**Core reservation algorithm:**

```
POST /api/v1/capacity/reserve
  │
  ├─ Check idempotency cache (is this a retry?)
  │   └─ Yes → return cached result (no double-booking)
  │
  ├─ Sort segments by segment_id (deadlock prevention)
  │
  ├─ BEGIN SERIALIZABLE TRANSACTION
  │   ├─ For each segment:
  │   │   ├─ SELECT max_capacity FROM capacity.segments WHERE segment_id = $id FOR UPDATE
  │   │   │   (acquires row lock — blocks concurrent reservations on same segment)
  │   │   ├─ SELECT SUM(slots_used) FROM capacity.reservations
  │   │   │   WHERE segment_id = $id AND status = 'active'
  │   │   │   AND time windows overlap with requested window
  │   │   └─ IF current_load + requested_slots > max_capacity → ROLLBACK → "at_capacity"
  │   │
  │   └─ All segments pass → INSERT reservation rows → INSERT idempotency cache → COMMIT
  │
  └─ Return response (reserved | failed)
```

**Why pessimistic locking here?**  
Capacity reservation is a write-heavy operation where two concurrent requests may fight over the last slot. Optimistic locking would cause one to fail, retry, and still potentially lose — adding latency. `FOR UPDATE` + `SERIALIZABLE` makes the outcome deterministic: first request wins, second reads updated load and rejects cleanly.

**What locks, when:**
- Lock acquired: when each segment row is read via `SELECT ... FOR UPDATE` inside the transaction.
- Lock released: automatically at `COMMIT` or `ROLLBACK`. Not held for the duration of a user session.

**Retry on serialization failure:** CockroachDB occasionally returns `SQLSTATE 40001` (serialization conflict) under high concurrency. The service retries the full transaction up to 5 times before returning an error.

**Segment closures:** Admins can close road segments (e.g. for maintenance). A closed segment is effectively treated as having zero remaining capacity — all reservation attempts on it fail with `segment_closed` reason.

**Orphan cleanup:** Background goroutine runs every 5 minutes. Any reservation row in `status='active'` where `time_window_end < now - 5 minutes` is marked `status='released'`. This is the safety net for reservations whose corresponding journeys were deleted or failed to emit a release event.

**Cross-regional Saga:** If a journey's segments span multiple CRDB geo-regions (e.g. EU + APAC segments), the `Saga Coordinator` takes over instead of the single transaction:

```
segments grouped by crdb_region:
  { "eu": [seg_A, seg_B], "apac": [seg_C, seg_D] }

Step 1: Reserve EU segments (local tx on EU CRDB node)  → saga step RESERVED
Step 2: Reserve APAC segments (local tx on APAC node)   → saga step RESERVED
Step 3: All RESERVED → saga COMMITTED

Failure at Step 2:
  Compensating tx: release EU reservations             → saga step COMPENSATED
  Journey → REJECTED
```

Saga state is persisted in `capacity.reservation_sagas` — crash-safe. On retry, the coordinator reads the saga record and compensates any already-reserved steps before retrying fresh.

**Occupancy endpoint:** Returns real-time load percentage per segment — used by the frontend traffic map and admin dashboard.

**Key files:**
- `capacity-service/internal/service/reservation_service.go` — `doReserveTx`, orphan cleanup
- `capacity-service/internal/service/saga_coordinator.go` — multi-region saga
- `capacity-service/internal/repository/reservation_repo.go` — DB access
- `capacity-service/migrations/001_create_schema.sql` — table definitions

### 4.4 Map Service (port 8084)

**Responsibility:** Computes real driving routes via OSRM, registers the resulting road segments in the capacity service, and geocodes place names via Nominatim.

---

> **The system uses ONE routing mode: dynamic OSRM.** The old static Dijkstra endpoint still exists in the codebase but is not called by anything — not the frontend, not journey-service. Do not confuse the two.

---

#### What actually runs in production: Dynamic OSRM routing

**Endpoint:** `POST /api/v1/routes/compute`  
**Called by:** Frontend (`BookJourneyPage`) and Journey Service (`map_client.go`)

Full pipeline for every new route:

```
User provides lat/lng origin + destination
  → OSRM (router.project-osrm.org) returns real driving steps
      e.g. [{ref: "N1", duration: 180s}, {ref: "M50", duration: 240s}, ...]
  → collapseSteps() groups consecutive steps by road ref
      → deriveSegmentID() applies geo-region prefix from maneuver coordinates
          geoRegion(lat, lng): lng < -25 → "us", lng < 29.1 → "eu", lng ≥ 29.1 → "ap"
          Named road  → <region>_<sanitised_name>  e.g. eu_n1, eu_m50, ap_nh_44
          Unnamed road → <region>_<lat2dp>_<lng2dp> e.g. ap_35.67_139.65
  → ensureCapacitySegments() → POST /api/v1/capacity/segments/register
      → capacity-service inserts each eu_*/ap_*/us_* ID with default 100-slot capacity
  → route + segments persisted to map.routes / map.route_segments / map.intercity_segments
  → next request for the same coord pair hits the DB cache (no OSRM round-trip)
```

Segment IDs produced by this flow: `eu_n1`, `eu_m50`, `eu_r108` (Dublin routes), `us_i_95` (US), `ap_nh_44` (APAC), etc. — derived from the road reference OSRM returns, prefixed by the geographic region of the road's maneuver coordinates. These are the IDs that capacity-service tracks and enforces.

**Default capacity for OSRM-discovered segments:** 100 slots (set in `capacity.settings.default_segment_max_capacity` via migration 004). Admins can change the default or override per-segment via the admin UI.

**Geocoding (Nominatim):** Free-text place search (e.g. "Dublin Airport") is proxied to `nominatim.openstreetmap.org` and cached in `map.places`. Coordinates returned are then passed to the OSRM route call.

---

#### What does NOT run: Static Dijkstra (dead endpoint)

**Endpoint:** `GET /api/v1/map/route?origin_node_id=...&destination_node_id=...`  
**Status: Dead code — not called by the frontend or journey-service.**

At startup the service still loads 10 hardcoded Dublin nodes and 13 segments from the DB into an in-memory graph (`GraphStore`) and can serve this endpoint — but nothing hits it. The 13 seeded segments (`seg_city_north`, `seg_n1_north`, etc. with capacities 55–120) exist in `defaultdb.capacity.segments` but are never reserved by real bookings.

This was an earlier implementation that was superseded when OSRM dynamic routing was added. It remains in the codebase as a fallback reference but serves no function in the live system.

| | Static Dijkstra | Dynamic OSRM |
|---|---|---|
| Endpoint | `GET /api/v1/map/route` | `POST /api/v1/routes/compute` |
| Called by frontend | **No** | **Yes** |
| Called by journey-service | **No** | **Yes** |
| Segment IDs | `seg_city_north`, `seg_n1_north` | `eu_n1`, `eu_m50` (Dublin); `us_i_95` (US); `ap_nh_44` (APAC) |
| Capacity | 55–120 (seeded) | 100 default (auto-registered) |
| Route source | Hardcoded Dublin graph | Real roads via OSRM |

---

**Key files:**
- `map-service/internal/http/handlers/compute_route_dynamic.go` — OSRM route pipeline (the live path)
- `map-service/internal/http/handlers/geo_client.go` — OSRM + Nominatim HTTP clients
- `map-service/internal/http/handlers/capacity_client.go` — registers new segment IDs in capacity-service
- `map-service/internal/http/handlers/search_handler.go` — Nominatim geocoding proxy
- `map-service/internal/http/handlers/graph_store.go` — in-memory Dijkstra graph (loaded but unused in booking flow)
- `map-service/internal/http/handlers/map_handler.go` — static Dijkstra handler (unused)

### 4.5 Notification Service (port 8085)

**Responsibility:** Listens for journey events on the Redis stream and pushes notifications to drivers. Also maintains an in-app notification inbox for each user.

**Event consumption flow:**

```
Journey Service
  │ writes lifecycle events to journey.outbox (DB)
  │
Outbox Relay goroutine (in Journey Service)
  │ polls every 1 second, publishes to Redis Stream "journey.events"
  │
Redis Stream "journey.events"
  │ consumer group: notification-service
  │
Notification Service consumer
  │ reads events via XREADGROUP (blocking, batch of 10)
  │ maps event type → notification message
  │ persists to notification.notifications (DB)
  │ pushes via Firebase FCM (if device token registered)
  └─ marks message as ACK'd in stream
```

**Events consumed:**

| Event | Notification message |
|---|---|
| `journey.approved` | "Your journey has been approved and capacity reserved" |
| `journey.rejected` | "Your booking was rejected: [reason]" |
| `journey.cancelled` | "Journey cancelled" |
| `journey.completed` | "Journey completed" |
| `journey.expired` | "Your approved journey has expired" |

**Firebase FCM (web push):**
The service uses a custom FCM HTTP v1 client (not the Firebase Admin SDK). It manages an OAuth2 access token using the service account JSON (injected as Docker secret). The access token is cached in-memory and refreshed before expiry.

**Current FCM status:** Device token registration requires HTTPS (enforced by browsers for service workers). HTTPS is working (nip.io certs active). However, device tokens may not be registering reliably due to service worker registration timing on the frontend. The frontend uses polling as a fallback — it periodically calls the notifications API and updates the UI badge count. FCM push works when device tokens are present; the frontend shows graceful "push unavailable" messaging when not.

**Admin notifications:** Admins can receive alerts for enforcement events (journeys without an active booking on a segment). These are sent via the same FCM pathway with a separate notification type.

**Deduplication:** The consumer tracks recently processed event IDs in-memory to prevent duplicate notifications if the same Redis stream message is delivered more than once (at-least-once delivery guarantee).

**Pending reclaim:** At startup, the consumer runs `XAUTOCLAIM` to reclaim any messages that were delivered to a crashed consumer but never acknowledged.

**Key files:**
- `notification-service/internal/event/consumer.go` — Redis stream consumer
- `notification-service/internal/fcm/client.go` — Firebase HTTP v1 client
- `notification-service/internal/service/memory_store.go` — deduplication state

---

## 5. Data Layer

### 5.1 CockroachDB

**Why CockroachDB (and not Postgres)?**  
The system started on PostgreSQL with streaming replication (EU primary → US replica → APAC replica). The fundamental problem: US and APAC cells could only read — any write had to go to EU, defeating the purpose of regional cells. A US user booking a journey would experience EU-level latency even when routed to the US cell.

CockroachDB solves this natively:
- Every node accepts writes (multi-master / active-active)
- Raft consensus ensures writes are durable across a quorum before acknowledging
- No manual promotion if a node fails — automatic
- PostgreSQL wire protocol compatible — application code unchanged

**Schema layout:**

| Schema | Owner service | Key tables |
|---|---|---|
| `auth` | IAM | `users`, `refresh_tokens`, `vehicles` |
| `journey` | Journey | `journeys`, `journey_segments`, `outbox`, `idempotency_cache` |
| `capacity` | Capacity | `segments`, `reservations`, `idempotency_cache`, `reservation_sagas` |
| `map` | Map | `nodes`, `segments`, `places`, `intercity_segments` |
| `notification` | Notification | `notifications`, `device_tokens` |
| — | CI | `schema_migrations` (tracks applied SQL files) |

**Replication model:**  
Every row exists on all 3 CRDB nodes. A write is not acknowledged until 2 out of 3 nodes (Raft quorum) have confirmed. This is automatic — no configuration needed.

**Geo-partitioning (planned / documented):**  
`docs/data-partitioning-and-sharding.md` documents the full DDL to pin `capacity.reservations` and `journey.journeys` leaseholders to their home region using `CONFIGURE ZONE` + `PARTITION BY LIST`. This reduces write latency by ensuring the local CRDB node is the leaseholder for data owned by that region. The DDL is staged but not yet applied on the live cluster — the current cluster operates with default CRDB zone configs.

**Consistency model:** `SERIALIZABLE` for all write transactions (CockroachDB default). Reservation transactions explicitly set `sql.LevelSerializable`. This prevents phantom reads and guarantees that two concurrent bookings for the last slot cannot both succeed.

**Migration tracking:**  
CI applies `.sql` files from each service's `migrations/` directory in filename-sort order. A `schema_migrations` table tracks which files have been applied (keyed as `<service>/<filename>`). Idempotent — already-applied migrations are skipped.

### 5.2 Redis

Redis runs locally within each cell. There is no cross-cell Redis sync.

**Usage:**

| Use | Key / Stream | TTL |
|---|---|---|
| Event bus | Stream: `journey.events` | Consumer group: `notification-service` |
| Route cache | `route:<origin>:<dest>:<date>` | 24 hours |
| Session state | — (tokens stored in CRDB, not Redis) | — |

**Config:**
- `maxmemory: 96mb`
- `maxmemory-policy: allkeys-lru` — least-recently-used keys evicted when full
- No persistence (`save ""`, `appendonly no`) — Redis is a cache/stream, not source of truth

**Implication:** If Redis restarts, the stream is empty. Events already ACK'd are gone (this is fine — they were processed). Unprocessed events are in `journey.outbox` with `published = FALSE` — the outbox relay will re-publish them on restart.

---

## 6. Event System (Outbox + Redis Streams)

**The problem being solved:** A naive approach would have Journey Service publish to Redis directly after the DB commit. But what if the process crashes between the DB commit and the Redis publish? The journey is saved, but no notification is ever sent.

**The solution: Transactional Outbox Pattern**

```
Journey Service (single DB transaction):
  ┌─────────────────────────────────────────────────────────┐
  │  INSERT INTO journey.journeys (status = 'APPROVED')     │
  │  INSERT INTO journey.outbox   (event_type, payload, ...) │
  │  COMMIT                                                  │
  └─────────────────────────────────────────────────────────┘
           ↕  Both rows committed atomically — always consistent

Outbox Relay goroutine (separate goroutine in Journey Service):
  ┌────────────────────────────────────────────────────────────────┐
  │  Every 1 second:                                               │
  │    SELECT * FROM journey.outbox WHERE published = FALSE        │
  │    For each event: XADD journey.events <payload>               │
  │    UPDATE journey.outbox SET published = TRUE WHERE id = $ids  │
  └────────────────────────────────────────────────────────────────┘

Notification Service consumer:
  XREADGROUP GROUP notification-service $consumer COUNT 10 BLOCK 5000
    → processes each message → XACK
    → on startup: XAUTOCLAIM to reclaim messages from crashed consumers
```

**Delivery guarantee:** At-least-once. The outbox relay may publish the same event twice if it crashes after the Redis `XADD` but before updating `published = TRUE`. The notification consumer deduplicates by event ID.

**Event types in the stream:**

```json
{
  "event_id": "evt_01J...",
  "event_type": "journey.approved",
  "journey_id": "jrn_01J...",
  "driver_id": "usr_01J...",
  "timestamp": "2026-04-14T10:00:00Z",
  "payload": { ... journey details ... }
}
```

---

## 7. API Gateway (nginx)

nginx serves three roles:
1. **Reverse proxy** — routes requests to backend services based on URL prefix
2. **Static file server** — serves the compiled React SPA
3. **Rate limiter** — protects backends from abuse

**Routing table:**

| URL prefix | Upstream | Rate zone |
|---|---|---|
| `/api/v1/auth/` | `iam-service:8082` | api (30 r/s) |
| `/api/v1/admin/auth/` | `iam-service:8082` | api (30 r/s) |
| `/.well-known/` | `iam-service:8082` | — |
| `/api/v1/journeys` | `journey-service:8083` | booking (5 r/s) |
| `/api/v1/admin/` | `journey-service:8083` | api (30 r/s) |
| `/api/v1/enforcement/` | `journey-service:8083` | api (50 r/s) |
| `/api/v1/capacity/` | `capacity-service:8081` | api (30 r/s) |
| `/api/v1/map/` | `map-service:8084` | api (30 r/s) |
| `/api/v1/routes/` | `map-service:8084` | api (30 r/s) |
| `/api/v1/notifications` | `notification-service:8085` | api (30 r/s) |
| `/api/v1/admin/notifications` | `notification-service:8085` | api (30 r/s) |
| `/docs/iam/`, `/docs/journey/`, etc. | respective service (Swagger) | — |
| `/` | SPA static files | — |
| `/api/v1/region` | `nginx/html/region.json` (static) | — |

**Booking endpoint rate limit** is stricter (5 r/s burst 10) to protect the DB-heavy reservation flow.

**Swagger proxying:** nginx rewrites `/docs/<svc>/` → `/swagger/` on the backend, then uses `sub_filter` to rewrite paths back in the response HTML/JS so Swagger UI loads assets correctly. `Accept-Encoding ''` disables gzip so `sub_filter` can read the body.

**X-Forwarded-Proto fix:** GCP LB terminates TLS and sends plain HTTP to nginx. Without a fix, nginx would always forward `X-Forwarded-Proto: http`. A `map` block preserves the original scheme:

```nginx
map $http_x_forwarded_proto $real_proto {
    default $scheme;
    https   https;
}
```

**Upstream DNS resolution:** `resolver 127.0.0.11 valid=10s ipv6=off` uses Docker Swarm's embedded DNS. Upstream names (`capacity-service`, etc.) are set as variables so nginx starts even if backends aren't up yet.

**All proxy timeouts:** 60s read, 5s connect. OSRM calls can legitimately take 20-30s, hence the generous read timeout.

---

## 8. Frontend

**Stack:** React 18 + Vite + TypeScript + Tailwind CSS + shadcn/ui

**Built and served by nginx.** The Vite build output is copied into `nginx/html/` during the CI pipeline's `build-frontend` job, then baked into the nginx Docker image.

**Pages:**

| Path | Role | Description |
|---|---|---|
| `/login` | Public | Login + registration. Demo credentials button for quick access |
| `/dashboard` | Driver | Upcoming journeys, quick actions, notifications badge |
| `/book` | Driver | Book a new journey: place search, map preview, vehicle selection, departure time picker |
| `/journeys` | Driver | All my journeys with status + detail view |
| `/journeys/:id` | Driver | Journey detail: route map, segments, time windows, status history |
| `/booking-result` | Driver | Post-booking result (approved/rejected) with route visualization |
| `/notifications` | Driver | In-app notification inbox |
| `/settings` | Driver | Profile, vehicle management, notification preferences |
| `/admin` | Admin | Dashboard with system metrics, journey overview |
| `/admin/journeys` | Admin | All journeys across all drivers, filtering, manual approve/reject |
| `/admin/journeys/:id` | Admin | Admin journey detail with action history |
| `/admin/analytics` | Admin | Usage analytics and segment occupancy |
| `/admin/traffic` | Admin | Live traffic map showing segment occupancy |
| `/admin/closures` | Admin | Manage road segment closures |
| `/admin/enforcement` | Admin | Journey enforcement checking |
| `/admin/notifications` | Admin | Admin notification management |

**Map:** OpenStreetMap tiles via MapLibre (replaced TomTom SDK which had API cost issues). Place search uses the custom `/api/v1/map/search` endpoint (Nominatim proxy).

**Push notifications:** Firebase SDK is integrated. The service worker (`firebase-messaging-sw.js`) handles background push messages. Device token is registered with notification-service after user grants permission. If FCM is unavailable or device is not registered, the frontend falls back to polling (`/api/v1/notifications`) every 30 seconds and shows a badge with unread count.

**Region awareness:** The frontend calls `/api/v1/region` to detect which regional cell it's connected to and displays this in the UI. Nominatim defaults are set per region.

**Build config (injected by CI at build time):**

```bash
VITE_FIREBASE_VAPID_KEY=BMtHzh3irkREAnJQEE2plxNqbTv44cGM9e3xHXRYy2ujgE8q89VXaBkgDX98ttEXeDlRVPptio2iDUeod5UokpU
```

---

## 9. Key Design Decisions & Why

### 9.1 Docker Swarm vs Kubernetes

**Chose: Docker Swarm**

Why not Kubernetes: K8s would require a managed GKE cluster (~3× the VM cost), or self-managed master nodes consuming VM quota needed for app workloads. For a 1-month academic project, the operational overhead and cost were prohibitive.

Docker Swarm gives: overlay networking, rolling updates, service discovery, secret management — everything needed — with minimal setup overhead. A `docker stack deploy` command deploys the entire stack.

**Trade-off accepted:** No auto-scaling, no advanced scheduling (node selectors beyond placement constraints), no built-in ingress controller.

### 9.2 HTTP vs gRPC for Inter-service Communication

**Chose: HTTP/JSON**

Why not gRPC: gRPC would require proto file management, code generation, and HTTP/2 setup — adding ~1 week of overhead. For a system built in 1 month, HTTP/JSON with proper timeout configuration achieves the same result with simpler debugging (curl works, Swagger works).

**Trade-off accepted:** Slightly higher per-request overhead than gRPC binary framing. Acceptable for academic scale.

### 9.3 Redis Streams vs Kafka

**Chose: Redis Streams**

Why not Kafka: Kafka requires ZooKeeper (or KRaft), brokers, and significant operational overhead. Redis is already in the stack for route caching. Redis Streams provide consumer groups, at-least-once delivery, and pending message reclaim — sufficient for this use case.

**For production:** Kafka would be the right choice for: guaranteed ordering per partition, long-term event retention, replay, and higher throughput. The Outbox → Stream architecture is Kafka-compatible — swapping Redis for Kafka would only change the `StreamWriter` interface implementation.

### 9.4 Pessimistic Locking for Capacity Reservation

**Chose: SELECT FOR UPDATE + SERIALIZABLE**

Two concurrent bookings fight over the last slot. With optimistic locking, both would read "1 slot available", both would try to write, one fails on version mismatch, retries, and still may race again. Under high contention this causes retry storms.

Pessimistic locking makes it deterministic:
- Request A acquires the row lock
- Request B blocks until A commits
- B reads the updated load, sees 0 remaining, returns `at_capacity` immediately
- No retry needed, no cascading failures

**Trade-off accepted:** Under low contention, pessimistic locking is slightly slower (lock acquisition overhead). For safety-critical capacity reservation, correctness trumps throughput.

### 9.5 Optimistic Locking for Journey Status Transitions

**Chose: WHERE version = $expected**

Journey status transitions (APPROVED → ACTIVE, ACTIVE → COMPLETED) don't fight — only the driver who owns the journey makes these changes. The version check catches unexpected concurrent modifications without the overhead of database row locks.

### 9.6 Transactional Outbox for Event Delivery

**Chose: Outbox pattern**

The alternative (publish to Redis in the same HTTP handler after DB commit) risks losing events if the process crashes between the commit and the publish. The outbox writes the event atomically with the journey state change — both succeed or both fail. The relay handles delivery asynchronously.

### 9.7 CockroachDB Over PostgreSQL

**Chose: CockroachDB (migrated from Postgres)**

The original Postgres setup had EU as the single primary. US and APAC cells were read-only replicas. Any write from US or APAC required a round-trip to EU — negating the point of regional cells.

CockroachDB's Raft consensus means every node is a first-class write node. A US user booking a journey hits the US CockroachDB node. The write commits after Raft quorum (2 of 3 nodes acknowledge), then replicates to EU and APAC.

**Application compatibility:** CockroachDB uses the PostgreSQL wire protocol. The Go services use `lib/pq` — no application code changed. The migration was a database infrastructure change only.

### 9.8 nip.io for HTTPS

**Chose: nip.io + GCP managed certs**

Firebase Cloud Messaging web push requires HTTPS — browsers refuse to register service workers on plain HTTP. No real domain was available. nip.io maps `<IP>.nip.io → <IP>`, giving a valid FQDN that GCP can issue a managed TLS cert for.

**Trade-off accepted:** nip.io is a third-party service. If it goes down, DNS resolution for these URLs fails. For an academic project this is acceptable. A real domain + Cloud DNS would be the production path.

---

## 10. Distributed Systems Concepts Implemented

| Concept | Where | How |
|---|---|---|
| **Replication** | CockroachDB | Raft — every row on all 3 nodes, 2/3 quorum required for commit |
| **Consistency model** | CockroachDB | Serializable isolation (default). `doReserveTx` retries on SQLSTATE 40001 |
| **Distributed transactions** | Capacity service | Single serializable tx spans multiple CRDB partitions for cross-regional journeys |
| **Saga pattern** | Capacity service `saga_coordinator.go` | Compensating transactions for cross-regional segment reservations. **Verified working in production** — Istanbul→Ankara route generates eu_ + ap_ segments; saga fires and commits to `capacity.reservation_sagas` (status=`COMMITTED`, both region steps recorded). |
| **Pessimistic locking** | Capacity service | `SELECT FOR UPDATE` on segment rows during reservation |
| **Optimistic locking** | Journey service | `WHERE version = $expected` on journey status transitions |
| **Idempotency** | Journey + Capacity | Idempotency keys stored in DB — safe retry for bookings |
| **Circuit breaker** | Journey service | `gobreaker` on Map and Capacity HTTP clients — opens after 5 consecutive failures, 10s timeout |
| **Transactional outbox** | Journey service | Events written atomically with journey state, polled by relay |
| **Event-driven architecture** | Journey → Notification | Redis Streams consumer group with at-least-once delivery |
| **Fault tolerance** | Docker Swarm mode: global | Every VM runs all services — no single point of failure within a cell |
| **Geographic distribution** | 3 GCP cells | EU, US, APAC — independent Swarms, shared CRDB cluster |
| **Geo-partitioning** | Documented in `docs/data-partitioning-and-sharding.md` | Leaseholder pinning via `CONFIGURE ZONE` + `PARTITION BY LIST` (DDL staged, not yet applied) |
| **Follower reads** | Documented | `AS OF SYSTEM TIME follower_read_timestamp()` for staleness-tolerant reads |
| **Auto-expiry + cleanup** | Journey + Capacity | Expiry job (5min), orphan cleanup (5min) |
| **Rate limiting** | nginx | `limit_req_zone` per IP — api (30r/s), booking (5r/s) |
| **Chaos engineering** | `scripts/chaos/chaos-monkey.sh` | 8 experiments: drain node, scale to 0, firewall block |
| **Observability** | Prometheus + Grafana + Loki | Metrics, dashboards, log aggregation |

---

## 11. CI/CD Pipeline

**File:** `.github/workflows/pipeline.yml`  
**Trigger:** Manual only (`workflow_dispatch`) — no automatic trigger on push.  
**Concurrency:** `cancel-in-progress: true` — a newer dispatch cancels a stuck run.

**Pipeline jobs:**

```
setup
  │  Computes build matrix from input services
  │  Determines image tag (git SHA prefix or override)
  │
  ├─► build-frontend (if nginx is in the service list)
  │     npm ci → npm run build (injects VITE_FIREBASE_VAPID_KEY)
  │     Uploads dist/ as artifact
  │
  ├─► build-push (parallel matrix, one job per service)
  │     Downloads frontend artifact (nginx only)
  │     docker buildx build --push
  │     Tags: ghcr.io/.../service:SHA + :latest
  │     Layer cache: GitHub Actions cache per service
  │
  ├─► deploy-eu
  │     SSH to eu1 + eu2 → docker login ghcr.io
  │     scp migration .sql files to eu1
  │     Run migrations via cockroach sql on running db container
  │     Grant permissions, docker stack deploy
  │
  ├─► deploy-us (same pattern, SWARM_HOST_US secrets)
  │
  └─► deploy-apac (same pattern, SWARM_HOST_AP secrets)
```

**GitHub secrets required:**

| Secret | Purpose |
|---|---|
| `SWARM_SSH_KEY` | Private key for `deploy` user on all VMs |
| `SWARM_KNOWN_HOSTS_EU/US/AP` | SSH host fingerprints |
| `SWARM_HOST_EU`, `SWARM_HOST_EU2` | EU VM IPs |
| `SWARM_HOST_US` | US VM IP |
| `SWARM_HOST_AP` | APAC VM IP |
| `GHCR_PAT` | Classic PAT with `read:packages` scope — for docker login on VMs |
| `GITHUB_TOKEN` | Auto-provided — used for GHCR push from GH Actions |

**Images stored in:** GitHub Container Registry (GHCR) — `ghcr.io/ajinkyataranekar/distributed-vehicle-capacity-system/<service>:latest`

**Rolling update behaviour:**  
`update_config: parallelism: 1, order: start-first` — new container starts and passes healthcheck before old one stops. Zero-downtime on single-node cells; EU 2-node Swarm updates one node at a time.

**Automatic rollback:** `failure_action: rollback` in service deploy config — if the new container fails health checks, Swarm rolls back to the previous image automatically.

---

## 12. Observability Stack

**Prometheus → Grafana → Loki** — the classic PLG stack.

**Metric sources:**

| Source | Prometheus job | What it provides |
|---|---|---|
| All Go services | `scrape_configs` via DNS SD | HTTP request counts, latencies, error rates, Go runtime |
| CockroachDB | `/_status/vars` endpoint | SQL ops, Raft lag, storage size, slow queries |
| Redis | `redis-exporter:9121` | Memory usage, hit rate, stream lag |
| Docker containers | `cadvisor:8080` | CPU, memory, network per container |
| nginx | (logs via promtail) | Access logs |

**Log shipping:** promtail (global — one per node) reads Docker container logs from `/var/lib/docker/containers` and ships to Loki on the manager node. Labels: service name, node ID, cell.

**Grafana dashboards (in `observability/grafana/provisioning/dashboards/`):**

| Dashboard | What it shows |
|---|---|
| `vcs-overview.json` | SRE Command Center — service health, request rates, error budgets |
| `cockroachdb.json` | CRDB SQL ops, Raft health, slow queries, node status |
| `container-resources.json` | CPU, memory per container across all nodes |
| `log-explorer.json` | Loki log search with service/level filters |
| `redis.json` | Memory, keyspace hits, stream consumer lag |
| `service-deep-dive.json` | Per-service request breakdown, endpoint-level latency |

**Access:**
- EU Grafana: http://34.76.63.61:3000 (admin/admin)
- US Grafana: http://34.138.242.217:3000
- APAC Grafana: http://34.80.180.64:3000

**Current state:** Dashboards are live with real data. No alerts configured — monitoring is manual (dashboards only). Adding Grafana alerting (or PagerDuty) would be the next step for a production system.

---

## 13. Chaos Engineering

**Goal:** Verify the system degrades gracefully and self-heals when individual components fail.

**Tool:** `scripts/chaos/chaos-monkey.sh` — runs from GCP Cloud Shell against the live deployed stack.

**Experiment types:**

| Type | What it does | Restore |
|---|---|---|
| `scale-service` | Adds impossible placement constraint → Swarm schedules 0 tasks | Remove constraint |
| `drain-node` | Marks Swarm node as drain → all containers evacuated | Set node active |
| `block-firewall` | GCP firewall DENY rule on target VM | Delete firewall rule |

**Safety guarantees:**
- Pre-flight health check before every experiment
- `EXIT` trap — always runs cleanup regardless of how the script exits
- CRDB quorum guard — blocks `drain-node` if it would leave fewer than 3 CRDB nodes
- Single-node limit — only one VM or service under chaos at any time
- Early abort — if non-target cells degrade, script aborts and restores

**Full test suite: `scripts/chaos/run-chaos-suite.sh`**

The suite runs 8 experiments in sequence (EU cell):

**Phase 1 — App services (scale to 0):**

| # | Service killed | Expected behaviour |
|---|---|---|
| 1 | `capacity-service` | Journey service circuit breaker trips after 5 consecutive failures. New bookings return 502. Existing journeys unaffected. Breaker auto-closes 10s after capacity-service restores. |
| 2 | `map-service` | Journey service map circuit breaker trips. Booking fails gracefully (route not computable). |
| 3 | `iam-service` | Auth on this node fails. Existing valid JWTs still work (verified locally by other services). New logins fail. |
| 4 | `notification-service` | Notifications fail silently. Journey booking still works — notification failure is not on the critical path. |

**Phase 2 — Infrastructure (scale to 0):**

| # | Component killed | Expected behaviour |
|---|---|---|
| 5 | `redis` | Outbox relay stops (no Redis to publish to). Notifications queued in outbox DB table. Route cache misses (all cache reads return nothing, fresh routes computed). Journeys still book successfully (DB is source of truth). |
| 6 | `db` | All DB operations on this node fail. Services return 503/500. Other Swarm nodes (eu2) continue serving from their local CRDB instance. |

**Phase 3 — Node-level (EU only, requires 2-node Swarm):**

| # | What | Expected behaviour |
|---|---|---|
| 7 | Drain `vcs-vm-eu2` | All eu2 containers evacuated. EU manager (eu1) continues serving all services with 1 container each. Grafana shows reduced resource usage. |
| 8 | Block `vcs-vm-eu2` firewall | External traffic to eu2 is blocked. GCP LB health checks fail for eu2 → traffic only routes to eu1. EU cell continues serving. |

**Report:** Full timeline written to `debug-artifacts/chaos/<timestamp>/`.

**Run the suite:**
```bash
# From GCP Cloud Shell, project root
./scripts/chaos/run-chaos-suite.sh --cell eu --duration 60 --pause 60

# Dry run (no actual changes)
./scripts/chaos/run-chaos-suite.sh --dry-run

# Single experiment
./scripts/chaos/chaos-monkey.sh --service capacity-service scale-service vcs-vm-eu2
```

---

## 14. Load Testing

**Tool:** k6  
**Script:** `scripts/load-testing/k6-load-test.js`  
**Executed against:** Live GCP EU cell (`https://35.244.162.92.nip.io`)

**Scenarios:**

| Scenario | VUs | Duration | Purpose |
|---|---|---|---|
| `smoke` | 1 | 30s | Sanity — does it work at all? |
| `load` | 0→10→20→30→0 | ~11 min | Normal traffic ramp — p95 target < 500ms, error rate < 1% |
| `stress` | Ramped beyond normal | Variable | Find the breaking point |
| `soak` | Steady moderate | Long | Detect memory leaks, resource exhaustion |

**Custom metrics tracked:**
- `journey_create_success` — rate of successful bookings
- `journey_create_errors` — count of failed bookings
- `auth_success` — login success rate
- `capacity_check_duration` — time for capacity reserve calls
- `route_compute_duration` — time for route computation
- `circuit_breaker_trips` — count of circuit breaker open events

**Run:**
```bash
# Smoke against EU prod
K6_SCENARIO=smoke BASE_URL=https://35.244.162.92.nip.io k6 run scripts/load-testing/k6-load-test.js

# Full load test
K6_SCENARIO=load BASE_URL=https://35.244.162.92.nip.io k6 run scripts/load-testing/k6-load-test.js

# Export metrics for Grafana
k6 run --out json=results.json scripts/load-testing/k6-load-test.js
```

---

## 15. Known Limitations & Tech Debt

> **All issues below are verified against the live GCP cluster as of April 2026.**

### Critical (affects correctness or data integrity)

| Issue | Impact | Fix |
|---|---|---|
| **Services write to `defaultdb`, not `trafficservice`** | **Verified live.** `defaultdb` holds the real operational data (users, journeys, reservations). `trafficservice` holds only the CI-seeded data (1 admin, 20 segments). This is not breaking functionality but means the CI `schema_migrations` table in `trafficservice` is tracking a database nobody uses. | Fix the CI pipeline to grant `CREATE ON DATABASE trafficservice TO postgres`. Run a one-time `pg_dump defaultdb \| psql trafficservice` if data migration is needed. Or accept `defaultdb` as the live database. |

### Operational (no data loss, but affects reliability)

| Issue | Impact | Fix |
|---|---|---|
| **APAC writes to eu_* segments time out (~15s)** | **Verified live.** Booking from the APAC cell for a Dublin route (all `eu_*` segments) hits a 15-second CRDB timeout: `pq: query execution canceled`. Root cause: `eu_*` segment leaseholders live on EU CRDB nodes. The APAC capacity-service must acquire Raft quorum from EU nodes across the Pacific for every `SELECT FOR UPDATE` — roundtrip latency exceeds the DB query deadline. APAC _reads_ (occupancy, check) work fine. The `demo.sh` suite explicitly skips APAC write tests and documents this as a known limitation. | Apply `CONFIGURE ZONE USING lease_preferences` geo-partitioning DDL from `docs/data-partitioning-and-sharding.md` to pin `eu_*` leaseholders to EU nodes and `ap_*` leaseholders to APAC nodes. This is staged but not yet applied. |
| **VM resource limits too low** | `e2-medium` (2 vCPU, 4 GB RAM) runs all services + CockroachDB + full observability stack. Under k6 stress, CPU contention causes p99 latencies > 2s | Increase to e2-standard-4, or move CRDB to dedicated VMs |
| **No connection pooler (PgBouncer/HAProxy)** | Each service opens its own `lib/pq` connection pool to CRDB. Under high concurrency, CRDB starts refusing new connections before services saturate. Observed in load test. | Add PgBouncer sidecar in front of CRDB on each node |
| **Firebase FCM device registration unreliable** | **Verified live.** No FCM-related errors in notification-service logs (no push attempts at all — no device tokens registered). Frontend falls back to polling `/api/v1/notifications` every ~60 seconds (visible in logs). Notification inbox works perfectly. FCM push doesn't fire because no device tokens reach the service. | Debug service worker registration in browser devtools on the live HTTPS URL; check `navigator.serviceWorker.register` and `PushManager.subscribe` console output |
| **Redis is cell-local** | **Verified live.** APAC Redis has 2 consumer groups (`capacity-service` + `notification-service`) and 613 events processed. EU Redis has 0 events processed (separate stream per cell). US/APAC bookings generate notifications only in their own cell — EU Notification service never sees them. | Add cross-cell Redis pub/sub, or switch to GCP Pub/Sub for global event fanout |
| **No alerting configured** | Only dashboards — issues spotted only if someone is watching Grafana | Add Grafana alerting rules + PagerDuty/Slack notification channel |
| **Single-node US and APAC** | **Verified live.** US has 1 VM, APAC has 1 VM. Node goes down → cell fully unavailable until VM restarts. No Swarm HA within the cell. | Add a second VM per cell, join Swarm as worker |

### Academic scope (documented, not production-ready)

| Issue | Impact | Fix |
|---|---|---|
| **Geo-partitioning DDL not applied** | `CONFIGURE ZONE` + `PARTITION BY LIST` DDL documented in `docs/data-partitioning-and-sharding.md` but not applied. Leaseholders are not pinned to home regions. CRDB uses default zone configs. | Apply DDL from docs section 5 during maintenance window; requires CRDB Enterprise or CRDB v22.2+ |
| **HTTP-only inter-service communication** | JSON overhead vs gRPC; no streaming | Replace with gRPC for high-throughput paths |
| **Swagger base URL is per-region** | If you open EU Swagger UI and call US endpoints, requests go to EU | Per-cell Swagger is correct for demo purposes |
| **`admin@traffic.ie` seed has placeholder bcrypt hash** | The admin seed in `iam-service/migrations/003_seed_admin.sql` has `$2a$12$REPLACE_THIS_WITH_REAL_BCRYPT_HASH__________________`. The working admin account is `admin@vcs.local` / `admin123` (role: admin). `ajinkyataranekar26@gmail.com` / `test1234` also exists but with role `driver`. | Replace the seed migration with a real bcrypt hash, or document that admin access is via `admin@vcs.local` |

---

## 16. Live System Verification Commands

**Run all of these from GCP Cloud Shell.**

> **Quick option:** `./demo.sh all` runs the complete verification suite (29 tests, coloured output, pass/fail summary). Individual modes: `eu`, `us`, `apac`, `saga`, `clean`. Requires `SSH_KEY=~/.ssh/vcs_key` in environment.

### Check service health per region

```bash
# EU
curl -s https://35.244.162.92.nip.io/api/v1/region | jq .
curl -s https://35.244.162.92.nip.io/health      # nginx health

# US
curl -s https://35.227.198.68.nip.io/api/v1/region | jq .

# APAC
curl -s https://34.8.134.246.nip.io/api/v1/region | jq .
```

### Check running Swarm services (EU manager)

```bash
ssh deploy@35.187.121.12 "docker service ls"
ssh deploy@35.187.121.12 "docker service ps vcs_journey-service"
```

### Check CockroachDB cluster status

```bash
# SSH to EU manager and check all nodes in the Raft cluster
ssh deploy@35.187.121.12 "
  DB=\$(docker ps --filter name=vcs_db --format '{{.ID}}' | head -1)
  docker exec \$DB /cockroach/cockroach node status \
    --insecure --host=localhost:26257
"

# Check database schemas and table sizes
ssh deploy@35.187.121.12 "
  DB=\$(docker ps --filter name=vcs_db --format '{{.ID}}' | head -1)
  docker exec \$DB /cockroach/cockroach sql \
    --insecure --host=localhost:26257 \
    --database=trafficservice \
    --execute=\"SHOW SCHEMAS; SELECT count(*) FROM journey.journeys;\"
"
```

### Tail service logs

```bash
ssh deploy@35.187.121.12 "docker service logs -f --tail 50 vcs_journey-service"
ssh deploy@35.187.121.12 "docker service logs -f --tail 50 vcs_capacity-service"
ssh deploy@35.187.121.12 "docker service logs -f --tail 50 vcs_notification-service"
```

### Check schema migrations applied (CI tracking table — in trafficservice)

```bash
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "
  DB=\$(docker ps --filter name=vcs_db --format '{{.ID}}' | head -1)
  docker exec \$DB /cockroach/cockroach sql \
    --insecure --host=localhost:26257 \
    --database=trafficservice \
    --execute=\"SELECT name, applied_at FROM schema_migrations ORDER BY applied_at;\"
"
```

> **Important:** The live operational data is in `defaultdb`, not `trafficservice`.  
> Always query `defaultdb` for real user/journey/reservation counts:

```bash
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "
  DB=\$(docker ps --filter name=vcs_db --format '{{.ID}}' | head -1)
  CRDB=\"docker exec \$DB /cockroach/cockroach sql --insecure --host=localhost:26257 --database=defaultdb --format=table\"
  \$CRDB -e 'SELECT count(*) as users FROM auth.users;'
  \$CRDB -e 'SELECT count(*) as journeys FROM journey.journeys;'
  \$CRDB -e 'SELECT count(*) as reservations FROM capacity.reservations;'
  \$CRDB -e 'SELECT status, count(*) FROM journey.journeys GROUP BY status ORDER BY 2 DESC;'
"
```

### Check Redis stream consumer lag (EU)

```bash
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "
  REDIS=\$(docker ps --filter name=vcs_redis --format '{{.ID}}' | head -1)
  docker exec \$REDIS redis-cli XINFO GROUPS journey.events
"
```

### Check active capacity reservations (query defaultdb)

```bash
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "
  DB=\$(docker ps --filter name=vcs_db --format '{{.ID}}' | head -1)
  docker exec \$DB /cockroach/cockroach sql \
    --insecure --host=localhost:26257 \
    --database=defaultdb \
    --execute=\"
      SELECT segment_id, count(*) as active_reservations, sum(slots_used) as total_slots
      FROM capacity.reservations
      WHERE status = 'active'
      GROUP BY segment_id
      ORDER BY total_slots DESC;
    \"
"
```

### Check outbox relay backlog (query defaultdb)

```bash
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "
  DB=\$(docker ps --filter name=vcs_db --format '{{.ID}}' | head -1)
  docker exec \$DB /cockroach/cockroach sql \
    --insecure --host=localhost:26257 \
    --database=defaultdb \
    --execute=\"
      SELECT event_type, count(*) 
      FROM journey.outbox 
      WHERE published = FALSE 
      GROUP BY event_type;
    \"
"
```

### End-to-end booking (verified API format)

```bash
EU="https://35.244.162.92.nip.io"

# 1. Login and get token
TOKEN=$(curl -s -X POST "$EU/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"driver1@demo.ie","password":"Demo1234!"}' | jq -r '.data.token')

# 2. Get admin token (for closure / capacity overrides)
ADMIN_TOKEN=$(curl -s -X POST "$EU/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@vcs.local","password":"admin123"}' | jq -r '.data.token')

# 3. Geocode origin + destination
curl -s "$EU/api/v1/search?q=Dublin+Airport" | jq '.data[0]'

# 4. Compute route (origin/destination as nested objects — NOT flat fields)
ROUTE=$(curl -s -X POST "$EU/api/v1/routes/compute" \
  -H 'Content-Type: application/json' \
  -d '{"origin":{"lat":53.3498,"lng":-6.2603},"destination":{"lat":53.4264,"lng":-6.2499}}')
ROUTE_ID=$(echo "$ROUTE" | jq -r '.data.route_id')
echo "Segments: $(echo "$ROUTE" | jq -r '[.data.segments[].segment_id] | join(", ")')"

# 5. Book journey (departure ≥ 60 min from now)
DEP=$(date -u -d '+70 minutes' +%Y-%m-%dT%H:%M:%SZ)
BOOKING=$(curl -s -X POST "$EU/api/v1/journeys" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"route_id\":\"$ROUTE_ID\",\"departure_time\":\"$DEP\"}")
echo "Status: $(echo "$BOOKING" | jq -r '.data.status')"
echo "Journey: $(echo "$BOOKING" | jq -r '.data.journey_id')"
```

### Capacity exhaustion demo (M50 → max 2 slots)

```bash
# Set eu_m50 to max 2 slots
curl -s -X PUT "$EU/api/v1/capacity/segments/eu_m50/capacity" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"max_capacity": 2}'

# Check current capacity
curl -s "$EU/api/v1/capacity/check?segment_id=eu_m50" | jq '.data | {available_slots, is_closed}'

# Book twice → APPROVED, third → REJECTED with "Segment eu_m50 is at capacity"
# (reset after demo)
curl -s -X PUT "$EU/api/v1/capacity/segments/eu_m50/capacity" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"max_capacity": 100}'
```

### Road closure demo

```bash
# Create a 12-hour closure (must be long enough to overlap booking's TRAVERSAL window)
curl -s -X POST "$EU/api/v1/capacity/closures" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"segment_id":"eu_m50","duration_minutes":720,"reason":"Emergency bridge inspection - M50 Palmerstown"}'

# Verify the closure is active
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
END=$(date -u -d '+2 hours' +%Y-%m-%dT%H:%M:%SZ)
curl -s "$EU/api/v1/capacity/check?segment_id=eu_m50&time_window_start=$NOW&time_window_end=$END" | jq .
# → {"is_closed":true,"closure_reason":"Emergency bridge inspection...","available_slots":0}

# Now try to book a journey through eu_m50 with +70min departure — it will be REJECTED
# ...
# Remove closure by cancelling it in CRDB (API doesn't support DELETE):
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "
  DB=\$(docker ps -qf name=vcs_db)
  docker exec \$DB /cockroach/cockroach sql --insecure --host=localhost:26257 \
    -e \"UPDATE defaultdb.capacity.closures SET status='cancelled' WHERE segment_id='eu_m50' AND status='active';\"
"
```

### Saga verification (cross-regional booking)

```bash
# 1. Istanbul → Ankara — produces both eu_ and ap_ segments
ROUTE=$(curl -s -X POST "$EU/api/v1/routes/compute" \
  -H 'Content-Type: application/json' \
  -d '{"origin":{"lat":41.0082,"lng":28.9784},"destination":{"lat":39.9334,"lng":32.8597}}')
ROUTE_ID=$(echo "$ROUTE" | jq -r '.data.route_id')
EU_SEGS=$(echo "$ROUTE" | jq '[.data.segments[]|select(.segment_id|startswith("eu_"))]|length')
AP_SEGS=$(echo "$ROUTE" | jq '[.data.segments[]|select(.segment_id|startswith("ap_"))]|length')
echo "eu_ segments: $EU_SEGS  ap_ segments: $AP_SEGS"
# Expected: eu_=17, ap_=12 (saga fires when both > 0)

# 2. Book journey using that route
DEP=$(date -u -d '+70 minutes' +%Y-%m-%dT%H:%M:%SZ)
BOOKING=$(curl -s -X POST "$EU/api/v1/journeys" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"route_id\":\"$ROUTE_ID\",\"departure_time\":\"$DEP\"}")
echo "Status: $(echo "$BOOKING" | jq -r '.data.status')"

# 3. Confirm saga committed in CRDB
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "
  DB=\$(docker ps -qf name=vcs_db)
  docker exec \$DB /cockroach/cockroach sql --insecure --host=localhost:26257 \
    -e 'SELECT saga_id, journey_id, status, region_steps FROM defaultdb.capacity.reservation_sagas ORDER BY created_at DESC LIMIT 3;'
"
# Expected output:
#   saga_id          | journey_id | status    | region_steps
# ------------------+------------+-----------+-------------------------------------------------
#   saga_cb1920ab... | jrn_...    | COMMITTED | [{"region":"eu",...},{"region":"apac",...}]
```

### Manual stack redeploy (EU, all services)

```bash
ssh deploy@35.187.121.12 "
  cd ~/vcs
  CRDB_JOIN='35.187.121.12:26257,34.76.63.61:26257' \
  GITHUB_REPOSITORY='ajinkyataranekar/distributed-vehicle-capacity-system' \
  IMAGE_TAG='latest' \
  SWAGGER_PUBLIC_BASE_URL='https://35.244.162.92.nip.io' \
  REGION='EU' \
  docker stack deploy --with-registry-auth -c docker-stack.yml vcs
"
```

### Run chaos experiment

```bash
# Scale capacity-service to 0 (tests circuit breaker in journey-service)
./scripts/chaos/chaos-monkey.sh --service capacity-service \
  --duration 60 scale-service vcs-vm-eu2

# Full 8-experiment suite
./scripts/chaos/run-chaos-suite.sh --cell eu --duration 60
```

---

## 17. Quick Reference

### Service ports

| Service | Port |
|---|---|
| iam-service | 8082 |
| capacity-service | 8081 |
| journey-service | 8083 |
| map-service | 8084 |
| notification-service | 8085 |
| nginx | 80 (public) |
| CockroachDB SQL | 26257 |
| CockroachDB UI | 8080 |
| Redis | 6379 |
| Prometheus | 9090 |
| Grafana | 3000 |

### Live endpoints

| Region | App | Grafana | CRDB UI |
|---|---|---|---|
| EU | https://35.244.162.92.nip.io | http://34.76.63.61:3000 | http://34.76.63.61:8080 |
| US | https://35.227.198.68.nip.io | http://34.138.242.217:3000 | http://34.138.242.217:8080 |
| APAC | https://34.8.134.246.nip.io | http://34.80.180.64:3000 | http://34.80.180.64:8080 |

### API docs (Swagger)

| Service | EU Swagger |
|---|---|
| IAM | https://35.244.162.92.nip.io/docs/iam/ |
| Journey | https://35.244.162.92.nip.io/docs/journey/ |
| Capacity | https://35.244.162.92.nip.io/docs/capacity/ |
| Map | https://35.244.162.92.nip.io/docs/map/ |

### GCP VMs

| VM | Zone | Internal IP | Role |
|---|---|---|---|
| vcs-vm-eu1 | europe-west1-b | 10.0.1.11 | EU Swarm manager |
| vcs-vm-eu2 | europe-west1-b | 10.0.1.12 | EU Swarm worker |
| vcs-vm-us1 | us-east1-d | 10.0.2.11 | US Swarm manager (single-node) |
| vcs-vm-ap1 | asia-east1-b | 10.0.3.11 | APAC Swarm manager (single-node) |

### Journey status machine (quick)

```
PENDING → APPROVED (capacity OK) or REJECTED (at_capacity / error)
APPROVED → ACTIVE (driver activates) or CANCELLED or EXPIRED (30min past departure)
ACTIVE → COMPLETED or CANCELLED
```

### Capacity release triggers

1. **Event-driven (fast):** journey cancelled / completed / expired → outbox → Redis stream → capacity consumer releases reservations
2. **Orphan cleanup (fallback):** background job every 5 min releases reservations where `time_window_end + 5min < now`

### Circuit breaker settings (journey-service)

```
MaxRequests: 1 (half-open probe)
Interval:   30s (counter reset)
Timeout:    10s (open → half-open)
ReadyToTrip: 5 consecutive failures
Protected: Map client (fetchNodes, ComputeRoute), Capacity client (Reserve)
```

---

## 18. Report vs Reality — Accuracy Check

> Cross-reference of every significant claim in the "Clearway - Group C.pdf" report against the live GCP cluster state verified in April 2026.  
> Status key: **CONFIRMED** = verified live | **INACCURATE** = report is wrong | **PARTIAL** = partially true | **NOT DEPLOYED** = code exists, not running in production

### Inaccuracies — Things the Report Gets Wrong

| # | Report Section | What the Report Says | Reality | Evidence |
|---|---|---|---|---|
| 1 | §3.3 — Why CockroachDB | "any two form a quorum" / "majority (two of three nodes)" — implies a 3-node cluster | **INACCURATE.** There are **4 CRDB nodes** (eu1, eu2, us1, ap1 — one per VM). Majority of 4 = **3** nodes, not 2. "Two of three" is simply wrong. | `cockroach node status` from EU manager shows 4 nodes; VMs: eu1 (35.187.121.12), eu2 (34.76.63.61), us1, ap1. The chaos pre-flight guard itself checks "fewer than **three** nodes". |
| 2 | §3.4 — Data Partitioning and Locality | "Read-mostly reference tables (capacity.segments, map.segments, map.nodes) carry **LOCALITY GLOBAL** so every region keeps a local replica, avoiding WAN round-trips" | **NOT APPLIED.** The `ALTER TABLE ... LOCALITY GLOBAL` DDL and `CONFIGURE ZONE USING lease_preferences` commands are documented in `docs/data-partitioning-and-sharding.md` but are **not present in any migration file** that has run. Live tables use default CRDB zone configs. | No migration `005_add_crdb_region.sql` or later has run (capacity-service is on old image). Even for other services, no locality DDL appears in any applied migration. |
| 3 | §3.4 + §7.2 | "The Capacity service implements a full Saga coordinator for cross-region journeys" / "The Saga coordinator for cross-region reservations was implemented" | **CONFIRMED — deployed and verified.** capacity-service was redeployed with the saga image. Migrations `005_add_crdb_region.sql` and `006_create_reservation_sagas.sql` applied. An Istanbul→Ankara booking (17 eu_ + 12 ap_ segments) was booked live: CRDB `capacity.reservation_sagas` shows `status=COMMITTED` with both `{"region":"eu"}` and `{"region":"apac"}` in `region_steps`. | Live CRDB query on `defaultdb.capacity.reservation_sagas` returns saga record `saga_cb1920ab...` with `COMMITTED` status. |
| 4 | §5.2 — E2E Scenario Coverage (Table 5) | Happy path: "PENDING → APPROVED; capacity reduces; **push notification delivered**" | **PARTIAL — push notification NOT delivered.** The journey transitions work and capacity reduces correctly. But FCM push notifications do not fire in production: zero FCM log entries in notification-service, no device tokens registered. The UI falls back to polling `/api/v1/notifications` every ~60s. The **in-app notification inbox** (polling) works; FCM push does not. | `docker service logs vcs_notification-service` — zero lines containing "FCM" or "firebase". 11 notifications visible in DB (1 unread for real user), served via polling endpoint. |
| 5 | §7.2 — Design Decisions in Retrospect | "The Saga coordinator for cross-region reservations was implemented and proved the right approach" (past tense, implies production) | **CONFIRMED — accurate.** The saga coordinator is deployed and has been tested against the production CRDB cluster. A live Istanbul→Ankara booking confirmed the saga fires, both region steps commit, and the result is persisted in `capacity.reservation_sagas` with `status=COMMITTED`. | CRDB query on live cluster; `demo.sh saga` mode reproduces this end-to-end. |

### Confirmed Correct — Things the Report Gets Right

| Claim | Section | Verification |
|---|---|---|
| Three regional cells: EU (35.244.162.92), US (35.227.198.68), APAC (34.8.134.246) | §3.2 | Live HTTPS endpoints all respond; confirmed via `curl` |
| e2-medium VMs, 2 vCPU, 4 GB RAM | §3.2 | GCP Cloud Console; confirmed via `docker stats` showing RSS ~600MB/service |
| IAM: RSA-2048, RS256 JWT, bcrypt cost-12, JWKS at `/.well-known/jwks.json` | §3.5.1 | `iam-service/internal/auth/jwt.go`; `/.well-known/jwks.json` returns RSA public key live |
| Journey: three-phase booking (route → reserve → persist); no partial commit possible | §3.5.2 | `journey_handler.go:CreateJourney()` — verified code path |
| Journey: expiry job every 60s, outbox relay every 1s | §3.5.2 | `journey-service/internal/job/expiry_job.go`; `event/outbox_relay.go` |
| Journey: circuit breaker wraps Map + Capacity clients; 5 failures → open, 10s timeout | §3.5.2 | `client/map_client.go`, `client/capacity_client.go`; gobreaker settings confirmed |
| Capacity: SERIALIZABLE, SELECT FOR UPDATE, alphabetically-sorted segment locking | §3.5.3 | `reservation_service.go:doReserveTx()` — sort before lock acquisition, explicit SERIALIZABLE |
| Capacity: up to 5 retries on SQLSTATE 40001 | §3.5.3 | `reservation_service.go` retry loop |
| Map: in-memory Dijkstra (GraphStore) boots at startup with hardcoded fallback if DB unavailable | §3.5.4 | Code confirmed — but this endpoint is **not called** by the frontend or journey-service; the live booking flow uses OSRM only |
| Map: OSRM (router.project-osrm.org) + Nominatim for dynamic routes | §3.5.4 | `map-service/internal/client/osrm_client.go`; live route tested successfully |
| Map: newly discovered segment IDs registered in Capacity via `ensureCapacitySegments()` | §3.5.4 | `map-service/internal/service/route_service.go:ensureCapacitySegments()`. IDs use geo-region prefix: `eu_m50`, `ap_o_4`, `us_i_95` (not `osrm_*` as early docs implied) |
| Notification: XREADGROUP blocking 5s, batch 10; XAUTOCLAIM on startup; UNIQUE(event_id) | §3.5.4 | `notification-service/internal/event/consumer.go` |
| Transactional outbox: same TX as business record; relay replays unpublished on restart | §4.1 | `journey-service/internal/event/outbox_relay.go`; APAC Redis shows 613 events processed |
| Journey state machine: 7 states, 3 terminal; optimistic locking via `version` integer | §4.2 | `journey-service/internal/model/journey.go`; `WHERE id=$1 AND version=$expected` in handler |
| Idempotency cache checked twice (outside + inside TX with SELECT FOR UPDATE) | §4.3 | `capacity-service/internal/service/reservation_service.go` |
| Orphan cleanup: every 5 minutes, threshold 5 minutes | §4.4 | `capacity-service/internal/service/orphan_cleanup.go` |
| Follower reads in capacity occupancy query | §4.4 | `capacity-service/internal/repository/reservation_repo.go` — `AS OF SYSTEM TIME follower_read_timestamp()` |
| k6 results (Table 6): EU Smoke 0 errors p95=1.10s; EU Load 0.34% errors p95=4.23s | §5.3 | `debug-artifacts/` k6 artifacts from 2026-04-10 |
| Chaos testing: 8/8 experiments passed | §5.4 | `debug-artifacts/chaos/` artifacts; `run-chaos-suite.sh` exit 0 |
| Drain-node + block-firewall: 100% availability in surviving cell | §5.4 | Chaos suite Phase 3 results confirmed |
| CockroachDB: scaling one node to 0 → quorum absorbed writes without downtime | §5.4 | Chaos suite Phase 2 exp #6: verified live |
| Redis scaling to 0 → graceful cache-miss fallback, no client-visible errors | §5.4 | Chaos suite Phase 2 exp #5: verified live |
| 6 Grafana dashboards live with real data | §5.5 | Grafana at http://34.76.63.61:3000 — all dashboards accessible |
| Work allocation (Table 7) — each member's contributions | §6 | Cross-referenced against git blame and code ownership |

### Partially Correct / Nuanced

| Claim | Reality |
|---|---|
| "leaseholders co-located with the origin gateway" (§3.4) | **Not applied.** Leaseholder pinning requires `CONFIGURE ZONE USING lease_preferences` — not in any applied migration. Default leaseholder placement is used. |
| "Notification service calls Firebase Admin SDK" (§3.5.4) | Code is correct and the client is functional. But because no browser device tokens are registering (likely service worker registration failing on the HTTPS URL), the FCM dispatch code path is never reached. |
| EU p95 < 5s NFR met | EU Smoke: 1.10s ✓. EU Load: 4.23s ✓. EU Stress: 30s ✗ (exceeds NFR under saturation). |
| US/APAC p95 < 12s cross-region NFR | US Smoke: 6.41s ✓. US Load: 5.70s ✓. APAC Smoke: 7.25s ✓. APAC Load: 6.89s ✓. US/APAC Stress: 30s+ ✗. |

---

## 19. CS7NS6 Exercise 2 Checklist Coverage

> Mapping every rubric checkbox from "2026-04-01vc Exercise 2 2025-2026 Checklist.pdf" to implementation evidence and live GCP status.  
> Status: **PASS** = fully addressed | **PARTIAL** = partially addressed, gaps noted | **FAIL** = not addressed or broken in production

### Overall Architecture

| Checklist Item | Status | Evidence |
|---|---|---|
| Appropriate services provided | **PASS** | 5 Go microservices (IAM 8082, Journey 8083, Capacity 8081, Map 8084, Notification 8085) + React frontend. Clear separation of concerns: auth, booking orchestration, slot management, routing, notifications. |
| Appropriate requirements specified — per service class | **PASS** | Report Table 1: FR1–FR10 functional requirements. Report Table 2: non-functional requirements. Each service has explicit responsibilities listed in the report. |
| Performance requirements | **PASS (quantitative)** | EU p95 < 5s, cross-region p95 < 12s — quantitative targets defined and measured. EU smoke/load tests meet targets. |
| Scalability requirements | **PASS** | Docker Swarm `mode: global` — each service runs on every node. Adding VMs to the Swarm scales capacity linearly. Cell-based architecture scales per-region independently. |
| Availability requirements | **PASS** | Cell-based isolation: regional outage does not affect other cells. CockroachDB 4-node cluster: single-node loss transparent. Chaos testing verified availability under drain and firewall block. |
| Reliability requirements | **PASS** | Transactional outbox (no event loss on crash), idempotency keys (safe retries), orphan cleanup (stale reservation recovery), circuit breakers (fail-fast on dependency outage). |
| Data consistency requirements | **PASS** | SERIALIZABLE isolation level explicitly set. SELECT FOR UPDATE for pessimistic reservation locking. Optimistic locking (version integer) for journey state transitions. |
| Data durability requirements | **PASS** | CockroachDB 4-node Raft: every committed write is on 3+ replicas before the client gets ACK. Single-node loss cannot lose committed data. |
| Requirements quantitative (not just qualitative) | **PASS** | Latency targets in milliseconds (EU p95 < 5s), error rate targets, capacity limits per segment (55–120 slots). |
| Motivated by reference to historic data / load pattern | **FAIL** | Requirements are not motivated by historic traffic data or observed load patterns. Targets are engineering estimates. No baseline data cited. |

### Appropriate Choice of Techniques

| Checklist Item | Status | Evidence |
|---|---|---|
| Transactions | **PASS** | `BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE` in capacity-service. Transactional outbox writes atomically with journey records. |
| Sharding | **PARTIAL** | Geo-partitioning DDL documented (`docs/data-partitioning-and-sharding.md`): `PARTITION BY LIST (crdb_region)`, hash sharding of reservations. Migration 005 (`005_add_crdb_region.sql`) applied — `crdb_region` column exists. Full leaseholder pinning via `CONFIGURE ZONE` not yet applied. Cell-based deployment is itself a form of application-level sharding by region. |
| Caching | **PASS** | Redis: route cache (1-hour TTL), Redis availability cache in capacity-service (30s). In-memory: JWKS cache (hourly refresh). OSRM route results DB-cached in `map.routes` (same coord pair skips OSRM on repeat). |
| Replication | **PASS** | CockroachDB 4-node Raft replication. Every CRDB write replicated to 3+ nodes. Raft leader election auto-heals on node failure. |
| Load balancing | **PASS** | GCP Global HTTPS Load Balancer (external, across cells). nginx rate-limiting within each cell (api: 30r/s, booking: 5r/s). Docker Swarm DNS round-robin within a cell. |
| Request handling | **PASS** | nginx API gateway: path-based routing to all 5 services, HTTPS termination at GCP LB, Swagger sub_filter proxying, health check endpoints. |
| Consistency model | **PASS** | Explicitly SERIALIZABLE (strongest isolation). CAP: CP — consistency over availability under partition. Documented in report §3.2. |
| Update strategy | **PASS** | Docker Swarm rolling updates (Swarm handles task-by-task replacement). GitHub Actions pipeline deploys per-region sequentially (eu → us → apac). |
| Replacement strategy | **PASS** | Docker Swarm `restart_policy: condition=any`. Failed containers auto-restart. `mode: global` reschedules on surviving nodes if a VM is lost. |
| Isolation level | **PASS** | `SERIALIZABLE` explicitly set in `doReserveTx()`. CRDB default is also SERIALIZABLE but it is set explicitly for clarity. |
| Exploit locality | **PARTIAL** | Follower reads (`AS OF SYSTEM TIME follower_read_timestamp()`) in capacity occupancy query reduces cross-region latency for read traffic. Cell-based deployment keeps service-to-DB traffic local (no cross-region service calls). Full geo-partitioning (leaseholder pinning) **not applied**. |
| In-memory data structures | **PASS** | GraphStore (map nodes + segments loaded into `sync.RWMutex`-protected graph), JWKS cache, idempotency cache in memory, 30s Redis availability flag in capacity-service. |

### Concurrency and Conflict Handling

| Checklist Item | Status | Evidence |
|---|---|---|
| Concurrent requests properly synchronized | **PASS** | Alphabetically-sorted `SELECT FOR UPDATE` acquires locks in deterministic order — verified by integration test `reservation_distributed_test.go` (10 goroutines competing for last slot; none over-book). |
| Immediate access to "earned" resources | **PASS** | On journey APPROVE, capacity is immediately deducted (reservation committed). No delayed credit. |
| Conflicting requests properly handled | **PASS** | SERIALIZABLE + retry on SQLSTATE 40001: one transaction commits, the other retries and correctly sees the updated capacity. At-capacity → REJECTED with reason code. |

### Failure Handling

| Checklist Item | Status | Evidence |
|---|---|---|
| Communication failures tolerated | **PASS** | Circuit breakers on Map and Capacity clients (journey-service): 5 consecutive failures → open, fast-fail 502. Redis treated as optional: services degrade gracefully if Redis unavailable (confirmed in chaos Phase 2). |
| Node/replica failure detected/suspected | **PASS** | CRDB Raft: 200ms heartbeats, ~3s election timeout. Swarm: health checks every 30s, unhealthy containers restarted. GCP LB health checks remove unhealthy VMs from backend pool. |
| Replica recovery supported | **PASS** | CRDB: automatic range rebalancing after node failure. Swarm: `restart_policy: condition=any` restarts failed containers. Node rejoin: `docker swarm join` re-adds VM to cluster. |
| Double spending possible | **PASS (prevented)** | No double-booking possible. SERIALIZABLE + SELECT FOR UPDATE ensures exactly one transaction commits when two compete for the last slot. Verified by distributed integration test and chaos testing. |
| Disconnected nodes/replicas tolerated | **PASS** | Chaos Phase 3: drain vcs-vm-eu2 → eu1 continued serving with 0 errors. Block-firewall on eu2 → GCP LB health checks failed eu2, all traffic to eu1, 0 errors in surviving cell. |
| Total failure tolerated | **PARTIAL** | Single-cell failure: other cells unaffected (cell isolation verified). Total cluster failure (all 3 cells down): system unavailable (no active-active cross-cell failover for the application layer). Within a cell: US and APAC are **single-node Swarm** — node loss = cell unavailable until VM restarts. |
| Consistency of data maintained across failures/recoveries | **PASS** | Transactional outbox: journey state and event publication are atomic. On crash, relay replays unpublished events. CRDB durable commit: write not lost even if node fails post-ACK. |

### Partition Handling (CAP)

| Checklist Item | Status | Evidence |
|---|---|---|
| Partitions handled | **PASS** | CAP position: CP (consistency + partition tolerance over availability). On partition, minority CRDB partition cannot commit writes (no quorum). Report §3.2 documents this explicitly. |
| n partitions without majority partition | **PASS** | CRDB: minority partition (< 3 of 4 nodes) cannot form quorum — returns error rather than allowing inconsistent writes. Application returns 503, which is the correct behavior. |
| Merging of partitions supported | **PASS** | CRDB automatic partition recovery: when the network heals, the minority partition rejoins, replays the Raft log, and converges to the majority's state automatically. No manual intervention required. |
| Consistency of data maintained across partitions/merges | **PASS** | CRDB guarantees log replay preserves SERIALIZABLE semantics after merge. Minority cannot commit conflicting writes (no quorum), so no divergence to resolve. |

### Other Features

| Checklist Item | Status | Evidence |
|---|---|---|
| Test application / testing framework | **PASS** | 4-layer testing: (1) Go unit tests per service, (2) `reservation_distributed_test.go` integration with real CRDB, (3) `TestProductionGradeE2ESystemF` against live GCP HTTPS, (4) k6 load + chaos suite. All tests in CI. |
| Middleware used — appropriately motivated | **PASS** | nginx (routing + rate limiting, motivated by single-gateway pattern), Redis Streams (async event bus, motivated by outbox reliability), CockroachDB (HA + SERIALIZABLE, motivated by rejection of single-node PostgreSQL). Alternatives documented in §7.3 (Kafka, gRPC, Kubernetes). |
| GUI Interface | **PASS** | React 18 + TypeScript + Vite PWA. Features: booking form, route preview (OSRM polyline), journey list, admin review, live traffic map, in-app notification inbox with badge. Served via nginx as static build. |

### Summary Score

| Category | Pass | Partial | Fail |
|---|---|---|---|
| Architecture & Requirements | 8 | 0 | 2 |
| Techniques | 9 | 2 | 0 |
| Concurrency | 3 | 0 | 0 |
| Failure Handling | 5 | 1 | 0 |
| Partition Handling (CAP) | 4 | 0 | 0 |
| Other Features | 3 | 0 | 0 |
| **Total** | **32** | **3** | **2** |

**The 2 FAILs:**
1. Requirements not motivated by historic data / load patterns — targets are engineering estimates, no baseline cited.
2. (Implicit in the checklist context) Push notifications via FCM not working in production — a grader testing the live system will see polling fallback, not FCM push.

**The 3 PARTIALs:**
1. Sharding — geo-partitioning DDL designed and documented but not applied on live cluster.
2. Exploit locality — follower reads in place, leaseholder pinning not applied.
3. Total failure tolerance — US and APAC are single-node cells with no intra-cell HA.

---

## 20. Segment ID Design — Limitations and Saga Trigger

### How segment IDs are generated (post-April 2026 fix)

The map-service `geo_client.go` produces segment IDs from OSRM step data:

```
Named road   → <region>_<sanitised_road_name>   e.g. eu_m7, ap_nh_44, us_i_95
Unnamed road → <region>_<lat2dp>_<lng2dp>       e.g. ap_35.67_139.65
```

Region is determined by longitude (`geoRegion()` in `geo_client.go`):

| Longitude range | Region | Examples |
|---|---|---|
| lng < −25° | `us` | New York, São Paulo |
| −25° ≤ lng < 29.1° | `eu` | Dublin, Paris, Istanbul (European side) |
| lng ≥ 29.1° | `ap` | Istanbul (Asian side), Delhi, Tokyo |

29.1°E is the Bosphorus midpoint — the natural EU/Asia geographic boundary.

### Why the Bosphorus works for the demo

A real OSRM route from Istanbul's European district (lng ~28.97°) to Ankara (lng ~32.86°) crosses 29.1°E on the O-4 motorway bridge, producing:

```
eu_zeynepsultan_camii_soka  (Istanbul EU, lng 28.98)
eu_galata_k_pr_s            (Galata Bridge, lng 28.97)
eu_o_4                      (O-4 approach, lng 29.02)
ap_o_4                      ← same road, crosses into Asia at lng 29.10
ap_o_7                      (Asian Turkey)
ap_d750                     (Ankara approach)
```

`groupByRegion()` sees both `eu` and `ap` → `len(groups) == 2` → **saga fires naturally**, no manually seeded test data needed.

To demo this:
```bash
curl -X POST https://35.244.162.92.nip.io/api/v1/routes/compute \
  -H "Content-Type: application/json" \
  -d '{"origin": {"lat": 41.0082, "lng": 28.9784}, "destination": {"lat": 39.9334, "lng": 32.8597}}'
```
Then book a journey using the returned route — capacity reservation will go through the saga coordinator.

### Live proof — saga verified in production (April 2026)

An Istanbul (EU side, lng 28.98°) → Ankara (lng 32.86°) booking was executed against the live EU endpoint. The route crossed the 29.1°E Bosphorus boundary, producing 17 `eu_*` segments and 12 `ap_*` segments (29 total). `groupByRegion()` saw both `eu` and `ap` groups, triggering the saga coordinator instead of a single-region transaction.

CRDB query result on `defaultdb.capacity.reservation_sagas`:

```
         saga_id          |       journey_id       |  status   |                        region_steps
--------------------------+------------------------+-----------+--------------------------------------------------------------
  saga_cb1920ab-...       | jrn_5a2e91f2-...       | COMMITTED | [{"region":"eu","status":"committed","segments":[...]},
                          |                        |           |  {"region":"apac","status":"committed","segments":[...]}]
```

Both region steps show `"status":"committed"` — the saga coordinator successfully reserved capacity on EU CRDB nodes for the European road segments and on APAC CRDB nodes for the Asian segments in a single cross-regional saga, then recorded the composite result.

The `demo.sh saga` mode reproduces this end-to-end and verifies the CRDB table automatically.

### Limitations

**1. Same-name collision within a region (known, acceptable)**

"Ring Road" in Delhi (`ap_ring_road`) and "Ring Road" in Nairobi (`ap_ring_road`) share the same capacity pool. This is wrong physically but acceptable because:
- Both are in APAC; no cross-region correctness issue
- The critical bug (Dublin Ring Road colliding with Delhi Ring Road) is fixed — `eu_ring_road` ≠ `ap_ring_road`
- Fixing within-region collisions would require country-code-aware IDs (not available from public OSRM)

**2. No country codes from public OSRM**

`router.project-osrm.org` does not expose ISO country codes in its response (annotations only return speed/distance/node data). A self-hosted OSRM with a custom Lua profile could expose `country_code` per step, enabling proper country→region mapping. The longitude split is a practical approximation.

**3. Long roads spanning the boundary**

A motorway that runs along the 29.1°E meridian (unlikely but theoretically possible) would be split into `eu_*` and `ap_*` segments, giving each half a separate capacity pool. This is geographically correct — the two halves are in different continents.

**4. Africa / Middle East ambiguity**

Cairo (lng 31.2°) maps to `ap`. Istanbul maps correctly to `eu` (west) and `ap` (east). Russia west of 29.1° (Moscow, lng 37.6°) maps to `ap`. These are imprecise but don't affect the system's primary use case (Irish routes → all `eu`) or the demo (Istanbul→Ankara crossing).

### Key files

| File | Role |
|---|---|
| `map-service/internal/http/handlers/geo_client.go` | `deriveSegmentID()`, `geoRegion()`, `collapseSteps()` |
| `map-service/internal/http/handlers/geo_client_test.go` | 14 tests covering region mapping, naming, Bosphorus crossing, concurrency |
| `capacity-service/internal/service/saga_coordinator.go` | `SegmentCRDBRegion()` — maps `eu_/us_/ap_` prefixes to CRDB regions (legacy `US-/AP-/JP-` dash format also supported) |
| `capacity-service/internal/service/saga_coordinator_test.go` | Tests for new prefix format + legacy dash format backwards compat |
| `demo.sh` | End-to-end verification script — `./demo.sh saga` for saga flow, `./demo.sh all` for full 29-test suite |
