# Map / Route Service (S4) - Complete Specification

> **Owner:** Xiaoxuan Duan
> **Language:** Go 1.22+ (gorilla/mux)
> **Database:** PostgreSQL 16 (own schema: `map`)
> **Port:** 8084
> **Status:** Planning Phase

---

## 1. Purpose

The Map / Route Service is the route computation and topology service of the Distributed Traffic Service. It owns the predefined road-segment graph used by Journey Service to turn an origin and destination into a concrete sequence of reservable segments. Without this route decomposition, Journey Service cannot calculate cascading segment time windows and cannot ask Capacity Service to reserve the correct road resources.

In a multi-VM deployment, every VM runs the same Map Service alongside Journey, Capacity, IAM, and Notification services. Each Map Service instance reads its route metadata from its own local PostgreSQL replica and keeps the graph in memory for fast shortest-path computation. PostgreSQL logical replication ensures that topology data stays aligned across VMs, but route calculation itself always happens locally on the VM handling the request. There are no cross-VM routing calls.

Map Service also provides the node and segment data needed by the frontend. The driver booking page needs node labels and coordinates. The admin traffic map needs static topology from Map Service and live occupancy from Capacity Service. TomTom is used only as a visualization aid and integration convenience; it is not part of the core booking dependency chain.

---

## 2. Responsibilities

The Map / Route Service is responsible for:

- Maintaining the predefined graph of road nodes and road segments for the prototype
- Computing the shortest path between two graph nodes using Dijkstra's algorithm
- Returning an ordered list of road segments for Journey Service
- Returning traversal-time estimates for each segment and for the full route
- Exposing graph node data to the frontend for origin/destination selection
- Exposing graph segment data for map rendering and diagnostics
- Building the admin traffic-map response by combining static topology with occupancy data from the co-located Capacity Service
- Loading the graph into memory so route computation stays fast on the booking critical path

---

## 3. Architecture Context

### 3.1 Where Map Service Sits

The system is deployed as **N identical VMs behind a load balancer**. Every VM runs all 5 services, one PostgreSQL instance, and one Redis instance. Journey Service always calls the Map Service on the same VM that received the driver request.

```text
                    +--------------------------------------+
                    |            Load Balancer             |
                    |        (Nginx / AWS ALB)            |
                    +------------------+-------------------+
                                       |
             +-------------------------+-------------------------+
             |                         |                         |
    +--------+---------+     +--------+---------+     +--------+---------+
    |      VM A        |     |      VM B        |     |      VM C        |
    |------------------|     |------------------|     |------------------|
    | IAM Service      |     | IAM Service      |     | IAM Service      |
    | Journey Service  |     | Journey Service  |     | Journey Service  |
    | Capacity Service |     | Capacity Service |     | Capacity Service |
    | Map Service      |     | Map Service      |     | Map Service      |
    | Notification Svc |     | Notification Svc |     | Notification Svc |
    |------------------|     |------------------|     |------------------|
    | PostgreSQL       |     | PostgreSQL       |     | PostgreSQL       |
    | Redis            |     | Redis            |     | Redis            |
    +--------+---------+     +--------+---------+     +--------+---------+
             |                         |                         |
             +------------ Multi-Master PostgreSQL -------------+
                         Logical Replication
```

Within a single VM, Journey Service calls Map Service synchronously during booking, while the frontend calls Map Service directly for node and traffic-map data:

```text
  Driver / Admin Browser
       |  GET /api/v1/map/nodes
       |  GET /api/v1/map/traffic
       v
  Map Service :8084
       |  reads static graph metadata
       v
  PostgreSQL (same VM)

  Journey Service :8083
       |  GET /api/v1/map/route   (sync, booking critical path)
       v
  Map Service :8084

  Map Service :8084
       |  GET /api/v1/capacity/segments/occupancy
       |  (sync, admin traffic view only)
       v
  Capacity Service :8081
```

### 3.2 Communication Pattern Summary

