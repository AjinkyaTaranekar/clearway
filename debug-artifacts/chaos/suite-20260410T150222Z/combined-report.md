# VCS Chaos Suite — Combined Report

| | |
|---|---|
| **Generated** | 2026-04-10T15:28:52 |
| **Suite directory** | `debug-artifacts/chaos/suite-20260410T150222Z` |
| **Total experiments** | 8 |
| **Passed** | 8 |
| **Failed** | 0 |
| **Aborted (early exit)** | 0 |
| **Collateral damage** | 0 experiments affected non-target cells |
| **Overall result** | **PASS** |

---

## Experiment Results Summary

| # | Phase | Service | Type | Availability | Outage | Recovery | Non-target fails | Result |
|---|-------|---------|------|-------------|--------|----------|-----------------|--------|
| 1 | 1 | capacity-service | scale-service | 0% | 60s | — | ✓ 0 | ✓ PASS |
| 2 | 1 | map-service | scale-service | 0% | 60s | — | ✓ 0 | ✓ PASS |
| 3 | 1 | iam-service | scale-service | 0% | 60s | — | ✓ 0 | ✓ PASS |
| 4 | 1 | notification-service | scale-service | 0% | 60s | — | ✓ 0 | ✓ PASS |
| 5 | 2 | redis | scale-service | 100% | — | — | ✓ 0 | ✓ PASS |
| 6 | 2 | db | scale-service | 0% | 60s | — | ✓ 0 | ✓ PASS |
| 7 | 3 | drain-node | drain-node | 100% | — | — | ✓ 0 | ✓ PASS |
| 8 | 3 | block-firewall | block-firewall | 100% | — | — | ✓ 0 | ✓ PASS |

---

## Per-Experiment Analysis

### [1] capacity-service  _(Phase 1 — scale-service)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 0 (0%) |
| Endpoint DOWN/ERROR polls | 6 |
| Outage duration (approx) | 60s |
| First DOWN at | 10s elapsed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

Journey creates fail (502 from journey-service). Circuit breaker opens after 10 consecutive failures (~10–15s under load). **Observable:** `/api/v1/capacity/segments` → ERROR (502).

**Recovery path:**

CB closes after 30s probe window with 3 successes. Fast.

**Key events (from chaos-monkey.sh report):**

- 15:02:25 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:02:34 [chaos_start] capacity-service scaled to 0 (constraint: node.hostname==chaos-no-match-1775833349-capacity-service)
- 15:04:08 [restore_intermediate] Restoring capacity-service
- 15:04:21 [recovered_intermediate] capacity-service recovered after 10s
- 15:04:27 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T150223Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T150223Z/timeline.csv)_

---

### [2] map-service  _(Phase 1 — scale-service)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 0 (0%) |
| Endpoint DOWN/ERROR polls | 6 |
| Outage duration (approx) | 60s |
| First DOWN at | 10s elapsed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

Journey creates fail (map route unavailable). CB opens after 10 consecutive failures. **Observable:** `/api/v1/map/segments` → ERROR (502).

**Recovery path:**

CB closes after 30s probe window with 3 successes. Fast.

**Key events (from chaos-monkey.sh report):**

- 15:05:36 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:05:44 [chaos_start] map-service scaled to 0 (constraint: node.hostname==chaos-no-match-1775833539-map-service)
- 15:07:16 [restore_intermediate] Restoring map-service
- 15:07:30 [recovered_intermediate] map-service recovered after 10s
- 15:07:35 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T150533Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T150533Z/timeline.csv)_

---

### [3] iam-service  _(Phase 1 — scale-service)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 0 (0%) |
| Endpoint DOWN/ERROR polls | 6 |
| Outage duration (approx) | 60s |
| First DOWN at | 10s elapsed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

Login and registration fail on this node. JWKS endpoint served from memory — token verification still works. **Observable:** `/.well-known/jwks.json` → ERROR (502).

**Recovery path:**

Service restarts and reconnects; sessions already issued remain valid.

**Key events (from chaos-monkey.sh report):**

- 15:08:44 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:08:52 [chaos_start] iam-service scaled to 0 (constraint: node.hostname==chaos-no-match-1775833727-iam-service)
- 15:10:24 [restore_intermediate] Restoring iam-service
- 15:10:37 [recovered_intermediate] iam-service recovered after 10s
- 15:10:42 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T150842Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T150842Z/timeline.csv)_

---

### [4] notification-service  _(Phase 1 — scale-service)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 0 (0%) |
| Endpoint DOWN/ERROR polls | 6 |
| Outage duration (approx) | 60s |
| First DOWN at | 10s elapsed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

Push notifications not sent. Journey creates and capacity ops unaffected. **Observable:** `/api/v1/notifications` → ERROR (502).

**Recovery path:**

Service restarts; missed notifications not retried.

**Key events (from chaos-monkey.sh report):**

- 15:11:52 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:12:00 [chaos_start] notification-service scaled to 0 (constraint: node.hostname==chaos-no-match-1775833915-notification-service)
- 15:13:32 [restore_intermediate] Restoring notification-service
- 15:15:36 [recovery_timeout_intermediate] notification-service did not recover within 120s
- 15:15:41 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T151149Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T151149Z/timeline.csv)_

---

### [5] redis  _(Phase 2 — scale-service)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 6 (100%) |
| Endpoint DOWN/ERROR polls | 0 |
| Outage duration (approx) | 0s |
| First DOWN at | not observed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

