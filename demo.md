# Clearway — Professor Demo Guide

> **Course:** CS7NS6 Distributed Systems — TCD  
> **EU:** https://35.244.162.92.nip.io | **US:** https://35.227.198.68.nip.io | **APAC:** https://34.8.134.246.nip.io  
> **Admin login:** `admin@vcs.local` / `admin123`  
> **Driver logins:** `driver1@demo.ie`, `driver2@demo.ie`, `driver3@demo.ie` / `Demo1234!`

---

## Quick Links

| Tool | URL | Credentials |
|---|---|---|
| Frontend (EU) | https://35.244.162.92.nip.io | driver1@demo.ie / Demo1234! |
| Frontend (US) | https://35.227.198.68.nip.io | same drivers work (shared DB) |
| Frontend (APAC) | https://34.8.134.246.nip.io | same drivers work (shared DB) |
| Grafana (EU) | http://34.76.63.61:3000 | admin / admin |
| Grafana (US) | http://34.138.242.217:3000 | admin / admin |
| Grafana (APAC) | http://34.80.180.64:3000 | admin / admin |
| CockroachDB UI | http://35.187.121.12:8080 | no login required |
| CRDB UI (eu2) | http://34.76.63.61:8080 | no login required |
| GitHub Actions | https://github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/actions | — |

---

## Quick Debug Commands

```bash
# SSH keys
EU_MGR="deploy@35.187.121.12"   # vcs-vm-eu1 (EU Swarm manager)
EU_WKR="deploy@34.76.63.61"     # vcs-vm-eu2 (EU Swarm worker)
US_MGR="deploy@34.138.242.217"  # vcs-vm-us1
AP_MGR="deploy@34.80.180.64"    # vcs-vm-ap1
KEY="~/.ssh/vcs_key"

# Check all services are running (run on any manager)
ssh -i $KEY $EU_MGR "docker service ls"

# Tail any service log
ssh -i $KEY $EU_MGR "docker service logs vcs_journey-service --tail 30 --follow"
ssh -i $KEY $EU_MGR "docker service logs vcs_capacity-service --tail 30 --follow"
ssh -i $KEY $EU_MGR "docker service logs vcs_map-service --tail 30"

# CRDB node status — shows all 3 nodes in cluster
ssh -i $KEY $EU_MGR "docker exec -i \$(docker ps -qf name=vcs_db) \
  /cockroach/cockroach node status --insecure --host=localhost:26257"

# Run SQL directly against CRDB
ssh -i $KEY $EU_MGR "docker exec -i \$(docker ps -qf name=vcs_db) \
  /cockroach/cockroach sql --insecure --host=localhost:26257 \
  -e 'SELECT * FROM capacity.reservation_sagas ORDER BY created_at DESC LIMIT 5;'"

# Redis stream length (events in queue)
ssh -i $KEY $EU_MGR "docker exec -i \$(docker ps -qf name=vcs_redis) redis-cli XLEN journey.events"

# Check which containers are on which node
ssh -i $KEY $EU_MGR "docker service ps vcs_capacity-service"

# Restart a crashed service
ssh -i $KEY $EU_MGR "docker service update --force vcs_capacity-service"

# Check 3-region health in one shot
for url in https://35.244.162.92.nip.io https://35.227.198.68.nip.io https://34.8.134.246.nip.io; do
  echo "$url: $(curl -s --max-time 5 $url/api/v1/region)"
done
```

---

## How to explain this system in one sentence

> "Clearway is a booking system for road trips — like reserving seats on a plane, but for road capacity. Roads have a maximum number of vehicles. A driver books a journey from A to B; the system checks every road on the route has space, locks those slots atomically, and notifies the driver. The hard part: the database is split across 3 continents and every region must accept writes independently."

---

## PART 1 — Three Live Regions (2 min)

### What you're showing
Three completely independent application stacks running on GCP virtual machines in different continents. Each is a full Docker Swarm cluster with all services. They don't talk to each other over the network — they share only the CockroachDB database over Raft.

### Design decision
> "We chose Docker Swarm over Kubernetes because Kubernetes would cost 3× as much in managed cluster fees on top of the VMs. Swarm gives us rolling updates, secrets, overlay networking, service discovery — everything we need — in a single `docker stack deploy` command."

