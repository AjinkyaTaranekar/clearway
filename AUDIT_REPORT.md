# AUDIT REPORT — Distributed Vehicle Capacity System

> **Auditor:** Principal-level distributed systems review  
> **Date:** 2026-04-07  
> **Commit:** `8c94971` (main)  
> **Method:** Full codebase read + spec cross-reference; three lenses applied throughout (first principles, second-order, inversion)

---

## Section 1: Critical Findings

| ID | Area | Service | Finding | Failure Mode | Severity | Assignee |
|----|------|---------|---------|--------------|----------|----------|
| F-01 | Booking — segment reservation | Map Service + Capacity Service | **Segment ID namespace mismatch.** Map Service hardcodes IDs like `seg_m50`, `seg_m1_n`, `seg_quays_e`. Capacity Service seeds `seg_city_north`, `seg_north_airport`, `seg_city_east`. The seed file comment claims they align — they do not. Every set of IDs is completely different. | Journey Service calls Capacity with the segment IDs returned by Map Service. Capacity does a `SELECT … WHERE segment_id = $1` against its seeded rows. Because no such rows exist, every booking request returns `unknown_segment` on the very first segment. **The entire booking flow is broken end-to-end in production.** No journey can ever be APPROVED. | P0 | Xiaoxuan Duan (Map Service), Jai Nagle (Capacity Service) |
| F-02 | Event bus — notification delivery | Journey Service + Notification Service | **Redis stream key mismatch.** Journey publisher writes events to stream key `"data"`: `Values: map[string]interface{}{"data": string(data)}`. Notification consumer looks for key `"payload"` first. When that fails it falls through to `mapToJSON(msg.Values)`, which produces `{"data":"<escaped json>"}`. Unmarshalling that as `Envelope` succeeds structurally but yields an empty `EventType`. `MapEvent` returns "unknown event type" → message is silently ACKed and discarded. | **Every journey lifecycle event is silently dropped by the notification consumer.** No notification records are ever written to PostgreSQL. The in-app notifications list will always be empty for every driver. The Capacity Service consumer uses the correct `"data"` key and works correctly; the Notification consumer is the outlier. | P0 | Ziwei Zhao (Notification Service) |
| F-03 | Event reliability — outbox pattern | Journey Service (cross-cutting) | **Fire-and-forget event publishing with no outbox.** `publisher.Publish()` calls `redis.XAdd` and silently swallows the error: *"Silently ignores errors — events are best-effort, not on the critical path."* Events are published **after** the PostgreSQL commit, not atomically with it. If Redis is unavailable or the process crashes between the DB commit and the XAdd, the event is permanently lost. | **Capacity slots are never released.** Capacity Service consumes `journey.cancelled`, `journey.completed`, and `journey.expired` events to free reserved slots. If those events are dropped, the reserved slots remain occupied forever. Over time, every segment approaches its capacity ceiling regardless of actual vehicle counts. Subsequent bookings are incorrectly rejected. This is a silent data corruption path. | P0 | Ajinkya Taranekar (Journey Service) |
| F-04 | Push notifications — FCM | Notification Service | **FCM push dispatch is completely absent.** The notification consumer writes a `Notification` record to PostgreSQL and immediately ACKs. There is no Firebase SDK imported anywhere in `notification-service/go.mod`. No FCM client is constructed in `main.go`. Device tokens are accepted and stored via `POST /api/v1/notifications/device-token` but are never fetched or used to push. The `DeliveryStatus` field is set to `pending` on insert and never updated. | Drivers receive zero push notifications for any journey lifecycle event (booked, approved, cancelled, expired). The spec explicitly requires FCM as the delivery mechanism. Delivery status is permanently `pending` for every record. | P0 | Ziwei Zhao (Notification Service) |
| F-05 | Admin traffic visualisation | Frontend + Map Service | **Traffic map page uses hardcoded mock data; live API endpoint does not exist.** `TrafficMapPage.tsx` imports `trafficSegments` and `mapNodes` directly from `mockData.ts` and never calls any backend. Map Service router (`router.go`) only registers `/api/v1/map/nodes`, `/api/v1/map/route`, and `/api/v1/routes/compute`. There is no `/api/v1/map/traffic` route. The nginx config has a proxy block for `/api/v1/map/` that would route the call if it existed, but it never arrives because the frontend never makes it. | The admin sees permanently static, fabricated traffic data with no relationship to actual bookings. The primary admin-facing feature of the system — live road occupancy — is entirely non-functional. The spec defines this as the core admin capability and provides a detailed response contract. | P0 | Xiaoxuan Duan (Map Service + Frontend) |
| F-06 | Map Service — missing endpoints | Map Service | **`GET /api/v1/map/traffic` and `GET /api/v1/map/segments` are not implemented.** The spec requires both. The handler file (`map_handler.go`) has no `GetTraffic` or `GetSegments` function. Map Service also has no internal client to call Capacity Service, and no `CAPACITY_BASE_URL` env var in `docker-compose.yml`. Even implementing the handler skeleton would immediately fail on the Capacity Service call. | Admin traffic map cannot be built without the traffic endpoint. Segment metadata (static topology for admin UI) cannot be retrieved. Any future frontend or tooling relying on `/api/v1/map/segments` will get a 404. | P1 | Xiaoxuan Duan (Map Service) |
| F-07 | Map Service — graph not DB-backed | Map Service | **Graph is fully hardcoded in Go source, not loaded from PostgreSQL.** The spec says Map Service reads nodes and segments from its own PostgreSQL schema (`map`) and keeps the graph in memory. In reality, `hardcodedNodes` and `hardcodedEdges` are Go slice literals in `map_handler.go`. The `VCS_DATABASE_*` env vars are injected but never read by the map handler. There is no `map` database schema or migration. | In a multi-VM deployment, topology changes (segment additions, capacity updates) must be made by redeploying code rather than DB updates. More critically, the graph cannot be updated at runtime; the spec's topology-consistency guarantee via PostgreSQL replication does not hold. Admin cannot manage the road network through a data plane — it requires code changes. | P1 | Xiaoxuan Duan (Map Service) |
| F-08 | Admin analytics | Frontend | **Analytics page renders hardcoded mock data.** `AnalyticsPage.tsx` imports `analyticsData` and `trendData` directly from `mockData.ts`. No analytics API exists on any backend service. KPI figures (approval rate, rejection rate, active journeys) never reflect actual booking data. | Admin analytics are completely detached from reality. The system could be rejecting 90% of bookings and the dashboard would still show a healthy 88% approval rate. There is no backend analytics endpoint to implement against. | P1 | Ajinkya Taranekar (backend endpoint) + Xiaoxuan Duan (Frontend) |
| F-09 | Device token registration | Frontend + Notification Service | **FCM device token is never registered from the frontend.** `registerDeviceToken()` exists in `notificationApi.ts` and is correctly defined, but is never called from any React component, `AppContext`, or application lifecycle hook. There is no service worker file, no VAPID key, and no web push configuration. | Even if FCM dispatch were implemented server-side, no device token would ever exist in the database for any driver because the registration call is never made. Push notifications would fail to deliver because there is no token to push to. | P1 | Ziwei Zhao (Notification Service + Frontend) |
| F-10 | Admin force-cancel — audit trail | Journey Service | **Admin identity is not recorded in the cancel audit event.** `AdminCancelJourney` publishes `CancelledPayload{CancelledBy: "admin"}` without the admin's user ID. The JWT middleware extracts the caller's subject (`driverID`) but the admin handler never passes it to the service. No durable audit log table exists in the journey schema. | The spec requires force-cancel to be "logged with the admin's identity." At present, there is no way to determine which admin cancelled a journey after the fact. This is a compliance gap, not just a hardening issue. | P1 | Ajinkya Taranekar (Journey Service) |
| F-11 | Enforcement role — RBAC gap | Journey Service + IAM Service | **`enforcement` role is referenced in middleware but cannot be issued.** The journey router applies `middleware.EnforcementOnly` to the `GET /api/v1/enforcement/verify` route. The IAM spec defines only two roles: `driver` and `admin`. The IAM registration and login endpoints have no mechanism to issue a JWT with `role: enforcement`. | The enforcement verify endpoint is effectively inaccessible without an enforcement-role JWT. Admins cannot use their JWT on this route; drivers cannot either. The `EnforcementPage.tsx` calls `enforcementVerify()` from a logged-in driver session, which will hit a 403 if the middleware strictly checks for the enforcement role. | P1 | Ajinkya Taranekar (Journey Service), Deepika Nag (IAM Service) |
| F-12 | Inter-service resilience | Journey Service | **No circuit breakers or retry policies on Map Service and Capacity Service calls.** Both `map_client.go` and `capacity_client.go` use a plain `http.Client` with a flat timeout (`5s` for Map, inferred for Capacity). A single slow response holds the goroutine for the full timeout. No exponential backoff, no circuit breaker half-open state, no fallback. | Under Map Service degradation, every concurrent booking request holds a thread for 5 s before returning 502. A traffic spike during a brief Map Service hiccup will exhaust the Journey Service goroutine pool. Without circuit breaking, the Journey Service will continue hammering an already-degraded Map Service rather than fast-failing. | P1 | Ajinkya Taranekar (Journey Service) |
| F-13 | Booking flow — node-ID/coordinate round-trip ambiguity | Frontend + Journey Service + Map Service | **The frontend sends human-readable labels (not node IDs) as `origin`/`destination`, then Journey Service resolves them to nodes via nearest-neighbour coordinate lookup.** `BookJourneyPage.tsx` stores `node.label` in `form.origin` and passes `originNode.lat/lng` as `originCoords`. Journey Service calls `ComputeRoute(origin_coords, dest_coords)`. The Map Client fetches all nodes and finds the nearest by Euclidean lat/lng distance. For nodes that are geographically close, this can silently resolve to the wrong node if coordinates were rounded or if the user did not select a node (e.g., because the `getMapNodes()` fetch failed and the dropdowns were empty). | If `getMapNodes()` fails on page load, both dropdowns are empty. The user cannot select a node. `form.originNode` is undefined. `originCoords` is `undefined`. Journey Service receives `{lat: 0, lng: 0}` which resolves to the nearest node to the origin — always the same node regardless of user intent. Validation (`origin.lat == 0 && origin.lng == 0`) catches this, but the error message says "origin coordinates required" — confusing to a user who selected "City Centre". | P1 | Xiaoxuan Duan (Map Service + Frontend), Ajinkya Taranekar (Journey Service) |
| F-14 | Notification service — PostgreSQL repos wired but FCM layer absent | Notification Service | **PostgreSQL `notification_repo.go` and `token_repo.go` are correctly instantiated in `main.go`, but the notification handler has no FCM dispatch layer between record insertion and ACK.** The `NotificationHandler` and the `Consumer` both hold only a `NotificationRepository` reference. There is no `FCMClient` or `PushDispatcher` interface or concrete type anywhere in the service. | Notifications are persisted correctly (F-02 fix aside), but drivers can only discover them by polling the list endpoint. The entire push channel is missing. The spec requires exponential-backoff retry on failed FCM sends, stale token deactivation on `UNREGISTERED` FCM response, and a `delivery_status` state machine. None of this exists. | P1 | Ziwei Zhao (Notification Service) |
| F-15 | Capacity Service — event consumer group start offset | Capacity Service | **Both capacity and notification consumers create their consumer group with `"$"` (read only new messages).** If the service restarts after events have already been published, it will not replay events that occurred during the downtime. Any `journey.cancelled`, `journey.completed`, or `journey.expired` event published while the Capacity Service was down will be permanently missed. Slots from those journeys are never released. | This compounds F-03: even when Redis remains available, a brief Capacity Service restart during peak booking time means all events fired during that window are silently discarded. Segment occupancy drifts above reality permanently. | P1 | Jai Nagle (Capacity Service), Ziwei Zhao (Notification Service) |
| F-16 | Map Service — no JWT auth on any endpoint | Map Service | **`GET /api/v1/map/traffic` is specified as requiring an admin JWT.** The Map Service router applies no auth middleware to any route. All map endpoints are publicly accessible. In the current implementation this means traffic data (once implemented) would be publicly readable. More immediately, there is no JWKS fetch and no JWT middleware wired in the map router at all. | Any unauthenticated caller can enumerate all road nodes and compute routes. This is lower risk for a university prototype, but if `/api/v1/map/traffic` is ever added without auth it exposes all real-time occupancy data publicly. | P2 | Xiaoxuan Duan (Map Service) |
| F-17 | Hardcoded dev secret in docker-compose | Journey Service | **`VCS_SERVICES_JWT_SECRET: dev-secret`** is committed in plaintext in `docker-compose.yml`. This secret is used by the journey service JWT validation configuration. | The dev secret leaks into source control. Anyone with repo access can forge JWTs if the service ever falls back to HMAC validation rather than RSA. In the current implementation the journey service appears to use JWKS (RSA), so this value may be inert — but its presence is misleading and dangerous if a fallback path exists. | P2 | Ajinkya Taranekar (Journey Service) |
| F-18 | Capacity Service — `"$"` start on consumer group means no replay | Capacity Service | See F-15 above — duplicated for clarity in service grouping. | | P1 | Jai Nagle (Capacity Service) |
| F-19 | Admin cancel event missing per-admin actor ID | Journey Service | The `AdminCancelJourney` handler extracts the admin user ID via `middleware.GetDriverID(r.Context())` but passes only `"admin"` string literal as `CancelledBy` to the service, not the actual user ID. | No post-hoc audit of which admin cancelled which journey is possible. | P1 | Ajinkya Taranekar (Journey Service) |
| F-20 | Segment capacity seed covers bidirectional roads under one ID | Capacity Service | The seed file `002_seed_segments.sql` inserts one row per physical road (e.g. `seg_city_north`). Map Service models some roads with separate forward and reverse IDs (`seg_m1_n` / `seg_m1_s`, `seg_n4` / `seg_m4`). Even after fixing F-01, any road that gets separate directional segment IDs in Map Service will have no corresponding capacity row for the reverse direction, producing `unknown_segment` for reverse-direction bookings. | Bookings in the "wrong" direction on bidirectional roads will always fail. | P1 | Jai Nagle (Capacity Service) |