| From -> To | Protocol | Sync/Async | Why |
|-----------|----------|------------|-----|
| Journey Service -> Map Service (`GET /api/v1/map/route`) | REST GET | **Sync** | Booking cannot continue until Journey Service knows the ordered segment list and traversal times. This is part of the driver's critical path. |
| Frontend -> Map Service (`GET /api/v1/map/nodes`) | REST GET | **Sync** | The booking page is waiting for the node list before the driver can choose origin and destination. |
| Frontend/Admin -> Map Service (`GET /api/v1/map/traffic`) | REST GET | **Sync** | The admin traffic page is rendering a live view and needs an immediate aggregated response. |
| Map Service -> Capacity Service (`GET /api/v1/capacity/segments/occupancy`) | REST GET | **Sync** | This call exists only to build the admin traffic view. It is not part of booking and not a core routing dependency. |
| Map Service -> PostgreSQL | SQL | **Sync** | Topology metadata is read from the local PostgreSQL instance and used to build the in-memory graph. |
| PostgreSQL VM-A -> VM-B -> VM-C | Logical Replication | **Async** | Topology changes propagate across VMs through PostgreSQL. Map Service does not implement replication logic itself. |

### 3.3 Why These Choices

**Route computation is synchronous because the booking flow blocks on it.**
Journey Service cannot compute cascading traversal windows or call Capacity Service until it knows the exact route. Returning that information later would force the booking flow into a pending state and add a second coordination step for no practical benefit.

**There are no cross-VM route calls because each VM already has the same topology.**
The architecture is intentionally designed so that every VM is self-sufficient. Calling Map Service on another VM would add a remote dependency even though the same graph data is already available locally.

**The graph is held in memory because it is small and mostly static.**
The prototype only needs around 20-30 segments and a small node set. Dijkstra on an in-memory adjacency list is fast and predictable. PostgreSQL remains the durable source of truth, but route calculation should not wait on repeated database reads.

**TomTom is not a core dependency.**
The system must still approve or reject bookings even if an external mapping provider is unavailable. TomTom can help the frontend render routes, but it must not be required for shortest-path calculation.

**Map Service only calls Capacity Service for the admin traffic view.**
Capacity data is needed to colour segments on the admin map, but it is not needed to compute the shortest route itself. This keeps the booking path dependent only on topology and route computation, not on occupancy aggregation.

---

## 4. API Contract

### 4.1 GET /api/v1/map/route

Compute the shortest route between two nodes and return the ordered segment list used by Journey Service during booking.

**Query Parameters:**

| Param | Required | Description |
|-------|----------|-------------|
| `origin_node_id` | Yes | Graph node ID for the start node |
| `destination_node_id` | Yes | Graph node ID for the end node |

**Example:**
```http
GET /api/v1/map/route?origin_node_id=city&destination_node_id=airport
```

**Response (200 OK):**
```json
{
  "origin": {
    "node_id": "city",
    "label": "City Centre",
    "lat": 53.3498,
    "lng": -6.2603
  },
  "destination": {
    "node_id": "airport",
    "label": "Airport",
    "lat": 53.4264,
    "lng": -6.2499
  },
  "total_traversal_time_minutes": 24,
  "segments": [
    {
      "sequence": 1,
      "segment_id": "seg_city_north",
      "segment_name": "City Centre to North Gate",
      "from_node_id": "city",
      "to_node_id": "north",
      "traversal_time_minutes": 8
    },
    {
      "sequence": 2,
      "segment_id": "seg_north_airport",
      "segment_name": "North Gate to Airport",
      "from_node_id": "north",
      "to_node_id": "airport",
      "traversal_time_minutes": 16
    }
  ]
}
```

**Error Responses:**
- `400` - missing query parameters or origin equals destination
- `404` - `origin_node_id` or `destination_node_id` not found
- `422` - no route exists between the two nodes
- `503` - graph not loaded

---

### 4.2 GET /api/v1/map/segments

Return all configured road segments and their static metadata.

**Query Parameters (all optional):**
- `from_node_id`
- `to_node_id`

**Response (200 OK):**
```json
{
  "segments": [
    {
      "segment_id": "seg_city_north",
      "segment_name": "City Centre to North Gate",
      "from_node_id": "city",
      "to_node_id": "north",
      "from_label": "City Centre",
      "to_label": "North Gate",
      "distance_km": 4.5,
      "traversal_time_minutes": 8,
      "is_active": true
    },
    {
      "segment_id": "seg_north_airport",
      "segment_name": "North Gate to Airport",
      "from_node_id": "north",
      "to_node_id": "airport",
      "from_label": "North Gate",
      "to_label": "Airport",
      "distance_km": 9.7,
      "traversal_time_minutes": 16,
      "is_active": true
    }
  ]
}
```

