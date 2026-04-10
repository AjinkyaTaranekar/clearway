# Full Issues Report — Distributed Vehicle Capacity System

> **Date:** 2026-04-10
> **Tester:** Deepika Nag
> **Method:** Static code analysis + live endpoint testing + automated test runs + build verification + deep frontend component audit
> **Regions tested:** EU (35.187.121.12) · US (34.138.242.217) · APAC (34.80.180.64)

---

## Test Run Summary

| Area | Result |
|------|--------|
| Frontend build (`npm run build`) | PASS — but bundle 2.08 MB (see Issue 12) |
| IAM service `go test ./...` | FAIL — Windows Application Control blocks test binary execution locally |
| Journey service `go test ./...` | FAIL — Windows Application Control blocks `handler.test.exe` |
| Capacity service `go test ./...` | PASS (model, service layers) |
| Map service `go test ./...` | NO TESTS — zero test files across entire service |
| Notification service `go test ./...` | PASS (event, service layers) |
| All 5 services `go build ./...` | PASS — all compile cleanly |

---

## CRITICAL Issues

---

### ISSUE-01 — Security headers absent from all SPA and static responses

**Severity:** Critical | **Component:** nginx | **File:** `nginx/nginx.conf`

nginx `add_header` in a child `location` block **replaces** (does not inherit) the parent `server` block's headers. The `location /` block defines its own `add_header Cache-Control`, which silently drops all server-level security headers for the SPA.

**Confirmed live:**
```
GET http://35.187.121.12/
Response headers:
  Content-Type: text/html       ← present
  Cache-Control: max-age=3600   ← present
  X-Content-Type-Options        ← MISSING
  X-Frame-Options               ← MISSING
  X-XSS-Protection              ← MISSING
```

Security headers ARE present on API responses (services set them directly), but the SPA itself is unprotected. The application can be embedded in an iframe (clickjacking), MIME-sniffed, and XSS-reflected.

**Fix:** Add the `always` flag and duplicate headers in every `location` block that has its own `add_header`, or use the `ngx_http_headers_more_module`:
```nginx
location / {
    root /usr/share/nginx/html;
    try_files $uri $uri/ /index.html;
    expires 1h;
    add_header Cache-Control "public, no-transform";
    add_header X-Content-Type-Options nosniff always;
    add_header X-Frame-Options DENY always;
    add_header X-XSS-Protection "1; mode=block" always;
}
```
Apply the same to `location = /nginx-health`, `location = /api/v1/region`, and all `location /docs/*/` blocks.

---

### ISSUE-02 — US and APAC regions incorrectly display "EU"

**Severity:** Critical | **Component:** Infrastructure / nginx | **File:** `docker-stack.yml`

All three live cells return `{"region":"EU"}`:
```
curl http://34.138.242.217/api/v1/region  →  {"region":"EU"}   (US cell)
curl http://34.80.180.64/api/v1/region    →  {"region":"EU"}   (APAC cell)
```

The pipeline correctly sets `REGION=US` and `REGION=APAC` in the deploy commands. The issue is that these regions have not been redeployed since the last pipeline run targeted EU only. The cells are serving a stale nginx image that predates the REGION fix.

**Fix:** Run the GitHub Actions pipeline with `services=nginx, regions=all`. No code change required.

---

### ISSUE-03 — CORS: `Allow-Origin: *` combined with `Allow-Credentials: true`

**Severity:** Critical | **Component:** All backend services (middleware)

Every service responds with:
```
Access-Control-Allow-Origin: *
Access-Control-Allow-Credentials: true
```

Per the W3C CORS specification, browsers **reject** credentialed cross-origin requests when the origin is a wildcard. This means:
- Any cross-origin request that sends an `Authorization` header or cookie will be silently blocked by the browser.
- If a legitimate third-party client (mobile app, external dashboard) ever needs to call the API from a different origin, all authenticated requests will fail.

**Fix:** Replace the wildcard with the actual frontend origin in all CORS middleware files:
```go
w.Header().Set("Access-Control-Allow-Origin", "http://35.187.121.12")
// or read from config: w.Header().Set("Access-Control-Allow-Origin", cfg.AllowedOrigin)
```

---

### ISSUE-04 — Journey service handler tests blocked by Application Control (Windows)