---

## Section 2: Phase-by-Phase Remediation Plan

---

## Service: Map Service

**Owner:** Xiaoxuan Duan

### Phase 1: Fix Segment ID Alignment (blocks all booking)
**Goal:** Make Map Service return segment IDs that exist in the Capacity Service seed, so that at least one booking can succeed end-to-end.  
**Actions:**
- Pick one canonical ID set (recommendation: adopt the human-readable `seg_city_north` naming already seeded in Capacity Service, since it aligns with topology intent).
- Replace all hardcoded edge `SegmentID` values in `map_handler.go::buildHardcodedEdges()` to use the capacity-aligned IDs: `seg_city_north`, `seg_north_airport`, `seg_city_east`, `seg_east_airport`, `seg_city_riverside`, `seg_riverside_south`, `seg_south_industrial`, `seg_industrial_east`, `seg_city_west`, `seg_west_port`, `seg_port_south`, `seg_west_northfield`, `seg_northfield_north`.
- For bidirectional roads that currently have separate forward/reverse IDs (`seg_m1_n`/`seg_m1_s`, `seg_n4`/`seg_m4`, `seg_m7n`/`seg_m7s`, `seg_n3`/`seg_n2`, `seg_quays_e`/`seg_quays_w`, `seg_port_n`/`seg_port_s`): consolidate to a single segment ID per physical road (same ID in both directions), matching the capacity seed. Verify the single-ID rows in `002_seed_segments.sql` are sufficient, or add missing reverse-direction segments to the seed.
- Update the `buildHardcodedEdges()` reverse entries to use the same `SegmentID` as the forward entry (already done for `seg_m50`, `seg_n11`, `seg_m8`, `seg_m2`, `seg_n81`; apply the same to all others).
- Run a smoke test: create a journey, verify Capacity Service does not return `unknown_segment`.  

**Risks and Assumptions:**
- Changing segment IDs will break any existing reservations in Capacity Service (acceptable in dev; wipe and re-seed).
- The capacity seed comment already claims alignment — the mismatch was introduced by a later edit to the map handler. Confirm by diffing git history.  

**Assigned To:** Xiaoxuan Duan (Map Service) + Jai Nagle (Capacity Service seed sign-off)

---

### Phase 2: Implement `/api/v1/map/traffic` Endpoint
**Goal:** Provide the admin traffic map with live occupancy data, replacing the mock in the frontend.  
**Actions:**
- Add `GetTrafficMap(w http.ResponseWriter, r *http.Request)` to `map_handler.go`. The function must:
  1. Validate admin JWT (add JWKS-based auth middleware to map router, mirroring journey-service middleware pattern; fetch JWKS from `JWKS_URL` env var).
  2. Call Capacity Service `GET /api/v1/capacity/segments/occupancy` using a new internal HTTP client (add `CAPACITY_BASE_URL=http://capacity-service:8081` to map-service docker-compose env).
  3. Join occupancy data with the in-memory node/segment graph by `segment_id`.
  4. Return the merged response matching the spec's `GET /api/v1/map/traffic` contract (including `level`, `occupancy_pct`, `vehicles`, `capacity`, `trend`, `from_node`, `to_node`).
- Register route: `r.mux.HandleFunc("/api/v1/map/traffic", r.mapHandler.GetTrafficMap).Methods("GET")` — behind admin auth middleware.
- Add `CAPACITY_BASE_URL` and `JWKS_URL` environment variables to `map-service` block in `docker-compose.yml`.
- Implement `GET /api/v1/map/segments` returning all hardcoded segments and their metadata (no auth required per spec).  

**Risks and Assumptions:**
- If Capacity Service is unavailable, the spec says return `502` rather than guessed occupancy — implement accordingly.
- The traffic endpoint introduces a synchronous dependency from Map Service to Capacity Service on every admin map load; accept this for the prototype.  

**Assigned To:** Xiaoxuan Duan

---

### Phase 3: Wire Graph to PostgreSQL (spec compliance)
**Goal:** Graph backed by database so topology can be seeded and updated without redeployment.  
**Actions:**
- Create `map` schema migrations: `nodes` table and `segments` table matching the spec structure.
- Seed the 10 nodes and 13 segments into the migration file.
- On startup, load graph from PostgreSQL into in-memory adjacency list (current Dijkstra logic unchanged).
- Remove the hardcoded Go slice literals. Fall back to hardcoded data only as a startup guard if DB is unreachable, with a log warning.
- Implement `GET /health` to return `"graph": "loaded"` only after DB load succeeds.  

**Risks and Assumptions:**
- Map Service currently connects to CockroachDB as a `map_svc` user — ensure the DB user/schema is created in init scripts.  

**Assigned To:** Xiaoxuan Duan

---

## Service: Notification Service

**Owner:** Ziwei Zhao

### Phase 1: Fix Redis Stream Key Mismatch (blocks all in-app notifications)
**Goal:** Notification consumer correctly parses journey lifecycle events.  
**Actions:**
- In `notification-service/internal/event/consumer.go`, change:
  ```go
  raw, ok := msg.Values["payload"].(string)
  if !ok {
      raw = mapToJSON(msg.Values)
  }
  ```
  to:
  ```go
  raw, ok := msg.Values["data"].(string)
  if !ok {
      c.log.Warn().Str("msg_id", msg.ID).Msg("event consumer: missing 'data' key in message")
      c.ack(ctx, msg.ID)
      return
  }
  ```
- Deploy and verify: create a booking, observe that a notification row is created in PostgreSQL `notification.notifications`.  

**Risks and Assumptions:**
- The fix is a one-line change. No data migration required (historical events are already ACKed and gone from the stream under the current `"$"` offset policy).  

**Assigned To:** Ziwei Zhao

---

### Phase 2: Implement FCM Push Dispatch
**Goal:** Drivers receive push notifications on journey lifecycle events.  
**Actions:**
- Add `firebase.google.com/go/v4` to `go.mod`.
- Create `internal/fcm/client.go` with a `Client` type wrapping `firebase.App`. Expose `Send(ctx, token, title, body string) error`.
- In `main.go`, construct the FCM client from `GOOGLE_APPLICATION_CREDENTIALS` or `FCM_SERVICE_ACCOUNT_JSON` env var.
- Extend `Consumer` to hold an `FCMClient` reference.
- After the `notifRepo.Insert` succeeds in `consumer.go::process()`, look up active device tokens via `tokenRepo.FindActiveByDriver(ctx, n.DriverID)` and call `fcmClient.Send()` for each token.
- On FCM error `UNREGISTERED` or `INVALID_ARGUMENT`, call `tokenRepo.Deactivate(ctx, tokenID, "fcm_unregistered")`.
- Update `notification.delivery_status` to `sent` on success, `failed` after exhausting retries. Implement exponential backoff for transient FCM errors (429, 503).
- Add `FCM_SERVICE_ACCOUNT_JSON` and `FCM_PROJECT_ID` to docker-compose env for notification-service.  

**Risks and Assumptions:**
- Requires a real Firebase project. For the university demo, a test Firebase project is sufficient.
- Device tokens are currently stored in PostgreSQL (F-09 fix in frontend must happen in parallel for tokens to actually exist).  

**Assigned To:** Ziwei Zhao

---

### Phase 3: Handle Consumer Group Start Offset for Restart Resilience
**Goal:** Capacity slots are not leaked when Notification Service restarts.  
**Actions:**
- Change consumer group creation from `"$"` (new messages only) to `"0"` (replay from beginning) only if the group does not already exist. Current "already exists" guard is correct — the issue is the initial `"$"` offset.
- For Notification Service specifically, consider `"0"` at first creation (replays all unprocessed events). Idempotency is already handled via `event_id` dedup in the DB.
- Note: Capacity Service has the same issue (F-15); Capacity Service owner must apply the same fix independently.  

**Risks and Assumptions:**
- On first deploy with `"0"`, the consumer will replay all historical events. With idempotent insert this is safe.  

**Assigned To:** Ziwei Zhao

---

## Service: Journey Service

**Owner:** Ajinkya Taranekar

### Phase 1: Fix Admin Cancel Audit Identity
**Goal:** Admin force-cancel events record the acting admin's ID.  
**Actions:**
- In `admin_handler.go`, extract the admin's user ID from the JWT context:
  ```go
  adminID := middleware.GetDriverID(r.Context()) // reuses same claim extractor
  ```
- Pass `adminID` to `svc.AdminCancelJourney(ctx, journeyID, adminID)`.
- In `journey_service.go::AdminCancelJourney`, add `adminID string` param and populate `CancelledPayload.CancelledBy = adminID` (the actual UUID, not the string `"admin"`).
- Add an `admin_actions` table to the journey schema migration (`journey_id`, `action`, `admin_id`, `timestamp`) and insert a row on force-cancel.  

**Risks and Assumptions:**
- The middleware already extracts `sub` claim as `driverID` — the same extraction works for admins (the claim name is the same, role differs).  

**Assigned To:** Ajinkya Taranekar

---

### Phase 2: Implement Outbox Pattern for Event Reliability
**Goal:** Journey events are never lost, even when Redis is temporarily unavailable.  
**Actions:**
- Add an `outbox` table to the journey schema: `(id SERIAL, event_type TEXT, payload JSONB, published_at TIMESTAMPTZ, created_at TIMESTAMPTZ DEFAULT now())`.
- In `CreateJourney`, `CancelJourney`, `ActivateJourney`, `CompleteJourney`, and `expireJourneys`: within the same PostgreSQL transaction that updates journey state, INSERT a row into `outbox`.
- Remove the synchronous `publisher.Publish()` call from service methods.
- Add a background goroutine (outbox relay) that polls `outbox` every 500ms, calls `redis.XAdd`, and on success DELETEs the outbox row.
- The relay must handle Redis unavailability gracefully: log, sleep with backoff, and retry on the next tick.  

**Risks and Assumptions:**
- CockroachDB supports multi-statement transactions that span tables — verified against the current schema patterns.
- This introduces at-least-once delivery (duplicates possible if relay crashes after XAdd but before DELETE). Capacity consumer already has idempotency keys; Notification consumer must add event_id dedup (already has it).  

**Assigned To:** Ajinkya Taranekar

---

### Phase 3: Add Enforcement Role to JWT Middleware
**Goal:** Resolve the undefined enforcement role so the verify endpoint is reachable.  
**Actions:**
- Either: (a) remove the `EnforcementOnly` middleware and accept admin JWT for the enforcement endpoint (simplest for prototype), or (b) add `enforcement` as a third role in IAM and coordinate with Deepika Nag to issue enforcement tokens.
- If option (a): replace `middleware.EnforcementOnly` with `middleware.AdminOnly` in the journey router.
- If option (b): update `EnforcementPage.tsx` to surface a login flow for enforcement users.  

**Risks and Assumptions:**
- Option (a) is the recommended path for the prototype given the IAM spec does not define the enforcement role.  

**Assigned To:** Ajinkya Taranekar

---

### Phase 4: Add Circuit Breakers on Map and Capacity Calls
**Goal:** Journey Service degrades gracefully when Map or Capacity Service is slow.  
**Actions:**
- Add `github.com/sony/gobreaker` or `github.com/afex/hystrix-go` to `go.mod`.
- Wrap `mapClient.ComputeRoute()` and `capacityClient.Reserve()` in circuit breakers with: open threshold = 5 consecutive failures, half-open probe = 10s, timeout = current values.
- On open circuit, return `502` with `"map service circuit open"` or `"capacity service circuit open"`.  

**Risks and Assumptions:**
- Circuit breakers must not share state between concurrent requests — use one breaker instance per client, created at startup.  

**Assigned To:** Ajinkya Taranekar

---

## Service: Capacity Service

**Owner:** Jai Nagle

### Phase 1: Fix Consumer Group Start Offset
**Goal:** Capacity slots are not leaked on service restart.  
**Actions:**
- In `capacity-service/internal/event/consumer.go::ensureConsumerGroup()`, change the initial offset from `"$"` to `"0"`:
  ```go
  err := c.redis.XGroupCreateMkStream(ctx, streamName, consumerGroup, "0").Err()
  ```
