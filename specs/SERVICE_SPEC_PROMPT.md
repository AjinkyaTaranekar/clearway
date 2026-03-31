# Prompt for Generating Service Specification Documents

## How to Use This

1. Copy everything below the `---` line
2. Replace `[YOUR SERVICE PLACEHOLDER]` section at the bottom with your specific service details
3. Paste the whole thing into a new Claude conversation
4. Claude will ask you clarifying questions about your service. Answer them.
5. After the Q&A, ask Claude to generate a markdown spec document similar to the Journey Service spec

Each team member has a pre-filled placeholder section at the bottom of this file. Copy the one that matches your service.

---

## PROMPT START (copy from here)

You are a senior distributed systems engineer helping me design and document a microservice. I need you to produce a comprehensive markdown specification document for my service, similar in depth and structure to the Journey Service spec my teammate already created.

### Project Context

We are a team of 5 building a Distributed Traffic Service for our CS7NS6 Distributed Systems module at Trinity College Dublin. The system allows road-vehicle drivers to prebook every journey before travel. Road capacity is treated as a finite, reservable resource. Each road segment has a maximum number of vehicles allowed per 15-minute time window. No driver may start a journey without prior system approval.

### Architecture Decisions (Already Finalized)

These decisions have been made by the team. Do not question or redesign them. Build your spec around them.

**Multi-VM Load Balanced Architecture:**
- The system runs as N identical VMs behind a load balancer (Nginx or AWS ALB)
- Every VM runs all 5 services, its own PostgreSQL instance, and its own Redis instance — a complete, self-contained stack
- The load balancer distributes driver requests across VMs using round-robin or least-connections. Any VM can handle any request.
- All VMs share state via **PostgreSQL multi-master logical replication**: every VM is both a publisher and a subscriber, so writes on VM A propagate to VM B and VM C within milliseconds
- There are NO cross-VM service-to-service REST calls. Journey Service always calls the Capacity Service and Map Service running on the same VM.
- Redis is per-VM (not shared). Each VM's event bus (Redis Streams) only serves the services on that VM.
- This design demonstrates distributed architecture: horizontal scale-out, data replication, and fault tolerance (a VM can go down without stopping the system)

**The 5 Services:**

| # | Service | Owner | Brief |
|---|---------|-------|-------|
| S1 | IAM / Auth | Deepika Nag | JWT authentication, user registration, profiles, RBAC |
| S2 | Journey Service | Ajinkya Taranekar | Booking lifecycle orchestrator. Coordinates Map + Capacity. Saga pattern. Publishes events. Owns route decomposition logic using Map Service data. |
| S3 | Capacity Service | Jai Nagle | Road segment slot management. 15-min time windows. Vehicle-type weights (car=1, van=1.5, truck=3, motorcycle=0.5). Optimistic concurrency control. |
| S4 | Map / Route Service | Xiaoxuan Duan | Pre-defined road segment graph. Shortest-path (Dijkstra). Route decomposition. TomTom API wrapper for visualization. Exposes node list and traffic data endpoints for the frontend. |
| S5 | Notification Service | Ziwei Zhao | Consumes events from Redis Streams. Sends push notifications via Firebase Cloud Messaging. Retry with exponential backoff. |

**Communication Patterns (Finalized):**

Synchronous (REST/HTTP, driver is waiting for response — all intra-VM):
- Journey Service -> IAM: JWT validation is done LOCALLY using cached JWKS public keys. No runtime REST call to IAM. IAM just serves a JWKS endpoint that Journey Service fetches on startup and refreshes hourly.
- Journey Service -> Map Service: GET route segments. Journey Service caches responses in Redis with 24-hour TTL. Both run on the same VM.
- Journey Service -> Capacity Service: Reserve/check slots for all segments. Both run on the same VM.

Asynchronous (Redis Streams, fire-and-forget after responding to driver — all intra-VM):
- Journey Service publishes events: journey.booked, journey.rejected, journey.cancelled, journey.activated, journey.completed, journey.expired
- Notification Service (same VM) consumes these events and sends Firebase push notifications
- Capacity Service (same VM) consumes journey.cancelled, journey.completed, journey.expired events to RELEASE reserved slots