**Error Responses:**
- `400` - invalid query parameter value
- `503` - graph metadata unavailable

---

### 4.3 GET /api/v1/map/nodes

Return all graph nodes. Used by the booking page to populate origin and destination selectors.

**Response (200 OK):**
```json
{
  "nodes": [
    {
      "node_id": "city",
      "label": "City Centre",
      "lat": 53.3498,
      "lng": -6.2603,
      "x": 300,
      "y": 250
    },
    {
      "node_id": "north",
      "label": "North Gate",
      "lat": 53.4200,
      "lng": -6.2603,
      "x": 320,
      "y": 120
    },
    {
      "node_id": "airport",
      "label": "Airport",
      "lat": 53.4264,
      "lng": -6.2499,
      "x": 410,
      "y": 100
    }
  ]
}
```

**Error Responses:**
- `503` - graph metadata unavailable

---

### 4.4 GET /api/v1/map/traffic

Return the topology and current occupancy data needed by the admin traffic map.

**Auth:** Requires admin JWT.

**Query Parameters (all optional):**
- `from`
- `to`

**Response (200 OK):**
```json
{
  "window_start": "2026-04-15T08:00:00Z",
  "window_end": "2026-04-15T08:15:00Z",
  "segments": [
    {
      "segment_id": "seg_city_north",
      "name": "City Centre to North Gate",
      "region": "north",
      "level": "moderate",
      "occupancy_pct": 63.0,
      "vehicles": 63.0,
      "capacity": 100.0,
      "trend": "worsening",
      "from_node": "city",
      "to_node": "north"
    },
    {
      "segment_id": "seg_north_airport",
      "name": "North Gate to Airport",
      "region": "north",
      "level": "high",
      "occupancy_pct": 82.5,
      "vehicles": 66.0,
      "capacity": 80.0,
      "trend": "stable",
      "from_node": "north",
      "to_node": "airport"
    }
  ],
  "nodes": [
    { "node_id": "city", "label": "City Centre", "x": 300, "y": 250 },
    { "node_id": "north", "label": "North Gate", "x": 320, "y": 120 },
    { "node_id": "airport", "label": "Airport", "x": 410, "y": 100 }
  ]
}
```

**Error Responses:**
- `400` - malformed query parameters
- `401` - invalid JWT
- `403` - caller is not an admin
- `502` - Capacity Service occupancy request failed
- `503` - topology unavailable

---

### 4.5 GET /health

```json
{
  "status": "healthy",
  "db": "connected",
  "graph": "loaded",
  "uptime_seconds": 3600
}
```

Returns `200` when healthy, `503` if DB or graph state is unavailable.

---

### 4.6 GET /ready

Returns `200 OK` only when PostgreSQL is reachable and the in-memory graph has been loaded. Returns `503` during startup or on dependency failure.

---

## 5. What Map Service Provides to Other Services

### To Journey Service (S2, Ajinkya)

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/map/route` | Return the ordered segment list and traversal times needed to compute cascading windows |
| `GET /api/v1/map/nodes` | Optional node lookup for label/coordinate alignment |

**Contract guarantees Map Service must uphold:**
- Segment order must be deterministic for a given route
- Every segment in the route must include `segment_id` and `traversal_time_minutes`
- Map Service must return `422` when no route exists instead of an empty segment array

### To the Frontend / Admin UI

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/map/nodes` | Driver origin/destination selector data |
| `GET /api/v1/map/segments` | Static topology metadata |
| `GET /api/v1/map/traffic` | Admin traffic map response |

---

## 6. What Map Service Needs from Other Services

### 6.1 From IAM Service (S1) - no runtime calls

Map Service validates JWTs locally using the JWKS public keys fetched from IAM. It does not call IAM on every request.

Required JWT claims:

```json
{
  "sub": "usr_a1b2c3d4",
  "role": "admin",
  "email": "alice@example.com",
  "iss": "traffic-iam",
  "iat": 1745305201,
  "exp": 1745308801
}
```

### 6.2 From Capacity Service (S3) - for admin traffic view only

Map Service calls Capacity Service only when building `GET /api/v1/map/traffic`. This is not used during booking and is not part of the route computation path.

```http
GET /api/v1/capacity/segments/occupancy?from=2026-04-15T08:00:00Z&to=2026-04-15T08:15:00Z
```

