# Data Partitioning & Sharding — VCS on CockroachDB

**CS7NS6 Exercise 2 checklist coverage:** Sharding, Exploit locality, Replication, Consistency model, Transactions, Isolation level

---

## 1. Three concepts to keep separate

| Concept | What it means in CRDB | Controlled by |
|---|---|---|
| **Replication** | Every row exists on ALL 3 nodes (Raft followers). A write is not committed until a majority (2/3) acknowledge it. | Automatic — cannot be turned off |
| **Sharding** | Each table is split into ~512 MiB Ranges. Each Range is an independent Raft group. Splits/merges automatically as data grows. | Automatic — no DDL needed |
| **Geo-partitioning** | Which node is the **leaseholder** (Raft leader) for a given Range, i.e. who serves reads and coordinates writes. | Explicit — `PARTITION BY` + `CONFIGURE ZONE` DDL |

### On cross-region visibility

> "A booking made in EU for NY→Miami should be visible in US DB."

**This is already true without extra work.** Raft replication means a booking written via the EU cell is physically copied to US and APAC before the client receives `200 OK`. Geo-partitioning controls which node is the **leaseholder** — not which nodes store the data.

### On locking across regions

> "When we take a lock in EU, does it lock in US too?"

Yes. When `doReserveTx` (reservation_service.go:169) opens a serializable transaction and calls `LockForUpdate`, that write goes through the **leaseholder** of the relevant Range. The leaseholder sends the Raft entry to the other 2 replicas and waits for quorum (2/3) before committing. Cross-region acknowledgement is unavoidable for serializable consistency — this is CAP theorem.

What you minimise by geo-partitioning: the **first hop** (app → leaseholder) is local. Raft replication to other cells happens in parallel, not blocking the commit.

---

## 2. The cross-continental journey problem

### What happens today (single transaction spans multiple partitions)

`doReserveTx` opens **one serializable transaction** and locks+checks+inserts every segment sequentially:

```
reservation_service.go:169  BeginTx(LevelSerializable)
  └─ for each segment (sorted by segment_id):
       segment_repo.LockForUpdate()          → SELECT ... FOR UPDATE
       reservation_repo.SumActiveOverlapping()
       reservation_repo.Insert()
  └─ Commit
```

For a intra-region journey (e.g. Dublin → Cork), all segments are on the EU leaseholder. The transaction touches one partition group — fast.

For a **cross-continental journey** (e.g. Istanbul → Mumbai overland), segments span multiple geo-partitions:

```
EU segments:   seg_TR_E80, seg_TR_O4  → EU leaseholder
APAC segments: seg_IN_NH8, seg_IN_NH4 → APAC leaseholder
```

This single transaction becomes a **distributed transaction** in CockroachDB. CRDB internally uses a 2PC-like protocol (via its transaction coordinator on the gateway node) to commit atomically across both partition groups.

### This is correct but has a latency cost

CockroachDB's distributed transactions are **fully serializable and atomic**. The booking either succeeds for ALL segments across ALL regions, or fails for none. No partial booking is possible — the `tx.Rollback()` on any failure in `doReserveTx` rolls back all writes across all partitions.

The cost is latency: committing a distributed transaction requires a roundtrip to EACH partition's leaseholder. For EU → APAC: ~150–200 ms additional latency per booking attempt.

For a road journey spanning continents, this is acceptable — the booking happens once, not in a tight loop.

---

## 3. Two strategies for cross-regional journeys

### Strategy A — Accept CRDB's native distributed transaction (what we use)

**No code changes required.** CRDB handles cross-partition atomicity internally. The lock ordering (`sort.Slice` by `segment_id` in `reservation_service.go:105`) prevents application-level deadlocks, and CRDB's distributed deadlock detector handles any cross-partition cycles.

**Optimisation available:** Use **parallel commits** (CRDB v19.2+, enabled by default). Instead of sequential 2PC, CRDB can mark the transaction as implicitly committed as soon as writes are replicated, without a second round of messages. This cuts distributed transaction latency roughly in half.