```bash
# Three different regions, one command each
curl -s https://35.244.162.92.nip.io/api/v1/region | jq .    # → {"region":"EU"}
curl -s https://35.227.198.68.nip.io/api/v1/region | jq .    # → {"region":"US"}
curl -s https://34.8.134.246.nip.io/api/v1/region | jq .     # → {"region":"APAC"}
```

### Show all services are running
```bash
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 "docker service ls"
```

Point out: 10 services. `2/2` on EU (2 VMs in the swarm), `1/1` on US/APAC.

---

## PART 2 — CockroachDB: One Database, Three Continents (3 min)

### What you're showing
CockroachDB connects the 3 cells. It looks like one database but the data physically lives on all 3 nodes simultaneously. Every write must be acknowledged by 2 out of 3 nodes (Raft quorum) before it commits. There is no "primary" node — all three are equal.

### Design decision
> "We started with PostgreSQL with EU as the primary. US and APAC were read-only replicas. Any write from a US user had to go to EU and back — adding 150ms of extra latency on every booking. CockroachDB fixes this: US users write to the US node, and Raft replicates it. The wire protocol is 100% PostgreSQL-compatible so our Go code didn't change at all."

```bash
# Show 3 equal nodes — no 'primary' label
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 \
  "docker exec -i \$(docker ps -qf name=vcs_db) \
  /cockroach/cockroach node status --insecure --host=localhost:26257"
```

### Open CockroachDB Admin UI
Open `http://35.187.121.12:8080` in browser.

Show:
- **Cluster Overview** → 3 nodes, all green
- **Replication** → leaseholder map showing data distributed across nodes
- **Metrics** → Raft proposals/sec (will spike when bookings happen)

### Multi-master write demo
```bash
# Write to US — register a user
curl -s -X POST https://35.227.198.68.nip.io/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"us_user@demo.ie","password":"Demo1234!","name":"US User",
       "vehicle_type":"car","license_info":{"license_number":"US-001","country":"US"}}' \
  | jq '{success: .success, where_written: "US cell"}'

# Immediately read from EU — data is already there via Raft
curl -s -X POST https://35.244.162.92.nip.io/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"us_user@demo.ie","password":"Demo1234!"}' \
  | jq '{success: .success, where_read: "EU cell"}'
```

> "That user was created in the US database node and is immediately readable from EU. Raft committed the write across 2 of 3 nodes before the US acknowledged. There's no sync delay, no eventual consistency window — it's strong consistency across continents."

---

## PART 3 — Frontend Flow: What Happens When a Driver Books (5 min)

### What you're showing
The full user journey from browser to database. Each step calls a different microservice. This demonstrates service decomposition and the booking pipeline.

Open browser → `https://35.244.162.92.nip.io`

### Step 1: Login
Use **Demo Driver** button, or `driver1@demo.ie` / `Demo1234!`

**Design decision:**
> "JWT RS256 authentication. The IAM service holds the RSA private key. Every other service downloads the RSA public key from IAM's `/.well-known/jwks.json` endpoint at startup and caches it. Tokens are validated locally in-process — zero IAM calls per request. If IAM goes down, existing sessions keep working."

### Step 2: Search for a place
Type **"Dublin Airport"** in the origin field.

**What's happening:**
> "That search calls our map-service at `/api/v1/map/search`, which proxies to Nominatim — OpenStreetMap's free geocoder. We cache results in the database so repeat searches don't hit Nominatim again."

### Step 3: Book (Dublin Airport → UCD, 2h from now)
Click Book Journey.

**What happens in the backend (narrate while loading):**

```
1. Journey Service receives the booking request
       ↓
2. Calls Map Service → OSRM (real road router) computes the actual driving route
   OSRM returns: M1 (4min), M50 (6min), R131 (5min)...
       ↓
3. Journey Service computes per-segment time windows:
   M1: depart+4min → depart+8min
   M50: depart+8min → depart+14min ...
       ↓
4. Calls Capacity Service → checks + reserves every segment atomically
   All segments have space → APPROVED
       ↓
5. Writes booking to DB + event to outbox (same transaction)
       ↓
6. Outbox relay publishes to Redis Stream → Notification Service sends push
```