**Response shape expected by Map Service:**
```json
{
  "window_start": "2026-04-15T08:00:00Z",
  "window_end": "2026-04-15T08:15:00Z",
  "segments": [
    {
      "segment_id": "seg_city_north",
      "segment_name": "City Centre to North Gate",
      "region": "north",
      "max_capacity": 100.0,
      "reserved_slots": 63.0,
      "available_slots": 37.0,
      "occupancy_pct": 63.0,
      "level": "moderate",
      "trend": "worsening"
    }
  ]
}
```

### 6.3 From PostgreSQL Replication

Map Service relies on PostgreSQL logical replication to keep node and segment metadata aligned across VMs. The service does not implement any custom cross-VM synchronization.

---

## 7. Internal Logic

### 7.1 Route Computation

```text
1. Validate origin and destination node IDs
2. Load the route graph from in-memory state
3. Run Dijkstra using traversal time as the edge weight
4. Reconstruct the shortest path as an ordered segment list
5. Sum the traversal times for the total route estimate
6. Return the ordered segments to Journey Service
```

### 7.2 Traffic Map Aggregation

```text
1. Read static node and segment topology from Map Service state
2. Call Capacity Service for segment occupancy in the requested window
3. Join occupancy data with segment topology by segment_id
4. Return the merged response for the admin map
```

### 7.3 Why the Graph Is In Memory

The graph is small, fixed for the prototype, and read much more often than it changes. Keeping it in memory makes shortest-path computation fast and keeps the booking path predictable.

---

## 8. Error Handling

| Scenario | Response | Handling |
|----------|----------|----------|
| Unknown origin or destination node | `404` | Return a node-not-found error immediately |
| No route between nodes | `422` | Return `ROUTE_NOT_FOUND` |
| Graph not loaded | `503` | Fail readiness and reject route requests |
| Capacity Service failure during `/map/traffic` | `502` | Fail the admin traffic request rather than returning guessed occupancy |
| Invalid admin JWT on `/map/traffic` | `401` / `403` | Reject the request in auth middleware |

**Error body example:**
```json
{
  "error": {
    "code": "ROUTE_NOT_FOUND",
    "message": "No connected path exists between node city and node port."
  }
}
```

---

## 9. Multi-VM Behavior

### 9.1 Booking Flow

```text
1. Driver request is routed to VM B
2. VM B's Journey Service calls VM B's Map Service
3. VM B's Map Service computes the route locally
4. VM B's Journey Service uses that route to compute segment windows and call Capacity Service
```

Map Service does not call another VM for routing. All booking-critical route computation stays local to the handling VM.

### 9.2 Topology Consistency

Node and segment metadata are stored in PostgreSQL and replicated to all VMs through logical replication. This keeps the route graph broadly aligned across the cluster without introducing cross-VM service calls.

---

## 10. Configuration (Environment Variables)

```bash
# Server
PORT=8084
ENV=production
LOG_LEVEL=info
VM_ID=vm-a

# PostgreSQL (local instance on this VM)
DB_HOST=localhost
DB_PORT=5432
DB_NAME=trafficservice
DB_SCHEMA=map
DB_USER=map_svc
DB_PASSWORD=<secret>
DB_MAX_CONNS=20
DB_IDLE_CONNS=5
DB_CONN_TIMEOUT_SECONDS=5

# IAM JWKS
JWKS_URL=http://iam-service:8082/.well-known/jwks.json
JWKS_REFRESH_INTERVAL_SECONDS=3600

# Capacity dependency (admin traffic view only)
CAPACITY_BASE_URL=http://capacity-service:8081
CAPACITY_TIMEOUT_MS=2000

# CORS
CORS_ALLOWED_ORIGINS=http://localhost:5173,https://<production-domain>
```

---

## 11. Project Structure