**Gateway optimisation:** Connect the `capacity-service` to the CockroachDB node that holds the **majority** of the journey's segments. For a mostly-APAC journey, route the connection through the APAC node — it becomes the transaction coordinator (gateway) and reduces cross-region roundtrips from N to 1 for the local segments.

```go
// In connection.go: select gateway based on journey's segment majority region
// (passed via context or config). The DB pool for 'us' points to US CRDB node.
func PoolForRegion(region string) *sql.DB {
    switch region {
    case "us":   return usPool
    case "apac": return apacPool
    default:     return euPool
    }
}
```

---

### Strategy B — Saga pattern (per-region sub-transactions)

For high-throughput cross-continental booking at scale, the distributed transaction latency of Strategy A adds up. The Saga pattern decomposes the single cross-region transaction into **per-region sub-transactions**, each running locally on its regional leaseholder.

#### How it works

```
Cross-continental journey: EU segments + APAC segments

Step 1: Reserve EU segments
  → Tx on EU leaseholder (local, fast)
  → Writes to journey.outbox: {event: "eu_reserved", journey_id: ...}

Step 2: Reserve APAC segments  (triggered by outbox relay)
  → Tx on APAC leaseholder (local, fast)
  → Writes to journey.outbox: {event: "apac_reserved", journey_id: ...}

Step 3: Saga coordinator sees both events → mark journey APPROVED

Failure path:
  If Step 2 fails:
    → Compensating transaction: release EU reservations
    → journey.outbox: {event: "eu_released", journey_id: ...}
    → Mark journey REJECTED
```

#### Saga state machine (new table)

```sql
-- journey-service/migrations/007_cross_region_saga.sql

CREATE TABLE IF NOT EXISTS journey.reservation_sagas (
    saga_id          VARCHAR(30) PRIMARY KEY,
    journey_id       VARCHAR(20) NOT NULL REFERENCES journey.journeys(journey_id),
    crdb_region      VARCHAR(10) NOT NULL,  -- partition key
    status           VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    -- PENDING → RESERVED → COMMITTED | COMPENSATED
    region           VARCHAR(10) NOT NULL,  -- 'eu', 'us', 'apac'
    reservation_id   VARCHAR(30),
    error_reason     TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_saga_status CHECK (status IN ('PENDING','RESERVED','COMMITTED','COMPENSATED','FAILED'))
);

CREATE INDEX idx_sagas_journey ON journey.reservation_sagas (journey_id);
CREATE INDEX idx_sagas_status  ON journey.reservation_sagas (status, updated_at)
    WHERE status IN ('PENDING', 'RESERVED');
```

#### Saga coordinator (pseudocode)

```go
// ReserveCrossRegional orchestrates a saga for journeys spanning multiple regions
func (s *ReservationService) ReserveCrossRegional(ctx context.Context, req *model.ReserveRequest) error {
    // Group segments by their geo-region
    byRegion := groupSegmentsByRegion(req.Reservations)
    // e.g. { "eu": [seg_TR_E80, ...], "apac": [seg_IN_NH8, ...] }

    sagaID := generateID("saga")
    var reservedRegions []string

    for region, segments := range byRegion {
        pool := PoolForRegion(region)  // EU pool → EU CRDB node, etc.
        err := reserveRegionLocal(ctx, pool, sagaID, region, segments, req)
        if err != nil {
            // Compensate all already-reserved regions
            for _, doneRegion := range reservedRegions {
                compensateRegion(ctx, PoolForRegion(doneRegion), sagaID, doneRegion)
            }
            return fmt.Errorf("saga failed at region %s: %w", region, err)
        }
        reservedRegions = append(reservedRegions, region)
    }
    return nil
}
```

#### Strategy comparison

| | Strategy A (CRDB distributed tx) | Strategy B (Saga) |
|---|---|---|
| **Atomicity** | Guaranteed by CRDB 2PC | Eventual — compensating tx on failure |
| **Latency** | RTT to every partition's leaseholder | One RTT per region, in parallel |
| **Code complexity** | None — works today | New saga coordinator + outbox relay |
| **Failure handling** | Automatic rollback | Must implement compensating transactions |
| **When to use** | Cross-continental journeys are rare | High volume of cross-regional bookings |