- This causes the consumer to replay all unprocessed events on first group creation. Since Capacity Service inserts are idempotent (a `journey.cancelled` for an already-cancelled journey is a no-op in the release logic), this is safe.  

**Risks and Assumptions:**
- On a completely fresh deployment this is a no-op (stream is empty). On restart of an existing deployment it replays events from the stream's retention window.  

**Assigned To:** Jai Nagle

---

### Phase 2: Verify Segment Seed Completeness After Map Service Fix
**Goal:** Every segment ID returned by Map Service has a corresponding row in `capacity.segments`.  
**Actions:**
- After Map Service Phase 1 lands, enumerate all `SegmentID` values produced by `buildHardcodedEdges()`.
- Diff against `002_seed_segments.sql`. For any missing ID, add an `INSERT` row.
- Write an integration smoke test: call `GET /api/v1/map/route?origin_node_id=city&destination_node_id=airport`, take every `segment_id` in the response, call `GET /api/v1/capacity/segments/{id}/availability` for each. Verify no 404s.  

**Risks and Assumptions:**
- Bidirectional roads with a single segment ID (per Phase 1 of Map Service) need only one capacity row — confirm the seed covers both directions under one ID.  

**Assigned To:** Jai Nagle

---

## Service: Frontend

**Owner:** Team-wide (UI was built collaboratively)

### Phase 1: Wire Traffic Map to Live API
**Goal:** Admin traffic map shows real segment occupancy from the backend.  
**Actions:**
- In `TrafficMapPage.tsx`, remove the import of `trafficSegments` and `mapNodes` from `mockData.ts`.
- Add a `useEffect` that calls `GET /api/v1/map/traffic` (add a `getTrafficData()` function to `mapApi.ts` including the `Authorization` header from `authHeaders()`).
- Map the API response fields (`level`, `occupancy_pct`, `vehicles`, `capacity`, `trend`, `from_node`, `to_node`) to the component's existing display logic.
- Handle loading and error states. If the API call fails, show an error banner rather than stale mock data.
- The SVG map currently positions nodes via hardcoded `x`/`y` from `mockData.ts`; the API's `nodes` array returns these as well — use them.  

**Risks and Assumptions:**
- Depends on Map Service Phase 2 (traffic endpoint). Can develop against a local mock server before Map Service is ready.  

**Assigned To:** Frontend lead

---

### Phase 2: Wire Analytics Page to Live Data
**Goal:** Admin analytics show real booking metrics.  
**Actions:**
- Add a backend analytics endpoint. Recommended: add `GET /api/v1/admin/analytics?window=1h|24h|7d` to Journey Service that runs aggregate SQL queries against the `journey.journeys` table (COUNT by status, grouped by time bucket).
- In `AnalyticsPage.tsx`, replace the `analyticsData` import with an `apiFetch` call to `/api/v1/admin/analytics`.
- Implement loading state and error handling.  

**Risks and Assumptions:**
- Live aggregate queries on CockroachDB's OLTP store are acceptable for the prototype (small dataset). In production, use follower reads or a materialized view.  

**Assigned To:** Frontend lead (API) + Ajinkya Taranekar (backend endpoint)

---

### Phase 3: Register FCM Device Token on Login
**Goal:** Drivers receive push notifications after logging in.  
**Actions:**
- In `AppContext.tsx` inside the `login()` function (after tokens are stored), call `registerDeviceToken(fcmToken, 'web')`.
- To obtain an FCM web token, add a `firebase.ts` module that initializes the Firebase JS SDK with the project config, registers a service worker, and calls `getToken(messaging, { vapidKey })`.
- Add the Firebase web config (apiKey, projectId, messagingSenderId, appId, vapidKey) as `VITE_FIREBASE_*` env vars in `.env`.
- Create a `public/firebase-messaging-sw.js` service worker that handles background push messages.
- On token refresh (Firebase `onTokenRefresh` callback), re-call `registerDeviceToken` with the new token.  

**Risks and Assumptions:**
- Requires a real Firebase project. The same project used for server-side FCM in Notification Service Phase 2.
- HTTPS is required for service workers. In local development, `localhost` is an exception.  

**Assigned To:** Frontend lead + Ziwei Zhao (FCM config coordination)

---

### Phase 4: Fix Booking Flow When Map Nodes Unavailable
**Goal:** Booking page fails clearly when origin/destination cannot be loaded, rather than silently sending zero coordinates.  
**Actions:**
- In `BookJourneyPage.tsx`, if `getMapNodes()` fails, display an error state with a retry button rather than rendering empty dropdowns.
- Add validation in `validateStep1()`: ensure `form.originNode` and `form.destNode` are set (not just the label string).
- When calling `bookJourney`, always send `originCoords` from the selected node (not undefined). If no node is selected, block submission.  

**Risks and Assumptions:**
- The Journey Service validates that coordinates are non-zero, but the error message is confusing. Frontend validation is the user-facing fix; backend validation is the safety net.  

**Assigned To:** Frontend lead

---

## Service: IAM Service

**Owner:** Deepika Nag

### Phase 1: Clarify Enforcement Role (coordinate with Journey Service)
**Goal:** Resolve the undefined enforcement role referenced by the journey router.  
**Actions:**
- Coordinate with Ajinkya Taranekar on the chosen resolution (see Journey Service Phase 3).
- If an enforcement role is added: extend the IAM registration endpoint to accept `role: enforcement` (admin-created accounts only), update JWT claim issuance, and document in the spec.
- If enforcement role is dropped: no IAM changes needed.  

**Risks and Assumptions:**
- No changes to IAM needed if Journey Service adopts admin role for enforcement endpoint.  

**Assigned To:** Deepika Nag

---

## Section 3: Cross-Cutting Concerns

### XC-01: No Outbox Pattern — Event Reliability Across All Producers

**Services affected:** Journey Service (primary producer), Capacity Service (consumer of slot-release events), Notification Service (consumer of notification events)

**Problem:** Journey events are published to Redis Streams after the PostgreSQL transaction commits, with no atomicity guarantee between the two writes. If the process dies, network drops to Redis, or Redis is temporarily unavailable between the DB commit and the `XAdd` call, the event is permanently lost. The Journey Service `publisher.Publish()` explicitly documents this as a design choice: *"best-effort, not on the critical path."*

**Why this is catastrophic here:** The event stream is the only mechanism by which Capacity Service releases reserved slots (for cancelled/completed/expired journeys). If release events are dropped, segments permanently accumulate occupancy above reality. This cannot be detected or corrected without a manual DB audit.

**Unified fix:** Implement a transactional outbox table in the Journey Service PostgreSQL schema. All event writes become part of the DB transaction. A dedicated relay goroutine polls the outbox and publishes to Redis. On successful XAdd, delete the outbox row. This converts at-most-once delivery to at-least-once. All consumers (Capacity and Notification) must be idempotent — Capacity Service already uses `reservation_id` idempotency; Notification Service already deduplicates on `event_id`.

**Owner of coordination:** Ajinkya Taranekar (Journey Service), with Jai Nagle and Ziwei Zhao reviewing consumer idempotency handling.

---

### XC-02: Segment ID Namespace — Shared Contract Between Map and Capacity

**Services affected:** Map Service, Capacity Service, Journey Service

**Problem:** There is no enforced contract between Map Service's segment IDs and Capacity Service's segment rows. Both services evolved independently, and the IDs diverged. This is a distributed systems anti-pattern: two services share a logical key space (`segment_id`) with no schema registry, no validation at deployment time, and no integration test that verifies alignment.

**Unified fix:**
1. Establish a single source of truth for segment IDs: the Capacity Service seed SQL file (`002_seed_segments.sql`) is the authority.
2. Map Service must use only IDs that exist in that file. Add a startup validation check in Map Service: on boot, call `GET /api/v1/capacity/segments` (or read from shared config) and verify that every segment ID in the in-memory graph exists. Fail readiness (`/ready` returns 503) if any mismatch is found.
3. Alternatively, if graph is moved to PostgreSQL (Map Service Phase 3), seed the `map.segments` table with the same IDs as `capacity.segments` via a shared init migration.
4. Add a CI test that runs both services and verifies no `unknown_segment` response for any segment in the map graph.

**Owner of coordination:** Xiaoxuan Duan (Map Service) + Jai Nagle (Capacity Service)

---

### XC-03: FCM Infrastructure — Cross-Service Configuration Gap

**Services affected:** Notification Service (server-side push), Frontend (client-side token registration)

**Problem:** FCM requires coordinated configuration across two codebases: the server-side Firebase Admin SDK needs a service account JSON, and the frontend Firebase JS SDK needs a web app config and VAPID key. Neither exists anywhere in the repository. There is no shared Firebase project. Without both sides configured and deployed together, push notifications cannot function regardless of how complete the code is.

**Unified fix:**
1. Create a single Firebase project for the system.
2. Generate: (a) a service account JSON for server-side use; (b) a web app config (apiKey, projectId, messagingSenderId, appId) for client-side use; (c) a VAPID key for web push.
3. Add to `docker-compose.yml` for notification-service: `FCM_SERVICE_ACCOUNT_JSON` env var (path to mounted secret or inline JSON for dev).
4. Add to frontend `.env`: `VITE_FIREBASE_API_KEY`, `VITE_FIREBASE_PROJECT_ID`, `VITE_FIREBASE_MESSAGING_SENDER_ID`, `VITE_FIREBASE_APP_ID`, `VITE_FIREBASE_VAPID_KEY`.
5. Document the setup steps in `README.md` for the demo environment.

**Owner of coordination:** Ziwei Zhao (Notification Service FCM client) + Frontend lead (JS SDK + service worker)

---

### XC-04: Consumer Group Offset — Silent Event Loss on Restart

**Services affected:** Capacity Service, Notification Service

**Problem:** Both services create their consumer groups with start offset `"$"`, meaning they only consume events published after the group is first created. If either service restarts for any reason (container restart, deployment, crash), events published during the downtime window are never processed. For Capacity Service, this means reserved slots are never released. For Notification Service, drivers miss notifications for events that occurred while the service was restarting.

**Unified fix:**
- Both services should use `"0"` as the initial offset when creating the consumer group (only affects the very first `XGROUP CREATE`; the "BUSYGROUP" guard prevents re-creation on subsequent starts, so historical events are not replayed after the first successful run).
- Both services already implement pending message reclaim (`XPENDING` / `XCLAIM` loops) — this covers the case where a consumer crashes mid-processing after claiming a message. The offset fix covers the case where the whole service is down when events are produced.

**Owner of coordination:** Jai Nagle (Capacity) + Ziwei Zhao (Notification)

---

### XC-05: No Integration Test Covering the Full Booking Flow

**Services affected:** All services

**Problem:** There is no end-to-end test or smoke test that exercises the complete booking path: register user → login → get map nodes → create journey → verify Capacity reservation → verify notification event. The segment ID mismatch (F-01) and the stream key mismatch (F-02) both could have been caught by a single integration test run against the real services.

**Unified fix:**
- Add a `scripts/smoke_test.sh` (or Go integration test under `tests/integration/`) that:
  1. Creates a driver account via IAM.
  2. Logs in and gets a JWT.
  3. Fetches nodes from Map Service.
  4. Creates a journey between two nodes (departure = now + 2 hours).
  5. Asserts the response is `APPROVED` (not `REJECTED` with `unknown_segment`).
  6. Polls Notification Service for a notification for that driver.
  7. Asserts a notification record exists.
- Run this test in CI after `docker compose up --build`.

**Owner of coordination:** Ajinkya Taranekar (as Journey Service owner and primary integrator)

---

---

## Section 4: Screen-by-Screen Frontend Flow Audit

> Every user interaction traced from UI event → API call → backend handler → response → UI update. Detective-level detail.

---

### 4.1 Driver Registration (`/` → register tab)

**What the screen does:** User enters name, email, password, vehicle type (dropdown: car/van/truck/motorcycle), and licence number. Calls `register()` in `AppContext`.

**API call:** `POST /api/v1/auth/register` → IAM Service ✅ Implemented.

**Finding U-01 — Only one vehicle registered per driver; no multi-vehicle support** (P1) — *Assignee: Deepika Nag (IAM Service)*  
The IAM model (`auth.users`) stores a single `vehicle_type` VARCHAR and a single `license_info` JSONB object. There is no `vehicles` table, no `user_vehicles` relation, and no endpoint to register additional vehicles. The spec implies drivers have "a primary vehicle" and the question of a secondary vehicle with a valid licence is unaddressed by the current data model. There is no backend path to add a second vehicle.  
**What breaks at runtime:** A driver who switches between a car and an HGV cannot register both. When booking, they must manually select the vehicle type from the booking picker — there is no pre-population from their registered vehicle.

**Finding U-02 — Licence number is stored but never validated** (P2) — *Assignee: Deepika Nag (IAM Service)*  
Registration accepts any string as `license_number`. IAM stores it in `license_info` JSONB. There is no format validation (length, pattern, jurisdiction check) in the handler or service. The frontend validates only that the field is non-empty.  
**What breaks at runtime:** Invalid licence numbers pass registration silently. The booking flow cannot enforce that the vehicle type matches the licence class.

---

### 4.2 Driver Login (`/` → login tab)