Cross-VM data sync (handled by the database layer, NOT by services):
- PostgreSQL logical replication propagates all writes from each VM to every other VM automatically
- Services do not need to implement any replication logic — this is handled at the infrastructure level

**Key Business Rules:**
- Journeys must be booked at least 1 hour before departure
- Cancellation allowed only if departure_time minus now is strictly greater than 30 minutes
- One active (APPROVED or ACTIVE) journey per driver at a time
- Vehicle types: car (1 slot), van (1.5 slots), truck (3 slots), motorcycle (0.5 slots)
- Road capacity uses 15-minute time windows
- Road segment graph is simplified (approx 20-30 segments for prototype)

**Journey State Machine:**
PENDING -> APPROVED (all segments reserved)
PENDING -> REJECTED (capacity unavailable)
APPROVED -> ACTIVE (driver starts journey)
APPROVED -> CANCELLED (driver cancels 30min+ before departure, or admin force-cancel)
APPROVED -> EXPIRED (departure time + 30min passed, driver never activated, background job)
ACTIVE -> COMPLETED (driver finishes or estimated arrival time reached)
REJECTED, CANCELLED, COMPLETED, EXPIRED are terminal states.

**Cascading Time Window Model:**
When a driver books City Centre to Airport departing at 08:00, and the route has 4 segments with traversal times of 25, 45, 30, and 35 minutes:
- Segment 1: 08:00 to 08:25
- Segment 2: 08:25 to 09:10
- Segment 3: 09:10 to 09:40
- Segment 4: 09:40 to 10:15
Each segment gets its own specific time window based on cumulative travel time. Capacity is reserved per segment per time window.

**Request Flow (single VM, load balanced):**
1. Load balancer routes driver's request to VM B (arbitrary)
2. VM B's Journey Service validates JWT (local), checks business rules
3. VM B's Journey Service calls VM B's Map Service for route segments
4. VM B's Journey Service calls VM B's Capacity Service to atomically reserve all segments
5. If all succeed: APPROVED. If any fail: Capacity Service rolls back atomically, journey REJECTED.
6. Journey record written to VM B's PostgreSQL
7. PostgreSQL replication propagates record to VM A and VM C (async, < 100ms)
8. VM B publishes event to VM B's Redis Streams (async, after responding to driver)

**Tech Stack:**
- Backend: Go 1.22+ with gorilla/mux
- Database: PostgreSQL 16 (one instance per VM, schema-per-service isolation, multi-master logical replication across VMs)
- Cache + Event Bus: Redis 7 (caching, sessions, Redis Streams with consumer groups)
- API Gateway: Nginx (TLS, rate limiting, routing)
- Container Orchestration: Docker Swarm (multi-node)
- Maps: TomTom Maps API
- Push Notifications: Firebase Cloud Messaging
- Infrastructure: AWS (no budget constraints)

**Frontend (already built — React PWA):**

A React 18 + TypeScript Progressive Web App exists at `frontend/`. It is currently fully mocked (no real API calls). The frontend has two roles: **driver** and **admin**. Your service spec must describe the API contract your service exposes so the frontend can be wired up.

Driver pages and what they need:
- `/auth` — Login. Needs IAM `POST /api/v1/auth/login` returning JWT + user profile.
- `/driver` — Dashboard. Needs Journey Service `GET /api/v1/journeys` (recent bookings).
- `/driver/book` — Book journey. Needs Map Service `GET /api/v1/map/nodes` (origin/destination lookup) + Journey Service `POST /api/v1/journeys`.
- `/driver/journeys` — Journey list. Needs Journey Service `GET /api/v1/journeys` (paginated, filterable).
- `/driver/journeys/:id` — Journey detail. Needs Journey Service `GET`, activate, complete, cancel endpoints.
- `/driver/notifications` — Notifications. Needs Notification Service `GET /api/v1/notifications`, mark-read endpoints.
- `/driver/settings` — Profile. Needs IAM `GET/PUT /api/v1/auth/profile` + Notification Service `POST /api/v1/notifications/device-token` (FCM registration).

