# CockroachDB Migration Plan

## The Problem with the Current Setup

The system currently runs **PostgreSQL 16** on each of the three regional cells (EU, US, APAC) with
unidirectional streaming replication: EU → US → APAC.

This means:

- **EU** is the only read/write primary
- **US and APAC** are read-only replicas

The Vercel edge middleware correctly geo-routes users to their nearest cell based on IP country.
However, when a US user's request hits the US cell and attempts a write (e.g. booking a journey),
it hits a **read-only replica** - the write either fails or the app must forward it back to EU,
adding a full transatlantic round trip.

**Example of the broken flow:**
```
US user → US LB → US cell (replica) → write fails / forwarded to EU → EU primary
```

This defeats the purpose of having regional cells. The user in the US experiences EU-level latency
for writes despite being routed to a US node.

---

## The Requirement

> A user in the US booking a journey for a relative in the EU should have their request served by
> the nearest datacenter (US). The write should land locally, then replicate to other regions -
> not the other way around.

This is the classic **multi-master / active-active** distributed database requirement:
every node accepts writes, and data propagates globally after the fact.

---

## Why CockroachDB

CockroachDB is a distributed SQL database built specifically for this pattern:

- **Postgres wire-protocol compatible** - no application code changes required. Services keep
  connecting on the same port with the same SQL dialect.
- **Built-in multi-master** - every node accepts writes. No primary/replica distinction.
- **Raft consensus** - writes are durably committed across a quorum of nodes before acknowledging,
  giving strong consistency without manual replication config.
- **`REGIONAL BY ROW` tables** - pins the Raft leaseholder for each row to the region that owns
  it, meaning local writes achieve local latency (the leaseholder is already on the same node).
- **Automatic failover** - if a node goes down, the cluster continues serving from the remaining
  nodes. No manual promotion of a replica to primary.

Compared to the alternatives:

| Option | Why not |
|---|---|
| pglogical / BDR | Requires significant Postgres config, conflict resolution is manual, not suitable for academic demo timeline |
| Citus | Write sharding, not true multi-master across regions |
| Manual write forwarding | App-layer hack, adds latency, doesn't solve the root problem |
| CockroachDB | Solves it natively, Postgres-compatible, open source |

---

## How It Will Solve the Problem

### New write flow (after migration):

```
US user → US LB → US CockroachDB node → write committed locally (Raft quorum)
                                       → replicates to EU + APAC nodes asynchronously
```

With `REGIONAL BY ROW` configured:
- The leaseholder for a US user's rows lives on the US node
- Reads and writes for that row are served locally without cross-region round trips
- EU users' rows are pinned to the EU leaseholder - same benefit

### What changes at the infrastructure level:

The three Swarm cells, which are currently isolated, will now run CockroachDB nodes that form
**a single distributed cluster**. Inter-node communication happens over port `26257` using
CockroachDB's internal Raft protocol.

```
[ EU cell ]  ←──── Raft ────→  [ US cell ]  ←──── Raft ────→  [ APAC cell ]
 crdb node                       crdb node                       crdb node
 (leaseholder                    (leaseholder                    (leaseholder
  for EU rows)                    for US rows)                    for APAC rows)
```

Each application service still connects to its **local** CockroachDB node - nothing changes from
the service's perspective except the port number (`5432` → `26257`).

---

## What Changes

### 1. GCP Firewall Rules
Open TCP port `26257` between the three VM external IPs so CockroachDB nodes can form a cluster.

```bash
gcloud compute firewall-rules create allow-crdb-internal \
  --allow tcp:26257 \
  --source-ranges=<EU_IP>/32,<US_IP>/32,<APAC_IP>/32 \
  --target-tags=vcs-node \
  --description="CockroachDB inter-node Raft traffic"
```

### 2. docker-stack.yml

**Before:**
```yaml
db:
  image: postgres:16-alpine
  environment:
    POSTGRES_USER: postgres
    POSTGRES_DB: trafficservice
    POSTGRES_PASSWORD_FILE: /run/secrets/db_password
```