**API call:** `POST /api/v1/auth/login` → IAM Service ✅ Implemented.

**Finding U-03 — User profile data not fetched after login; vehicle type not populated in context** (P1) — *Assignee: Deepika Nag (IAM Service + Frontend)*  
After login, `AppContext` stores `tokens.user` in state. The `User` interface includes `vehicle_type`, and this is correctly stored. However, `BookJourneyPage` never reads `user.vehicle_type` to pre-select the vehicle type in the booking picker. The booking picker always starts blank, forcing the driver to re-select their vehicle on every booking even though it is already known.  
**What breaks at runtime:** The driver sees no pre-selection. If they forget to pick a vehicle type, form validation catches it. But the UX expectation — "I registered a car, so just show me car pre-selected" — is not met.

---

### 4.3 Book Journey Page (`/driver/book`)

**Finding U-04 — Origin/destination selection via node dropdown, but coordinate round-trip introduces silent resolution errors** (P1) — *Assignee: Xiaoxuan Duan (Map Service + Frontend), Ajinkya Taranekar (Journey Service)*  
The booking page calls `GET /api/v1/map/nodes` on mount and populates two `<select>` dropdowns. The selected node's `lat/lng` is stored, and when the form is submitted, coordinates are sent to Journey Service via `createJourney({originCoords: {lat, lng}, destCoords: {lat, lng}})`. Journey Service then calls `ComputeRoute(lat, lng → lat, lng)` on Map Client, which re-fetches all nodes from Map Service and finds the nearest node by Euclidean distance.

This is a **double resolution**: user selects node A (frontend knows exact `node_id`) → converts to `{lat, lng}` → Map Client converts back to nearest node ID. If two nodes share similar coordinates or if the fetch in `MapClient.getNodes()` returns a slightly different node set than what the frontend showed, the resolved node can silently differ.

**Direct fix:** The frontend already knows the `node_id`. Journey Service should accept `origin_node_id` / `destination_node_id` directly (matching the `GET /api/v1/map/route` interface) instead of coordinates. The coordinate path was designed for a case where users pin a location on a real map; for a dropdown-driven picker of predefined nodes, it is unnecessary indirection.

**Finding U-05 — No route preview before submission** (P2) — *Assignee: Xiaoxuan Duan (Map Service + Frontend)*  
`BookJourneyPage` has a step-2 "Review & submit" screen, but it shows only origin, destination, departure time, and vehicle type. It never calls `GET /api/v1/map/route` to show the driver which road segments their booking will traverse, or what the estimated journey time is. `mapApi.ts` exports a `getRoute()` function that could power this, but it is never invoked.  
**What breaks at runtime:** The driver submits blind. They discover the route only after approval (in `JourneyDetailPage`), where segments are shown with hardcoded occupancy `50` and level `"medium"` regardless of actual values (see U-08).

**Finding U-06 — Booking failure falls back to fake mock, hiding real errors from users** (P0 UX) — *Assignee: Ajinkya Taranekar (Journey Service + Frontend)*  
`bookJourney()` in `AppContext` wraps `api.createJourney()` in a try/catch. On **any** exception — network error, 400, 409 conflict, 422 time constraint, 500 — it calls `bookJourneyMock()`, which randomly approves or rejects the booking with a 30% rejection rate. The user sees a plausible-looking result (correct approval/rejection UI) but it is entirely fabricated.  
**What breaks at runtime:** The backend may be completely unreachable, returning HTTP 502, or rejecting the booking for a valid business reason (duplicate journey, departure too soon). The driver sees a fake "approved" 70% of the time and believes the booking succeeded. Their real booking history in the database is empty. If the issue is a conflict or capacity rejection, the real reason is hidden behind the mock's vague "capacity full on one or more segments."

**Finding U-07 — Admin force-cancel calls the wrong backend endpoint** (P0) — *Assignee: Ajinkya Taranekar (Journey Service + Frontend)*  
`AdminJourneyDetailPage.handleForceCancel()` calls `updateJourneyStatus(journey.id, 'cancelled', 'Admin')`. `AppContext.updateJourneyStatus` routes `'cancelled'` to `api.cancelJourney(id)` which calls `PUT /api/v1/journeys/{id}/cancel` — the **driver** cancel endpoint. The admin cancel endpoint is `PUT /api/v1/admin/journeys/{id}/cancel`.

The driver cancel enforces a 30-minute departure constraint: `apperrors.Forbidden("cannot cancel within 30 minutes of departure")`. An admin trying to force-cancel an ACTIVE journey or an approved journey close to departure will get a 403, the error is caught by `updateJourneyStatus`'s catch block, which falls through to a local state mutation with no backend call — so the journey appears cancelled in the UI but remains ACTIVE in the database.

The admin cancel endpoint (`PUT /api/v1/admin/journeys/{id}/cancel`) has no time constraint and is implemented correctly in the backend, but is **never called** by the frontend. `journeyApi.ts` has `adminCancelJourney(id)` which calls the correct path — but `AppContext` and `AdminJourneyDetailPage` never use it.

---

### 4.4 Journey Detail Page (`/driver/journeys/:id`)

**Finding U-08 — Segment occupancy and level are hardcoded, not from the backend** (P1) — *Assignee: Jai Nagle (Capacity Service + Frontend)*  
`mapSegments()` in `journeyApi.ts`:
```ts
return apiSegments.map((s) => ({
  id: s.segment_id ?? s.id,
  name: s.segment_name ?? s.name,
  occupancy: 50,       // hardcoded
  level: 'medium',     // hardcoded
}));
```
Journey Service returns segments with `segment_id` and `segment_name` from the database, but no occupancy or level field (that data lives in Capacity Service). The frontend fills in `50` and `"medium"` for every segment on every journey. The progress bars and colour indicators in the detail view are decorative only.  
**What breaks at runtime:** A driver viewing a journey on a segment at 95% capacity sees "50% / Medium" — the opposite of reality. This makes the occupancy visualisation actively misleading.

**Finding U-09 — Driver name is hardcoded as "Driver" in all journey API responses** (P1) — *Assignee: Ajinkya Taranekar (Journey Service + Frontend)*  
`mapApiJourney()` in `journeyApi.ts`:
```ts
driverName: 'Driver',
```
The Journey Service API does not return the driver's name (it returns `driver_id`). The frontend hardcodes the string "Driver". Every journey in the admin journeys list shows "Driver" as the driver name, making driver identification impossible without a separate IAM lookup.  
**Fix:** Journey Service should enrich the journey response with the driver's name by joining against IAM data, or the frontend should call `GET /api/v1/auth/profile` for the driver ID shown in admin view. Neither is currently implemented.

**Finding U-10 — Frontend allows activation without time enforcement** (P2) — *Assignee: Ajinkya Taranekar (Journey Service + Frontend)*  
`JourneyDetailPage` shows the "Activate" button when `journey.status === 'approved'` with no time check. The backend enforces that activation can only happen between `departure_time` and `departure_time + 30 min`. If a driver tries to activate too early, they get a 403 from the backend. On error, `updateJourneyStatus` falls through to a local state mutation, so the UI shows the journey as "Active" even though the backend rejected it.  
**What breaks at runtime:** Driver UI shows an ACTIVE journey; backend still has it APPROVED. Any downstream action (complete, admin view) sees mismatched state between browser and server until the driver refreshes.

---

### 4.5 Driver Settings Page (`/driver/settings`)

**Finding U-11 — Profile save button is entirely fake** (P0 UX) — *Assignee: Deepika Nag (IAM Service + Frontend)*  
`SettingsPage.handleSave()`:
```ts
const handleSave = async () => {
  await new Promise((r) => setTimeout(r, 500));  // fake delay
  setSaved(true);
  setTimeout(() => setSaved(false), 2500);
};
```
There is no API call. The `PUT /api/v1/auth/profile` endpoint exists and is implemented in IAM Service, but `SettingsPage` never calls it. Name, email, and phone field changes are written to local component state and discarded on navigation. The "Saved ✓" checkmark is shown after 500ms regardless.  
**What breaks at runtime:** The driver believes their profile has been updated. It has not. On next login (if the page is refreshed), the old values reappear.

**Finding U-12 — Push notification toggle is cosmetic, does not register FCM device token** (P1) — *Assignee: Ziwei Zhao (Notification Service + Frontend)*  
`SettingsPage` has a push notification toggle that updates `pushEnabled` local state. Toggling it on does not call `registerDeviceToken()`. There is no service worker, no FCM JS SDK initialisation, no VAPID key. The toggle's only effect is the visual on/off state, which resets to `false` on every page load (state is not persisted).  
**What breaks at runtime:** Enabling push in settings does nothing. Drivers will never receive push notifications regardless of what the toggle shows.

---

### 4.6 Driver Notifications Page (`/driver/notifications`)

**API call on load:** `notifApi.listNotifications()` → `GET /api/v1/notifications` ✅ Called correctly from `AppContext` on mount.  
**Fallback on error:** `setNotifications(mockNotifications)` — same silent mock fallback pattern.

**Finding U-13 — Notifications rendered correctly but sourced from Redis-dropped events** (P0) — *Assignee: Ziwei Zhao (Notification Service — resolves when F-02 is fixed)*  
Due to F-02 (stream key mismatch), no notification records are written to PostgreSQL when journey events occur. The `GET /api/v1/notifications` endpoint returns an empty list for every driver. `AppContext` receives an empty array, and the notifications page shows "All caught up" — even for a driver whose journey was just rejected for capacity reasons.  
This finding documents the user-visible symptom of F-02. Fix F-02 first; this symptom resolves automatically.

---

### 4.7 Admin Dashboard (`/admin`)

**Finding U-14 — Admin KPI cards display hardcoded mock analytics** (P1) — *Assignee: Ajinkya Taranekar (Journey Service backend endpoint + Frontend)*  
`AdminDashboardPage` imports `analyticsData` from `mockData.ts` and reads `analyticsData.kpis` for total bookings, approval rate, active journeys, and rejection rate. These are fixed numbers (`totalBookings: 247, approvalRate: 88.3, activeJourneys: 14, rejectionRate: 11.7`). They do not change and have no relationship to real booking data.  
**What breaks at runtime:** Admin believes 88.3% of bookings are being approved. If the booking flow is completely broken (which it is, due to F-01), the actual approval rate is 0%. The dashboard shows the opposite.

---

### 4.8 Admin Traffic Map (`/admin/map`)

**Finding U-15 — No external map library is used; the "map" is a hand-drawn SVG with fake data** (P0) — *Assignee: Xiaoxuan Duan (Map Service + Frontend — see Section 6.12 for TomTom Maps SDK integration)*  
Checking `package.json` dependencies: no `tomtom-web-sdk`, `mapbox-gl`, `leaflet`, `@react-google-maps/api`, or any mapping library. The traffic map is a `<svg viewBox="0 0 600 460">` with `<line>` elements drawn between hardcoded pixel coordinates from `mockData.mapNodes`. Segment colours are derived from `mockData.trafficSegments`.

The spec's `MAP_SERVICE_SPEC.md` explicitly says TomTom is used "only as a visualization aid and integration convenience" and is not part of the core booking path. So the spec does not mandate TomTom for the map. However, the SVG map has no geographic accuracy — segment lines are drawn between pixel positions like `{x: 300, y: 250}` for City Centre, not real coordinates. The `GET /api/v1/map/nodes` API returns real `lat`/`lng` values (Dublin coordinates) for each node but the frontend SVG never uses them.

**What breaks at runtime:** Even after fixing F-05 (connecting the traffic API), the SVG map cannot render road geometry accurately without either TomTom/Mapbox for tile rendering or a coordinate-to-pixel projection system. Segment lines will be geometrically incorrect relative to the actual road layout.

**Finding U-16 — Traffic map data auto-refresh is cosmetic** (P2) — *Assignee: Xiaoxuan Duan (Frontend)*  
The page shows "Updated HH:MM" and has a `RefreshCw` icon. The `lastRefresh` state is set once on mount with `useState(() => new Date())` and never updated. There is no `setInterval`, no polling, and no WebSocket. The timestamp shown is always the page load time.

---

### 4.9 Admin All Journeys (`/admin/journeys`)

**API call:** `api.adminListJourneys()` → `GET /api/v1/admin/journeys` ✅ Called from AppContext on login.  
**Finding U-17 — Region filter is frontend-only; backend has no region concept on journeys** (P2) — *Assignee: Ajinkya Taranekar (Journey Service + Frontend)*  
The `AllJourneysPage` has a "Region" filter dropdown. The `Journey` type has a `region: Region` field. The `mapApiJourney()` function hardcodes `region: 'Central'` for every journey returned from the backend. The backend `journey.journeys` table has no region column. Filtering by any region except "Central" will always return empty results, and filtering by "Central" returns everything.

---

### 4.10 Admin Enforcement Verify (`/admin/enforcement`)