HTTP endpoints stay UP (cache falls back to DB — graceful). **Silent failure:** `journey.events` Stream consumer stops; completed journeys do NOT release capacity → capacity leak. Route cache bypassed → more OSRM calls. **Observable:** endpoint stays UP (200) — check stream backlog post-restore.

**Redis note:** Endpoint showing UP (200) during outage is _expected behaviour_ (cache degrades to DB). The critical signal is the stream backlog check after restore — see key events below.

**Recovery path:**

Consumer group reconnects; outbox relay re-publishes backed-up events. Stream backlog clears within 1–2 poll cycles. Route cache warms up on first cache-miss calls.

**Key events (from chaos-monkey.sh report):**

- 15:16:50 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:17:00 [chaos_start] redis scaled to 0 (constraint: node.hostname==chaos-no-match-1775834214-redis)
- 15:18:38 [restore_intermediate] Restoring redis
- 15:18:41 [recovered_intermediate] redis recovered after 0s
- 15:18:58 [stream_backlog_detected] journey.events has un-ACKed messages after Redis restore
- 15:19:03 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T151648Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T151648Z/timeline.csv)_

---

### [6] db  _(Phase 2 — scale-service)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 0 (0%) |
| Endpoint DOWN/ERROR polls | 6 |
| Outage duration (approx) | 60s |
| First DOWN at | 10s elapsed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

All services on this node lose DB access. Reads may be cached briefly; writes fail immediately. Other cells unaffected. CRDB quorum maintained. **Observable:** `/api/v1/capacity/segments` → ERROR (500).

**Recovery path:**

Services auto-reconnect to local CRDB node once it restarts. No data loss (CRDB Raft log intact). Connection pool may need full TTL to cycle.

**Key events (from chaos-monkey.sh report):**

- 15:20:13 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:20:28 [chaos_start] db scaled to 0 (constraint: node.hostname==chaos-no-match-1775834416-db)
- 15:22:00 [restore_intermediate] Restoring db
- 15:22:29 [recovered_intermediate] db recovered after 20s
- 15:22:35 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T152011Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T152011Z/timeline.csv)_

---

### [7] drain-node  _(Phase 3 — drain-node)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 6 (100%) |
| Endpoint DOWN/ERROR polls | 0 |
| Outage duration (approx) | 0s |
| First DOWN at | not observed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

All services on the worker node stop. EU manager still serves. US and APAC unaffected. **Observable:** EU cell may briefly show degraded; other cells healthy.

**Recovery path:**

Node set back to 'active'; Swarm reschedules tasks within ~30s.

**Key events (from chaos-monkey.sh report):**

- 15:23:48 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:23:57 [chaos_start] Drained node vcs-vm-eu2 (ojbjodjz1bhgp5vngzaiugnan)
- 15:25:36 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T152341Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T152341Z/timeline.csv)_

---

### [8] block-firewall  _(Phase 3 — block-firewall)_

| Metric | Value |
|--------|-------|
| Total polls | 6 |
| Endpoint UP polls | 6 (100%) |
| Endpoint DOWN/ERROR polls | 0 |
| Outage duration (approx) | 0s |
| First DOWN at | not observed |
| Endpoint UP after restore | not observed |
| Non-target cell failures | 0 polls |
| All-cells-down polls | 0 |

**Expected impact:**

Internet traffic blocked to the worker node. Internal Docker overlay traffic unaffected. GCP LB stops routing to this backend. **Observable:** EU cell may briefly show degraded.

**Recovery path:**

Firewall rule deleted; traffic restores within seconds.

**Key events (from chaos-monkey.sh report):**

- 15:26:44 [preflight_ok] All cells healthy, CRDB quorum verified
- 15:26:54 [chaos_start] Firewall rule chaos-deny-vcs-vm-eu2-1775834809 blocking vcs-vm-eu2
- 15:28:46 [recovered] All cells healthy after 0s

_Raw files: [`report.md`](debug-artifacts/chaos/20260410T152642Z/report.md) · [`timeline.csv`](debug-artifacts/chaos/20260410T152642Z/timeline.csv)_

---

## Circuit Breaker Analysis

Journey-service circuit breaker configuration (as of last update):

| Parameter | Value |
|-----------|-------|
| Consecutive failures to open | 10 |
| Open duration (timeout) | 30s |
| Half-open probe requests | 3 |
| HTTP client timeout | 15s |

Under K6 concurrent load, the CB opens within ~10–15s of a downstream service going to 0 tasks. Without concurrent traffic the CB state is not observable via curl — the endpoint probe only confirms gateway reachability.

## Redis Streams — Capacity Leak Risk

The `journey.events` Redis Stream is the mechanism by which journey completions and cancellations release reserved capacity slots. When Redis goes to 0 tasks:

1. `capacity-service` consumer group (`XReadGroup`) stops receiving events.
2. `journey-service` outbox relay (`XADD`) fails; events buffer in the DB outbox.
3. Capacity slots for completed journeys remain reserved until Redis recovers.
4. **Risk:** if Redis is down during a high-volume period, the backlog of un-released slots can make segments appear at capacity when they are not.

**Recovery:** Once Redis restarts, the outbox relay re-publishes buffered events and the consumer group processes them within 1–2 poll cycles (~10–20s). Verify with: `redis-cli XPENDING journey.events capacity-consumer-group - + 10`

## Recommendations

- **Redis Stream backlog detected after restore.** Consider adding a Redis health probe to the monitoring stack so Stream consumer downtime is alerted before capacity leaks accumulate.

---

_Generated by `scripts/chaos/analyze-chaos-results.py`_