**After:**
```yaml
db:
  image: cockroachdb/cockroach:v24.1.0
  command: >
    start --insecure
    --advertise-addr=<THIS_NODE_PUBLIC_IP>
    --join=<EU_IP>:26257,<US_IP>:26257,<APAC_IP>:26257
    --cache=.25
    --max-sql-memory=.25
```

### 3. Application Environment Variables

All services: change `VCS_DATABASE_MASTER_PORT` and `VCS_DATABASE_SLAVE_PORT` from `5432` to `26257`.
Remove `db_password` secret references - CockroachDB runs in insecure mode for this demo.

### 4. One-time Cluster Bootstrap (SSH, run once on EU node)

```bash
docker exec -it <crdb_container> cockroach init --insecure --host=localhost
```

### 5. Multi-region SQL (run once after cluster is up)

```sql
ALTER DATABASE trafficservice PRIMARY REGION "eu-west1";
ALTER DATABASE trafficservice ADD REGION "us-east1";
ALTER DATABASE trafficservice ADD REGION "us-east1";

-- Pin rows to the region of the user who owns them
ALTER TABLE journeys SET LOCALITY REGIONAL BY ROW;
ALTER TABLE bookings SET LOCALITY REGIONAL BY ROW;
```

---

## What Stays the Same

- All Go service source code - zero changes
- Docker overlay network (`vcs-internal`) within each cell
- Redis (still local per cell, no cross-region sync needed)
- Nginx / LB setup
- Vercel edge middleware geo-routing
- CI/CD pipeline

---

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Insecure mode (no TLS between nodes) | Acceptable for academic demo; production would use `cockroach cert` |
| e2-medium only has 4GB RAM | CockroachDB `--cache=.25 --max-sql-memory=.25` caps it at ~1GB - fits within current limits |
| 3-node Raft quorum - losing 2 nodes halts writes | Acceptable for demo; production would run 5+ nodes |
| Schema compatibility (Postgres → CRDB) | CockroachDB is highly compatible; any issues surface immediately on first migration run |
| Existing Postgres data | Demo environment - no production data to migrate |

---

## Migration Steps (in order)

- [x] Update `docker-stack.yml` - swap image, command, remove `db_password` secret from `db` service
- [x] Update all service env vars - port `5432` → `26257`, remove password secret mounts
- [x] Update `docker-compose.yml` for local dev (`start-single-node --insecure`)
- [x] Replace `postgres-exporter` with CockroachDB native Prometheus endpoint in `prometheus.yml`
- [ ] Add GCP firewall rule for port `26257`
- [ ] Deploy updated stack to all three cells
- [ ] SSH into EU node - run `cockroach init` to bootstrap the cluster
- [ ] Verify cluster: `cockroach node status --insecure`
- [ ] Run multi-region SQL to configure `REGIONAL BY ROW`
- [ ] Smoke test: book a journey from a US IP, confirm write lands on US node

---

## TODO - Next Steps After Initial Deploy

> **Context:** As of the initial migration, the system runs a single-region CockroachDB cluster
> (EU cell only). The steps below are required to achieve the full multi-region active-active
> architecture described in this document.

### Step 1 - Open GCP Firewall for Inter-Node Traffic

CockroachDB nodes need to reach each other on TCP port `26257`. Run this once in GCP Cloud Shell:

```bash
gcloud compute firewall-rules create allow-crdb-internal \
  --allow tcp:26257 \
  --source-ranges=<EU_IP>/32,<US_IP>/32,<APAC_IP>/32 \
  --target-tags=vcs-node \
  --description="CockroachDB inter-node Raft traffic"
```

Replace `<EU_IP>`, `<US_IP>`, `<APAC_IP>` with the external IPs of each cell's manager VM.

---

### Step 2 - Deploy Stack to US and APAC Cells