Admin pages and what they need:
- `/admin` — Dashboard. Needs Journey Service admin list + analytics summary.
- `/admin/journeys` — All journeys. Needs Journey Service `GET /api/v1/admin/journeys` (filterable by status, region, date).
- `/admin/journeys/:id` — Admin detail. Needs Journey Service force-cancel endpoint.
- `/admin/analytics` — Recharts analytics. Needs Journey Service `GET /api/v1/admin/analytics` (booking trend, regional stats, KPIs).
- `/admin/map` — Traffic map SVG. Needs Map Service `GET /api/v1/map/traffic` (segment occupancy + node topology).
- `/admin/notifications` — All notifications. Needs Notification Service admin list endpoint.

Frontend data model notes:
- Vehicle types in frontend: `Car`, `Van`, `Motorcycle`, `HGV`. Backend must support: `car`, `van`, `motorcycle`, `truck` (HGV maps to truck). `van` is not in the original spec — it must be added.
- Journey statuses in frontend: lowercase (`approved`, `active`, etc.). Backend uses uppercase. Frontend handles the display-layer conversion.
- Origin/destination: frontend uses human-readable node names. Map Service must expose `GET /api/v1/map/nodes` so the frontend can resolve names to lat/lng coordinates before calling Journey Service.
- Auth: JWT stored in `localStorage["cw_token"]`. All API calls send `Authorization: Bearer <token>` header. On 401, frontend attempts token refresh then redirects to `/auth`.

**Redis Streams Event Envelope:**
All events follow this structure:
```json
{
  "event_id": "evt_uuid",
  "event_type": "journey.booked",
  "timestamp": "2026-04-15T08:00:01Z",
  "source_region": "ireland",
  "payload": { ... }
}
```

Events include a `regions_involved` field so that Capacity Services in each region know whether they need to act on the event.

### What I Need From You

1. First, ask me 5-8 clarifying questions specific to MY service. Things like edge cases, failure modes, data model choices, interaction patterns with other services, and anything ambiguous.

2. After I answer your questions, generate a comprehensive markdown specification document for my service covering ALL of the following sections:

   - Purpose and Responsibilities
   - Architecture Context (where this service sits, communication patterns with justification for sync vs async)
   - API Contract (all endpoints with request/response examples, error codes)
   - What this service provides to other services (the API contract OTHER services depend on)
   - What this service needs from other services (dependencies)
   - Database Schema (PostgreSQL, with indexes and constraints)
   - Redis usage (caching strategy, Streams consumer/producer patterns)
   - State management (if applicable)
   - Edge Cases (at least 10, with specific mitigations)
   - Background Jobs (if applicable)
   - Multi-VM behavior (how this service behaves when N identical VMs run behind a load balancer, and how PostgreSQL replication affects it)
   - Configuration (environment variables)
   - Project Structure (Go project layout)
   - Sequence Diagrams (for primary flows)
   - **Frontend Integration** — which frontend pages call this service, exact endpoint + request/response shapes the frontend expects, any data model alignment notes (e.g. casing, field names), and CORS requirements. The frontend is a React 18 PWA already built with mocked data; this section tells the frontend developer exactly what to wire up.

3. For the communication pattern with each other service, explain WHY it is sync or async. Don't just state the choice. Justify it.

4. For edge cases, think about: concurrent requests, network failures, partial failures, timeout scenarios, data consistency issues, replication lag between VMs, race conditions under load balancing, and retry behavior.

5. Use the same level of detail as this example section from the Journey Service spec:

```
### 6.3 From Capacity Service (S3, Jai)

#### Reserve (sync, on booking)
POST /api/v1/capacity/reserve

Request:
{
  "journey_id": "jrn_a1b2c3d4",
  "idempotency_key": "idk_x1y2z3",
  "vehicle_type": "car",
  "reservations": [
    {
      "segment_id": "seg_m50",
      "time_window_start": "2026-04-15T08:00:00Z",
      "time_window_end": "2026-04-15T08:25:00Z"
    }
  ]
}

Response (success):
{
  "status": "reserved",
  "reservation_id": "rsv_abc123",
  "journey_id": "jrn_a1b2c3d4"
}

Response (failure):
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

### My Service Details

[YOUR SERVICE PLACEHOLDER - replace with the appropriate section below]

## PROMPT END

---
---
---

## Pre-filled Service Sections (each teammate copies their section into the placeholder above)

---

### FOR DEEPIKA (S1: IAM / Auth Service)

```
### My Service Details