### Step 4: Show the journey detail
Navigate `/journeys` → click the journey.

Point out:
- Route on OpenStreetMap (MapLibre tiles — we replaced TomTom which cost money)
- Segment list with actual time windows: "on M50 from 11:02 to 11:14"
- Status: APPROVED, Reservation ID: rsv_xxxxxxxx

---

## PART 4 — The Segment ID System (explain this before Part 5)

### What you're showing
Every road segment gets a unique ID. The ID encodes the geographic region so the capacity service knows which database region owns that segment.

### Why this matters
> "When a driver books Dublin Airport → UCD, all segments are `eu_m1`, `eu_m50`, `eu_r131` — all European. Simple single-region transaction. But when a driver books Istanbul → Ankara, the route crosses the Bosphorus strait. The exact same motorway O-4 gets two different IDs: `eu_o_4` on the European side (west of 29.1°E longitude) and `ap_o_4` on the Asian side. Two different regions → two separate database transactions → Saga coordinator needed."

```bash
EU="https://35.244.162.92.nip.io"
TOKEN=$(curl -s -X POST "$EU/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"driver1@demo.ie","password":"Demo1234!"}' | jq -r '.data.access_token')

# Dublin → UCD: all eu_ segments (single region)
curl -s -X POST "$EU/api/v1/routes/compute" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"origin":{"lat":53.4282916,"lng":-6.2472741},"destination":{"lat":53.3068763,"lng":-6.2246251}}' \
  | jq '[.data.segments[] | .segment_id] | unique'
# All eu_* → single CRDB transaction, no saga needed

# Istanbul → Ankara: eu_ AND ap_ segments (cross-region)
curl -s -X POST "$EU/api/v1/routes/compute" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"origin":{"lat":41.0082,"lng":28.9784},"destination":{"lat":39.9334,"lng":32.8597}}' \
  | jq '[.data.segments[] | .segment_id | split("_")[0]] | unique'
# ["ap", "eu"] → two regions → Saga fires
```

**Design decision for the region split:**
> "We use longitude to determine region. West of -25°E is Americas (us), between -25° and 29.1° is Europe (eu), east of 29.1° is Asia-Pacific (ap). 29.1°E is the midpoint of the Bosphorus Strait — the natural geographic boundary between European and Asian Turkey. This is a simple, fast, stateless calculation — no database lookup needed per road segment."

---

## PART 5 — Capacity Enforcement: Pessimistic Locking (5 min)

### What you're showing
The core safety guarantee: if a road segment can only hold 2 vehicles at a given time, exactly 2 bookings succeed — guaranteed, not approximately.

### Design decision
> "We use `SELECT FOR UPDATE` inside a `SERIALIZABLE` database transaction. When a booking arrives, we lock each segment row, read the current load, check capacity, and insert. The lock is held until commit. A second concurrent booking tries to lock the same row — it blocks until the first commits. Then it reads the updated load (which now shows 1 slot taken) and makes its decision cleanly. No retry loops, no race conditions. The first request wins, the second fails deterministically in one database round-trip."
>
> "Why not optimistic locking? With optimistic locking, two requests both read '1 slot available', both try to write, one fails on a version mismatch, retries, possibly races again. Under load this creates retry storms. Pessimistic locking makes it deterministic."