SSH into each cell's manager and deploy with the correct `CRDB_ADVERTISE_IP` and `CRDB_JOIN`:

```bash
# On EU manager
export CRDB_ADVERTISE_IP=<EU_IP>
export CRDB_JOIN=<EU_IP>:26257,<US_IP>:26257,<APAC_IP>:26257
docker stack deploy -c docker-stack.yml vcs

# On US manager
export CRDB_ADVERTISE_IP=<US_IP>
export CRDB_JOIN=<EU_IP>:26257,<US_IP>:26257,<APAC_IP>:26257
docker stack deploy -c docker-stack.yml vcs

# On APAC manager
export CRDB_ADVERTISE_IP=<APAC_IP>
export CRDB_JOIN=<EU_IP>:26257,<US_IP>:26257,<APAC_IP>:26257
docker stack deploy -c docker-stack.yml vcs
```

---

### Step 3 - Bootstrap the CockroachDB Cluster

Run this **once only** on the EU manager after all three nodes are up:

```bash
docker exec -it $(docker ps -qf name=vcs_db) \
  /cockroach/cockroach init --insecure --host=localhost:26257
```

Verify all nodes joined:

```bash
docker exec -it $(docker ps -qf name=vcs_db) \
  /cockroach/cockroach node status --insecure --host=localhost:26257
```

You should see 3 rows - one per cell.

---

### Step 4 - Create the Database

```bash
docker exec -it $(docker ps -qf name=vcs_db) \
  /cockroach/cockroach sql --insecure --host=localhost:26257 \
  --execute="CREATE DATABASE IF NOT EXISTS trafficservice;"
```

---

### Step 5 - Configure Multi-Region Localities

Tell CockroachDB which region each node belongs to. Add `--locality` to the `command` in
`docker-stack.yml` for each cell before deploying:

```yaml
# EU cell
command: >
  start --insecure
  --advertise-addr=${CRDB_ADVERTISE_IP}:26257
  --join=${CRDB_JOIN}
  --locality=region=eu-west1
  --cache=.25 --max-sql-memory=.25

# US cell
command: >
  start --insecure
  --advertise-addr=${CRDB_ADVERTISE_IP}:26257
  --join=${CRDB_JOIN}
  --locality=region=us-east1
  --cache=.25 --max-sql-memory=.25

# APAC cell
command: >
  start --insecure
  --advertise-addr=${CRDB_ADVERTISE_IP}:26257
  --join=${CRDB_JOIN}
  --locality=region=asia-east1
  --cache=.25 --max-sql-memory=.25
```

---

### Step 6 - Configure Multi-Region Tables

Once localities are set, run this SQL to enable regional pinning:

```sql
ALTER DATABASE trafficservice PRIMARY REGION "eu-west1";
ALTER DATABASE trafficservice ADD REGION "us-east1";
ALTER DATABASE trafficservice ADD REGION "asia-east1";

-- Pin rows to the region of the user who created them
-- (requires a crdb_region column - CockroachDB adds it automatically)
ALTER TABLE journeys   SET LOCALITY REGIONAL BY ROW;
ALTER TABLE bookings   SET LOCALITY REGIONAL BY ROW;
ALTER TABLE users      SET LOCALITY REGIONAL BY ROW;
```

---

### Step 7 - Smoke Test

1. Make a booking from a US IP (or use a VPN)
2. On the US node, run:
   ```sql
   SELECT crdb_region, id, created_at FROM bookings ORDER BY created_at DESC LIMIT 5;
   ```
3. Confirm `crdb_region = 'us-east1'` - the write landed locally

---

### Notes

- **No data migration needed** - this is a fresh cluster; schema is created by service migrations on first boot
- **`db_password` secret** - the Docker secret still exists on nodes but is no longer used; safe to leave or remove with `docker secret rm db_password` after confirming all services are healthy
- **Grafana dashboard** - the existing VCS overview dashboard will pick up CockroachDB metrics automatically once Prometheus starts scraping `db:8080/_status/vars`