**For this system: Strategy A is correct and sufficient.** Cross-continental road journeys are rare and booking happens once. The Saga pattern is documented here for completeness and scale.

---

## 4. Region model for genuinely global journeys

The 3-region model (`eu / us / apac`) is too coarse for a real Silk Road scenario. A production system would use finer granularity:

| Region tag | Coverage | CRDB cell |
|---|---|---|
| `eu` | Western & Northern Europe | EU (Ireland) |
| `me` | Middle East, Turkey, Central Asia | EU (or dedicated ME node) |
| `in` | South Asia (India, Pakistan, Bangladesh) | APAC (or dedicated IN node) |
| `apac` | East Asia, South-East Asia, Pacific | APAC (Singapore) |
| `us` | Americas | US (Virginia) |
| `af` | Africa | EU (closest) or dedicated |

For the current assignment scope (Irish road network + Irish intercity routes), the 3-region model with `eu` as the only active region is correct. All current segments (`seg_city_*`, `IE-M*`) map to `eu`.

When US or APAC road segments are added (e.g. `US-I95`, `JP-E1`), their `crdb_region` is set at insert time in `002_seed_segments.sql` — no schema change needed.

---

## 5. Partition DDL (corrected — eu/us/apac, not road directions)

### 5.1 Locality labels on CRDB nodes

Add to `docker-stack.yml` `db` service command:

```yaml
# EU cell
--locality=region=eu,zone=eu-west1
# US cell  
--locality=region=us,zone=us-central1
# APAC cell
--locality=region=apac,zone=ap-southeast1
```

### 5.2 Multi-region database setup

```sql
-- Run once after cluster bootstrap (from GCP Cloud Shell)
ALTER DATABASE trafficservice SET PRIMARY REGION 'eu';
ALTER DATABASE trafficservice ADD REGION 'us';
ALTER DATABASE trafficservice ADD REGION 'apac';
```

### 5.3 Partition `capacity.reservations` by segment geo-region

```sql
-- capacity-service/migrations/005_partition_reservations.sql

-- Add geo-region column (all current segments are Irish = eu)
ALTER TABLE capacity.reservations
    ADD COLUMN IF NOT EXISTS crdb_region VARCHAR(10) NOT NULL DEFAULT 'eu';

-- Backfill: all existing segments are in Ireland
UPDATE capacity.reservations SET crdb_region = 'eu';

-- Rebuild PK: partition column must be part of the primary key
ALTER TABLE capacity.reservations
    ALTER PRIMARY KEY USING COLUMNS (crdb_region, id);

-- Partition by the 3 geo-regions
ALTER TABLE capacity.reservations PARTITION BY LIST (crdb_region) (
    PARTITION p_eu   VALUES IN ('eu'),
    PARTITION p_us   VALUES IN ('us'),
    PARTITION p_apac VALUES IN ('apac')
);

-- Pin leaseholders to their home cell
ALTER PARTITION p_eu   OF TABLE capacity.reservations
    CONFIGURE ZONE USING num_replicas = 3, lease_preferences = '[[+region=eu]]';

ALTER PARTITION p_us   OF TABLE capacity.reservations
    CONFIGURE ZONE USING num_replicas = 3, lease_preferences = '[[+region=us]]';

ALTER PARTITION p_apac OF TABLE capacity.reservations
    CONFIGURE ZONE USING num_replicas = 3, lease_preferences = '[[+region=apac]]';

-- Rebuild hot-path indexes with crdb_region as leading column
DROP INDEX IF EXISTS idx_reservations_segment_window;
CREATE INDEX idx_reservations_segment_window
    ON capacity.reservations (crdb_region, segment_id, time_window_start, time_window_end)
    WHERE status = 'active';

DROP INDEX IF EXISTS idx_reservations_journey;
CREATE INDEX idx_reservations_journey
    ON capacity.reservations (crdb_region, journey_id)
    WHERE status = 'active';

-- Hash sharding: eliminates write hotspot from sequential BIGSERIAL id
ALTER INDEX capacity.reservations@primary
    USING HASH WITH (bucket_count = 8);
```