I am building the IAM / Auth Service (S1).

My service is responsible for:
- User registration (drivers and admins)
- JWT-based authentication (login, token issuance, token refresh)
- Serving a JWKS endpoint so other services can validate tokens locally without calling me at runtime
- Driver profile management (name, email, vehicle type, license info)
- Role-based access control (two roles: "driver" and "admin")
- Password hashing and secure credential storage

Key interactions with other services:
- Journey Service does NOT call me at runtime for auth validation. Instead, it fetches my JWKS public keys on startup and refreshes hourly. JWT validation happens locally inside Journey Service. This means my service being down does not block bookings for users with existing valid tokens.
- Frontend + Admin Service calls me for login and registration flows
- All other services may fetch my JWKS endpoint for local token validation

My service owns the "auth" schema in PostgreSQL.

Important constraints:
- Tokens must include claims: sub (driver_id), role ("driver" or "admin"), exp (expiry timestamp)
- Token expiry should be configurable (suggested: 1 hour for access tokens, 7 days for refresh tokens)
- In a multi-cell deployment, each cell runs its own IAM Service instance with its own user database
- User registration in one cell does NOT automatically replicate to other cells (users register per-region)
- JWKS keys should be RSA-based for compatibility

Frontend integration notes:
- The login page (`/auth`) calls my `POST /api/v1/auth/login` endpoint. It sends `{ email, password }` and expects `{ access_token, refresh_token, user: { id, name, email, role, vehicle_type } }`.
- The driver settings page (`/driver/settings`) calls `GET /api/v1/auth/profile` and `PUT /api/v1/auth/profile`.
- The admin settings page (`/admin/settings`) calls `GET /api/v1/auth/profile`.
- Frontend stores the JWT in `localStorage["cw_token"]` and sends it as `Authorization: Bearer <token>` on every request.
- On 401, the frontend calls `POST /api/v1/auth/refresh` with `{ refresh_token }` to get a new access token.
- The frontend role-toggles between "driver" and "admin" on the login screen — my login response `role` field must be `"driver"` or `"admin"` exactly (lowercase).
- CORS: must allow `http://localhost:5173` in development (Vite dev server).

I need you to ask me clarifying questions before generating the spec.
```

---

### FOR JAI (S3: Capacity Service)

```
### My Service Details

I am building the Capacity Service (S3).

My service is responsible for:
- Managing road segment capacity (each segment has a max vehicle count per 15-minute time window)
- Accepting reservation requests from Journey Service and atomically reserving slots on multiple segments in a single database transaction (all-or-nothing)
- Supporting idempotency keys on reserve calls so that retries from Journey Service don't double-book
- Releasing reserved slots when journeys are cancelled, completed, or expired (consumed from Redis Streams asynchronously)
- Providing availability check endpoints (read-only, no reservation)
- Supporting vehicle type weights: car = 1 slot, truck = 3 slots, motorcycle = 0.5 slots
- Using optimistic concurrency control (version-based) to handle concurrent reservation attempts on the same segment/time-window

Key interactions with other services:
- Journey Service calls me SYNCHRONOUSLY via REST to reserve and check capacity. This is on the driver's critical path. The driver is waiting for the booking response. Both services run on the same VM so this is a fast intra-VM call.
- I consume Redis Streams events (journey.cancelled, journey.completed, journey.expired) ASYNCHRONOUSLY to release reserved slots. This is NOT on the critical path. The driver already has their cancellation/completion confirmation.
- I use Redis to cache hot segment availability data with 30-second TTL for read-heavy availability queries. Actual reservations always go through PostgreSQL.

Critical design requirement from Journey Service:
The reserve endpoint MUST be atomic. Journey Service sends a list of segments to reserve in one API call. Either ALL succeed or NONE succeed. This must be a single PostgreSQL transaction. Journey Service should NOT have to call reserve per-segment and handle rollback externally.

When a reservation fails, I must return WHICH segment failed and WHY (at capacity, unknown segment, version conflict), so Journey Service can give the driver a meaningful error message.

My service owns the "capacity" schema in PostgreSQL.