**API call:** `enforcementVerify({segmentId, vehiclePlate, timestamp})` → `GET /api/v1/enforcement/verify` → Journey Service  
**Finding U-18 — Enforcement verify uses driver JWT, which fails the EnforcementOnly middleware** (P1) — *Assignee: Ajinkya Taranekar (Journey Service)*  
`EnforcementPage.tsx` calls the enforcement verify endpoint. It is served under the admin route layout, so a logged-in admin reaches it. However, the journey service router applies `middleware.EnforcementOnly` to this route, which requires `role: enforcement` in the JWT. Admin JWTs have `role: admin`. The middleware check is:
```go
middleware.EnforcementOnly
```
If `EnforcementOnly` checks for `role == "enforcement"` specifically (not `role == "admin"` as a supertype), admin users will get 403 on every enforcement verify call.  
**What breaks at runtime:** The enforcement page always shows an error. The feature is inaccessible even to administrators. (This is the frontend-visible consequence of the server-side F-11 finding.)

**Finding U-19 — Vehicle plate field is optional but is not used server-side** (P2) — *Assignee: Ajinkya Taranekar (Journey Service)*  
The form has a "Vehicle plate" field which is sent as `vehicle_plate` query param. The backend `EnforcementVerify` service method only checks `segmentID` and `timestamp` — it queries for any active journey covering that segment at that time, regardless of the vehicle. The `vehicle_plate` param is accepted by the handler but not passed to the service or repository query.  
**What breaks at runtime:** An enforcement officer looking up plate "ABC-123" on a segment will be told "authorized" even if the active journey on that segment belongs to plate "XYZ-999". The plate field is decorative.

---

### 4.11 Admin Notifications (`/admin/notifications`)

**Finding U-20 — Admin notifications page re-uses the driver notifications page verbatim** (P2) — *Assignee: Ziwei Zhao (Notification Service + Frontend)*  
`AdminNotificationsPage.tsx` is a single-line re-export:
```ts
export { default } from '../driver/NotificationsPage';
```
The spec defines `GET /api/v1/admin/notifications` as a separate endpoint returning all driver notifications across the system. The current page instead calls `GET /api/v1/notifications` which returns only the admin user's own notifications (of which there are none, since admins don't receive journey events). The admin notification centre is always empty.

---

## Section 5: Team Ownership Matrix and Frontend Communication Protocol

### 5.1 Current State

There is no defined protocol for frontend-backend API changes. Each backend service is owned by one team member; the frontend is a shared artefact with no designated owner. This creates the following failure pattern observed throughout the codebase:

- Notification Service updated its API (e.g., response envelope shapes, endpoint paths) without a corresponding frontend update.
- Journey Service implemented `PUT /api/v1/admin/journeys/{id}/cancel` (admin cancel) but the frontend still calls the driver cancel endpoint.
- IAM Service implements `PUT /api/v1/auth/profile` but the SettingsPage never calls it.
- Map Service implements `GET /api/v1/map/nodes` with real data, but the traffic map SVG still uses the `mockData.mapNodes` pixel positions.

### 5.2 Ownership Assignment

Each service owner is responsible for the frontend integration of their service's API. Concretely, when a backend API is modified or added, the service owner must either: (a) implement the corresponding frontend call themselves, or (b) produce a written integration spec and coordinate with the team member who will implement it before the PR is merged.

| Service | Backend Owner | Frontend Responsibility |
|---------|--------------|------------------------|
| IAM Service (auth, profile, JWKS) | Deepika Nag | Login, registration, `SettingsPage.handleSave`, profile update, vehicle type pre-population |
| Capacity Service (reservation, availability, occupancy) | Jai Nagle | Occupancy data in `JourneyDetailPage` segment list; `TrafficMapPage` real data feed (via Map Service traffic endpoint) |
| Map Service (nodes, route, traffic, segments) | Xiaoxuan Duan | `BookJourneyPage` node dropdown, route preview on step 2, `TrafficMapPage` API connection, SVG coordinate mapping |
| Journey Service (journeys, admin, enforcement) | Ajinkya Taranekar | All of `journeyApi.ts`; `AppContext.updateJourneyStatus` routing to correct endpoint; `AdminJourneyDetailPage.handleForceCancel` using `adminCancelJourney`; enforcement verify role fix |
| Notification Service (notifications, device tokens) | Ziwei Zhao | `notificationApi.ts` device token registration call on login; push toggle wiring in SettingsPage; FCM service worker; admin notifications endpoint integration |

### 5.3 Immediate Cross-Service Frontend Fixes Required

The following frontend bugs are directly caused by a backend owner not updating the frontend after implementing their API. Each is a named task for the responsible owner:

| Fix | Owner | File | Change |
|-----|-------|------|--------|
| `adminCancelJourney()` not called on force-cancel | Ajinkya Taranekar | `AppContext.tsx` | Route `'cancelled'` status from admin context to `api.adminCancelJourney(id)`, not `api.cancelJourney(id)` |
| Profile save calls IAM API | Deepika Nag | `SettingsPage.tsx` | Replace the fake `setTimeout` with `PUT /api/v1/auth/profile` call; add `iamUpdateProfile()` to `iamApi.ts` |
| FCM token registered on login | Ziwei Zhao | `AppContext.tsx` | After `storeTokens()`, call `registerDeviceToken(fcmToken, 'web')`; add Firebase JS SDK init |
| Push toggle wires to token registration | Ziwei Zhao | `SettingsPage.tsx` | On toggle-on: call Firebase `getToken()` and `registerDeviceToken()`; on toggle-off: call deactivate |
| Vehicle type pre-selected in booking | Deepika Nag | `BookJourneyPage.tsx` | Read `user.vehicle_type` from context and set as initial `form.vehicleType` |
| Traffic map calls real API | Xiaoxuan Duan | `TrafficMapPage.tsx` | Replace `trafficSegments` import with `GET /api/v1/map/traffic` fetch; add `getTrafficData()` to `mapApi.ts` |
| Admin KPI cards call analytics API | Ajinkya Taranekar | `AdminDashboardPage.tsx` + new backend endpoint | Add `GET /api/v1/admin/analytics` to journey service; call from frontend |
| Admin notifications use admin endpoint | Ziwei Zhao | `AdminNotificationsPage.tsx` | Implement separate fetch for `GET /api/v1/admin/notifications`; do not reuse driver page |
| Journey segment occupancy from capacity | Jai Nagle | `journeyApi.ts` | After map route is fetched, call `GET /api/v1/capacity/segments/{id}/availability` per segment; or add occupancy to journey response |
| Enforcement verify uses admin role | Ajinkya Taranekar | Journey Service router | Change `middleware.EnforcementOnly` to accept both `admin` and `enforcement` roles, or replace with `middleware.AdminOnly` |
| Driver name in journey list | Ajinkya Taranekar | `journeyApi.ts` | Journey Service should return `driver_name` in response, or frontend should call IAM for name lookup |

### 5.4 Process Going Forward

1. **API contract first.** Before writing backend code, produce a one-page interface doc (request/response JSON shape, HTTP method, auth requirement). All affected team members review and sign off.
2. **Frontend change is part of the same PR.** A backend API change is not "done" until the corresponding frontend call is added or updated. No backend-only PRs for endpoints the UI uses.
3. **Mock data is a stub, not a feature.** Any page that uses `mockData.ts` is explicitly marked as `[STUB - BACKEND REQUIRED]` in the component JSX. When the real API is wired, the stub is deleted. No mock data survives in a demo-ready build.
4. **Service owner runs the E2E flow for their service before marking a PR ready.** If Map Service adds `/api/v1/map/traffic`, the Map Service owner opens the traffic map page in a browser against the real backend and verifies segments render with live data before requesting review.

---

---

## Section 6: TomTom Integration — Real Search, Real Routing, Real Segments

> This section supersedes Map Service Phases 1 and 3 and XC-02 from earlier sections. Read this as the definitive architecture for how the system gets road data.

---

### 6.1 The Problem Being Solved

The system currently has three compounding failures around location and routing:

1. **No search.** Origin and destination are chosen from a 10-item hardcoded dropdown. A driver cannot type "Cork" and find it.
2. **No real routing.** The route from City A to City B is computed by Dijkstra on a fake in-process graph of made-up edges. The M50, M7, and M8 do not exist in the system.
3. **No real segments.** Capacity is reserved against segment IDs like `seg_city_north` that correspond to nothing on an actual map. These are fictional labels that were never aligned with any external source.

Replacing all three with TomTom APIs unblocks the system at the root. Everything downstream — capacity reservation, journey approval, traffic map, enforcement verify — works correctly once Map Service returns real segment IDs derived from real roads.

---

### 6.2 TomTom APIs Selected

**API 1 — Fuzzy Search (place autocomplete)**
```
Endpoint: GET https://api.tomtom.com/search/2/search/{query}.json
Free tier: 2,500 requests/day, no credit card required
Signup:    developer.tomtom.com → create account → copy API key
```

Used by: the frontend `BookJourneyPage` search box. The backend does not call this — the browser calls it directly.

**API 2 — Routing (route + segment extraction)**
```
Endpoint: GET https://api.tomtom.com/routing/1/calculateRoute/{origin}:{dest}/json
          ?key=API_KEY
          &instructionsType=tagged
          &routeType=fastest
          &traffic=false
Free tier: 2,500 requests/day, no credit card required
```

Used by: Map Service. Called only on cache miss (first time a route is requested). Result stored in PostgreSQL permanently.

Both APIs use the same API key.

---

### 6.3 Segment ID Derivation — The Stable Key Contract

When TomTom returns a route, the `guidance.instructions` array contains road numbers like `["M50"]`, `["M7"]`, `["M8"]`. Adjacent instructions on the same road are merged into one segment. The segment ID is derived deterministically from the road reference:

```
Road number from TomTom → Canonical segment ID
"M50"                   → "IE-M50"
"M7"                    → "IE-M7"
"M8"                    → "IE-M8"
"A1" (Northern Ireland) → "GB-A1"
"A40" (London)          → "GB-A40"
"E40" (Belgium/Germany) → "EU-E40"
"I-95" (US East Coast)  → "US-I95"
"N1"  (generic)         → "IE-N1"
```

**Prefix rule:** derived from the `countryCode` field in the TomTom Search result for the origin. Ireland routes → `IE-`. UK routes → `GB-`. European E-roads → `EU-`. US Interstates → `US-`.

This derivation is deterministic: Dublin → Cork will always produce `["IE-M50", "IE-M7", "IE-M8"]`, regardless of when the route is computed or which TomTom data centre responds. The same segment ID can be used as the stable key in `capacity.segments` across all VMs.

---

### 6.4 Route Database Cache — Architecture

The key insight: a Dublin → Cork route via the M7/M8 corridor does not change day-to-day. The road geometry, the segment sequence, and the traversal times are stable. Computing this route from TomTom once and caching it permanently in PostgreSQL means:

- TomTom is called **once per unique origin-destination pair ever**.
- Every subsequent booking on the same route reads from the database in microseconds.
- Pre-seeded common intercity routes mean TomTom is never called for them at all.
- The 2,500 req/day free limit is effectively never hit in normal operation.

#### Database Schema (Map Service, `map` schema)

```sql
-- Migration: map-service/migrations/001_create_schema.sql

CREATE SCHEMA IF NOT EXISTS map;

-- Canonical places: a resolved TomTom place with stable coordinates.
-- Populated the first time a search result is selected by any driver.
CREATE TABLE IF NOT EXISTS map.places (
    place_id        VARCHAR(100) PRIMARY KEY,   -- TomTom entity ID (stable)
    label           VARCHAR(300) NOT NULL,       -- "Cork, County Cork, Ireland"
    lat             DOUBLE PRECISION NOT NULL,
    lng             DOUBLE PRECISION NOT NULL,
    country_code    VARCHAR(5)   NOT NULL,       -- "IE", "GB", "US"
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Named road segments: one row per physical road section ever seen.
-- Populated on first reservation; max_capacity set from defaults table.
CREATE TABLE IF NOT EXISTS map.segments (
    segment_id      VARCHAR(50)  PRIMARY KEY,   -- "IE-M50"
    segment_name    VARCHAR(200) NOT NULL,       -- "M50 Motorway"
    country_code    VARCHAR(5)   NOT NULL,
    max_capacity    DOUBLE PRECISION NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Cached routes: origin place → destination place → ordered segment list.
-- Populated once on first booking for a given O/D pair; read forever after.
CREATE TABLE IF NOT EXISTS map.routes (
    route_id                VARCHAR(60)  PRIMARY KEY,  -- "rte_<origin_id>_<dest_id>"
    origin_place_id         VARCHAR(100) NOT NULL REFERENCES map.places(place_id),
    dest_place_id           VARCHAR(100) NOT NULL REFERENCES map.places(place_id),
    total_traversal_min     INTEGER      NOT NULL,
    total_distance_km       DOUBLE PRECISION NOT NULL,
    tomtom_fetched_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_used_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (origin_place_id, dest_place_id)
);

-- Ordered segment list for each cached route.
CREATE TABLE IF NOT EXISTS map.route_segments (
    id              BIGSERIAL    PRIMARY KEY,
    route_id        VARCHAR(60)  NOT NULL REFERENCES map.routes(route_id),
    sequence        INTEGER      NOT NULL,
    segment_id      VARCHAR(50)  NOT NULL REFERENCES map.segments(segment_id),
    traversal_min   INTEGER      NOT NULL,
    distance_km     DOUBLE PRECISION NOT NULL,
    geometry        JSONB        NOT NULL,        -- [[lat,lng], ...] for map rendering
    UNIQUE (route_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_route_segments_route ON map.route_segments (route_id);
```

