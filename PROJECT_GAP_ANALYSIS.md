# Final Project Gap Analysis — Distributed Vehicle Capacity System
## CS7NS6 Exercise 2 · Strict Audit Against Spec, Checklist & Report Outline
### Audited: 2026-04-07 (post IAM-fix session)

> **Verdict key:** ✅ Fully correct · ⚠️ Partial / needs work · ❌ Broken / missing
> **Post-fix note (session 1):** IAM service and frontend→IAM connectivity fixed.
> **Post-fix note (session 2):** Journey service, map client, capacity client, segment IDs, map graph all fixed. See section 4 and SYS issues.

---

## TABLE OF CONTENTS

1. [System-Wide Broken Integrations](#1-system-wide-broken-integrations)
2. [IAM Service — Port 8082 (Deepika Nag)](#2-iam-service--port-8082-deepika-nag)
3. [Capacity Service — Port 8081 (Jai Nagle)](#3-capacity-service--port-8081-jai-nagle)
4. [Journey Service — Port 8083 (Ajinkya Taranekar)](#4-journey-service--port-8083-ajinkya-taranekar)
5. [Map Service — Port 8084](#5-map-service--port-8084)
6. [Notification Service — Port 8085 (Ziwei Zhao)](#6-notification-service--port-8085-ziwei-zhao)
7. [Frontend & API Gateway](#7-frontend--api-gateway)
8. [CS7NS6 Checklist — Item by Item](#8-cs7ns6-checklist--item-by-item)
9. [Report Outline — Section by Section](#9-report-outline--section-by-section)
10. [Ordered Fix List](#10-ordered-fix-list)

---

## 1. SYSTEM-WIDE BROKEN INTEGRATIONS

These issues render core end-to-end flows completely non-functional regardless of individual service correctness.

---

### ✅ SYS-1 FIXED — Map API Contract Corrected

The journey service `MapClient` and the map service handler are **completely incompatible**. The Map service is effectively never called at runtime; only the hardcoded fallback fires.

| Dimension | Journey `MapClient` sends | Map Service actually registers | Match? |
|---|---|---|---|
| HTTP path | `POST /api/v1/routes/compute` | `GET /api/v1/map/route` | ❌ |
| HTTP method | `POST` | `GET` | ❌ |
| Parameters | JSON body `{"origin":{"lat":…,"lng":…}, "destination":{…}}` | Query string `?origin_node_id=X&destination_node_id=Y` | ❌ |

**Fix applied:** `map_client.go` completely rewritten. Now calls `GET /api/v1/map/nodes` (cached 1h) to fetch node list, finds nearest node to origin/dest coordinates via Euclidean distance, then calls `GET /api/v1/map/route?origin_node_id=X&destination_node_id=Y`. Response envelope unwrapped correctly. `fallbackRoute()` deleted — errors propagated to caller.

---

### ✅ SYS-2 FIXED — Nginx Map Route Corrected (previous session)

```nginx
# nginx/nginx.conf — line 95
location /api/v1/routes/ {
    proxy_pass http://map-service:8084;   # forwards /api/v1/routes/* to map-service
}
```

The map service registers handlers at `/api/v1/map/nodes` and `/api/v1/map/route`. No handler exists under `/api/v1/routes/`. Any browser request through nginx to the map service returns 404.

**Fix:** Change nginx location to `/api/v1/map/` **and** align the `MapClient` path simultaneously (both must change together with SYS-1).

---

### ✅ SYS-3 FIXED — Segment IDs Unified Across Map and Capacity Services

Even after fixing SYS-1 and SYS-2, every reservation will fail because the two services use incompatible segment ID vocabularies:

| Map Service (`map_handler.go`) | Capacity Service (`002_seed_segments.sql`) |
|---|---|
| `seg_city_north` | `seg_m50` |
| `seg_north_airport` | `seg_m1_n` |
| `seg_city_east` | `seg_m1_s` |
| `seg_riverside_south` | `seg_n11` |
| `seg_south_industrial` | `seg_luas_red` |
| … (13 edges) | … (20 rows) |

**Fix applied:** `capacity-service/migrations/002_seed_segments.sql` rewritten to seed the 13 map-service segment IDs (`seg_city_north`, `seg_north_airport`, etc.) replacing the old Dublin road names. Map service and capacity service now speak the same segment ID vocabulary. Real reservations are now possible end-to-end.

---

### ✅ SYS-4 FIXED — Notification Service Route Added to Nginx (previous session)

`nginx/nginx.conf` has no `location` block for `/api/v1/notifications/`. All notification API requests from the browser fall through to the `try_files $uri /index.html` rule and receive the React SPA HTML. The notification service is completely isolated from the gateway.

**Missing block:**
```nginx
location /api/v1/notifications/ {
    proxy_pass http://notification-service:8085;
    proxy_set_header Host $host;
    …
}
```

---

### ✅ SYS-5 FIXED — Capacity Isolation Level Corrected to Serializable

`capacity-service/internal/service/reservation_service.go` line 79–80:
```go
// --- Begin serialisable transaction ---
tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
```

The comment promises Serializable isolation to prevent double-booking phantom reads. The code uses `LevelReadCommitted`. Under high concurrency, two transactions can simultaneously read the same capacity sum (both below the limit) and both commit, exceeding capacity. The core safety guarantee of the system is violated.

---

### ✅ SYS-6 FIXED — Silent Mock Fallbacks Removed from Both Clients

Both inter-service clients return successful mock responses when the downstream service is unreachable:

**`journey-service/internal/client/map_client.go`** — on any error: returns `seg_main`/`seg_ring`
**`journey-service/internal/client/capacity_client.go`** — on any error: returns `status: "reserved"` with a fake reservation ID

This means:
- If the map service is down → journeys are created with fake segment IDs
- If the capacity service is down → **all bookings are auto-approved regardless of actual road capacity**
- The system appears to work during a demo even when the capacity enforcement is completely bypassed
- No error is logged at a level that would trigger alerts

Fallbacks should return an error to the caller, not a fake success.

---

### ❌ SYS-7 SIGNIFICANT — Supabase Credentials Committed to Git

Three `config.yaml` files contain a live cloud database password in plaintext:

```yaml
# journey-service/config.yaml, map-service/config.yaml, notification-service/config.yaml
database:
  master:
    host: "db.qrbwcyidxadcahvxxbja.supabase.co"
    password: "3uuzHVMpT2CqxuwJ"   # ← REAL CREDENTIAL IN VERSION CONTROL
```

**Action required immediately:** Rotate this Supabase password. Move all secrets to a `.env` file (git-ignored) or environment variable injection. Config YAML files should use placeholder values only.

---

## 2. IAM SERVICE — Port 8082 (Deepika Nag)

### ✅ Fixed This Session

| Issue | Fix applied |
|---|---|
| Register/Refresh not atomic | `BeginTx` wraps user + token INSERT; Refresh uses `SELECT … FOR UPDATE` |
| JWT validation error JSON used capital field names | Added `json:"field"` and `json:"message"` struct tags |
| `GetUser` did O(n) full scan | Delegates to `users.GetByID` (single indexed lookup) |
| Fragile `strings.Contains` for pq errors | Replaced with `errors.As(err, &pqErr) && pqErr.Code == "23505"` |
| Slave pool created but never used | `UserRepo` now uses slave for admin reads, master for auth reads |
| No migration runner | `pkg/postgres/migrations.go` with `schema_migrations` tracking table |
| `/ready` only checked DB | `JWKSService.IsReady()` passed to `HealthHandler` |
| Frontend never called IAM login | `AppContext.login()` now calls `POST /api/v1/auth/login` |
| Frontend generated HS256 JWT in browser | Replaced with `iamApi.ts`; server-issued RS256 token stored |
| Journey service used HS256 | `auth.go` middleware replaced with JWKS-based RS256 validator |

### ❌ Remaining: Redis Rate Limiting (Spec §8)

Spec §8 requires per-IP rate limiting on auth endpoints using Redis (sliding window, 100 req/min). No `go-redis` client is imported in `iam-service/go.mod`. No rate-limiting middleware file exists. Nginx provides coarse rate limiting but not the per-user or token-aware limiting specified.

### ⚠️ Remaining: Swagger Docs Not Generated

`swag init` has never been run. The `docs/` directory contains a stub. The `/swagger/` route is registered but serves no useful documentation. **Fix:** Run `swag init -g cmd/server/main.go` from the iam-service directory.

### ⚠️ Remaining: No Integration Tests

Unit tests exist for pure functions (hash, token generation, validation). No integration tests exercise Register → Login → Refresh → Logout against a real or test DB.

---

## 3. CAPACITY SERVICE — Port 8081 (Jai Nagle)

### ❌ CRITICAL-CAP-1: Segment IDs Don't Exist (see SYS-3)

The seeded segment IDs (`seg_m50`, `seg_m1_n`, …) are never referenced by any other service. All reservation calls arrive with map-service IDs (`seg_city_north`, …) which don't exist in the `segments` table.

### ❌ CRITICAL-CAP-2: Wrong Isolation Level (see SYS-5)

Comment says "serialisable"; code uses `sql.LevelReadCommitted`. Must be `sql.LevelSerializable` or add `SELECT SUM(slots) … FOR UPDATE SKIP LOCKED` inside the transaction.

### ❌ SIGNIFICANT-CAP-1: Fragile Unique Violation Detection

`reservation_service.go` — `isUniqueViolation()`:
```go
func isUniqueViolation(err error) bool {
    return strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate")
}
```
Replace with:
```go
var pqErr *pq.Error
return errors.As(err, &pqErr) && pqErr.Code == "23505"
```

### ⚠️ MINOR-CAP-1: Stray TODO Comment in Production Code

`reservation_service.go` line 18: `}) // TODO: check import` — dead comment in production code.

### ⚠️ MINOR-CAP-2: No Tests

No `*_test.go` files in the capacity service. Core reservation logic (double-booking prevention, idempotency, vehicle slot weights) has zero test coverage.

### ✅ Correctly Implemented

- `SELECT … FOR UPDATE` row-lock on segment inside transaction ✅
- Sorted segment locking (deadlock prevention) ✅
- Idempotency key with 24-hour TTL (Redis + DB double-check inside tx) ✅
- Redis availability cache with TTL + invalidation on reservation ✅
- Vehicle slot weight mapping (car=1, truck=2, bus=3) ✅
- Redis Streams event consumer (XREADGROUP, XACK, retry backoff) ✅
- Master/slave DB split in `ReservationRepo` ✅
- Orphan reservation cleanup via `CleanupService` ✅

---

## 4. JOURNEY SERVICE — Port 8083 (Ajinkya Taranekar)

### ✅ CRITICAL-JRN-1 FIXED: Map Client Calls Correct Endpoint

`journey-service/internal/client/map_client.go` — line 59:
```go
req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
    c.baseURL+"/api/v1/routes/compute", bytes.NewReader(body))
```
Must become `GET /api/v1/map/route` with `origin_node_id` and `destination_node_id` as query parameters. The entire parameter contract must be redesigned to bridge coordinate-based input (what journey has) to node-ID-based input (what map expects).

### ✅ CRITICAL-JRN-2 FIXED: Pagination Returns Correct DB Total Count

`journey-service/internal/repository/journey_repo.go` — `scanJourneys()` line 279:
```go
return journeys, int64(len(journeys)), nil   // ← BUG: len(page) ≠ DB total
```
The `AdminList` query does a separate `SELECT COUNT(*)` (confirmed at line ~210) to get the real total. But `scanJourneys` ignores it and returns the page length instead. Every paginated response reports `total = page_size`, breaking any frontend pagination control.

**Fix applied:** `scanJourneys` now accepts a `total int64` parameter (the pre-computed `COUNT(*)` from the caller) and returns it unchanged. Both `ListByDriverID` and `AdminList` pass their `total` through. Page length no longer leaks as the total.

### ✅ SIGNIFICANT-JRN-1 FIXED: Handler Tests Use RSA Key + Mock JWKS Server

`journey-service/internal/handler/journey_handler_test.go` signs test tokens with `jwt.SigningMethodHS256` and a hardcoded string key. After the RS256 JWKS middleware fix, these tests will fail with "unexpected signing method". Tests must be updated to either:
- Mock the JWKS endpoint and sign test tokens with an RSA key, or
- Skip JWT middleware in handler-level unit tests (clearly documented)

### ✅ SIGNIFICANT-JRN-2 FIXED: Structured pq Error Detection

`journey-service/internal/repository/journey_repo.go` — `Create()`:
```go
if strings.Contains(err.Error(), "unique") {
    return nil, apperrors.ConflictError("duplicate journey")
}
```
Same fragile pattern as capacity service. Replace with structured pq error detection.

### ✅ SIGNIFICANT-JRN-3 FIXED: Slave Pool Added to Journey Repository

`journey-service/internal/repository/journey_repo.go`:
```go
type JourneyRepository struct {
    db *sql.DB   // single connection — all reads and writes hit master
}
```
`ListByDriverID`, `AdminList`, `CountByStatus` should use a slave connection. Journey service spec and system architecture describe master/slave replication.

### ✅ Correctly Implemented

- Full state machine (PENDING→APPROVED/REJECTED→ACTIVE→COMPLETED/EXPIRED/CANCELLED) ✅
- Cascading time window computation ✅
- `SELECT … FOR UPDATE` in status transitions ✅
- Optimistic locking (`version` field) in `UpdateStatus` ✅
- Idempotency caching (Redis 24-hour TTL) ✅
- Route result caching in Redis (3-decimal coordinate key) ✅
- `HasActiveJourney` guard before booking ✅
- `FindActiveJourneyForSegment` enforcement query ✅
- Event publishing to Redis Streams ✅
- Background expiry job (`RunExpiryJob`) ✅
- Migration runner ✅
- JWKS-based RS256 token validation ✅
- Map client API contract fixed (GET /api/v1/map/route + node lookup) ✅
- Capacity client mock fallback removed ✅
- Pagination total count corrected ✅
- pq unique violation detection uses structured errors.As ✅
- Slave pool for read queries ✅
- Handler tests use RSA key + mock JWKS server ✅

---

## 5. MAP SERVICE — Port 8084

### ❌ CRITICAL-MAP-1: API Contract Incompatible with Journey Client (see SYS-1)

The map service exposes `GET /api/v1/map/route?origin_node_id=X&destination_node_id=Y`. The journey service calls `POST /api/v1/routes/compute` with a JSON body. **This service is never called.** One of the two must change; they must agree on path, method, and parameter format.

### ✅ CRITICAL-MAP-2 FIXED: Segment IDs Unified (see SYS-3)

Dijkstra output uses `seg_city_north`, `seg_north_airport`, etc. Capacity service seeds `seg_m50`, `seg_m1_n`, etc. The IDs must be unified across both services.

### ✅ CRITICAL-MAP-3 FIXED: Bidirectional Edges Added to Graph

All 13 edges are added once (`A→B`). No reverse edges exist. City Centre→Airport works; Airport→City Centre does not. For a road network representing bidirectional roads, every edge should also add its reverse.

**Fix applied:** All 13 edges now have reverse counterparts in `hardcodedEdges`. Same `segment_id` is used for both directions (road is physically the same). All 10 nodes are now fully connected in both directions.

### ❌ SIGNIFICANT-MAP-1: No Tests

No `*_test.go` files anywhere in the map service. Dijkstra (a custom implementation) has zero unit test coverage. Critical cases: direct route, multi-hop, no route, same origin/destination, invalid node ID.

### ⚠️ MINOR-MAP-1: No Swagger Docs Generated

`/swagger/` is registered but `swag init` was never run. No useful API documentation is served.

### ✅ Correctly Implemented

- Dijkstra shortest-path algorithm ✅
- Path reconstruction (backtrack via `previous` map) ✅
- Returns ordered `RouteSegment` list with sequence numbers ✅
- `GetNodes` endpoint for node discovery ✅

---

## 6. NOTIFICATION SERVICE — Port 8085 (Ziwei Zhao)

This service has the largest gap between specification and implementation. It is functionally incomplete.

### ❌ CRITICAL-NOT-1: No Redis Client — Cannot Consume Events

`notification-service/go.mod` contains no `github.com/redis/go-redis/v9` dependency. Without the Redis client library, no consumer can be written. The spec requires subscribing to the `journey.events` Redis Stream.

### ❌ CRITICAL-NOT-2: No Redis Streams Consumer Implemented

Even if the Redis client were present, there is no consumer loop. The event type definitions (`events.go`) and mapper exist, but no code calls `XREADGROUP`, `XACK`, or processes any events. The notification service has no awareness of journey lifecycle events.

### ❌ CRITICAL-NOT-3: No FCM Integration — Push Notifications Never Sent

The spec (§5) requires Firebase Cloud Messaging delivery. No FCM library appears in `go.mod`. No FCM client, credential loading, or send function exists anywhere in the service.

### ❌ CRITICAL-NOT-4: All Data Lost on Restart — No PostgreSQL Persistence

```go
// notification-service/internal/service/memory_store.go
type MemoryNotificationRepo struct {
    mu            sync.RWMutex
    notifications map[string]*model.Notification
}
```
The spec requires PostgreSQL persistence for notification history and device token registry. On any restart, crash, or OOM kill, all notification history and all registered FCM device tokens are lost. Users stop receiving push notifications until they relaunch the app.

No PostgreSQL connection, no DB schema, and no migrations exist in the notification service.

### ❌ SIGNIFICANT-NOT-1: No Tests

No `*_test.go` files. Given the service is largely unimplemented, tests are blocked, but should be written alongside implementation.

---

## 7. FRONTEND & API GATEWAY

### ✅ Fixed This Session

| Issue | Fix applied |
|---|---|
| `BASE_URL = 'http://localhost:8083'` bypassed nginx | Changed to `BASE_URL = ''` (relative URL) |
| Login never called IAM service | `AppContext.login()` calls `POST /api/v1/auth/login` |
| Browser-side HS256 JWT generation | Replaced with server-issued RS256 token storage |
| No registration flow | Register form added to `LoginPage.tsx`; calls `POST /api/v1/auth/register` |
| "Create one" button was dead | Now toggles to register form |
| Password never validated | Validated client-side before API call; server validates server-side |
| Demo mode hint | Removed |
| Role selector determined auth role | Removed; role now comes from server response |

### ❌ Remaining: Frontend Notifications Use Mock Data Only

`frontend/src/app/context/AppContext.tsx` line 68:
```typescript
const [notifications, setNotifications] = useState<Notification[]>(mockNotifications);
```
No call to the notification service API is made anywhere. The notification service HTTP endpoints are unused. There is no `notificationApi.ts` file.

### ❌ Remaining: Frontend Never Calls Map Service Nodes Endpoint

The booking form uses hardcoded location names from `coordinates.ts`. The map service has a `GET /api/v1/map/nodes` endpoint that returns bookable locations dynamically. If the graph changes, the frontend won't reflect it.

### ❌ Remaining: Frontend Has No IAM Admin UI

The IAM service provides:
- `GET /api/v1/admin/auth/users` — list users
- `POST /api/v1/admin/auth/promote` — promote to admin
- `POST /api/v1/admin/auth/force-logout`

None of these have corresponding frontend pages. The admin dashboard only surfaces journey management.

### ❌ Remaining: Nginx Has No Health Condition on Depends_on

```yaml
# docker-compose.yml
nginx:
  depends_on:
    - iam-service      # No condition: service_healthy
    - journey-service
```
Nginx may start before backends are ready. With `set $upstream` variable DNS resolution, this may not cause hard failures, but is sloppy.

### ⚠️ Remaining: `docker-compose.yml` nginx Missing notification-service Dependency

Nginx neither depends on nor proxies the notification service, consistent with SYS-4.

---

## 8. CS7NS6 CHECKLIST — ITEM BY ITEM

### Services Provided
| Checklist Item | Status | Evidence / Gap |
|---|---|---|
| Services for drivers | ✅ | Journey booking, list, activate, cancel, complete; IAM auth |
| Services for enforcement | ⚠️ | `GET /api/v1/enforcement/verify` exists; no dedicated enforcement service or GUI page |

### Requirements
| Checklist Item | Status | Evidence / Gap |
|---|---|---|
| Requirements specified | ⚠️ | In 5 spec files; no unified requirements document |
| Per service class | ⚠️ | Each spec has a requirements section |
| Performance | ❌ | No latency targets stated anywhere |
| Scalability | ⚠️ | Master/slave mentioned; no throughput SLOs |
| Availability | ❌ | No uptime SLOs (e.g., "99.9%") |
| Reliability | ❌ | No MTBF/MTTR targets |
| Data consistency | ⚠️ | Mentioned implicitly; no formal consistency model stated |
| Data durability | ⚠️ | PostgreSQL used; no RPO/RTO stated |
| Qualitative | ⚠️ | Yes, informally in spec files |
| Quantitative | ❌ | No numbers: no latency, no throughput, no error rate budgets |
| Motivated by historic data | ❌ | No real traffic data referenced |
| Load pattern described | ❌ | No load characterisation (peak hours, request distribution) |

### Techniques
| Checklist Item | Status | Evidence / Gap |
|---|---|---|
| Replication | ⚠️ | IAM + Capacity: master/slave split; Journey/Map/Notification: single DB |
| Consistency model | ❌ | Not explicitly stated for any service (RC? RR? Serializable?) |
| Update strategy | ⚠️ | Refresh token rotation; automated migration runner; no documented rollback |
| Transactions | ✅ | IAM register/refresh; capacity reserve; journey status transitions |
| Isolation level | ✅ | Capacity now uses `sql.LevelSerializable` — matches comment and prevents phantom reads |
| Sharding | ❌ | Not implemented, not discussed |
| Exploit locality | ❌ | Not discussed |
| Caching | ✅ | Redis: route cache (journey), availability cache (capacity), idempotency cache |
| In-memory | ✅ | Redis used as in-memory cache layer |
| Replacement strategy | ⚠️ | `allkeys-lru` set in docker-compose Redis command; never justified in any document |
| Load balancing | ❌ | No load balancing; nginx is a single-node gateway, not a load balancer |

### Request Handling
| Checklist Item | Status | Evidence / Gap |
|---|---|---|
| Concurrent requests synchronized | ⚠️ | `SELECT … FOR UPDATE` + sorted locking in capacity; optimistic locking in journey; but isolation level undermines guarantee |
| Immediate access to earned points | ✅ | Journeys reflected in DB immediately after booking; no async delay |
| Double spending possible | ✅ | Idempotency key + FOR UPDATE + Serializable isolation — phantom reads no longer possible |
| Conflicting requests handled | ⚠️ | Optimistic locking returns 409 on version conflict; sorted lock order prevents deadlock |

### Failure Handling
| Checklist Item | Status | Evidence / Gap |
|---|---|---|
| Communication failures tolerated | ✅ | HTTP timeouts configured; map/capacity clients return errors on failure — no fake success fallbacks |
| Node/replica failure detected | ❌ | `/health` and `/ready` endpoints exist; no watchdog, no auto-restart beyond Docker `restart: unless-stopped` |
| Disconnected nodes/replicas | ❌ | Not handled; slave DB disconnect not detected or handled |
| Replica recovery supported | ❌ | Not described or implemented |
| Total failure tolerated | ❌ | Single PostgreSQL instance; single Redis instance; no failover |
| Data consistency across failures | ❌ | Not described; no WAL shipping, no log replication strategy |

### Partitions
| Checklist Item | Status | Evidence / Gap |
|---|---|---|
| Partitions handled | ❌ | Not addressed |
| n partitions without majority | ❌ | Not addressed |
| Merging of partitions | ❌ | Not addressed |
| Consistency across partitions/merges | ❌ | Not addressed |

### Other Features
| Checklist Item | Status | Evidence / Gap |
|---|---|---|
| Test application/testing framework | ⚠️ | IAM: 17 unit tests; Journey: handler tests fixed (RSA + mock JWKS); Capacity/Map/Notification: zero tests; no integration test suite |
| GUI Interface | ✅ | React SPA with login, register, journey booking, journey management, admin dashboard |
| Middleware used | ⚠️ | PostgreSQL, Redis, Docker Compose, gorilla/mux, zerolog used; **none is justified** in any document |
| Appropriately motivated | ❌ | No document explains why these specific middleware/technology choices were made |

**Checklist Score (post session 2): ~13/32 fully satisfied · 9/32 partial · 10/32 missing or broken**

---

## 9. REPORT OUTLINE — SECTION BY SECTION

### Section 1 — Requirements
**Required:** Functional + non-functional requirements; qualitative AND quantitative; historic data; load pattern.
**Status:** ❌ **Does not exist as a document.**
The spec files contain partial qualitative requirements. No document states: latency targets, throughput, availability SLO, load characterisation, or reference to any real traffic data.
**What is needed:** A dedicated `requirements.md` or report section with a table of both qualitative requirements (e.g., "system must prevent double-booking") and quantitative requirements (e.g., "booking API p99 latency < 500 ms at 100 concurrent users").

---

### Section 2 — Specification
**Required:** API specification; fault behaviour per requirement; note what is NOT fully addressed.
**Status:** ⚠️ **Partially exists.**
Five `specs/*.md` files document APIs. No spec documents fault behaviour (e.g., "what happens if capacity-service is unreachable when a journey is being created?"). No spec acknowledges known gaps (e.g., notification service non-functional).

---

### Section 3 — Architecture & Design
**Required:** System architecture diagram; how services are distributed/connected; each member describes their primary service.
**Status:** ❌ **Does not exist as a document.**
No architecture diagram exists. No document describes end-to-end service interaction. The critical broken wiring (SYS-1 to SYS-4) has never been documented.

---

### Section 4 — Implementation
**Required:** Behavioural diagrams for important algorithms; failure mode descriptions; member failure detection; consensus; partition tolerance.
**Status:** ❌ **Does not exist.**
No sequence diagrams, state machine diagrams, flow diagrams, or failure mode documents exist in the repository. The journey state machine and Dijkstra algorithm are implemented in code but never diagrammed.

---

### Section 5 — Testing
**Required:** Test plan; test results.
**Status:** ❌ **No test plan document; no test results document.**
Some unit tests exist (IAM: 17 tests; Journey: handler tests, now broken by RS256 change). Capacity, Map, and Notification services have zero tests. There is no integration test suite, no load test, and no documented test results.

---

### Section 6 — Allocation of Work
**Required:** Clear statement of who built what.
**Status:** ❌ **Does not exist anywhere in the repository.**
Service owners are implied by spec file authors but never formally documented.

---

### Section 7 — Summary
**Required:** Achievements and lessons learned.
**Status:** ❌ **Does not exist.**

**Report Score: 0/7 sections fully written · 1/7 (Specification) partially exists**

---

## 10. ORDERED FIX LIST

### 🔴 P1 — Must Fix: System Cannot Function Without These

| # | Fix | Files | Owner | Status |
|---|---|---|---|---|
| 1 | **Align map API contract** | `journey-service/internal/client/map_client.go` | Journey | ✅ DONE |
| 2 | **Fix nginx map route** | `nginx/nginx.conf` | Infra | ✅ DONE (prev session) |
| 3 | **Unify segment IDs** — capacity seed now uses map service segment IDs | `capacity-service/migrations/002_seed_segments.sql` | Journey | ✅ DONE |
| 4 | **Fix capacity isolation level** — `LevelSerializable` | `capacity-service/internal/service/reservation_service.go` | Capacity | ✅ DONE |
| 5 | **Add nginx notification route** | `nginx/nginx.conf` | Infra | ✅ DONE (prev session) |
| 6 | **Rotate Supabase credentials** — password in git | `*/config.yaml` | All | ❌ MANUAL ACTION REQUIRED |
| 7 | **Fix journey pagination** — pass DB total through `scanJourneys` | `journey-service/internal/repository/journey_repo.go` | Ajinkya | ✅ DONE |

### 🟠 P2 — Should Fix: Significant Correctness Issues

| # | Fix | Files | Owner | Status |
|---|---|---|---|---|
| 8 | **Remove silent fallback mocks** | `journey-service/internal/client/map_client.go`; `capacity_client.go` | Ajinkya | ✅ DONE |
| 9 | **Fix journey handler tests** — RSA key + mock JWKS server | `journey-service/internal/handler/journey_handler_test.go` | Ajinkya | ✅ DONE |
| 10 | **Fix fragile pq detection** in both services | `capacity-service/internal/service/reservation_service.go`; `journey-service/internal/repository/journey_repo.go` | Ajinkya | ✅ DONE |
| 11 | **Add redis/go-redis to notification service** | `notification-service/go.mod` | Ziwei | ❌ PENDING |
| 12 | **Implement notification Redis Streams consumer** | New: `notification-service/internal/event/consumer.go` | Ziwei | ❌ PENDING |
| 13 | **Add FCM library + send logic** | `notification-service/go.mod` | Ziwei | ❌ PENDING |
| 14 | **Replace notification in-memory store with PostgreSQL** | New: `notification-service/migrations/` | Ziwei | ❌ PENDING |
| 15 | **Add directed-graph reverse edges** in map service | `map-service/internal/http/handlers/map_handler.go` | Map | ✅ DONE |
| 16 | **Add slave pool to journey repo** | `journey-service/internal/repository/journey_repo.go` | Ajinkya | ✅ DONE |

### 🟡 P3 — Quality Fixes

| # | Fix | Files |
|---|---|---|
| 17 | Add tests to capacity service (concurrency, idempotency, slot weights) | `capacity-service/internal/service/reservation_service_test.go` |
| 18 | Add tests to map service (Dijkstra: direct, multi-hop, no-route, invalid node) | `map-service/internal/http/handlers/map_handler_test.go` |
| 19 | Add `condition: service_healthy` to nginx `depends_on` | `docker-compose.yml` |
| 20 | Remove stray `// TODO: check import` comment | `capacity-service/internal/service/reservation_service.go` line 18 |
| 21 | Fill in `capacity-service/config.yaml` empty DB host | `capacity-service/config.yaml` |
| 22 | Run `swag init` for IAM and Map services | `iam-service/`; `map-service/` |
| 23 | Implement IAM Redis rate limiting (spec §8) | New: `iam-service/internal/middleware/ratelimit.go` |

### 📝 P4 — Report / Documentation (Required for Grade)

| # | Required Document | Content Needed |
|---|---|---|
| 24 | **Requirements document** (Report §1) | Qualitative + quantitative requirements; load pattern; historic data reference |
| 25 | **Architecture diagram** (Report §3) | System diagram showing all 5 services + nginx + DB + Redis; sequence diagrams |
| 26 | **Implementation section** (Report §4) | Journey state machine diagram; Dijkstra flow diagram; token refresh sequence diagram; failure mode descriptions |
| 27 | **Test plan + results** (Report §5) | What was tested, how, results for each service |
| 28 | **Allocation of work** (Report §6) | Who built what |
| 29 | **Summary** (Report §7) | Achievements and known gaps |
| 30 | **Fault behaviour** (Report §2) | Document what each service does when its dependencies fail |
| 31 | **Middleware justification** | Why PostgreSQL, Redis, gorilla/mux, zerolog were chosen |

---

*Final audit — 2026-04-07*
*Checked against: CS7NS6 Exercise 2 Checklist · CS7NS6 Report Outline · Five service specs · Live codebase*