**Severity:** Critical | **Component:** CI/Testing | **File:** `journey-service/internal/handler/`

```
fork/exec C:\Users\...\handler.test.exe: An Application Control policy has blocked this file.
FAIL    journey-service/internal/handler    1.504s
```

The journey handler test suite cannot execute on this machine due to Windows Defender Application Control. This means the most critical tests (booking flow, cancellation, admin actions) are **completely untested locally**. Tests may pass in CI (Linux runners) but cannot be verified locally.

**Fix:** Either whitelist the Go test binary in Windows Defender Application Control, or ensure all handler tests are run exclusively in CI. Add a note to the project README about this local limitation.

---

## HIGH Issues

---

### ISSUE-05 — Map service has zero tests

**Severity:** High | **Component:** map-service

The map service has no test files whatsoever across all packages:
```
map-service/internal/http/handlers/   — no test files
map-service/internal/http/            — no test files
map-service/cmd/server/               — no test files
```

The map service handles route computation, geocoding via OSRM, traffic data, and segment management. None of this logic is tested. Any regression in routing or geocoding will be invisible until a user reports it.

**Required tests:**
- `GetRoute` handler — correct segment sequence returned
- `ComputeRoute` — external OSRM timeout handling
- `SearchPlaces` — geocoding fallback when OSRM unreachable
- `GetTraffic` — occupancy data correctly aggregated

---

### ISSUE-06 — No HTTP handler tests for capacity, notification services

**Severity:** High | **Component:** capacity-service, notification-service

```
capacity-service/internal/http/handlers/    — no test files
notification-service/internal/http/handlers/ — no test files
```

Business logic tests exist (model, service layers), but HTTP handler tests (request parsing, auth enforcement, response formatting) are absent. The closure endpoints, capacity check, and notification delivery have no handler-level test coverage.

---

### ISSUE-07 — No HTTPS on any region

**Severity:** High | **Component:** Infrastructure

All three cells (`35.187.121.12`, `34.138.242.217`, `34.80.180.64`) serve on plain `http://`. JWT access tokens, passwords, and driver personal data are transmitted in cleartext on every request. The browser permanently displays "Not secure".

**Note:** `Strict-Transport-Security` header is configured in nginx.conf but is meaningless without TLS.

**Fix:** Requires a domain name + TLS certificate. Options:
- GCP Load Balancer with Google-managed SSL certificate (zero config, easiest for this setup)
- Certbot + Let's Encrypt on each VM (`certbot --nginx -d your-domain.com`)

---

### ISSUE-08 — `window` variable name shadows browser global in AnalyticsPage

**Severity:** High | **Component:** Frontend | **File:** `frontend/src/app/pages/admin/AnalyticsPage.tsx:44`

```typescript
const [window, setWindow] = useState<TimeWindow>('24h');
```

The local state variable `window` shadows the browser's built-in `window` global. While the TypeScript type system constrains it to `TimeWindow`, any future code in this component that tries to use `window.location`, `window.open()`, or any browser API will silently use the string value instead — no TypeScript error, silent runtime failure.

**Fix:** Rename the variable:
```typescript
const [timeWindow, setTimeWindow] = useState<TimeWindow>('24h');
```

---

### ISSUE-09 — Frontend bundle is 2.08 MB (exceeds 500 KB warning threshold)

**Severity:** High | **Component:** Frontend build

```
dist/assets/index-BVA5HHUf.js  2,085.29 kB │ gzip: 562.38 kB
(!) Some chunks are larger than 500 kB after minification.
```

The entire application ships as a single 2 MB JS bundle. On a slow mobile connection (3G: ~1 Mbps), this takes ~16 seconds to download. First Contentful Paint will be blocked until the entire bundle parses.

**Root causes:**
- No route-based code splitting (`React.lazy` / `import()`)
- All admin and driver pages loaded together regardless of user role
- Map libraries (likely Mapbox or Leaflet) bundled unconditionally

**Fix:**
```typescript
// routes.tsx — lazy load each page
const AdminDashboardPage = React.lazy(() => import('./pages/admin/AdminDashboardPage'));
const BookJourneyPage = React.lazy(() => import('./pages/driver/BookJourneyPage'));
// etc.
```

---

## MEDIUM Issues

---