Multi-VM note:
- Each VM has its own PostgreSQL with its own capacity data
- When a reservation is written on VM A, PostgreSQL logical replication propagates it to VM B and VM C
- This means a booking made on VM A will have its capacity slots reflected on VM B within milliseconds
- The optimistic concurrency control (version field) prevents two VMs from simultaneously approving the last available slot: the DB transaction with SELECT FOR UPDATE ensures only one wins

Frontend integration notes:
- The frontend does NOT call my service directly. I am only called by Journey Service (synchronously for reserve/check) and I consume Redis Streams events asynchronously.
- However, the admin traffic map page (`/admin/map`) needs live segment occupancy data. This data originates from my service. Map Service will aggregate my occupancy data into its `GET /api/v1/map/traffic` response. I should expose a `GET /api/v1/capacity/segments/occupancy` endpoint that Map Service (or an admin aggregation layer) can call.
- Vehicle type alignment: the frontend sends `Car`, `Van`, `Motorcycle`, `HGV`. Journey Service will normalize these to `car`, `van`, `motorcycle`, `truck` before calling me. My service must accept `van` as a valid vehicle type (weight = 1.5 slots) in addition to `car`, `truck`, `motorcycle`.
- CORS: I don't need to allow browser origins since I'm not called directly by the frontend.

I need you to ask me clarifying questions before generating the spec.
```

---

### FOR XIAOXUAN (S4: Map / Route Service)

```
### My Service Details

I am building the Map / Route Service (S4).

My service is responsible for:
- Maintaining a pre-defined road segment graph (approximately 20-30 major road segments for the prototype)
- Each segment has metadata: segment_id, segment_name, start coordinates, end coordinates, traversal_time_minutes, max_capacity, and importantly a REGION tag (e.g., "ireland", "uk") that tells Journey Service which cell's Capacity Service to call
- Computing shortest paths between origin and destination using Dijkstra's algorithm on the segment graph
- Returning an ordered list of segments with traversal times and sequence order (route decomposition)
- Integrating with TomTom Maps API as a wrapper for client-side route visualization
- The road segment graph is loaded into memory at service startup and changes infrequently (road infrastructure updates)

Key interactions with other services:
- Journey Service calls me SYNCHRONOUSLY via REST to get route segments. This is on the critical path because Journey Service cannot compute cascading time windows without knowing the segments.
- Journey Service caches my responses in Redis with a 24-hour TTL keyed by origin+destination coordinates (rounded to 3 decimal places). This means for popular routes like Dublin to Cork, Journey Service only calls me once per day.
- If Journey Service gets an "unknown segment" error from Capacity Service, it invalidates the cache and calls me again (handles stale cache scenario).
- My data (the segment graph) is globally consistent across all VMs via PostgreSQL logical replication. Every VM has the full segment graph so it can compute any route.

My service owns the "map" schema in PostgreSQL (for segment metadata persistence), but the active graph is held in memory for fast path computation.

Important considerations:
- The segment graph must include segments from ALL regions (not just the local cell), because a Dublin user might request a route to Manchester which involves UK segments
- Segments have a region/area label for display purposes but no routing logic depends on it — there are no cross-VM Capacity calls
- TomTom API is called client-side (from the React PWA) for map rendering. My service provides a routing/directions URL or coordinates that the frontend uses.
- For the prototype, the graph can be hardcoded/seeded from a JSON file rather than dynamically managed

Frontend integration notes:
- I am called directly by the frontend in two cases:
  1. `GET /api/v1/map/nodes` — The `BookJourneyPage` (`/driver/book`) needs all graph nodes with labels and coordinates so the driver can select origin/destination by name. The frontend converts the selected name to lat/lng before calling Journey Service. Response shape: `{ nodes: [{ node_id, label, lat, lng }] }`.
  2. `GET /api/v1/map/traffic` — The admin traffic map page (`/admin/map`) needs segment topology (from/to nodes, x/y positions for SVG rendering) plus live occupancy from Capacity Service. Response shape: `{ segments: [{ segment_id, name, region, level, occupancy_pct, vehicles, capacity, trend, from_node, to_node }], nodes: [{ node_id, label, x, y }] }`. I need to either call Capacity Service internally or let this endpoint be served by an aggregation layer.