#### Route Lookup Logic in Map Service

```
GET /api/v1/map/route?origin_place_id=<tomtom_id>&dest_place_id=<tomtom_id>

1.  Look up route_id = "rte_{origin_id}_{dest_id}" in map.routes
2.  If found:
      - UPDATE map.routes SET last_used_at = now()
      - JOIN map.route_segments ORDER BY sequence
      - Return cached segments immediately  ← zero TomTom calls
3.  If not found:
      - Call TomTom Routing API once
      - Extract segments from guidance.instructions (group by road number)
      - Upsert each segment into map.segments (with capacity default)
      - INSERT into map.places for origin and dest if not exists
      - INSERT route + route_segments in a single transaction
      - Return the newly computed segments
```

All of step 3 happens inside a single PostgreSQL transaction. If TomTom is unavailable, no partial data is written — the error propagates to Journey Service as 502.

---

### 6.5 Pre-Seeded Intercity Routes

Common Irish intercity routes are seeded at deployment time. TomTom is never called for these; they go straight to the database lookup path on their first request.

```sql
-- map-service/migrations/002_seed_places.sql

INSERT INTO map.places (place_id, label, lat, lng, country_code) VALUES
    ('tt_dublin_city',    'Dublin City Centre, Ireland',    53.3498, -6.2603, 'IE'),
    ('tt_cork_city',      'Cork City, Ireland',             51.8979, -8.4706, 'IE'),
    ('tt_galway_city',    'Galway City, Ireland',           53.2707, -9.0568, 'IE'),
    ('tt_limerick_city',  'Limerick City, Ireland',         52.6638, -8.6267, 'IE'),
    ('tt_waterford_city', 'Waterford City, Ireland',        52.2593, -7.1101, 'IE'),
    ('tt_belfast_city',   'Belfast City, Northern Ireland', 54.5973, -5.9301, 'GB'),
    ('tt_killarney',      'Killarney, County Kerry',        52.0599, -9.5044, 'IE'),
    ('tt_sligo_city',     'Sligo, Ireland',                 54.2766, -8.4761, 'IE'),
    ('tt_drogheda',       'Drogheda, County Louth',         53.7184, -6.3566, 'IE'),
    ('tt_athlone',        'Athlone, County Westmeath',      53.4239, -7.9407, 'IE')
ON CONFLICT (place_id) DO NOTHING;

-- map-service/migrations/003_seed_segments.sql

INSERT INTO map.segments (segment_id, segment_name, country_code, max_capacity) VALUES
    -- Major Irish motorways (vehicles/hour based on TII traffic data)
    ('IE-M50',  'M50 Motorway',                    'IE', 5800),
    ('IE-M7',   'M7 Motorway (Naas Road)',          'IE', 3900),
    ('IE-M8',   'M8 Motorway (Cork Road)',          'IE', 3600),
    ('IE-M1',   'M1 Motorway (Belfast Road)',       'IE', 4200),
    ('IE-M4',   'M4 Motorway (Galway Road West)',   'IE', 3400),
    ('IE-M6',   'M6 Motorway (Galway Road East)',   'IE', 3000),
    ('IE-M9',   'M9 Motorway (Waterford Road)',     'IE', 3200),
    ('IE-M11',  'M11 Motorway (Wexford Road)',      'IE', 2800),
    ('IE-M17',  'M17 Motorway (Tuam Road)',         'IE', 2400),
    ('IE-M18',  'M18 Motorway (Ennis Road)',        'IE', 2600),
    ('IE-M20',  'M20 Motorway (Cork–Limerick)',     'IE', 3000),
    ('IE-N20',  'N20 (Cork–Limerick pre-M20)',      'IE', 1800),
    ('IE-N40',  'N40 Cork Ring Road',               'IE', 3200),
    -- National primary roads (single carriageway / dual)
    ('IE-N7',   'N7 Naas Road',                    'IE', 1800),
    ('IE-N8',   'N8 Cork Road (pre-motorway)',      'IE', 1500),
    ('IE-N11',  'N11 Wexford Road',                'IE', 1600),
    ('IE-N17',  'N17 Galway–Sligo',                'IE', 1200),
    ('IE-N18',  'N18 Ennis Road',                  'IE', 1400),
    ('IE-N22',  'N22 Kerry Road',                  'IE', 1200),
    ('IE-N25',  'N25 Waterford–Rosslare',          'IE', 1200),
    ('IE-N3',   'N3 Navan Road',                   'IE', 1500),
    -- Northern Ireland / cross-border
    ('GB-A1',   'A1 (Belfast–Newry)',               'GB', 2400),
    ('GB-M1NI', 'M1 (Northern Ireland)',            'GB', 3600)
ON CONFLICT (segment_id) DO NOTHING;

-- map-service/migrations/004_seed_routes.sql

-- Dublin → Cork via M50/M7/M8
INSERT INTO map.routes (route_id, origin_place_id, dest_place_id, total_traversal_min, total_distance_km)
VALUES ('rte_dublin_cork', 'tt_dublin_city', 'tt_cork_city', 145, 256)
ON CONFLICT DO NOTHING;

INSERT INTO map.route_segments (route_id, sequence, segment_id, traversal_min, distance_km, geometry) VALUES
    ('rte_dublin_cork', 1, 'IE-M50', 14, 18.2,
     '[[53.3498,-6.2603],[53.3600,-6.2900],[53.3700,-6.3200],[53.3400,-6.3500]]'),
    ('rte_dublin_cork', 2, 'IE-M7',  68, 95.1,
     '[[53.3400,-6.3500],[53.2500,-6.6000],[52.9000,-7.2000],[52.6800,-7.8000]]'),
    ('rte_dublin_cork', 3, 'IE-M8',  55, 115.4,
     '[[52.6800,-7.8000],[52.3000,-8.0500],[52.0000,-8.2000],[51.8979,-8.4706]]')
ON CONFLICT DO NOTHING;

-- Dublin → Galway via M50/M4/M6
INSERT INTO map.routes (route_id, origin_place_id, dest_place_id, total_traversal_min, total_distance_km)
VALUES ('rte_dublin_galway', 'tt_dublin_city', 'tt_galway_city', 135, 219)
ON CONFLICT DO NOTHING;

INSERT INTO map.route_segments (route_id, sequence, segment_id, traversal_min, distance_km, geometry) VALUES
    ('rte_dublin_galway', 1, 'IE-M50', 12, 15.0,
     '[[53.3498,-6.2603],[53.3600,-6.3500]]'),
    ('rte_dublin_galway', 2, 'IE-M4',  65, 87.0,
     '[[53.3600,-6.3500],[53.3500,-7.0000],[53.3300,-7.8000]]'),
    ('rte_dublin_galway', 3, 'IE-M6',  48, 85.0,
     '[[53.3300,-7.8000],[53.3100,-8.3000],[53.2707,-9.0568]]')
ON CONFLICT DO NOTHING;

-- Dublin → Limerick via M7
INSERT INTO map.routes (route_id, origin_place_id, dest_place_id, total_traversal_min, total_distance_km)
VALUES ('rte_dublin_limerick', 'tt_dublin_city', 'tt_limerick_city', 125, 198)
ON CONFLICT DO NOTHING;

INSERT INTO map.route_segments (route_id, sequence, segment_id, traversal_min, distance_km, geometry) VALUES
    ('rte_dublin_limerick', 1, 'IE-M50', 14, 18.2, '[[53.3498,-6.2603],[53.3400,-6.3500]]'),
    ('rte_dublin_limerick', 2, 'IE-M7',  95, 155.0, '[[53.3400,-6.3500],[52.6638,-8.6267]]')
ON CONFLICT DO NOTHING;

-- Dublin → Belfast via M1
INSERT INTO map.routes (route_id, origin_place_id, dest_place_id, total_traversal_min, total_distance_km)
VALUES ('rte_dublin_belfast', 'tt_dublin_city', 'tt_belfast_city', 115, 167)
ON CONFLICT DO NOTHING;

INSERT INTO map.route_segments (route_id, sequence, segment_id, traversal_min, distance_km, geometry) VALUES
    ('rte_dublin_belfast', 1, 'IE-M1',  80, 120.0, '[[53.3498,-6.2603],[54.1000,-6.2500]]'),
    ('rte_dublin_belfast', 2, 'GB-A1',  35,  47.0, '[[54.1000,-6.2500],[54.5973,-5.9301]]')
ON CONFLICT DO NOTHING;

-- Dublin → Waterford via M9
INSERT INTO map.routes (route_id, origin_place_id, dest_place_id, total_traversal_min, total_distance_km)
VALUES ('rte_dublin_waterford', 'tt_dublin_city', 'tt_waterford_city', 105, 163)
ON CONFLICT DO NOTHING;

INSERT INTO map.route_segments (route_id, sequence, segment_id, traversal_min, distance_km, geometry) VALUES
    ('rte_dublin_waterford', 1, 'IE-M50', 12, 15.0, '[[53.3498,-6.2603],[53.3000,-6.3500]]'),
    ('rte_dublin_waterford', 2, 'IE-M9',  85, 130.0, '[[53.3000,-6.3500],[52.2593,-7.1101]]')
ON CONFLICT DO NOTHING;

-- Cork → Limerick via N20/M20
INSERT INTO map.routes (route_id, origin_place_id, dest_place_id, total_traversal_min, total_distance_km)
VALUES ('rte_cork_limerick', 'tt_cork_city', 'tt_limerick_city', 90, 126)
ON CONFLICT DO NOTHING;

INSERT INTO map.route_segments (route_id, sequence, segment_id, traversal_min, distance_km, geometry) VALUES
    ('rte_cork_limerick', 1, 'IE-N40', 15, 12.0, '[[51.8979,-8.4706],[51.9200,-8.3500]]'),
    ('rte_cork_limerick', 2, 'IE-N20', 75, 114.0, '[[51.9200,-8.3500],[52.6638,-8.6267]]')
ON CONFLICT DO NOTHING;
```

Geometry coordinates above are approximate straight-line interpolations for seeding. TomTom will overwrite them with accurate road geometry on the first live call (if the route is ever requested and the seed geometry is replaced). For the prototype demo, approximate lines are sufficient for rendering.

---

### 6.6 TomTom Routing Integration in Map Service (Go)

```go
// map-service/internal/client/tomtom_client.go

package client

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"
)

const tomtomRoutingBase = "https://api.tomtom.com/routing/1/calculateRoute"

type TomTomClient struct {
    apiKey     string
    httpClient *http.Client
}

func NewTomTomClient(apiKey string) *TomTomClient {
    return &TomTomClient{
        apiKey: apiKey,
        httpClient: &http.Client{Timeout: 10 * time.Second},
    }
}

// RoutedSegment is one named road section extracted from TomTom guidance.
type RoutedSegment struct {
    SegmentID    string      // "IE-M50"
    SegmentName  string      // "M50 Motorway"
    CountryCode  string      // "IE"
    TraversalMin int
    DistanceKm   float64
    Geometry     [][2]float64 // [lat, lng] pairs
}

// GetRoute calls TomTom and returns ordered named road segments.
func (c *TomTomClient) GetRoute(
    ctx context.Context,
    originLat, originLng,
    destLat, destLng float64,
    countryCode string,
) ([]RoutedSegment, int, float64, error) {

    url := fmt.Sprintf(
        "%s/%.6f,%.6f:%.6f,%.6f/json?key=%s&instructionsType=tagged&routeType=fastest&traffic=false",
        tomtomRoutingBase, originLat, originLng, destLat, destLng, c.apiKey,
    )

    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, 0, 0, fmt.Errorf("tomtom routing: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return nil, 0, 0, fmt.Errorf("tomtom routing: status %d", resp.StatusCode)
    }

    var result struct {
        Routes []struct {
            Summary struct {
                LengthInMeters  float64 `json:"lengthInMeters"`
                TravelTimeInSec int     `json:"travelTimeInSeconds"`
            } `json:"summary"`
            Legs []struct {
                Points []struct {
                    Lat float64 `json:"latitude"`
                    Lng float64 `json:"longitude"`
                } `json:"points"`
            } `json:"legs"`
        } `json:"routes"`
        Guidance struct {
            Instructions []struct {
                RouteOffsetInMeters int      `json:"routeOffsetInMeters"`
                TravelTimeInSec     int      `json:"travelTimeInSeconds"`
                RoadNumbers         []string `json:"roadNumbers"`
                PossibleCombineWith string   `json:"possibleCombineWith"`
                DriveAlongRoadNames []struct {
                    Value string `json:"value"`
                } `json:"driveAlongRoadNames"`
            } `json:"instructions"`
        } `json:"guidance"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, 0, 0, fmt.Errorf("tomtom routing decode: %w", err)
    }
    if len(result.Routes) == 0 {
        return nil, 0, 0, fmt.Errorf("tomtom routing: no routes returned")
    }

    route := result.Routes[0]
    totalMin := route.Summary.TravelTimeInSec / 60
    totalKm := route.Summary.LengthInMeters / 1000

    // Collect all route points for geometry
    allPoints := [][2]float64{}
    for _, leg := range route.Legs {
        for _, p := range leg.Points {
            allPoints = append(allPoints, [2]float64{p.Lat, p.Lng})
        }
    }

    // Extract named segments by grouping consecutive instructions on the same road
    segments := extractSegments(result.Guidance.Instructions, allPoints, countryCode)

    return segments, totalMin, totalKm, nil
}