```bash
EU="https://35.244.162.92.nip.io"

# Step 1: Admin sets M50 capacity to 2 (simulate a narrow road)
ADMIN_TOKEN=$(curl -s -X POST "$EU/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@vcs.local","password":"admin123"}' | jq -r '.data.access_token')

curl -s -X PUT "$EU/api/v1/capacity/segments/eu_m50/capacity" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"max_capacity": 2}' | jq '{segment_id, max_capacity}'
# → {"segment_id": "eu_m50", "max_capacity": 2}

# Step 2: Three drivers, same departure time, same route
T1=$(curl -s -X POST "$EU/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"email":"driver1@demo.ie","password":"Demo1234!"}' | jq -r '.data.access_token')
T2=$(curl -s -X POST "$EU/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"email":"driver2@demo.ie","password":"Demo1234!"}' | jq -r '.data.access_token')
T3=$(curl -s -X POST "$EU/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"email":"driver3@demo.ie","password":"Demo1234!"}' | jq -r '.data.access_token')

DEPARTURE=$(date -u -d '+2 hours' '+%Y-%m-%dT%H:%M:%SZ')
PAYLOAD="{\"origin\":{\"lat\":53.4282916,\"lng\":-6.2472741},\"destination\":{\"lat\":53.3068763,\"lng\":-6.2246251},\"vehicle_type\":\"car\",\"departure_time\":\"$DEPARTURE\"}"

# Fire all 3 simultaneously
curl -s -X POST "$EU/api/v1/journeys" -H "Authorization: Bearer $T1" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: cap-d1-$(date +%s)" -d "$PAYLOAD" | jq '{driver:"1", status:.data.status, reason:.data.rejection_reason}' &
curl -s -X POST "$EU/api/v1/journeys" -H "Authorization: Bearer $T2" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: cap-d2-$(date +%s)" -d "$PAYLOAD" | jq '{driver:"2", status:.data.status, reason:.data.rejection_reason}' &
curl -s -X POST "$EU/api/v1/journeys" -H "Authorization: Bearer $T3" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: cap-d3-$(date +%s)" -d "$PAYLOAD" | jq '{driver:"3", status:.data.status, reason:.data.rejection_reason}' &
wait
```

**Expected output:**
```json
{"driver":"1", "status":"APPROVED",  "reason": null}
{"driver":"2", "status":"APPROVED",  "reason": null}
{"driver":"3", "status":"REJECTED",  "reason": "Segment eu_m50 is at capacity"}
```

> "Two succeed, one fails. Which two? Whichever acquired the database row lock first. Totally deterministic. The third driver's booking reads the already-updated slot count and rejects in the same transaction — no second attempt needed."

### Verify occupancy via check API

```bash
NOW=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
LATER=$(date -u -d '+3 hours' '+%Y-%m-%dT%H:%M:%SZ')

curl -s "$EU/api/v1/capacity/check?segment_id=eu_m50&time_window_start=$NOW&time_window_end=$LATER" | jq .
# → {"max_capacity":2,"reserved_slots":2,"available_slots":0,"can_reserve":false}
```

### Reset M50 to 100 after demo

```bash
curl -s -X PUT "$EU/api/v1/capacity/segments/eu_m50/capacity" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"max_capacity": 100}' | jq '{segment_id, max_capacity}'
```

---

## PART 6 — Road Closure: Admin Power (3 min)

### What you're showing
An admin can close a road segment (maintenance, accident, works) for a specified duration. Any booking whose route traverses that segment during the closure window is automatically rejected with a clear reason — without touching any existing approved bookings.

### Design decision
> "Closures are stored in a separate `segment_closures` table with a start and end timestamp. When a reservation is being processed, the capacity service checks whether any active closure overlaps the requested time window for each segment — inside the same serializable transaction that checks capacity. This means closure enforcement is atomic with capacity enforcement: you can't have a partial state where a booking is approved for some segments but then a closure check runs separately."

```bash
# Step 1: List active closures (should be empty)
curl -s "$EU/api/v1/capacity/closures" \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq .
# → []

# Step 2: Admin closes M50 for 12 hours (use 720 minutes)
curl -s -X POST "$EU/api/v1/capacity/closures" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "segment_id": "eu_m50",
    "duration_minutes": 720,
    "reason": "Emergency bridge inspection - M50 Palmerstown"
  }' | jq '{closure_id, segment_id, reason, status}'

# Step 3: Try to book through the closed segment
# NOTE: Departure must be >= 60 min from now (system requirement)
# Use +70 min so M50 traversal falls inside the closure window
DEPARTURE=$(date -u -d '+70 minutes' '+%Y-%m-%dT%H:%M:%SZ')

curl -s -X POST "$EU/api/v1/journeys" \
  -H "Authorization: Bearer $T3" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: closure-test-$(date +%s)" \
  -d "{\"origin\":{\"lat\":53.4282916,\"lng\":-6.2472741},\"destination\":{\"lat\":53.3068763,\"lng\":-6.2246251},\"vehicle_type\":\"car\",\"departure_time\":\"$DEPARTURE\"}" \
  | jq '{status: .data.status, rejection_reason: .data.rejection_reason}'
# → {"status":"REJECTED","rejection_reason":"Segment eu_m50 is closed for Emergency bridge inspection until ..."}

# Step 4: Verify the check API shows closed state
NOW=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
LATER=$(date -u -d '+4 hours' '+%Y-%m-%dT%H:%M:%SZ')
curl -s "$EU/api/v1/capacity/check?segment_id=eu_m50&time_window_start=$NOW&time_window_end=$LATER" \
  | jq '{is_closed, closure_reason, closure_end}'
# → {"is_closed":true,"closure_reason":"Emergency bridge inspection - M50 Palmerstown","closure_end":"..."}
```

