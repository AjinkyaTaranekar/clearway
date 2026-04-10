# Eraser AI Diagram Prompts
## Distributed Vehicle Capacity System

> **How to use:** Open [eraser.io](https://eraser.io) → New Diagram → AI Generate → paste prompt → Generate.
>
> **Global rules applied to every diagram:**
> - **Canvas: A4 Landscape (297 × 210 mm), horizontal layout only — never taller than wide**
> - **Compact: short labels only — no sentences, no explanations on the diagram**
> - **Text style: node/box labels max 3 words, arrow labels max 4 words**
> - **White background, flat colours, no shadows, no gradients**
> - **Font: Inter or Helvetica, 11–12pt max**
> - **Margins: 8 mm all sides**
> - **Arrow: solid = sync HTTP · dashed = async/event · double-headed = bidirectional**

---

## DIAGRAM INDEX

| # | Title | Type |
|---|-------|------|
| 1.1 | System Overview | Cloud Architecture |
| 1.2 | Service Communication | Cloud Architecture |
| 2.1 | Booking: Auth + Route | Sequence |
| 2.2 | Booking: Reservation | Sequence |
| 2.3 | Booking: Events + Push | Sequence |
| 3.1 | JWT Lifecycle | Sequence |
| 3.2 | JWKS Verification | Flow |
| 4.1 | Concurrent Reservation | Sequence |
| 4.2 | Reservation Failures | Sequence |
| 5 | Journey State Machine | State |
| 6 | Transactional Outbox | Sequence |
| 7 | Route Computation | Flow |
| 8.1 | DB Schema: Auth + Journey | ER |
| 8.2 | DB Schema: Capacity + Map + Notif | ER |
| 9 | Frontend Page Flow | Flow |
| 10 | Infrastructure Stack | Cloud Architecture |
| 11 | Redis Event Pipeline | Flow |
| 12 | Observability Stack | Cloud Architecture |

---

---

## 1.1 — System Overview
**Cloud architecture diagram. A4 landscape. 4 horizontal swim lanes top to bottom.**

```
Canvas: A4 landscape, horizontal layout, flat design, labels only (max 3 words per node).

Draw 4 horizontal swim lanes:

LANE 1 "Client":
  [Browser / PWA] [FCM Client]

LANE 2 "Gateway":
  [Nginx :80 | Rate Limit · Routing]

LANE 3 "Services" — 5 boxes left to right, each with port label:
  [IAM :8082] [Journey :8083] [Capacity :8081] [Map :8084] [Notification :8085]
  Colours: blue, indigo, orange, teal, purple

LANE 4 "Data" — 2 boxes:
  [CockroachDB | auth · journey · capacity · map · notification]  [Redis :6379 | journey.events stream]

Float to the right outside lanes — "External":
  [Firebase FCM]  [Nominatim]  [OSRM]

Connections (short arrow labels):
  Browser → Nginx: "HTTPS"
  Nginx → IAM: "/auth"
  Nginx → Journey: "/journeys"
  Nginx → Capacity: "/capacity"
  Nginx → Map: "/map /routes"
  Nginx → Notification: "/notifications"
  All 5 services → CockroachDB: solid thin arrows
  Journey → Redis: "XADD"
  Capacity → Redis: "XREADGROUP" (dashed)
  Notification → Redis: "XREADGROUP" (dashed)
  Notification → Firebase FCM: "FCM push"
  Map → Nominatim: "geocode"
  Map → OSRM: "validate route"
  All services → IAM: dashed grey "JWKS fetch"

No description text anywhere. Labels only.
```

---

## 1.2 — Service Communication Map
**Cloud architecture diagram. A4 landscape. Pentagon layout, infrastructure in centre.**

```
Canvas: A4 landscape, horizontal, compact node labels only.

5 service nodes in a wide pentagon, left to right:
  [IAM] top-left  [Journey] top-centre  [Map] top-right
  [Capacity] bottom-right  [Notification] bottom-left

Centre two nodes:
  [Redis] and [CockroachDB]

Solid arrows (sync HTTP) — short labels:
  Journey → Map: "compute route"
  Journey → Capacity: "reserve" + small lightning bolt = "circuit breaker"
  Map → Capacity: "check capacity"

Dashed arrows (async) — short labels:
  Journey → Redis: "XADD events"
  Capacity → Redis: "XREADGROUP"
  Notification → Redis: "XREADGROUP"

Grey dashed to all services from IAM:
  IAM ← all: "JWKS"

Legend box bottom-right (tiny):
  — solid = HTTP   -- dashed = async   ⚡ = circuit breaker
```

---

---

## 2.1 — Booking: Phase 1 (Auth) + Phase 2 (Route Preview)
**Sequence diagram. A4 landscape. Left-to-right participants. Two sections.**

```
Canvas: A4 landscape, horizontal sequence diagram, labels only — no explanation text.
Participant boxes left to right (short names):
  [Driver] [Nginx] [IAM] [Map] [Nominatim] [DB]

--- AUTH ---
Driver → Nginx: POST /auth/login
Nginx → IAM: proxy
IAM → DB: SELECT user
DB → IAM: user row
IAM → IAM: bcrypt verify (self-loop)
IAM → IAM: sign RS256 JWT (self-loop)
IAM → DB: INSERT refresh_token
IAM → Driver: {access_token, refresh_token}
Driver → Driver: localStorage (self-loop)

--- ROUTE PREVIEW ---
Driver → Nginx: GET /routes/compute + Bearer JWT
Nginx → Map: proxy
Map → IAM: GET /.well-known/jwks.json
IAM → Map: JWKS keys
Map → Map: verify JWT (self-loop)
Map → DB: SELECT route_cache

alt "HIT"
  DB → Map: cached route
else "MISS"
  Map → Map: Dijkstra (self-loop)
  Map → Nominatim: geocode
  Nominatim → Map: coords
  Map → DB: INSERT cache
end

Map → Driver: {segments, total_minutes}

Section banners: "PHASE 1: AUTH" and "PHASE 2: ROUTE PREVIEW"
No prose. Short labels only. Compact vertical spacing.
```

---

## 2.2 — Booking: Phase 3 (Core Reservation)
**Sequence diagram. A4 landscape. Five participants. Three steps.**

```
Canvas: A4 landscape, horizontal sequence diagram, compact, labels only.
Participants left to right (short names):
  [Driver] [Journey] [Map] [Capacity] [DB]

Driver → Journey: POST /journeys + Idempotency-Key
Journey → Journey: validate JWT (self-loop)
Journey → DB: SELECT idempotency_cache

alt "DUPLICATE"
  DB → Journey: cached response
  Journey → Driver: 200 (cached)
else "NEW"
  Journey → Journey: validate rules (self-loop)
  Journey → DB: SELECT active journey

  alt "HAS ACTIVE"
    Journey → Driver: 409 Conflict
  else "OK"

    Note: "STEP A: ROUTE"
    Journey → Map: POST /routes/compute
    Map → Journey: segments + windows

    Note: "STEP B: RESERVE"
    Journey → Capacity: POST /capacity/reserve
    Capacity → DB: BEGIN SERIALIZABLE
    Capacity → DB: SELECT segments FOR UPDATE
    Capacity → DB: SUM slots_used
    DB → Capacity: current_usage
    Capacity → DB: INSERT reservations
    Capacity → DB: COMMIT
    Capacity → Journey: 201 reserved

    Note: "STEP C: PERSIST"
    Journey → DB: BEGIN TXN
    Journey → DB: INSERT journey (APPROVED)
    Journey → DB: INSERT journey_segments
    Journey → DB: INSERT outbox (published=false)
    Journey → DB: INSERT idempotency_cache
    Journey → DB: COMMIT
    Journey → Driver: 201 APPROVED
  end
end

Group steps A/B/C with coloured background bands. Labels only, no prose.
```

---

## 2.3 — Booking: Phase 4 (Outbox) + Phase 5 (Notification)
**Sequence diagram. A4 landscape. Two async processes side by side.**

```
Canvas: A4 landscape, horizontal sequence diagram, labels only.
Participants left to right:
  [Journey] [DB] [Relay Goroutine] [Redis] [Notification] [FCM] [Driver]

--- PHASE 4: OUTBOX RELAY ---
Relay → DB: SELECT outbox WHERE published=false
DB → Relay: events[]
Relay → Redis: XADD journey.events
Redis → Relay: entry-id
Relay → DB: SET published=true

--- PHASE 5: NOTIFICATION ---
Notification → Redis: XREADGROUP
Redis → Notification: event
Notification → DB: INSERT notification (PENDING)
Notification → DB: SELECT fcm_token

alt "token found"
  Notification → FCM: push message
  alt "success"
    FCM → Notification: messageId
    Notification → DB: SET status=SENT
  else "fail"
    Notification → DB: SET status=RETRYING
  end
else "no token"
  Notification → DB: SET status=SKIPPED
end

Notification → Redis: XACK
FCM → Driver: push popup
Driver → Notification: GET /notifications
Notification → Driver: notifications[]

Section banners: "PHASE 4: OUTBOX RELAY" and "PHASE 5: NOTIFICATION DELIVERY"
Short labels. No prose on diagram.
```

---

---

## 3.1 — JWT Lifecycle
**Sequence diagram. A4 landscape. Four sections in one compact diagram.**

```
Canvas: A4 landscape, horizontal, compact sequence, labels only.
Participants: [Client] [IAM] [Protected Service] [DB]

--- LOGIN ---
Client → IAM: POST /auth/login
IAM → DB: SELECT user
IAM → IAM: bcrypt + sign JWT (self-loop)
IAM → DB: INSERT refresh_token
IAM → Client: {JWT, refresh_token}

--- API CALL ---
Client → Protected Service: request + Bearer JWT
Protected Service → IAM: GET /jwks (cached 1h)
Protected Service → Protected Service: verify RS256 (self-loop)
Protected Service → Client: 200 resource

--- REFRESH ---
Client → IAM: POST /auth/refresh {refresh_token}
IAM → DB: SELECT token (not revoked, not expired)
IAM → Client: new JWT

--- LOGOUT ---
Client → IAM: POST /auth/logout
IAM → DB: SET revoked_at=NOW all sessions
IAM → Client: 200

Section banners: LOGIN / API CALL / REFRESH / LOGOUT
Very compact — minimal row height. Labels only.
```

---

## 3.2 — JWKS Token Verification
**Flow diagram. A4 landscape, left-to-right flow.**

```
Canvas: A4 landscape, horizontal left-to-right flowchart, labels only.

Nodes (left to right):
[Request + Bearer JWT] → [Extract kid from JWT header] → [JWKS cache valid?]

Decision "cache valid?":
  YES → [Find key where kid matches]
  NO  → [GET /.well-known/jwks.json] → [Cache hit from IAM?]
            YES → [Store + find key]
            NO  → [Use last known key]

→ [Key found?]
  NO  → [401 unknown key] (red terminal)
  YES → [RSA verify signature]

→ [Valid signature?]
  NO  → [401 invalid] (red terminal)
  YES → [Check exp ± 30s skew]

→ [Expired?]
  YES → [401 expired] (red terminal)
  NO  → [Extract driver_id, role from claims] → [Pass to handler] (green terminal)

Diamonds = amber. Red terminals = red. Green terminal = green. All nodes: 2-3 word labels max.
Horizontal flow only — no vertical columns.
```

---

---

## 4.1 — Concurrent Reservation (Happy Path)
**Sequence diagram. A4 landscape. Two parallel transactions shown.**

```
Canvas: A4 landscape, horizontal sequence, labels only, compact.
Participants: [Journey A] [Journey B] [Capacity] [DB]

Journey A → Capacity: reserve [seg_city_north, seg_north_port]
Journey B → Capacity: reserve [seg_city_north, seg_south_gate]

Capacity → Capacity: sort segments A-Z (self-loop)

Note: "TXN A"
Capacity → DB: BEGIN SERIALIZABLE (TXN A)
Capacity → DB: SELECT seg_city_north FOR UPDATE (TXN A — acquires lock)

Note: "TXN B — BLOCKED"
Capacity → DB: BEGIN SERIALIZABLE (TXN B)
Capacity → DB: SELECT seg_city_north FOR UPDATE (TXN B — waiting)

DB → Capacity: max_capacity=100 (TXN A)
Capacity → DB: SUM slots_used → 95.0 (TXN A)
Capacity → Capacity: 95+1=96 ≤ 100 PASS (self-loop)
Capacity → DB: INSERT reservation seg_city_north (TXN A)
Capacity → DB: COMMIT TXN A
DB → Journey A: 201 reserved

Note: "TXN B — UNBLOCKED"
DB → Capacity: lock released → TXN B resumes
Capacity → DB: SUM slots_used → 96.0 (TXN B sees TXN A)
Capacity → Capacity: 96+1=97 ≤ 100 PASS (self-loop)
Capacity → DB: INSERT reservation (TXN B)
Capacity → DB: COMMIT TXN B
DB → Journey B: 201 reserved

TXN A in blue activation. TXN B blocking period as grey bar.
Labels only. No prose.
```

---

## 4.2 — Reservation Failures (3 Scenarios)
**Sequence diagram. A4 landscape. Three parallel columns, one per scenario.**

```
Canvas: A4 landscape, horizontal sequence. Three separate columns side by side.
Each column has same participants: [Journey] [Capacity] [DB]

COLUMN 1 — "At Capacity":
  Journey → Capacity: reserve
  Capacity → DB: BEGIN SERIALIZABLE
  Capacity → DB: SUM slots_used
  DB → Capacity: usage=100.0
  Capacity → Capacity: 100+1>100 FAIL (self-loop)
  Capacity → DB: ROLLBACK
  Capacity → Journey: 200 {status:failed, reason:at_capacity}
  Journey → Journey: status=REJECTED (self-loop)

COLUMN 2 — "Segment Closed":
  Journey → Capacity: reserve
  Capacity → DB: BEGIN SERIALIZABLE
  Capacity → DB: SELECT segment_closures (window overlap)
  DB → Capacity: closure found
  Capacity → DB: ROLLBACK
  Capacity → Journey: 200 {status:failed, reason:segment_closed}

COLUMN 3 — "Serialization Retry":
  Journey → Capacity: reserve
  Capacity → DB: BEGIN SERIALIZABLE (attempt 1)
  DB → Capacity: ERROR 40001
  Capacity → DB: ROLLBACK
  Capacity → DB: BEGIN SERIALIZABLE (attempt 2)
  Capacity → DB: COMMIT OK
  Capacity → Journey: 201 reserved

  Note on col 3: "max 5 retries → 503 if all fail"

Failure terminals red. Success terminal green. Column dividers as thin vertical lines.
Short labels only.
```

---

---

## 5 — Journey State Machine
**State diagram. A4 landscape. Horizontal left-to-right flow.**

```
Canvas: A4 landscape, horizontal state machine, compact, labels only.

States as rounded rectangles, arranged left to right:

[●] → [PENDING] → [APPROVED] → [ACTIVE] → [COMPLETED]
                                        ↘ [CANCELLED]
               ↘ [REJECTED]
               ↘ [EXPIRED]
[APPROVED] → [CANCELLED]

State colours:
  PENDING=yellow  APPROVED=green  ACTIVE=blue
  COMPLETED=dark green  REJECTED=red  CANCELLED=grey  EXPIRED=dark grey

Arrow labels (short):
  PENDING → APPROVED: "capacity reserved"
  PENDING → REJECTED: "no capacity / closed"
  PENDING → EXPIRED: "expiry job"
  APPROVED → ACTIVE: "PUT /activate"
  APPROVED → CANCELLED: "PUT /cancel"
  ACTIVE → COMPLETED: "PUT /complete"
  ACTIVE → CANCELLED: "admin cancel"
  COMPLETED → [●]: terminal
  REJECTED → [●]: terminal
  CANCELLED → [●]: terminal (note: "capacity released")
  EXPIRED → [●]: terminal

Small annotation next to APPROVED: "UNIQUE(driver_id) — 1 active max"
No prose. State names only in boxes. Arrow labels max 3 words.
Horizontal flow, not vertical.
```

---

---

## 6 — Transactional Outbox
**Sequence diagram. A4 landscape. Three swimlanes.**

```
Canvas: A4 landscape, horizontal sequence, compact, labels only.
Participants: [Handler] [DB] [Relay Goroutine] [Redis] [Consumer]

--- ATOMIC WRITE ---
Handler → DB: BEGIN TXN
Handler → DB: UPDATE journey status (optimistic lock version++)
Handler → DB: INSERT outbox {event_type, payload, published=false}

alt "TXN OK"
  DB → Handler: COMMIT
else "version conflict"
  DB → Handler: ROLLBACK (no phantom event)
end

--- RELAY (1s poll loop) ---
Relay → DB: SELECT outbox WHERE published=false LIMIT 100
DB → Relay: events[]
Relay → Redis: XADD journey.events * {event_id, type, payload}
Redis → Relay: entry-id
Relay → DB: SET published=true

--- CONSUMER ---
Consumer → Redis: XREADGROUP (block 5s)
Redis → Consumer: entry
Consumer → Consumer: process (idempotent on event_id) (self-loop)
Consumer → Redis: XACK

Small note bottom: "XAUTOCLAIM: reclaim idle>60s from crashed consumer"

Section banners: ATOMIC WRITE / RELAY / CONSUMER
Labels only. Compact row height.
```

---

---

## 7 — Map Route Computation
**Flow diagram. A4 landscape. Left-to-right pipeline.**

```
Canvas: A4 landscape, horizontal left-to-right flowchart, labels only.

[POST /routes/compute] → [Verify JWT] → [valid?]
  NO → [401] (red)
  YES → [Build cache key] → [SELECT route_cache]

→ [cache hit?]
  YES → [Return cached route] (green terminal)
  NO  → [Load graph (adjacency list)] → [Dijkstra: weight=traversal_min] → [path found?]
    NO  → [404] (red)
    YES → [Build segment list] → [Register new segments w/ Capacity] → [INSERT route_cache TTL=1h]
          → [Compute estimated_arrival] → [Return {segments, total_min}] (green terminal)

Side note floating right (small box):
  "Journey Service computes windows:
   seg[0]: T → T+8min
   seg[1]: T+8min → T+24min"

Diamonds in amber. All node labels: max 4 words. Horizontal only.
```

---

---

## 8.1 — DB Schema: Auth + Journey
**ER diagram. A4 landscape. Two schema groups side by side.**

```
Canvas: A4 landscape, horizontal ER diagram, compact, short field labels only.

LEFT GROUP — "auth schema" (blue header):

auth.users:
  id PK | name | email_lower UK | password_hash | role | vehicle_type | created_at

auth.refresh_tokens:
  id PK | user_id FK | token_hash UK | expires_at | revoked_at | ip_address

auth.user_vehicles:
  id PK | user_id FK | vehicle_type | is_primary

Relationships (crow's foot):
  auth.users ||--o{ auth.refresh_tokens
  auth.users ||--o{ auth.user_vehicles

RIGHT GROUP — "journey schema" (indigo header):

journey.journeys:
  journey_id PK | driver_id | idempotency_key UK | origin_lat/lng | dest_lat/lng
  departure_time | vehicle_type | status | reservation_id | version

journey.journey_segments:
  id PK | journey_id FK | segment_id | sequence_order | time_window_start/end | traversal_min

journey.journey_events:
  event_id PK | journey_id FK | event_type | actor_type | metadata

journey.outbox:
  id PK | event_id UK | event_type | payload | published | published_at

journey.idempotency_cache:
  idempotency_key PK | journey_id | response_body | expires_at

Relationships (crow's foot):
  journey.journeys ||--o{ journey.journey_segments
  journey.journeys ||--o{ journey.journey_events

Cross-schema dashed arrow: journey.journeys.driver_id -.-> auth.users.id

Status enum box floating: PENDING|APPROVED|REJECTED|ACTIVE|COMPLETED|CANCELLED|EXPIRED

Field names only — no data types, no lengths. Compact column height.
```

---

## 8.2 — DB Schema: Capacity + Map + Notification
**ER diagram. A4 landscape. Three schema groups in a row.**

```
Canvas: A4 landscape, horizontal ER diagram, compact, field labels only.

LEFT GROUP — "capacity schema" (orange header):

capacity.segments:
  segment_id PK | segment_name | region | max_capacity | version

capacity.reservations:
  id PK | reservation_id | journey_id | segment_id FK | time_window_start/end
  vehicle_type | slots_used | status

capacity.segment_closures:
  id PK | segment_id FK | reason | closes_at | reopens_at | created_by

Relationships:
  capacity.segments ||--o{ capacity.reservations
  capacity.segments ||--o{ capacity.segment_closures

Small slot legend box (tiny, 2×4):
  car=1.0 | van=1.5 | motorcycle=0.5 | truck=3.0

CENTRE GROUP — "map schema" (teal header):

map.nodes:
  node_id PK | label | lat | lng

map.segments:
  segment_id PK | from_node FK | to_node FK | traversal_min | region

Relationships:
  map.nodes ||--o{ map.segments (from_node)
  map.nodes ||--o{ map.segments (to_node)
  Note arrow: "directed graph"

RIGHT GROUP — "notification schema" (purple header):

notification.notifications:
  notification_id PK | event_id UK | driver_id | journey_id | event_type
  title | type | delivery_status | retry_count | is_read | sent_at

notification.device_tokens:
  device_token_id PK | driver_id | fcm_token UK | platform | is_active | last_seen_at

Delivery status enum box: PENDING|SENT|FAILED|SKIPPED|RETRYING
Platform enum box: web|android|ios

Field names only. No types. Compact.
```

---

---

## 9 — Frontend Page Flow
**Flow diagram. A4 landscape. Two swim lanes side by side.**

```
Canvas: A4 landscape, horizontal flow, two vertical swim lanes.

TOP: Auth entry point (full width):
  [App Entry] → [JWT in localStorage?]
    NO  → [LoginPage /auth] → [role=driver] or [role=admin]
    YES → role-based redirect

LEFT LANE — "Driver" (blue):
  [Dashboard /driver]
    → [BookJourneyPage /driver/book]
        → geocode: GET /map/search
        → preview: GET /routes/compute
        → book: POST /journeys + Idempotency-Key
        → [BookingResultPage] → APPROVED (green) or REJECTED (red)
    → [MyJourneysPage /driver/journeys]
        → GET /journeys (30s cache)
        → [JourneyDetailPage /driver/journeys/:id]
            → GET /journeys/:id (60s cache)
            → GET /journeys/:id/events
            → GET /capacity/segments/occupancy (15s cache)
            → [Activate] [Cancel] [Complete] buttons
    → [NotificationsPage]
        → GET /notifications

RIGHT LANE — "Admin" (red):
  [AdminDashboard /admin]
    → GET /admin/analytics
    → [AllJourneysPage] → GET /admin/journeys → [AdminJourneyDetail]
    → [ClosuresPage] → GET + POST /capacity/closures
    → [TrafficMapPage] → GET /map/traffic + /capacity/segments/occupancy
    → [AdminNotifications] → GET /admin/notifications

Floating amber box right side (AppContext):
  user | isAuthenticated | journeys[] | notifications[] | unreadCount | lastBookingResult
  Dashed arrows to pages that read/write state.

Page boxes: rectangles. API calls: small inline labels on arrows. Max 3 words per node.
```

---

---

## 10 — Infrastructure Stack
**Cloud architecture diagram. A4 landscape. Four horizontal rows.**

```
Canvas: A4 landscape, horizontal cloud architecture, 4 rows, labels only.

ROW 0 (above stack) — "Entry":
  [GCP Load Balancer (HTTPS)] → [X-Forwarded-Proto: https]

ROW 1 — "Gateway":
  [Nginx :80 | api: 30req/s · booking: 5req/s · static SPA]

ROW 2 — "Services" (5 boxes, equal width):
  [IAM :8082 | Go · Chi · RSA keys]
  [Journey :8083 | Go · Outbox Relay · Expiry job]
  [Capacity :8081 | Go · Redis consumer · Orphan cleanup]
  [Map :8084 | Go · in-mem graph · route cache]
  [Notification :8085 | Go · Redis consumer]

ROW 3 — "Data" (2 large boxes):
  [CockroachDB :26257 | 5 schemas | Serializable isolation]
  [Redis :6379 | Stream: journey.events]

ROW 4 — "Observability" (6 small boxes):
  [Prometheus :9090] [Grafana :3000] [Loki :3100] [Promtail] [cAdvisor] [redis-exporter]

EXTERNAL (right side, cloud icons):
  [Firebase FCM] [Nominatim] [OSRM]

Connections (thin arrows, short labels):
  GCP LB → Nginx
  Nginx → all services
  All services → CockroachDB
  Journey → Redis: "XADD"
  Capacity + Notification → Redis: "XREADGROUP"
  Notification → Firebase FCM: "push"
  Map → Nominatim: "geocode"
  Map → OSRM: "route"
  All services → Prometheus: "/metrics"
  Promtail → Loki → Grafana
  Prometheus → Grafana
  cAdvisor + redis-exporter → Prometheus

Small CI/CD box bottom-left: [GitHub Actions | test → build → push → deploy] → stack

Labels only. Flat design. No prose text.
```

---

---

## 11 — Redis Event Pipeline
**Flow diagram. A4 landscape. Left-to-right pipeline.**

```
Canvas: A4 landscape, horizontal left-to-right pipeline, labels only.

LEFT — "Producers" (vertical stack of 7 small boxes):
  [journey.requested] [journey.booked] [journey.rejected]
  [journey.activated] [journey.cancelled] [journey.completed] [journey.expired]
  All feed into → [outbox table] → [Relay Goroutine XADD]

CENTRE — "Redis Stream: journey.events" (horizontal tape/ribbon):
  Show 4 entry cards on the tape:
    {event_id | journey.booked | payload}
    {event_id | journey.activated | payload}
    {event_id | journey.cancelled | payload}
    {event_id | journey.release | payload}

RIGHT — "Consumers" (two boxes stacked):

TOP: [Notification Service | consumer group: notification-service]
  Reads all event types →
    → [DB: INSERT notification]
    → [FCM push (if token)]
    → [XACK]

BOTTOM: [Capacity Service | consumer group: capacity-service]
  Reads journey.release only →
    → [DB: SET reservations released]
    → [XACK]

Small note bottom: "XAUTOCLAIM: idle > 60s → reclaim"

Colour per event: booked=green, rejected=red, cancelled=grey, activated=blue, completed=dark green.
Labels only. No prose.
```

---

---

## 12 — Observability Stack
**Cloud architecture diagram. A4 landscape. Three vertical columns.**

```
Canvas: A4 landscape, horizontal, 3 columns side by side, labels only.

COLUMN 1 — "Metrics Sources" (left):
  7 source boxes stacked:
    [IAM /metrics] [Journey /metrics] [Capacity /metrics]
    [Map /metrics] [Notification /metrics]
    [cAdvisor — container stats]
    [redis-exporter — stream lag]
  All → [Prometheus :9090 | scrape 15s]

COLUMN 2 — "Logs" (centre):
  [All services stdout | structured JSON]
  → [Promtail | labels: service, level, trace_id]
  → [Loki :3100 | LogQL]

COLUMN 3 — "Visualisation" (right):
  [Grafana :3000]
  Receives arrows from Prometheus (PromQL) and Loki (LogQL)
  
  5 small dashboard panel boxes inside Grafana:
    [Service health | req rate · p99 latency]
    [Capacity heatmap | occupancy %]
    [Journey funnel | PENDING→COMPLETED]
    [Redis lag | pending entries]
    [Infrastructure | CPU · memory]

  Below Grafana: [Alert Rules | p99>2s · errors>5% · stream lag>1000]

Labels only. Max 4 words per node. No prose. Horizontal layout.
```

---

---

## PRINT GUIDE — A4 Landscape Pack

| Page | Diagram | Notes |
|------|---------|-------|
| 1 | 1.1 System Overview | Full width |
| 2 | 1.2 Service Communication | Full width |
| 3 | 2.1 Auth + Route Booking | Full width |
| 4 | 2.2 Core Reservation | Full width |
| 5 | 2.3 Events + Push | Full width |
| 6 | 3.1 JWT Lifecycle | Full width |
| 7 | 3.2 JWKS Verification | Full width |
| 8 | 4.1 Concurrent Reservation | Full width |
| 9 | 4.2 Three Failure Scenarios | Full width |
| 10 | 5 State Machine | Full width |
| 11 | 6 Outbox Pattern | Full width |
| 12 | 7 Route Computation | Full width |
| 13 | 8.1 DB Schema Auth+Journey | Full width |
| 14 | 8.2 DB Schema Cap+Map+Notif | Full width |
| 15 | 9 Frontend Page Flow | Full width |
| 16 | 10 Infrastructure Stack | Full width |
| 17 | 11 Redis Event Pipeline | Full width |
| 18 | 12 Observability Stack | Full width |

> **Export from Eraser:** Share → Export PNG at 2x → import into Word/Pages set to A4 landscape → fit to page width → Print.