type instruction struct {
    RouteOffsetInMeters int
    TravelTimeInSec     int
    RoadNumbers         []string
}

func extractSegments(instructions []struct {
    RouteOffsetInMeters int      `json:"routeOffsetInMeters"`
    TravelTimeInSec     int      `json:"travelTimeInSeconds"`
    RoadNumbers         []string `json:"roadNumbers"`
    PossibleCombineWith string   `json:"possibleCombineWith"`
    DriveAlongRoadNames []struct {
        Value string `json:"value"`
    } `json:"driveAlongRoadNames"`
}, allPoints [][2]float64, countryCode string) []RoutedSegment {

    segments := []RoutedSegment{}
    var current *RoutedSegment

    for _, instr := range instructions {
        roadRef := primaryRoadRef(instr.RoadNumbers)
        if roadRef == "" {
            // Unnamed connector road — absorb into current segment if any
            if current != nil {
                current.TraversalMin += instr.TravelTimeInSec / 60
            }
            continue
        }

        segID := countryCode + "-" + strings.ReplaceAll(roadRef, " ", "")
        segName := roadRef + " Motorway/Road" // TomTom also returns full name in driveAlongRoadNames

        if current != nil && current.SegmentID == segID {
            // Same road — extend
            current.TraversalMin += instr.TravelTimeInSec / 60
        } else {
            if current != nil {
                segments = append(segments, *current)
            }
            current = &RoutedSegment{
                SegmentID:    segID,
                SegmentName:  segName,
                CountryCode:  countryCode,
                TraversalMin: instr.TravelTimeInSec / 60,
            }
        }
    }
    if current != nil {
        segments = append(segments, *current)
    }

    // Distribute geometry points proportionally (simplified)
    // In production: use routeOffsetInMeters to slice allPoints accurately
    if len(allPoints) > 0 && len(segments) > 0 {
        chunkSize := len(allPoints) / len(segments)
        for i := range segments {
            start := i * chunkSize
            end := start + chunkSize
            if i == len(segments)-1 {
                end = len(allPoints)
            }
            segments[i].Geometry = allPoints[start:end]
        }
    }

    return segments
}

func primaryRoadRef(refs []string) string {
    // Prefer motorway refs (M-prefix for Ireland) over national roads
    for _, r := range refs {
        if strings.HasPrefix(r, "M") || strings.HasPrefix(r, "E") {
            return r
        }
    }
    if len(refs) > 0 {
        return refs[0]
    }
    return ""
}
```

```go
// map-service/internal/service/route_service.go  (DB cache layer)

func (s *RouteService) GetRoute(ctx context.Context, originPlaceID, destPlaceID string) (*RouteResult, error) {
    routeID := "rte_" + originPlaceID + "_" + destPlaceID

    // 1. Check DB cache
    cached, err := s.repo.GetCachedRoute(ctx, routeID)
    if err == nil && cached != nil {
        _ = s.repo.TouchRoute(ctx, routeID)   // update last_used_at
        return cached, nil
    }

    // 2. Resolve places from DB (inserted earlier by search flow)
    origin, err := s.repo.GetPlace(ctx, originPlaceID)
    if err != nil {
        return nil, fmt.Errorf("origin place not found: %w", err)
    }
    dest, err := s.repo.GetPlace(ctx, destPlaceID)
    if err != nil {
        return nil, fmt.Errorf("dest place not found: %w", err)
    }

    // 3. Call TomTom
    segments, totalMin, totalKm, err := s.tomtom.GetRoute(
        ctx, origin.Lat, origin.Lng, dest.Lat, dest.Lng, origin.CountryCode,
    )
    if err != nil {
        return nil, fmt.Errorf("tomtom unavailable: %w", err)
    }

    // 4. Upsert segments into map.segments with capacity defaults
    for _, seg := range segments {
        cap := defaultCapacity(seg.SegmentID)
        if err := s.repo.UpsertSegment(ctx, seg, cap); err != nil {
            return nil, err
        }
    }

    // 5. Store route + route_segments in one transaction
    result := &RouteResult{
        RouteID:      routeID,
        TotalMin:     totalMin,
        TotalKm:      totalKm,
        Segments:     segments,
    }
    if err := s.repo.StoreRoute(ctx, originPlaceID, destPlaceID, result); err != nil {
        return nil, err
    }

    return result, nil
}

var roadCapacityDefaults = map[string]float64{
    "IE-M50": 5800, "IE-M7": 3900, "IE-M8": 3600, "IE-M1": 4200,
    "IE-M4":  3400, "IE-M6": 3000, "IE-M9": 3200, "IE-M11": 2800,
    "IE-M17": 2400, "IE-M18": 2600, "IE-M20": 3000, "IE-N20": 1800,
    "IE-N40": 3200, "IE-N7":  1800, "IE-N8":  1500, "IE-N11": 1600,
    "IE-N17": 1200, "IE-N18": 1400, "IE-N22": 1200, "IE-N25": 1200,
    "IE-N3":  1500, "GB-A1":  2400, "GB-M1NI": 3600,
}

func defaultCapacity(segmentID string) float64 {
    if cap, ok := roadCapacityDefaults[segmentID]; ok {
        return cap
    }
    return 1000 // conservative default for unknown roads
}
```

---

### 6.7 TomTom Search — Backend Proxy (API Key Never Exposed to Frontend)

**Design principle:** The TomTom Search and Routing API keys must never appear in the frontend. All calls to `api.tomtom.com` are made server-side by Map Service. The frontend calls our own backend endpoint, which proxies to TomTom internally.

#### 6.7.1 Map Service — New `GET /api/v1/map/search` Endpoint

```go
// map-service/internal/http/handlers/map_handler.go — add SearchPlaces handler

// SearchPlaces godoc
// @Summary Search for a place by name
// @Description Proxies to TomTom Fuzzy Search API server-side; API key never exposed to browser.
// @Tags Map
// @Produce json
// @Param q      query string true  "Search query (e.g. 'Dublin', 'Cork Airport')"
// @Param limit  query int    false "Max results, default 6"
// @Success 200 {array} PlaceResult
// @Router /api/v1/map/search [get]
func (h *MapHandler) SearchPlaces(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query().Get("q")
    if len(query) < 2 {
        respondJSON(w, http.StatusOK, []PlaceResult{})
        return
    }
    limit := 6
    if l := r.URL.Query().Get("limit"); l != "" {
        if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 20 {
            limit = n
        }
    }

    results, err := h.tomtom.FuzzySearch(r.Context(), query, limit)
    if err != nil {
        http.Error(w, "search unavailable", http.StatusBadGateway)
        return
    }
    respondJSON(w, http.StatusOK, results)
}
```

```go
// map-service/internal/client/tomtom_client.go — FuzzySearch method