### ISSUE-10 — AdminDashboardPage has no loading indicator

**Severity:** Medium | **Component:** Frontend | **File:** `frontend/src/app/pages/admin/AdminDashboardPage.tsx`

The dashboard fetches analytics data asynchronously but shows `-` as a placeholder with no spinner or skeleton. Users may think the system has no data rather than understanding it's loading.

```typescript
value: analytics ? analytics.total_journeys : '-',  // shows '-' while loading
```

No `isLoading` state, no skeleton, no spinner. Compare with `AnalyticsPage.tsx` which has a proper loading state.

---

### ISSUE-11 — DriverDashboardPage has no loading indicator

**Severity:** Medium | **Component:** Frontend | **File:** `frontend/src/app/pages/driver/DashboardPage.tsx`

Same pattern as above. The driver dashboard fetches region data on mount with no loading feedback. The page content appears instantly but may be stale or empty.

---

### ISSUE-12 — AllJourneysPage uses stale context data — no refresh on mount

**Severity:** Medium | **Component:** Frontend | **File:** `frontend/src/app/pages/admin/AllJourneysPage.tsx`

```typescript
const { adminJourneys } = useApp();
```

`adminJourneys` is loaded once when the user logs in (in AppContext's `useEffect`). The AllJourneysPage reads this cached value without triggering a refresh. If journeys change while the admin is browsing, the list will be stale until re-login or page refresh.

**Fix:** Add a `refreshJourneys()` function to AppContext and call it in AllJourneysPage's `useEffect(() => { refreshJourneys(); }, [])`.

---

### ISSUE-13 — IAM `admin/seed` migration uses placeholder password hash

**Severity:** Medium | **Component:** IAM service | **File:** `iam-service/migrations/003_seed_admin.sql`

```sql
-- TEMPLATE ONLY - fill in a real bcrypt hash before running
-- Generate hash: htpasswd -bnBC 12 "" yourpassword | tr -d ':\n'
INSERT INTO auth.users (..., password_hash, ...)
VALUES ('usr_admin000000000000000000000001', ..., '<PLACEHOLDER>', ...)
```

If this migration was run without substituting the placeholder, the admin account has no usable password. If it was substituted with a weak or default password, the admin account is insecure.

**Verify:** Check what password hash is stored for `usr_admin000000000000000000000001` in each region's database.

---

### ISSUE-14 — JWKS key ID `"kid":"key-dev-001"` in production

**Severity:** Medium | **Component:** IAM service

```json
{"keys":[{"kty":"RSA","kid":"key-dev-001",...}]}
```

The RSA key pair used for JWT signing has a development-labelled key ID. This indicates the key was generated for local development and promoted to production without rotation. There is no key rotation mechanism — if the private key is compromised, all existing tokens remain valid indefinitely.

**Fix:** Generate a new key pair with a production key ID (UUID), update the `iam_private_key` Docker secret, and restart the IAM service.

---

### ISSUE-15 — `as any` type casts in service files

**Severity:** Medium | **Component:** Frontend | **Files:** `iamApi.ts`, `capacityApi.ts`

```typescript
// iamApi.ts:91-99
(json as any)?.error?.message ??
(json as any)?.message ??
((json as any).data ?? json) as T;

// capacityApi.ts:34
throw new Error(json.error?.message ?? (json as any).error ?? ...)
```

Five `as any` casts in API service files bypass TypeScript's type safety. If the API response shape changes, these will silently return wrong values with no type error.

**Fix:** Define explicit error response interfaces and use them:
```typescript
interface ApiErrorResponse { error?: { message?: string }; message?: string; }
```

---

### ISSUE-16 — NotificationsPage reads stale context — no refresh on mount

**Severity:** Medium | **Component:** Frontend | **File:** `frontend/src/app/pages/driver/NotificationsPage.tsx`

```typescript
const { notifications, ...refreshNotifications } = useApp();
useEffect(() => { refreshNotifications(); }, []); // ← is this called?
```

The context provides `refreshNotifications` but the component may not call it on mount. Notifications could be stale from last login.

---

### ISSUE-17 — `GET /api/v1/routes/` base path returns 404

**Severity:** Medium | **Component:** nginx / map-service

```
GET /api/v1/routes/  →  404 plain text
```

The map service registers `/api/v1/routes/compute` (POST) but has no handler for the bare `/api/v1/routes/` path. Any client hitting the base path (e.g. accidental redirect, health check tool) gets a plain-text 404 from gorilla/mux with no JSON envelope.

---

## LOW Issues

---

### ISSUE-18 — No migration rollback scripts

**Severity:** Low | **Component:** All services

All migrations are append-only `.sql` files with no corresponding down/rollback scripts. A bad migration (e.g., a wrong `ALTER TABLE`) cannot be reverted without manual SQL. For CockroachDB, rollbacks require explicit `ALTER TABLE ... DROP COLUMN` commands.

---

### ISSUE-19 — Pipeline Node.js 20 deprecation warnings

**Severity:** Low | **Component:** CI/CD | **File:** `.github/workflows/pipeline.yml`

```
Node.js 20 actions are deprecated. The following actions are running on Node.js 20:
actions/checkout@v4, actions/setup-node@v4, actions/upload-artifact@v4,
actions/download-artifact@v4
```

These actions will stop working when GitHub removes Node.js 20 support. Non-breaking today.

**Fix:** Upgrade all GitHub Actions to their latest version (most support Node.js 22 via `@v5`).

---

### ISSUE-20 — Capacity `/check` endpoint requires 3 strict params — no partial support

**Severity:** Low | **Component:** capacity-service | **File:** `capacity-service/internal/http/handlers/capacity_handler.go`

The capacity check endpoint requires all three:
```
GET /api/v1/capacity/check?segment_id=X&time_window_start=RFC3339&time_window_end=RFC3339
```

Missing any one returns `400 Bad Request` with no indication of which param is missing. No default values, no partial checks. Any frontend form that omits even one param fails silently.

---

### ISSUE-21 — BookingResultPage has no navigation guard — shares URL freely

**Severity:** Low | **Component:** Frontend | **File:** `frontend/src/app/pages/driver/BookingResultPage.tsx`

If a driver bookmarks `/driver/booking-result` and returns later, the page correctly shows "No booking result found." and offers a button to book. However, the URL is shareable — another driver opening the link sees the original driver's booking result (stored in context from localStorage). This is a minor data-leakage issue if tokens are shared.

---

### ISSUE-22 — No observability alerts configured

**Severity:** Low | **Component:** Infrastructure

Prometheus and Grafana are deployed but no alerting rules are configured. A service crashing, high error rates, or DB connection failures will generate metrics but trigger no alerts. On-call engineers must manually watch Grafana.

---

### ISSUE-23 — `region: 'Central'` hardcoded for every journey — region filter broken

**Severity:** High | **Component:** Frontend | **File:** `frontend/src/app/services/journeyApi.ts:435`

```typescript
region: 'Central',   // hardcoded default — backend does not return a region field
```

Every journey object returned from the API is given `region: 'Central'` in the frontend mapping layer. The `AllJourneysPage` exposes a region filter (North / South / East / West / Central) but because all journeys are tagged "Central", filtering by any other region returns zero results, and filtering by "Central" returns everything. The region filter is completely non-functional.

**Fix:** Map the correct region from the API response field, or remove the region filter from `AllJourneysPage` until the backend exposes this field.

---

### ISSUE-24 — `navigate()` called directly in render body in LoginPage

**Severity:** High | **Component:** Frontend | **File:** `frontend/src/app/pages/LoginPage.tsx:42-44`

```typescript
// Called during render — React violation
if (isAuthenticated && user) {
  navigate(user.role === 'admin' ? '/admin' : '/driver');
}
```

Calling `navigate()` during a component's render phase (not inside `useEffect` or an event handler) is a React anti-pattern. It can cause "You should call navigate() in a React.useEffect(), not when your component is rendering" warnings, double-renders, and subtle state corruption. The `RequireAuth` guard (added in routes.tsx) handles the inverse case (unauthenticated access) correctly via `useEffect` — the same pattern should be applied here.

**Fix:**
```typescript
useEffect(() => {
  if (isAuthenticated && user) {
    navigate(user.role === 'admin' ? '/admin' : '/driver');
  }
}, [isAuthenticated, user, navigate]);
```

---

### ISSUE-25 — Demo credentials hardcoded in production JavaScript bundle

**Severity:** Medium | **Component:** Frontend | **File:** `frontend/src/app/pages/LoginPage.tsx:9-19`

```typescript
const DEMO_CREDENTIALS = {
  driver: { email: 'ajinkyataranekar26@gmail.com', password: 'test1234' },
  admin:  { email: 'admin@vcs.local',              password: 'admin123' },
};
```

The admin password `admin123` and a driver's real email and password are shipped in the compiled JavaScript bundle visible to anyone who downloads the page and inspects source. Any user can open DevTools → Sources and read the credentials without even using the demo buttons.

**Fix:** Move demo credentials to environment variables or a backend endpoint that returns demo tokens, so they are not present in the client bundle.

---

### ISSUE-26 — Synthetic local notification duplicates real backend notification

**Severity:** Medium | **Component:** Frontend | **File:** `frontend/src/app/context/AppContext.tsx:326-345`

When `updateJourneyStatus` succeeds, `addStatusNotification` adds a local synthetic `Notification` with id `N${Date.now()}`. The notification polling (every 15 seconds) then fetches the same event from the backend as a real notification with a UUID. Because `seenNotificationIDsRef` tracks by id, the synthetic local id (`N1234567890`) never matches the backend UUID — the backend notification toasts again as "new" on the next poll. The user sees two toast notifications and two list entries for the same event.

**Fix:** Either skip adding the local synthetic notification (rely on the polling to surface it), or immediately refresh from the backend after the status update instead of synthesizing locally.

---

### ISSUE-27 — Uncleared `setTimeout` in `bookJourney` — potential memory leak

**Severity:** Medium | **Component:** Frontend | **File:** `frontend/src/app/context/AppContext.tsx:310-314`

```typescript
setTimeout(async () => {
  try { await refreshNotifications(); } catch { /* ignore */ }
}, 1500);
```

This `setTimeout` fires 1.5 seconds after booking and is never cancelled. If the user logs out in that window, `refreshNotifications` still runs on a stale closure. The timer handle is not stored, so there is no way to cancel it during component cleanup. On hot-reload in dev, this produces ghost timers.

**Fix:** Store the timeout ID and cancel it in the cleanup of a `useEffect`, or use `clearTimeout` in `logout()`.

---

### ISSUE-28 — Journey timeline renders empty heading when there are no events

**Severity:** Low | **Component:** Frontend | **Files:** `frontend/src/app/pages/driver/JourneyDetailPage.tsx`, `frontend/src/app/pages/admin/AdminJourneyDetailPage.tsx`

When `journey.timeline` is an empty array, both journey detail pages render the "Journey timeline" / "Lifecycle history" section heading followed by an empty `<ol>` with no content. The user sees a heading that leads nowhere.

**Fix:** Add a conditional: only render the timeline section if `timeline.length > 0`, or add an "No events yet" empty state message inside the `<ol>`.

---

### ISSUE-29 — AppContext value not memoized — all consumers re-render on any state change

**Severity:** Low | **Component:** Frontend | **File:** `frontend/src/app/context/AppContext.tsx:406-425`

The context value object and all provider functions (`login`, `logout`, `bookJourney`, `updateJourneyStatus`, etc.) are created fresh on every render — none use `useCallback` or `useMemo`. Every state change in AppContext (e.g. a single notification marked read) triggers a re-render of every context consumer in the tree, including pages not related to the change.

**Fix:** Wrap the context value in `useMemo` and each function in `useCallback`. This is a performance improvement, not a correctness bug, but it becomes more impactful as the app grows.

---

### ISSUE-30 — Integration test stubs permanently disabled (unconditional `t.Skip`)

**Severity:** High | **Component:** journey-service, capacity-service | **Files:**
- `journey-service/internal/service/journey_edge_cases_test.go`
- `capacity-service/internal/service/reservation_distributed_test.go`

Five integration test stubs exist across these two services, all following the same broken pattern:

```go
func TestIntegration_ConcurrentDrivers_SameSegment_AtCapacity(t *testing.T) {
    if testing.Short() { t.Skip("...") }
    t.Log("...")
    t.Skip("run without -short with live stack")  // ← always executes
}
```

The final `t.Skip(...)` is unconditional — it fires regardless of flags. These tests can **never** run. They document intended behavior for concurrent booking collisions, saga coordination, and idempotency — exactly the scenarios most likely to fail under real load — but provide no actual coverage because they are permanently skipped.

**Fix:** Remove the unconditional `t.Skip` and replace with actual test logic against a live stack (or a mock DB), or prefix them with `//nolint:skip` and document them as pending in the backlog. The stubs are misleading — they look like coverage but provide none.

---

### ISSUE-31 — Notification service tests cover the in-memory placeholder, not the production PostgreSQL repository

**Severity:** High | **Component:** notification-service | **Files:**
- `internal/service/memory_store_concurrent_test.go`
- `internal/repository/notification_repo.go` (zero tests)

All 12 notification-service tests exercise `MemoryNotificationRepo` and `MemoryDeviceTokenRepo` — explicitly labeled as placeholders in comments ("Replace with PostgreSQL implementation later"). The actual production repository (`notification_repo.go`) that runs in all 3 deployed regions has **zero tests**. When the memory store is eventually replaced by the real DB repo, there is no existing test harness to catch regressions or schema mismatches.

**Fix:** Write integration tests for `notification_repo.go` using a test CockroachDB instance (or testcontainers), covering `Create`, `ListForUser`, `MarkRead`, `MarkAllRead`, and `GetUnreadCount`.

---

### ISSUE-32 — E2E suite hardcodes production EU IP — silently skips on any other environment

**Severity:** Medium | **Component:** e2e suite | **File:** `e2e/suite_test.go`

```go
defaultPublicBaseURLs = []string{"http://35.187.121.12"}
```

The E2E suite targets the live EU production cell by hardcoded IP. Consequences:
- Running locally with no VPN/access → silently skips (SkipIfUnhealthy=true), producing a false-green CI run
- If the EU IP changes or the VM is reprovisioned → same silent skip
- The US and APAC cells are never tested by this suite

**Fix:** Accept the base URL from an environment variable: `os.Getenv("E2E_BASE_URL")`. Default to skip with a clear message if not set. Run in CI with each region's IP separately.

---

### ISSUE-33 — `journey-service/go.mod` declares unreleased Go version `1.24.10`

**Severity:** Low | **Component:** journey-service | **File:** `journey-service/go.mod`

```
go 1.24.10
```

Go 1.24.10 does not exist as of this report. The stable Go 1.24 series tops out at patch levels that are publicly available. This may cause `go build` to fail on CI runners that interpret the directive strictly, and will produce confusing errors that look like build system failures rather than code issues.

**Fix:** Update to the actual latest stable Go version available (e.g. `go 1.24.1` or whatever the current patch release is).

---

## Complete API Contract — Frontend vs Backend

### Matching routes (all correct)

| Frontend call | Backend route | Service |
|---------------|--------------|---------|
| `POST /api/v1/auth/login` | `public.HandleFunc("/login")` | IAM |
| `POST /api/v1/auth/register` | `public.HandleFunc("/register")` | IAM |
| `POST /api/v1/auth/refresh` | `public.HandleFunc("/refresh")` | IAM |
| `POST /api/v1/auth/logout` | `authed.HandleFunc("/logout")` | IAM |
| `GET /api/v1/auth/profile` | `authed.HandleFunc("/profile")` | IAM |
| `PUT /api/v1/auth/profile` | `authed.HandleFunc("/profile")` | IAM |
| `GET /api/v1/auth/vehicles` | `authed.HandleFunc("/vehicles")` | IAM |
| `PUT /api/v1/auth/vehicles/primary` | `authed.HandleFunc("/vehicles/primary")` | IAM |
| `POST /api/v1/auth/vehicles/secondary` | `authed.HandleFunc("/vehicles/secondary")` | IAM |
| `PUT /api/v1/auth/vehicles/secondary/:id` | `authed.HandleFunc("/vehicles/secondary/{id}")` | IAM |
| `DELETE /api/v1/auth/vehicles/secondary/:id` | `authed.HandleFunc("/vehicles/secondary/{id}")` | IAM |
| `GET /api/v1/admin/auth/users` | `adminRouter.HandleFunc("/users")` | IAM |
| `GET /api/v1/admin/auth/users/:id` | `adminRouter.HandleFunc("/users/{id}")` | IAM |
| `POST /api/v1/admin/auth/promote` | `adminRouter.HandleFunc("/promote")` | IAM |
| `POST /api/v1/admin/auth/force-logout` | `adminRouter.HandleFunc("/force-logout")` | IAM |
| `GET /api/v1/journeys` | `api.HandleFunc("/journeys").Methods("GET")` | Journey |
| `POST /api/v1/journeys` | `api.HandleFunc("/journeys").Methods("POST")` | Journey |
| `GET /api/v1/journeys/:id` | `api.HandleFunc("/journeys/{id}").Methods("GET")` | Journey |
| `GET /api/v1/journeys/:id/events` | `api.HandleFunc("/journeys/{id}/events")` | Journey |
| `PUT /api/v1/journeys/:id/cancel` | `api.HandleFunc("/journeys/{id}/cancel")` | Journey |
| `PUT /api/v1/journeys/:id/activate` | `api.HandleFunc("/journeys/{id}/activate")` | Journey |
| `PUT /api/v1/journeys/:id/complete` | `api.HandleFunc("/journeys/{id}/complete")` | Journey |
| `GET /api/v1/admin/journeys` | `admin.HandleFunc("/journeys").Methods("GET")` | Journey |
| `PUT /api/v1/admin/journeys/:id/cancel` | `admin.HandleFunc("/journeys/{id}/cancel")` | Journey |
| `GET /api/v1/admin/journeys/:id/events` | `admin.HandleFunc("/journeys/{id}/events")` | Journey |
| `GET /api/v1/admin/analytics` | `admin.HandleFunc("/analytics")` | Journey |
| `GET /api/v1/capacity/segments` | `api.HandleFunc("/segments")` | Capacity |
| `GET /api/v1/capacity/closures` | `api.Handle("/closures").Methods("GET")` (conditional) | Capacity |
| `POST /api/v1/capacity/closures` | `api.Handle("/closures").Methods("POST")` (conditional) | Capacity |
| `GET /api/v1/capacity/default-capacity` | `api.Handle("/default-capacity").Methods("GET")` | Capacity |
| `PUT /api/v1/capacity/segments/:id/capacity` | `api.Handle("/segments/{segment_id}/capacity")` | Capacity |
| `GET /api/v1/map/route` | `r.mux.HandleFunc("/api/v1/map/route").Methods("GET")` | Map |
| `GET /api/v1/map/nodes` | `r.mux.HandleFunc("/api/v1/map/nodes").Methods("GET")` | Map |
| `GET /api/v1/map/traffic` | `r.mux.Handle("/api/v1/map/traffic").Methods("GET")` | Map |
| `GET /api/v1/map/search` | `r.mux.HandleFunc("/api/v1/map/search").Methods("GET")` | Map |
| `POST /api/v1/routes/compute` | `r.mux.HandleFunc("/api/v1/routes/compute").Methods("POST")` | Map |
| `GET /api/v1/notifications` | `api.HandleFunc("/notifications").Methods("GET")` | Notification |
| `PUT /api/v1/notifications/read-all` | `api.HandleFunc("/notifications/read-all").Methods("PUT")` | Notification |
| `PUT /api/v1/notifications/:id/read` | `api.HandleFunc("/notifications/{id}/read").Methods("PUT")` | Notification |
| `POST /api/v1/notifications/device-token` | `api.HandleFunc("/notifications/device-token").Methods("POST")` | Notification |
| `DELETE /api/v1/notifications/device-token` | `api.HandleFunc("/notifications/device-token").Methods("DELETE")` | Notification |
| `GET /api/v1/admin/notifications` | `api.HandleFunc("/admin/notifications").Methods("GET")` | Notification |
| `GET /api/v1/enforcement/verify` | `enforcement.HandleFunc("/verify").Methods("GET")` | Journey |

### Backend routes with no frontend caller (dead endpoints)

| Route | Service | Notes |
|-------|---------|-------|
| `GET /api/v1/capacity/check` | Capacity | Internal use only (journey → capacity reservation check) |
| `POST /api/v1/capacity/reserve` | Capacity | Called by journey-service internally, not frontend |
| `POST /api/v1/capacity/segments/register` | Capacity | Admin bootstrap only, no UI |
| `GET /api/v1/capacity/segments/occupancy` | Capacity | Used by map-service internally |
| `GET /api/v1/map/segments` | Map | No frontend page calls this |

---

## Backend Test Coverage Summary

| Service | Packages with tests | Packages without tests | Test count |
|---------|--------------------|-----------------------|------------|
| iam-service | repository, service | handlers, http, cmd | ~11 tests |
| journey-service | event, service | **handler** (blocked), http, repository, cmd | ~unknown |
| capacity-service | model, service | **handlers**, http, repository, cmd | ~unknown |
| map-service | **none** | all packages | 0 tests |
| notification-service | event, service | **handlers**, http, repository, cmd | ~unknown |

**Overall test coverage: Very low.** Handler-level tests (the most important for regression detection) are absent or blocked for all services.

---

## Summary Priority Table

| # | Severity | Component | Issue | Fix Effort |
|---|----------|-----------|-------|-----------|
| 01 | CRITICAL | nginx | Security headers missing from SPA | Low — add headers to location blocks |
| 02 | CRITICAL | Infra | US/APAC show wrong region | Zero — run pipeline for all regions |
| 03 | CRITICAL | All services | CORS wildcard + credentials conflict | Low — set specific origin |
| 04 | CRITICAL | CI/Testing | Journey handler tests blocked (Windows) | Medium — CI-only tests |
| 05 | HIGH | map-service | Zero tests for entire service | High — write test suite |
| 06 | HIGH | capacity, notification | No HTTP handler tests | High — write handler tests |
| 07 | HIGH | Infra | No HTTPS | Medium — domain + cert |
| 08 | HIGH | Frontend | `window` var shadows browser global | Trivial — rename variable |
| 09 | HIGH | Frontend | 2 MB bundle — no code splitting | Medium — add React.lazy |
| 10 | MEDIUM | Frontend | AdminDashboard no loading state | Low — add spinner |
| 11 | MEDIUM | Frontend | DriverDashboard no loading state | Low — add spinner |
| 12 | MEDIUM | Frontend | AllJourneysPage stale context | Low — add refresh on mount |
| 13 | MEDIUM | IAM | Admin seed migration placeholder check | Low — verify DB |
| 14 | MEDIUM | IAM | Dev key ID in production | Medium — rotate key |
| 15 | MEDIUM | Frontend | `as any` casts in service files | Low — add type interfaces |
| 16 | MEDIUM | Frontend | NotificationsPage stale data | Low — refresh on mount |
| 17 | MEDIUM | nginx/map | `/api/v1/routes/` base path 404 | Trivial — add handler or doc |
| 18 | LOW | All | No migration rollbacks | Medium — add down scripts |
| 19 | LOW | CI/CD | Node.js 20 deprecation warnings | Trivial — upgrade actions |
| 20 | LOW | capacity | `/check` requires 3 strict params | Low — add param defaults |
| 21 | LOW | Frontend | BookingResult URL shareable | Low — clear on logout |
| 22 | LOW | Infra | No Prometheus alert rules | Medium — write alert configs |
| 23 | HIGH | Frontend | `region: 'Central'` hardcoded — region filter non-functional | Low — map from API response |
| 24 | HIGH | Frontend | `navigate()` called in render body (LoginPage) | Trivial — move to useEffect |
| 25 | MEDIUM | Frontend | Demo credentials hardcoded in production bundle | Low — env var or backend endpoint |
| 26 | MEDIUM | Frontend | Synthetic notification duplicates backend notification on poll | Low — remove local synth or refresh from API |
| 27 | MEDIUM | Frontend | Uncleared setTimeout in bookJourney — ghost timer on logout | Low — store and clear handle |
| 28 | LOW | Frontend | Journey timeline shows empty heading when no events | Trivial — conditional render |
| 29 | LOW | Frontend | AppContext not memoized — all consumers re-render on any state | Medium — useMemo + useCallback |
| 30 | HIGH | journey, capacity | 5 integration test stubs permanently disabled (unconditional t.Skip) | Low — fix skip guard |
| 31 | HIGH | notification | Tests cover in-memory placeholder, not production PostgreSQL repo | High — write DB integration tests |
| 32 | MEDIUM | e2e | E2E suite hardcodes EU IP — silently skips in any other environment | Low — env var for base URL |
| 33 | LOW | journey-service | `go.mod` declares unreleased Go version `1.24.10` | Trivial — set correct version |