### Show in Admin UI
Navigate to `/admin/closures` — the closure appears with reason and time window.

### Lift the closure (restore demo state)

```bash
# No DELETE API by design (audit trail) — expire it via database
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 \
  "docker exec -i \$(docker ps -qf name=vcs_db) \
  /cockroach/cockroach sql --insecure --host=localhost:26257 \
  -e \"UPDATE capacity.segment_closures SET status='cancelled', end_time=NOW()-INTERVAL '1s' WHERE segment_id='eu_m50' AND status='active';\""
```

---

## PART 7 — Idempotency: Safe Retries (1 min)

### What you're showing
A driver's phone retries on network timeout. Without idempotency, the driver gets double-booked. The system stores the result of every booking keyed by the `Idempotency-Key` header — inside the same transaction as the booking itself.

### Design decision
> "The idempotency record is written in the same database transaction as the booking. If you commit the booking, you commit the idempotency key. If the transaction rolls back, both are gone. This means a retry with the same key always gets either the original result or a fresh attempt — never a duplicate booking or a corrupted state."

```bash
# First call — creates the booking
curl -s -X POST "$EU/api/v1/journeys" \
  -H "Authorization: Bearer $T1" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: stable-key-for-retry-demo" \
  -d "$PAYLOAD" | jq '{attempt: "first", status: .data.status, journey_id: .data.journey_id}'

# Retry with same key — returns exact cached response, no new booking created
curl -s -X POST "$EU/api/v1/journeys" \
  -H "Authorization: Bearer $T1" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: stable-key-for-retry-demo" \
  -d "$PAYLOAD" | jq '{attempt: "retry", status: .data.status, journey_id: .data.journey_id}'
# → Identical journey_id both times. One booking. Safe retry.
```

---

## PART 8 — Event-Driven Notifications: Transactional Outbox (2 min)

### What you're showing
Journey bookings trigger push notifications without the journey service directly calling the notification service. They're completely decoupled via a Redis Stream.

### Design decision
> "The naive approach would be: save the booking, then publish to Redis. But if the process crashes between the DB commit and the Redis publish, the booking is saved but the notification is lost forever. The outbox pattern fixes this: we write the event to a DB table (`journey.outbox`) in the SAME transaction as the booking. If the transaction commits, both the booking and the event are guaranteed to be there. A background goroutine polls the outbox table every second, publishes to Redis Stream, then marks it as published. The notification service then consumes from the stream."

```bash
# Watch the outbox events being published
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 \
  "docker service logs vcs_journey-service --tail 20 2>&1 | grep -i outbox"

# See events accumulating in Redis stream
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 \
  "docker exec -i \$(docker ps -qf name=vcs_redis) redis-cli XLEN journey.events"
# → grows by 1 with each booking

# Show the last 3 events in the stream
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 \
  "docker exec -i \$(docker ps -qf name=vcs_redis) redis-cli XREVRANGE journey.events + - COUNT 3"

# Show notification service consuming
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 \
  "docker service logs vcs_notification-service --tail 20 2>&1 | grep -i approved"
```

> "At-least-once delivery: the outbox relay may publish the same event twice if it crashes after Redis XADD but before marking it published. The notification service deduplicates by event ID. This is a standard pattern — Kafka uses the same guarantee."

---

## PART 9 — Saga Pattern: Cross-Regional Booking (5 min)