### 5.4 Partition `journey.journeys` by origin geo-region

```sql
-- journey-service/migrations/006_partition_journeys.sql

ALTER TABLE journey.journeys
    ADD COLUMN IF NOT EXISTS crdb_region VARCHAR(10) NOT NULL DEFAULT 'eu';

UPDATE journey.journeys SET crdb_region = 'eu';

ALTER TABLE journey.journeys
    ALTER PRIMARY KEY USING COLUMNS (crdb_region, journey_id);

ALTER TABLE journey.journeys PARTITION BY LIST (crdb_region) (
    PARTITION p_eu   VALUES IN ('eu'),
    PARTITION p_us   VALUES IN ('us'),
    PARTITION p_apac VALUES IN ('apac')
);

ALTER PARTITION p_eu   OF TABLE journey.journeys
    CONFIGURE ZONE USING num_replicas = 3, lease_preferences = '[[+region=eu]]';

ALTER PARTITION p_us   OF TABLE journey.journeys
    CONFIGURE ZONE USING num_replicas = 3, lease_preferences = '[[+region=us]]';

ALTER PARTITION p_apac OF TABLE journey.journeys
    CONFIGURE ZONE USING num_replicas = 3, lease_preferences = '[[+region=apac]]';

-- Rebuild indexes
DROP INDEX IF EXISTS idx_journeys_driver;
CREATE INDEX idx_journeys_driver
    ON journey.journeys (crdb_region, driver_id, created_at DESC);

DROP INDEX IF EXISTS idx_journeys_status;
CREATE INDEX idx_journeys_status ON journey.journeys (crdb_region, status);

DROP INDEX IF EXISTS idx_one_active_per_driver;
CREATE UNIQUE INDEX idx_one_active_per_driver
    ON journey.journeys (crdb_region, driver_id)
    WHERE status IN ('APPROVED', 'ACTIVE');
```

### 5.5 Reference tables as GLOBAL

`capacity.segments`, `map.segments`, `map.nodes` — small, read by every cell, rarely written:

```sql
-- These tables have a local copy on every node, reads never leave the cell
ALTER TABLE capacity.segments      SET LOCALITY GLOBAL;
ALTER TABLE map.segments           SET LOCALITY GLOBAL;
ALTER TABLE map.nodes              SET LOCALITY GLOBAL;
ALTER TABLE map.intercity_segments SET LOCALITY GLOBAL;
```

---

## 6. Application changes

### Origin region resolver (journey-service)

```go
// journey-service/internal/service/region.go
func GeoRegion(lat, lng float64) string {
    switch {
    case lat >= 34 && lat <= 72 && lng >= -12 && lng <= 45:
        return "eu"   // Europe (includes Turkey)
    case lat >= 15 && lat <= 72 && lng >= -170 && lng <= -50:
        return "us"   // Americas
    default:
        return "apac" // Asia-Pacific / rest of world
    }
}
```

Set `crdb_region = GeoRegion(req.OriginLat, req.OriginLng)` before the journey INSERT.

### Segment region resolver (capacity-service)

`capacity.segments` already has a `region` column. Add a `crdb_region` column to it seeded from the segment's country:

```go
// In reservation_service.go — after fetching segment, set crdb_region on the reservation
res.CRDBRegion = segment.CRDBRegion  // sourced from capacity.segments.crdb_region
```

### Reservation INSERT (capacity-service / reservation_repo.go)

```go
const q = `
    INSERT INTO capacity.reservations
        (crdb_region, reservation_id, journey_id, segment_id,
         time_window_start, time_window_end, vehicle_type, slots_used, status, created_at)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', NOW())`

_, err := tx.ExecContext(ctx, q,
    res.CRDBRegion,  // <-- new
    res.ReservationID, res.JourneyID, res.SegmentID,
    res.TimeWindowStart, res.TimeWindowEnd,
    string(res.VehicleType), res.SlotsUsed,
)
```

---

## 7. Data flow examples

### Intra-region journey (Dublin → Cork)

```
Driver app → EU cell
crdb_region = 'eu' (origin in Ireland)

