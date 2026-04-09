# Booking Locking and Circuit Breaker Notes

This note documents current implementation behavior for:
1. Segment reservation during booking (including last-slot contention and release timing)
2. Circuit breaker behavior for inter-service calls

## 1) Segment Reservation During Booking

### End-to-end booking reservation flow
1. Journey Service computes route segments and per-segment time windows.
2. Journey Service calls Capacity Service `POST /api/v1/capacity/reserve` with:
   - `journey_id`
   - `idempotency_key`
   - `vehicle_type`
   - `priority_level`
   - segment reservations (segment ID + time window)
3. Capacity Service validates input and sorts segments by `segment_id` before locking.
4. Capacity Service opens a `SERIALIZABLE` transaction.
5. Capacity Service re-checks idempotency inside the transaction.
6. For each segment, Capacity Service executes `SELECT ... FOR UPDATE` on `capacity.segments`.
7. It computes overlapping active load from `capacity.reservations`.
8. It checks requested slot weight against effective capacity.
9. If any segment fails, the transaction rolls back and a deterministic failed response is cached by idempotency key.
10. If all segments pass, reservation rows are inserted as `status='active'`, idempotency success is written in the same transaction, and then committed.

### What happens with two bookings for the last remaining slot?
Given two concurrent requests on the same segment/time window where only one slot remains:
1. Both requests reach Capacity Service.
2. Request A acquires the row lock (`FOR UPDATE`) first.
3. Request B blocks at the same lock until A commits or rolls back.
4. A reads current active load, inserts reservation, commits.
5. Lock is released automatically on commit.
6. B proceeds, re-reads current active load (now including A), sees insufficient capacity, returns failed (`status="failed"`, reason `at_capacity`).

Expected outcome: exactly one reservation succeeds and the other fails for capacity.

### When exactly is it locked and unlocked?
- Lock acquisition: when each segment row is read via `SELECT max_capacity ... FOR UPDATE` inside the reservation transaction.
- Lock release: automatically at transaction end (`COMMIT` or `ROLLBACK`).

### Is there any time limit for the user?
There are two different timelines:

1. DB row-lock time (very short)
   - There is no separate "user lock TTL" in the database.
   - The row lock only lives for the life of the SQL transaction.
   - If request processing fails or times out, the transaction rolls back and lock is released.

2. Reservation hold time (business lifetime)
   - After success (`status="reserved"`), reservation rows remain `status='active'` until a release path runs.
   - They are released on journey lifecycle events (`cancelled`, `completed`, `expired`).
   - For APPROVED journeys that are never activated, Journey Service marks them `EXPIRED` after departure + 30 minutes; expiry job runs every 5 minutes.
   - If release event flow is missed/delayed, orphan cleanup is the fallback (`orphan_threshold=5m`, run every `5m`).

Request timeout bounds (user-facing request latency):
- Journey -> Capacity client timeout: `5s`
- Journey Service server timeout: `30s`
- Capacity Service server timeout: `30s`

So: lock is not held for a long user session; it is held only during the reserve transaction. The longer-lived concept is the reservation row (`active`), not the SQL lock.

### Is this pessimistic or optimistic locking?
- Capacity reservation path: pessimistic row locking (`FOR UPDATE`) plus `SERIALIZABLE` isolation.
- Journey status transitions: optimistic version locking (`WHERE version = $expected`) in Journey repository updates.

### When does reserved capacity become available to others?
Capacity rows remain `active` until one of these release paths runs:
1. Event-driven release:
   - Journey Service writes lifecycle events to outbox.
   - Capacity Service consumer handles `journey.cancelled`, `journey.completed`, and `journey.expired`.
   - It updates matching active reservations to `status='released'` and sets `released_at`.
2. Safety-net orphan cleanup:
   - Background job marks stale active reservations as released when `time_window_end < now - orphan_threshold`.
   - Default values in current config: cleanup interval `5m`, orphan threshold `5m`.
   - Effective fallback release can be up to roughly 10 minutes after `time_window_end` (5-minute age threshold checked on a 5-minute schedule).

So, release for others is immediate after cancellation/completion/expiry event consumption, with orphan cleanup as fallback.

## 2) Circuit Breaker Implementation

### Where it is implemented
- Journey Service Map client
- Journey Service Capacity client

Both use `github.com/sony/gobreaker` and create one breaker instance per client at service startup.

### Exact usage locations
- Map breaker setup:
   - `journey-service/internal/client/map_client.go` (`NewMapClient`)
- Map breaker-protected calls:
   - `journey-service/internal/client/map_client.go` (`fetchNodes` wraps `/api/v1/map/nodes` in `breaker.Execute`)
   - `journey-service/internal/client/map_client.go` (`ComputeRoute` wraps `/api/v1/routes/compute` in `breaker.Execute`)

- Capacity breaker setup:
   - `journey-service/internal/client/capacity_client.go` (`NewCapacityClient`)
- Capacity breaker-protected calls:
   - `journey-service/internal/client/capacity_client.go` (`Reserve` wraps `/api/v1/capacity/reserve` in `breaker.Execute`)

### Current breaker settings
- `MaxRequests: 1` (half-open probe limit)
- `Interval: 30s` (counter reset window)
- `Timeout: 10s` (open-state duration before half-open probe)
- `ReadyToTrip`: opens after `5` consecutive failures

### Protected calls
- Map client:
  - node fetch call
  - route compute call
- Capacity client:
  - reserve call

### Runtime behavior
1. Closed state: calls flow normally.
2. On repeated failures (5 consecutive): breaker opens.
3. Open state: calls fail fast with circuit-open error (no downstream call).
4. After timeout (10s): breaker enters half-open.
5. Half-open allows one probe:
   - success closes breaker
   - failure re-opens breaker

### Error propagation
- Client returns upstream error or `... circuit open` error.
- Journey Service wraps this as external dependency failure.
- HTTP handler returns `502` for booking when map/capacity dependency is unavailable.

## Notes
- Capacity reservation also retries CockroachDB serialization failures (`SQLSTATE 40001`) up to 5 attempts.
- Idempotency is used in both Journey and Capacity flows to make retries safe and avoid duplicate bookings.