func (c *TomTomClient) FuzzySearch(ctx context.Context, query string, limit int) ([]PlaceResult, error) {
    url := fmt.Sprintf(
        "https://api.tomtom.com/search/2/search/%s.json?key=%s&typeahead=true&limit=%d&idxSet=POI,PAD,Addr,Geo",
        url.QueryEscape(query), c.apiKey, limit,
    )
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    resp, err := c.http.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var raw struct {
        Results []struct {
            ID      string `json:"id"`
            Address struct {
                FreeformAddress string `json:"freeformAddress"`
                CountryCode     string `json:"countryCode"`
            } `json:"address"`
            Position struct {
                Lat float64 `json:"lat"`
                Lon float64 `json:"lon"`
            } `json:"position"`
        } `json:"results"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
        return nil, err
    }

    out := make([]PlaceResult, 0, len(raw.Results))
    for _, r := range raw.Results {
        out = append(out, PlaceResult{
            PlaceID:     r.ID,
            Label:       r.Address.FreeformAddress,
            Lat:         r.Position.Lat,
            Lng:         r.Position.Lon,
            CountryCode: r.Address.CountryCode,
        })
    }
    return out, nil
}
```

Register the route in `router.go`:

```go
r.mux.HandleFunc("/api/v1/map/search", r.mapHandler.SearchPlaces).Methods("GET")
```

#### 6.7.2 Frontend — Calls Our Own Backend (No TomTom Key in Browser)

```ts
// frontend/src/app/services/mapApi.ts — search calls our backend, not TomTom directly

export interface PlaceResult {
  placeId:     string;   // TomTom entity ID — stable, used as DB cache key
  label:       string;   // "Cork City, County Cork, Ireland"
  lat:         number;
  lng:         number;
  countryCode: string;   // "IE", "GB"
}

// NOTE: No VITE_TOMTOM_KEY here. The browser never talks to api.tomtom.com.
export async function searchPlaces(query: string): Promise<PlaceResult[]> {
  if (!query || query.length < 2) return [];
  const res = await fetch(
    `/api/v1/map/search?q=${encodeURIComponent(query)}&limit=6`,
    { headers: authHeaders() },
  );
  if (!res.ok) return [];
  const data = await res.json();
  // Map service returns PlaceResult array with snake_case keys
  return (data ?? []).map((r: any) => ({
    placeId:     r.place_id,
    label:       r.label,
    lat:         r.lat,
    lng:         r.lng,
    countryCode: r.country_code,
  }));
}
```

```tsx
// frontend/src/app/pages/driver/BookJourneyPage.tsx — search input component

function PlaceSearch({
  label, value, onChange, error,
}: {
  label:    string;
  value:    PlaceResult | null;
  onChange: (p: PlaceResult) => void;
  error?:   string;
}) {
  const [query, setQuery]     = useState(value?.label ?? '');
  const [results, setResults] = useState<PlaceResult[]>([]);
  const [open, setOpen]       = useState(false);

  useEffect(() => {
    if (query.length < 2) { setResults([]); return; }
    const t = setTimeout(async () => {
      const r = await searchPlaces(query);   // calls /api/v1/map/search
      setResults(r);
      setOpen(r.length > 0);
    }, 300);
    return () => clearTimeout(t);
  }, [query]);

  const handleSelect = (place: PlaceResult) => {
    setQuery(place.label);
    setOpen(false);
    setResults([]);
    onChange(place);
    // Map Service automatically stores the place in map.places when
    // /api/v1/map/route is later called with origin_place_id + dest_place_id
  };

  return (
    <div className="relative">
      <input
        value={query}
        onChange={e => { setQuery(e.target.value); setOpen(true); }}
        placeholder={`Search ${label}...`}
        style={{ border: error ? '1.5px solid #B42318' : '1.5px solid var(--border)' }}
        className="w-full px-4 py-2.5 rounded-lg outline-none"
      />
      {open && results.length > 0 && (
        <ul className="absolute z-50 w-full bg-white rounded-lg shadow-lg mt-1 overflow-hidden"
            style={{ border: '1px solid var(--border)' }}>
          {results.map(r => (
            <li key={r.placeId}
                onClick={() => handleSelect(r)}
                className="px-4 py-3 hover:bg-muted cursor-pointer text-sm"
                style={{ color: '#1F2421' }}>
              {r.label}
            </li>
          ))}
        </ul>
      )}
      {error && <p className="mt-1 text-sm" style={{ color: '#B42318' }}>{error}</p>}
    </div>
  );
}
```

```tsx
// frontend/src/app/pages/driver/BookJourneyPage.tsx — search input component

function PlaceSearch({
  label, value, onChange, error,
}: {
  label: string;
  value: PlaceResult | null;
  onChange: (p: PlaceResult) => void;
  error?: string;
}) {
  const [query, setQuery]     = useState(value?.label ?? '');
  const [results, setResults] = useState<PlaceResult[]>([]);
  const [open, setOpen]       = useState(false);

  useEffect(() => {
    if (query.length < 2) { setResults([]); return; }
    const t = setTimeout(async () => {
      const r = await searchPlaces(query);
      setResults(r);
      setOpen(r.length > 0);
    }, 300);
    return () => clearTimeout(t);
  }, [query]);

  const handleSelect = async (place: PlaceResult) => {
    setQuery(place.label);
    setOpen(false);
    setResults([]);
    await registerPlace(place);   // store in backend for route caching
    onChange(place);
  };

  return (
    <div className="relative">
      <input
        value={query}
        onChange={e => { setQuery(e.target.value); setOpen(true); }}
        placeholder={`Search ${label}...`}
        style={{ border: error ? '1.5px solid #B42318' : '1.5px solid var(--border)' }}
        className="w-full px-4 py-2.5 rounded-lg outline-none"
      />
      {open && results.length > 0 && (
        <ul className="absolute z-50 w-full bg-white rounded-lg shadow-lg mt-1 overflow-hidden"
            style={{ border: '1px solid var(--border)' }}>
          {results.map(r => (
            <li key={r.placeId}
                onClick={() => handleSelect(r)}
                className="px-4 py-3 hover:bg-muted cursor-pointer text-sm"
                style={{ color: '#1F2421' }}>
              {r.label}
            </li>
          ))}
        </ul>
      )}
      {error && <p className="mt-1 text-sm" style={{ color: '#B42318' }}>{error}</p>}
    </div>
  );
}
```

The booking form then passes `placeId` (not coordinates) to `createJourney`:

```ts
// journeyApi.ts — updated createJourney body
body: JSON.stringify({
  origin_place_id:      params.originPlaceId,   // TomTom stable ID
  dest_place_id:        params.destPlaceId,
  departure_time:       departureISO,
  vehicle_type:         toApiVehicleType(params.vehicleType),
}),
```

Journey Service passes `origin_place_id` + `dest_place_id` to Map Service's `GET /api/v1/map/route?origin_place_id=...&dest_place_id=...`. Map Service performs the DB cache lookup. If the route exists (pre-seeded or previously computed), TomTom is not called. If new, TomTom is called once and stored.

---

### 6.8 New Environment Variables Required

```bash
# Map Service — backend only (Search API + Routing API key, never exposed to browser)
TOMTOM_API_KEY=your_tomtom_api_key_here

# Frontend (.env) — map tile rendering key only (restricted to your domain in TomTom dashboard)
# See Section 6.12 for TomTom Maps SDK integration
VITE_TOMTOM_MAPS_KEY=your_tomtom_maps_key_here
```

**Key separation rationale:**
- `TOMTOM_API_KEY` (server-side): used by Map Service to call `api.tomtom.com/search` and `api.tomtom.com/routing`. Lives only in the backend container environment. Never shipped to the browser.
- `VITE_TOMTOM_MAPS_KEY` (frontend): used only by TomTom Maps SDK to fetch map tiles. Tile requests go directly from the browser to TomTom CDN. Restrict this key by HTTP referrer in the TomTom developer dashboard so it cannot be abused from other origins.
- Both keys may be the same TomTom account key if the free-tier quota is sufficient. For production, separate them so a leaked maps key cannot call the more expensive Routing/Search APIs.

Obtain free keys at `developer.tomtom.com` (2,500 free transactions/day per API).

```yaml
# docker-compose.yml — add to map-service environment section
TOMTOM_API_KEY: ${TOMTOM_API_KEY}
```

---

### 6.9 Updated Map Service Phases (Supersedes Section 2 Map Service Phases 1 and 3)

**Phase 1 (replaces old Phase 1 + Phase 3): DB schema + route caching + TomTom client**

| Step | Action | Owner | Done condition |
|------|--------|-------|----------------|
| 1a | Write migrations 001–004 (schema + places + segments + routes seeds) | Xiaoxuan Duan | `docker compose up` creates all tables with pre-seeded data |
| 1b | Implement `TomTomClient.GetRoute()` and `extractSegments()` | Xiaoxuan Duan | Unit test: Dublin→Cork returns `[IE-M50, IE-M7, IE-M8]` |
| 1c | Implement `RouteService.GetRoute()` with DB cache layer | Xiaoxuan Duan | Second call for same O/D pair does not hit TomTom (verified by log) |
| 1d | Add `GET /api/v1/map/search` backend proxy endpoint (calls TomTom Fuzzy Search server-side) | Xiaoxuan Duan | `curl /api/v1/map/search?q=Cork` returns place results; no TomTom key in frontend |
| 1e | Add `POST /api/v1/map/places` endpoint (upsert place from selected search result) | Xiaoxuan Duan | Calling endpoint stores place in `map.places` |
| 1f | Update `GET /api/v1/map/route` to accept `origin_place_id` + `dest_place_id` | Xiaoxuan Duan | Journey Service integration test returns real M-road segment IDs |
| 1g | Update `TOMTOM_API_KEY` env var in docker-compose; add `VITE_TOMTOM_MAPS_KEY` to frontend `.env` | Xiaoxuan Duan | `docker compose up` starts without missing key error; TomTom map tiles render |

**Phase 2 (unchanged): implement `/api/v1/map/traffic`** — see original Section 2.

**Phase 3 (new): remove hardcoded graph**

Once Phase 1 is working, delete `buildHardcodedEdges()`, `hardcodedNodes`, `hardcodedEdges`, `calculateShortestRoute()` from `map_handler.go`. Delete the `GET /api/v1/map/nodes` endpoint (no longer needed — search replaces it). Update `BookJourneyPage` to use `PlaceSearch` component.

---

### 6.10 Impact on Capacity Service Seed

The existing `002_seed_segments.sql` file in Capacity Service becomes redundant after this integration. The Capacity Service no longer needs a pre-seeded segment list — it creates segment rows dynamically via upsert when Journey Service sends a reservation for a new segment ID. The `map.segments` table (Map Service) is the authoritative segment registry. Capacity Service mirrors only the segments it has seen.

**Action for Jai Nagle (Capacity Service):** Remove `002_seed_segments.sql`. Add upsert-on-insert logic to `ReservationService.Reserve()`:

```go
// Before inserting the reservation, ensure the segment row exists
if err := s.segmentRepo.UpsertFromMapService(ctx, tx, r.SegmentID, r.SegmentName, defaultCapacity(r.SegmentID)); err != nil {
    return nil, 500, err
}
```

This eliminates F-01 (segment ID mismatch) permanently. There is no longer any pre-seeded segment list to fall out of sync.

---

### 6.11 Cache Invalidation Policy

Routes are cached permanently in PostgreSQL by default. Two conditions trigger a re-fetch from TomTom:

1. **Admin-forced refresh:** Add `DELETE /api/v1/admin/map/routes/{route_id}` to Map Service. Deleting the row causes the next request to re-fetch. Use this when a major road is closed permanently (e.g., the N20 is replaced by the M20 motorway).
2. **Age-based background refresh (optional):** A background goroutine can re-fetch any route where `tomtom_fetched_at < now() - interval '90 days'` to keep traversal times accurate as roads improve. Not required for the prototype.

Routes are **not** invalidated on traffic events. Traversal time estimates in the route cache reflect typical conditions. Real-time traffic slowdowns are not modelled for capacity reservation purposes — the system books a slot, not a specific travel time.

---

### 6.12 TomTom Maps SDK — Frontend Map Rendering (Fixes U-15)

**Assignee:** Xiaoxuan Duan (Frontend)

Replace the hand-drawn SVG map with a real TomTom interactive map. The map tile key (`VITE_TOMTOM_MAPS_KEY`) lives only in the frontend `.env` — it fetches tiles directly from TomTom CDN. All route geometry and segment data still comes from our own backend.

#### 6.12.1 Install

```bash
npm install @tomtom-international/web-sdk-maps
```

Add CSS to `index.html` (or `main.tsx`):
```html
<link rel="stylesheet"
  href="https://api.tomtom.com/maps-sdk-for-web/cdn/6.x/6.25.0/maps/maps.css" />
```

#### 6.12.2 Admin Traffic Map — `TrafficMapPage.tsx`

Replace the SVG with a TomTom map that draws segment overlays coloured by occupancy:

```tsx
import tt from '@tomtom-international/web-sdk-maps';
import { useEffect, useRef } from 'react';
import { getTrafficData } from '../../services/mapApi';  // calls /api/v1/map/traffic

export default function TrafficMapPage() {
  const mapRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const map = tt.map({
      key:       import.meta.env.VITE_TOMTOM_MAPS_KEY,
      container: mapRef.current!,
      center:    [-6.2603, 53.3498],  // Dublin city centre
      zoom:      7,
    });

    map.on('load', async () => {
      const traffic = await getTrafficData();  // { segments: [...], nodes: [...] }

      // Draw each segment as a coloured line on the map
      traffic.segments.forEach((seg: any) => {
        const color = seg.level === 'high' ? '#B42318'
                    : seg.level === 'medium' ? '#F79009'
                    : '#12B76A';

        map.addSource(`seg-${seg.segment_id}`, {
          type: 'geojson',
          data: {
            type: 'Feature',
            geometry: { type: 'LineString', coordinates: seg.geometry },
            properties: { segmentId: seg.segment_id, level: seg.level },
          },
        });
        map.addLayer({
          id:     `seg-line-${seg.segment_id}`,
          type:   'line',
          source: `seg-${seg.segment_id}`,
          paint:  { 'line-color': color, 'line-width': 5, 'line-opacity': 0.85 },
        });

        // Click popup showing occupancy
        map.on('click', `seg-line-${seg.segment_id}`, (e: any) => {
          new tt.Popup()
            .setLngLat(e.lngLat)
            .setHTML(`<strong>${seg.segment_name}</strong><br/>
                      Occupancy: ${seg.occupancy_pct}%<br/>
                      Level: ${seg.level}`)
            .addTo(map);
        });
      });

      // Draw node markers
      traffic.nodes.forEach((node: any) => {
        new tt.Marker()
          .setLngLat([node.lng, node.lat])
          .setPopup(new tt.Popup().setText(node.label))
          .addTo(map);
      });
    });

    // Refresh traffic data every 60 seconds
    const interval = setInterval(async () => {
      const traffic = await getTrafficData();
      // Re-paint segment colours based on updated occupancy
      traffic.segments.forEach((seg: any) => {
        const color = seg.level === 'high' ? '#B42318'
                    : seg.level === 'medium' ? '#F79009'
                    : '#12B76A';
        if (map.getLayer(`seg-line-${seg.segment_id}`)) {
          map.setPaintProperty(`seg-line-${seg.segment_id}`, 'line-color', color);
        }
      });
    }, 60_000);

    return () => { clearInterval(interval); map.remove(); };
  }, []);

  return (
    <div className="flex flex-col h-full">
      <h1 className="text-xl font-semibold mb-4">Live Traffic Map</h1>
      <div ref={mapRef} style={{ flex: 1, minHeight: '500px', borderRadius: '12px' }} />
    </div>
  );
}
```

**`getTrafficData()` in `mapApi.ts`:**

```ts
export async function getTrafficData() {
  const res = await fetch('/api/v1/map/traffic', { headers: authHeaders() });
  if (!res.ok) throw new Error('traffic API unavailable');
  return res.json();
  // Expected shape: { segments: [{ segment_id, segment_name, level, occupancy_pct,
  //                                vehicles, capacity, geometry: [[lng,lat], ...] }],
  //                   nodes: [{ node_id, label, lat, lng }] }
}
```

The `geometry` field in the traffic response must be populated by Map Service from the `map.route_segments` geometry stored during the TomTom routing call (Section 6.5).

#### 6.12.3 Driver Booking — Route Preview on Step 2

After a driver selects origin + destination and moves to step 2 (Review), show a small embedded map with the route drawn:

```tsx
// BookJourneyPage.tsx — step 2 route preview
import tt from '@tomtom-international/web-sdk-maps';

function RoutePreviewMap({ routeSegments }: { routeSegments: any[] }) {
  const mapRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!routeSegments?.length) return;

    const map = tt.map({
      key:       import.meta.env.VITE_TOMTOM_MAPS_KEY,
      container: mapRef.current!,
      zoom:      7,
    });

    map.on('load', () => {
      // Flatten all segment geometry into one LineString
      const allCoords = routeSegments.flatMap((s: any) => s.geometry ?? []);
      if (!allCoords.length) return;

      map.addSource('route', {
        type: 'geojson',
        data: { type: 'Feature', geometry: { type: 'LineString', coordinates: allCoords }, properties: {} },
      });
      map.addLayer({
        id: 'route-line', type: 'line', source: 'route',
        paint: { 'line-color': '#3B82F6', 'line-width': 4 },
      });

      // Fit map to route bounds
      const lngs = allCoords.map((c: number[]) => c[0]);
      const lats = allCoords.map((c: number[]) => c[1]);
      map.fitBounds(
        [[Math.min(...lngs), Math.min(...lats)], [Math.max(...lngs), Math.max(...lats)]],
        { padding: 40 },
      );
    });

    return () => map.remove();
  }, [routeSegments]);

  return <div ref={mapRef} style={{ height: '250px', borderRadius: '10px', marginTop: '16px' }} />;
}
```

Call `GET /api/v1/map/route?origin_place_id=...&dest_place_id=...` when the user moves to step 2, then pass the returned segments to `<RoutePreviewMap />`. This resolves U-05 (no route preview before submission).

#### 6.12.4 Geometry in Map Service Route Response

For the map overlays to work, Map Service must include `geometry` (array of `[lng, lat]` points) in both the route endpoint and the traffic endpoint responses. This geometry is extracted from TomTom's routing response (`legs[].points`) and stored in `map.route_segments.geometry` (PostGIS `LINESTRING` or JSONB array). Populate it during the TomTom `extractSegments()` call — the geometry distribution logic is already sketched in Section 6.6 (`allPoints[start:end]`).

#### 6.12.5 Dependencies Summary

| Package | Purpose | Key in browser? |
|---------|---------|-----------------|
| `@tomtom-international/web-sdk-maps` | Map tile rendering + overlays | `VITE_TOMTOM_MAPS_KEY` (restricted by referrer) |
| TomTom Fuzzy Search API | Place autocomplete | NO — proxied via Map Service backend |
| TomTom Routing API | Segment extraction | NO — called only by Map Service backend |

---

*End of Audit Report*