### What you're showing
When a booking spans two geographic regions, a single database transaction is not enough — you need a Saga. This is the most advanced distributed systems concept in the project.

### What a Saga is (explain simply)
> "Imagine you're booking a flight that has two legs: London → Dubai, then Dubai → Tokyo. Two airlines. You book both. If the second leg (Dubai → Tokyo) is sold out, you need to cancel the first leg (London → Dubai) too. That's a Saga: a sequence of steps where each step has a compensating action that undoes it if a later step fails.
>
> In our system: Istanbul → Ankara crosses the Bosphorus. The first half of the route is in Europe (EU database region), the second half is in Asia (APAC database region). We need to reserve capacity in BOTH regions. If the APAC reservation fails — maybe APAC is overloaded — we must release the EU reservation we already made. That's the compensating transaction."

### Design decision
> "Why not just use one big distributed transaction? CockroachDB supports them, but they have high latency when they span continents — every step must coordinate across all involved nodes. A Saga breaks it into independent local transactions per region. Each step writes its status to the `reservation_sagas` table. If the process crashes mid-saga, the coordinator reads the saga record on restart and either completes forward or compensates backward — it never gets stuck in an uncertain state."

### Demo

```bash
EU="https://35.244.162.92.nip.io"
TOKEN=$(curl -s -X POST "$EU/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"driver1@demo.ie","password":"Demo1234!"}' | jq -r '.data.access_token')

# Step 1: Show the route has segments in BOTH regions
curl -s -X POST "$EU/api/v1/routes/compute" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"origin":{"lat":41.0082,"lng":28.9784},"destination":{"lat":39.9334,"lng":32.8597}}' \
  | jq '[.data.segments[] | .segment_id] | group_by(split("_")[0]) | map({region: .[0] | split("_")[0], count: length})'
# → [{"region":"ap","count":12},{"region":"eu","count":17}]
# Both regions present → Saga will fire

# Step 2: Book it (cancel existing approved journey first if needed)
J=$(curl -s "$EU/api/v1/journeys" -H "Authorization: Bearer $TOKEN" | jq -r '[.data.journeys[] | select(.status=="APPROVED")] | .[0].journey_id // empty')
[ -n "$J" ] && curl -s -X PUT "$EU/api/v1/journeys/$J/cancel" -H "Authorization: Bearer $TOKEN" > /dev/null

DEPARTURE=$(date -u -d '+2 hours' '+%Y-%m-%dT%H:%M:%SZ')
curl -s -X POST "$EU/api/v1/journeys" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: saga-bosphorus-$(date +%s)" \
  -d "{\"origin\":{\"lat\":41.0082,\"lng\":28.9784},\"destination\":{\"lat\":39.9334,\"lng\":32.8597},\"vehicle_type\":\"car\",\"departure_time\":\"$DEPARTURE\"}" \
  | jq '{status: .data.status, journey_id: .data.journey_id}'
# → {"status":"APPROVED","journey_id":"jrn_..."}

# Step 3: THE PROOF — query the saga table
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12 \
  "docker exec -i \$(docker ps -qf name=vcs_db) \
  /cockroach/cockroach sql --insecure --host=localhost:26257 \
  -e 'SELECT saga_id, journey_id, status, region_steps FROM capacity.reservation_sagas ORDER BY created_at DESC LIMIT 3;'"
```

**What the saga table shows:**
```
saga_id        journey_id     status     region_steps
saga_cb1920ab  jrn_12201282   COMMITTED  [
  {"region": "eu",   "reservation_id": "rsv_xxx", "status": "RESERVED"},
  {"region": "apac", "reservation_id": "rsv_yyy", "status": "RESERVED"}
]
```

> "Status is COMMITTED. Two steps: EU reserved, APAC reserved. Both RESERVED → saga COMMITTED. The booking crossed two continents atomically. If APAC had failed, the saga would show COMPENSATED — and the EU reservation would have been released. The driver gets REJECTED with a clear reason, no dangling reservations left behind."

**The Bosphorus crossing explained:**
```
eu_o_4 (lng=29.02°E, west of boundary) → eu prefix
ap_o_4 (lng=29.10°E, east of boundary) → ap prefix
        ↑
     29.1°E = Bosphorus boundary
```