- The 10 nodes already defined in the frontend mock data (`mockData.ts mapNodes`): City Centre, North Gate, Airport, East Quay, South Terminal, Industrial Park, West Depot, Port Terminal, Northfield, Riverside. My actual node graph for the prototype should include at minimum these same named locations.
- CORS: must allow `http://localhost:5173` in development since the frontend calls me directly.

I need you to ask me clarifying questions before generating the spec.
```

---

### FOR ZIWEI (S5: Notification Service)

```
### My Service Details

I am building the Notification Service (S5).

My service is responsible for:
- Consuming journey lifecycle events from Redis Streams using consumer groups
- Sending push notifications to drivers via Firebase Cloud Messaging (FCM)
- Handling notification delivery with retry logic (exponential backoff)
- Tracking notification delivery status (sent, delivered, failed)
- Managing FCM device tokens (drivers register their device token on login)

Key interactions with other services:
- I do NOT receive any synchronous REST calls from Journey Service for notification delivery. All notifications are triggered by consuming events from the Redis Streams "journey.events" stream.
- This is ASYNCHRONOUS by design. The driver already has the booking response (APPROVED/REJECTED) from the HTTP response. The push notification is a secondary delivery channel that can arrive 1-5 seconds later.
- If my service is down, bookings continue to work. Notifications queue up in Redis Streams and are delivered when I come back online. Redis Streams consumer groups ensure no event is lost.
- Frontend + Admin Service may call me to register/update FCM device tokens for a user.

Events I consume from Redis Streams:
- journey.booked: Send "Your journey has been approved!" push notification
- journey.rejected: Send "Your journey was rejected: [reason]" push notification  
- journey.cancelled: Send "Your journey has been cancelled" push notification
- journey.activated: Send "Journey started. Drive safe!" push notification
- journey.completed: Send "Journey completed successfully" push notification
- journey.expired: Send "Your journey booking has expired" push notification

Each event includes driver_id which I use to look up the FCM device token.

My service owns the "notification" schema in PostgreSQL.

Important considerations:
- Redis Streams consumer group ensures each event is processed by exactly one instance of my service (enabling horizontal scaling)
- If FCM delivery fails, I retry up to 3 times with exponential backoff (1s, 4s, 16s)
- After 3 failures, I mark the notification as "failed" and move on (do not block the consumer group)
- I need to handle the case where a driver has no registered device token (they logged in on web but didn't grant notification permission)
- In a multi-cell deployment, each cell has its own Notification Service consuming from its own Redis Streams instance

Frontend integration notes:
- I am called directly by the frontend in these cases:
  1. `POST /api/v1/notifications/device-token` — Called after login when the driver grants browser notification permission. Body: `{ driver_id, fcm_token }`. This is how I learn which FCM token belongs to which driver.
  2. `GET /api/v1/notifications` — The notifications page (`/driver/notifications`) fetches the driver's notification history. Response shape matches the frontend `Notification` type: `{ notifications: [{ id, title, message, type, read, timestamp, journey_id? }], unread_count }`. `type` values: `"info" | "success" | "warning" | "error"`.
  3. `PUT /api/v1/notifications/:id/read` — Mark a single notification as read.
  4. `PUT /api/v1/notifications/read-all` — Mark all notifications as read for the authenticated driver.
  5. `GET /api/v1/admin/notifications` — Admin notifications page (`/admin/notifications`) needs recent notifications across all drivers (admin role required, paginated).
- Notification types map from journey events as follows (for `type` field):
  - `journey.booked` (approved) → `type: "success"`
  - `journey.rejected` → `type: "error"`
  - `journey.cancelled` → `type: "warning"`
  - `journey.activated` → `type: "info"`
  - `journey.completed` → `type: "success"`
  - `journey.expired` → `type: "warning"`
- CORS: must allow `http://localhost:5173` in development since the frontend calls me directly.

I need you to ask me clarifying questions before generating the spec.
```

---

## Instructions for Each Teammate

1. Copy everything from "PROMPT START" to "PROMPT END"
2. Find your pre-filled service section above (by your name)
3. Replace the `[YOUR SERVICE PLACEHOLDER]` text with your service section
4. Paste the complete prompt into a new Claude conversation
5. Answer Claude's clarifying questions thoughtfully
6. Ask Claude to generate the full markdown specification
7. Share the generated spec with the team for review
8. Share the API contract sections with teammates who depend on your service