journey.journeys INSERT   → EU leaseholder  (local)
capacity.reservations     → EU leaseholder  (all IE-M* segments = eu)

Raft replication → US, APAC (background, not on critical path)
Total cross-region hops: 0 on write path
```

### Cross-continental journey (Istanbul → Mumbai)

```
Driver app → EU cell (origin in Turkey = eu boundary)
crdb_region_journey = 'eu'

journey.journeys INSERT   → EU leaseholder  (local)

capacity.reservations:
  seg_TR_E80 (crdb_region='eu') → EU leaseholder  ─┐
  seg_IR_7   (crdb_region='me') → EU leaseholder  ─┤ Strategy A:
  seg_PK_N55 (crdb_region='in') → APAC leaseholder─┤ single CRDB distributed tx
  seg_IN_NH8 (crdb_region='apac')→ APAC leaseholder─┘

CRDB internal 2PC: EU gateway node coordinates both partition groups
Commit latency: local EU segments (fast) + 1 RTT to APAC leaseholder (~150ms)
Result: atomic across all segments — either all reserved or none
```

### Cross-continental journey with Saga (Strategy B, high scale)

```
Step 1: EU segments tx  → EU node   (local, ~5ms)
Step 2: APAC segments tx → APAC node (local, ~5ms)
Both steps in parallel → total ~5ms + coordination overhead

If Step 2 fails: compensating tx releases EU reservations
```

---

## 8. Follower reads for non-critical queries

Reads that don't need the absolute latest data (dashboards, occupancy checks) can be served from the **local replica** with zero cross-region traffic:

```sql
-- reservoir_repo.go: SumActiveAtTime — already uses slave pool
-- Add AS OF SYSTEM TIME for true follower read
SELECT segment_id, COALESCE(SUM(slots_used), 0.0)
FROM   capacity.reservations AS OF SYSTEM TIME follower_read_timestamp()
WHERE  status = 'active'
  AND  time_window_start <= $1
  AND  time_window_end    > $1
GROUP  BY segment_id;
```

`follower_read_timestamp()` returns a timestamp ~4.8 seconds in the past — acceptable staleness for an occupancy dashboard.

---

## 9. Verify on the live cluster (GCP Cloud Shell)

```bash
cockroach sql --insecure --host=localhost:26257

-- Check partitions
SHOW PARTITIONS FROM TABLE capacity.reservations;
SHOW PARTITIONS FROM TABLE journey.journeys;

-- Check zone configs (leaseholder placement)
SHOW ZONE CONFIGURATION FROM PARTITION p_eu   OF TABLE capacity.reservations;
SHOW ZONE CONFIGURATION FROM PARTITION p_apac OF TABLE capacity.reservations;

-- Check range distribution after writes
SELECT range_id, lease_holder, replicas, start_pretty, end_pretty
FROM   crdb_internal.ranges
WHERE  table_name = 'reservations'
ORDER  BY range_id;

-- Check multi-region setup
SHOW REGIONS FROM DATABASE trafficservice;
```

---

## 10. Checklist mapping

| CS7NS6 checklist item | Status | How |
|---|---|---|
| **Sharding** | Automatic + explicit | CockroachDB Range splitting; geo-partitioning pins shard leaseholders |
| **Exploit locality** | Explicit via `CONFIGURE ZONE` | Leaseholders pinned to the cell owning the segment's geography |
| **Replication** | Automatic (Raft) | 3 replicas across EU/US/APAC — all data visible everywhere |
| **Consistency model** | Serializable | CockroachDB default; `doReserveTx` retries on SQLSTATE 40001 |
| **Transactions** | Active | Single serializable tx for intra-region; CRDB distributed tx for cross-region |
| **Isolation level** | Serializable | `sql.LevelSerializable` in `doReserveTx`; prevents phantom reads / double booking |
| **Data durability** | Raft + disk | 2/3 quorum required for commit; survives single-node failure |
| **Partitions handled** | Via CRDB Raft | Network partition: majority partition (2/3 nodes) continues serving; minority stalls |