---

## PART 10 — Booking from Different Regions (2 min)

### What you're showing
Any region can accept bookings independently. The data lives in CockroachDB which all three cells share. A booking made in the US is immediately visible in EU.

### What actually works

```bash
# EU booking
curl -s -X POST https://35.244.162.92.nip.io/api/v1/journeys \
  -H "Authorization: Bearer $T_EU" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: eu-demo" -d "$PAYLOAD" \
  | jq '{region: "EU", status: .data.status}'

# US booking — different driver, same route, works independently
T_US=$(curl -s -X POST https://35.227.198.68.nip.io/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"driver2@demo.ie","password":"Demo1234!"}' | jq -r '.data.access_token')
curl -s -X POST https://35.227.198.68.nip.io/api/v1/journeys \
  -H "Authorization: Bearer $T_US" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: us-demo" -d "$PAYLOAD" \
  | jq '{region: "US", status: .data.status}'
```

### Known limitation: APAC write latency

> "APAC bookings for Dublin-route segments time out because all `eu_*` segment rows currently have their CockroachDB leaseholder on the EU node. A write from APAC must cross the Pacific to get Raft quorum on EU nodes — 200ms round-trip × several retries × serializable isolation overhead → timeout.
>
> The fix is geo-partitioning: pin `eu_*` segment leaseholders to EU nodes and `ap_*` leaseholders to APAC nodes using `CONFIGURE ZONE` SQL commands. The DDL for this is written in `docs/data-partitioning-and-sharding.md` but not yet applied to the live cluster. This is a known limitation documented in BIBLE.md Section 15."

---

## PART 11 — Fault Tolerance (3 min)

### Circuit breaker

### What you're showing
Killing one service doesn't crash the whole system. Journey service wraps calls to Capacity and Map services in a circuit breaker — after 5 consecutive failures it opens and fast-fails, preventing goroutine pile-up.

### Design decision
> "The circuit breaker pattern prevents cascading failures. If capacity-service is slow (not down, just slow), each request waits the full timeout. Without a breaker, 100 concurrent requests × 30s timeout = 3000 goroutines blocked. The breaker detects the pattern early and stops sending requests, freeing resources. It auto-closes 10 seconds after the service recovers."

```bash
ssh -i ~/.ssh/vcs_key deploy@35.187.121.12

# Kill capacity-service
docker service update --replicas 0 vcs_capacity-service

# Bookings fail fast with a clear error — no hanging
curl -s -X POST https://35.244.162.92.nip.io/api/v1/journeys \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: cb-test" \
  -d "$PAYLOAD" | jq .error.message
# → "capacity service unavailable"

# Restore
docker service update --replicas 1 vcs_capacity-service
# ~10 seconds later: circuit breaker closes, bookings work again
```

### Node drain (EU 2-node Swarm)

```bash
# Drain vcs-vm-eu2 — its containers evacuate to vcs-vm-eu1
docker node update --availability drain vcs-vm-eu2

docker service ls  # All still 1/1 — running on eu1

# Endpoint still works
curl -s https://35.244.162.92.nip.io/api/v1/region

# Restore
docker node update --availability active vcs-vm-eu2
```

---

## PART 12 — Observability (2 min)

Open Grafana: `http://34.76.63.61:3000` (admin/admin)

| Dashboard | What to show |
|---|---|
| **VCS Overview** | Service health panels, HTTP request rates, booking success/rejection counts |
| **CockroachDB** | Raft proposals/sec (spikes during bookings), SQL ops/sec, node status |
| **Redis** | `journey.events` stream consumer lag (should be 0 — notification-service keeping up) |
| **Log Explorer** | Filter `{service="journey-service"}` → show the booking pipeline trace logs |
| **Container Resources** | CPU/memory per container across all nodes |

Open CRDB UI: `http://35.187.121.12:8080`  
Show: Cluster Health → all 3 nodes → Replication → leaseholder map

---

## PART 13 — CI/CD Pipeline (1 min)

Open GitHub Actions: `https://github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/actions`