```text
map-service/
|-- cmd/
|   `-- server/
|       `-- main.go
|
|-- internal/
|   |-- handler/
|   |   |-- route_handler.go
|   |   |-- node_handler.go
|   |   |-- segment_handler.go
|   |   |-- traffic_handler.go
|   |   `-- health_handler.go
|   |
|   |-- middleware/
|   |   |-- auth.go
|   |   `-- logging.go
|   |
|   |-- model/
|   |   |-- node.go
|   |   |-- segment.go
|   |   `-- route.go
|   |
|   |-- service/
|   |   |-- route_service.go
|   |   |-- topology_service.go
|   |   `-- traffic_service.go
|   |
|   `-- repository/
|       |-- node_repo.go
|       `-- segment_repo.go
|
|-- migrations/
|   `-- 001_seed_graph.sql
|
|-- pkg/
|   |-- config/config.go
|   |-- errors/errors.go
|   |-- logger/logger.go
|   |-- postgres/connection.go
|   `-- response/response.go
|
|-- docs/
|   |-- docs.go
|   |-- swagger.json
|   `-- swagger.yaml
|
|-- Dockerfile
|-- Makefile
|-- config.yaml
`-- go.mod
```

---

## 12. Sequence Diagrams

### 12.1 Route Lookup During Booking

```text
Driver         Journey Svc        Map Svc
  |                |                |
  | POST /journeys |                |
  |--------------->|                |
  |                | GET /map/route |
  |                |--------------->|
  |                |                | run Dijkstra
  |                |<---------------|
  |                | ordered route   |
  |                | continue booking|
```

### 12.2 Admin Traffic Map Request

```text
Browser         Map Svc           Capacity Svc
  |                |                   |
  | GET /map/traffic                   |
  |--------------->|                   |
  |                | GET /segments/occupancy
  |                |------------------>|
  |                |<------------------|
  |                | merge topology    |
  |<---------------| + occupancy       |
```

---

## 13. Frontend Integration

Map Service is called directly by the frontend for booking-node lookup and the admin traffic map.

### 13.1 Routes That Call Map Service

```text
/driver/book      -> GET /api/v1/map/nodes
/admin/map        -> GET /api/v1/map/traffic
```

### 13.2 Page -> API Mapping

| Page | Backend Call | Endpoint | Notes |
|------|-------------|----------|-------|
| `/driver/book` | Load selectable locations | `GET /api/v1/map/nodes` | Replaces hardcoded node lists in the booking page |
| `/admin/map` | Load traffic map data | `GET /api/v1/map/traffic` | Returns topology plus occupancy |

### 13.3 Node Alignment

The seeded graph should include the same node labels already used in the frontend mock data:

- `City Centre`
- `North Gate`
- `Airport`
- `East Quay`
- `South Terminal`
- `Industrial Park`
- `West Depot`
- `Port Terminal`
- `Northfield`
- `Riverside`

### 13.4 Expected Response Shapes

**Driver booking node lookup:**
```json
{
  "nodes": [
    { "node_id": "city", "label": "City Centre", "lat": 53.3498, "lng": -6.2603 }
  ]
}
```

**Admin traffic map:**
```json
{
  "segments": [
    {
      "segment_id": "seg_city_north",
      "name": "City Centre to North Gate",
      "region": "north",
      "level": "moderate",
      "occupancy_pct": 63.0,
      "vehicles": 63.0,
      "capacity": 100.0,
      "trend": "worsening",
      "from_node": "city",
      "to_node": "north"
    }
  ],
  "nodes": [
    { "node_id": "city", "label": "City Centre", "x": 300, "y": 250 }
  ]
}
```

### 13.5 CORS

Because the browser calls Map Service directly, it must allow the frontend origin in development:

```text
Access-Control-Allow-Origin: http://localhost:5173
Access-Control-Allow-Methods: GET, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, X-Trace-ID
Access-Control-Max-Age: 86400
```

---

## 14. Assumptions

- The prototype graph contains roughly 20-30 predefined segments
- Journey Service sends known node IDs rather than arbitrary coordinates
- Capacity Service and Map Service use the same canonical `segment_id` values
- Traffic aggregation is only needed for the admin traffic view, not for booking

---

## 15. Future Improvements

- Add richer geometry output for frontend route visualization
- Support controlled admin editing of nodes and segments
- Add alternative route strategies if the product later needs them

---

## 16. Interface Contract Summary

**Provides to Journey Service:**
- `GET /api/v1/map/route`

**Provides to frontend/admin:**
- `GET /api/v1/map/nodes`
- `GET /api/v1/map/segments`
- `GET /api/v1/map/traffic`

**Depends on Capacity Service only for admin traffic aggregation:**
- `GET /api/v1/capacity/segments/occupancy`

**Depends on IAM only for local JWT validation setup:**
- JWKS fetch on startup / refresh, not per request

---

*Last updated: 2026-04-04*
*Service version: 0.1.0 (planning)*