> "Manual trigger only — no accidental deploys on push. The pipeline: build all 5 services in parallel as Docker images → push to GHCR → run DB migrations → rolling deploy to EU, US, APAC simultaneously. Rolling update: the new container must pass a health check BEFORE the old one stops. If health check fails → automatic rollback to previous image. This is Docker Swarm's built-in update_config."

---

## Summary Table: Distributed Concepts Demonstrated

| Concept | Where | Evidence |
|---|---|---|
| **Multi-master writes** | CockroachDB | 3 CRDB nodes all accept writes, no primary label |
| **Raft consensus** | CockroachDB | Write to US, read from EU instantly — quorum guarantees it |
| **Serializable isolation** | Capacity service | 3 concurrent bookings → exactly 2 APPROVED, deterministic |
| **Pessimistic locking** | Capacity service | `SELECT FOR UPDATE` on segment rows — first wins, second fails cleanly |
| **Saga pattern** | Capacity service | Istanbul→Ankara booking → `reservation_sagas` table shows 2 regions COMMITTED |
| **Compensating transaction** | Capacity service | Saga status would show COMPENSATED if APAC failed |
| **Transactional outbox** | Journey service | Booking commit and event write in one transaction |
| **At-least-once delivery** | Redis Streams | Outbox relay can republish; notification service deduplicates |
| **Idempotency** | Journey + Capacity | Same Idempotency-Key twice → same response, one booking |
| **Circuit breaker** | Journey service | Kill capacity-service → graceful "unavailable", auto-recover |
| **Geographic distribution** | 3 GCP cells | `curl /api/v1/region` × 3 shows EU/US/APAC |
| **Road closures** | Capacity service | Admin closes M50 → driver's booking REJECTED with reason |
| **Event-driven** | Redis Streams | Notifications decoupled from bookings |
| **Fault tolerance** | Docker Swarm | Drain EU node → system keeps serving |
| **Observability** | Prometheus + Grafana + Loki | Live dashboards, log search |

---

## One-Liner Answers to Likely Questions

**Why CockroachDB and not Postgres?**  
PostgreSQL had EU as the single primary — US and APAC were read-only replicas. Any US write required an EU round-trip (~150ms extra). CockroachDB makes all 3 nodes equal write nodes with no code change (same wire protocol as Postgres).

**Why Saga and not one distributed transaction?**  
A 2PC distributed transaction spanning EU + APAC blocks if either node is slow or unreachable. A Saga breaks it into independent local transactions per region, with the coordinator persisting each step so it can resume or compensate after a crash.

**Why pessimistic locking for capacity but optimistic for journey status?**  
Capacity has two drivers fighting for the last slot — optimistic locking causes retry storms under contention. Journey status transitions are driver-owned (only you can activate your own journey), conflicts are rare, so the cheaper optimistic version-check is sufficient.

**Why transactional outbox and not just publish to Redis after DB commit?**  
If the process crashes between DB commit and Redis publish, the booking is saved but the notification is lost forever. The outbox writes the event in the same transaction as the booking — both commit together or both roll back. The relay handles eventual delivery.

**Why Redis Streams and not Kafka?**  
Kafka adds ZooKeeper, broker management, and significant operational overhead. Redis is already in the stack for route caching. Redis Streams provide consumer groups, at-least-once delivery, and pending message reclaim — sufficient for our throughput. The architecture is Kafka-compatible: only the StreamWriter implementation would change.

**Why Docker Swarm and not Kubernetes?**  
GKE managed cluster costs ~3× extra on top of the VM bill. For a 1-month project Swarm gives everything needed — service discovery, secrets, rolling updates, overlay networking — in a single deploy command.

**Why nip.io?**  
Firebase Cloud Messaging requires HTTPS for service workers. No real domain available. nip.io maps `<IP>.nip.io → <IP>`, giving a valid FQDN for GCP managed TLS certificates.

**Why does APAC booking fail for Dublin routes?**  
All `eu_*` segment rows have their CockroachDB leaseholder on EU nodes. A write from APAC must get Raft quorum from EU nodes — 200ms+ round-trip per retry. The fix is geo-partitioning (`CONFIGURE ZONE` DDL in `docs/data-partitioning-and-sharding.md`) which was documented but not yet applied to the live cluster.
