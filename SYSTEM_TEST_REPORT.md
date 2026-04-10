# System Test Report — Distributed Vehicle Capacity System

> **Date:** 2026-04-09
> **Tester:** Deepika Nag
> **Branch:** `iam-auth-service`
> **Regions tested:** EU (35.187.121.12) · US (34.138.242.217) · APAC (34.80.180.64)

---

## What's Working (All 3 Regions)

| Service | Endpoint | Result |
|---------|----------|--------|
| IAM | `POST /api/v1/auth/login` | 401 - IAM responding correctly |
| IAM | `POST /api/v1/auth/register` | 400 - validation firing |
| IAM | `POST /api/v1/auth/refresh` | 400 - token required, correct |
| IAM | `GET /.well-known/jwks.json` | 200 application/json ✅ |
| IAM Admin | `GET /api/v1/admin/auth/users` | 401 - reaching IAM correctly ✅ |
| IAM Admin | `POST /api/v1/admin/auth/promote` | 401 - reaching IAM correctly ✅ |
| Journey | `GET /api/v1/journeys` | 401 - auth required ✅ |
| Journey | `GET /api/v1/admin/journeys` | 401 - auth required ✅ |
| Capacity | `GET /api/v1/capacity/segments` | 200 OK ✅ |
| Notifications | `GET /api/v1/notifications` | 401 - auth required ✅ |
| Notifications | `GET /api/v1/admin/notifications` | 401 - auth required ✅ |
| Enforcement | `GET /api/v1/enforcement/verify` | 401 - auth required ✅ |
| SPA | `GET /` | 200 HTML ✅ |
| Infra | `GET /nginx-health` | 200 OK ✅ |
| Build | All 5 Go services | Compile clean ✅ |

---

## Fixed Issues (This Session)

| Issue | Fix | Files Changed |
|-------|-----|---------------|
| `/.well-known/jwks.json` returned HTML | Added `location /.well-known/` block in nginx.conf | `nginx/nginx.conf`, `docker-stack.yml` |
| `/api/v1/admin/auth/` routed to journey-service | Added specific `location /api/v1/admin/auth/` block before broader `/api/v1/admin/` | `nginx/nginx.conf` |
| Admin page crash (`Cannot read properties of undefined (reading 'bg')`) | Added `expired` to `JourneyStatus` type and `StatusChip` config; added safety fallback | `frontend/src/app/types.ts`, `frontend/src/app/components/ui/StatusChip.tsx` |
| No auth guards on routes - anyone could access `/admin` directly | Added `RequireAuth` guard component in router | `frontend/src/app/routes.tsx` |
| External `nginx_conf` Docker config overriding image on every deploy | Removed `configs: nginx_conf: external: true` from docker-stack.yml | `docker-stack.yml` |

---

## Bugs Found

### BUG-1 - CRITICAL: US and APAC report wrong region "EU"

**All 3 regions return `{"region":"EU"}`**

```
curl http://34.138.242.217/api/v1/region  →  {"region":"EU"}   # should be US
curl http://34.80.180.64/api/v1/region    →  {"region":"EU"}   # should be APAC
```

**Root cause:** `docker-stack.yml` defines `REGION: ${REGION:-EU}` which defaults to EU. The CI pipeline never sets `REGION=US` or `REGION=APAC` when deploying those cells. Every cell picks up the default.

**Fix:** In `.github/workflows/pipeline.yml`, update the deploy-us and deploy-apac steps:

```yaml
# deploy-us - add REGION=US to the docker stack deploy command
GITHUB_REPOSITORY=\$REPO IMAGE_TAG=latest REGION=US CRDB_JOIN='...' \
  docker stack deploy --with-registry-auth --prune -c ~/vcs/docker-stack.yml vcs

# deploy-apac - add REGION=APAC
GITHUB_REPOSITORY=\$REPO IMAGE_TAG=latest REGION=APAC CRDB_JOIN='...' \
  docker stack deploy --with-registry-auth --prune -c ~/vcs/docker-stack.yml vcs
```

**Impact:** Region-aware features (the EU/US/APAC badge on admin dashboard) show wrong data on all cells.

---

### BUG-2 - CRITICAL: `/api/v1/capacity/closures` endpoint does not exist

The frontend `SegmentClosuresPage` and `capacityApi.ts` call:
```
GET  /api/v1/capacity/closures
POST /api/v1/capacity/closures
```

The capacity service only registers these routes:
```
POST /api/v1/capacity/reserve
GET  /api/v1/capacity/check
POST /api/v1/capacity/segments/register
GET  /api/v1/capacity/segments
GET  /api/v1/capacity/segments/occupancy
```

No `/closures` route exists anywhere in the backend. The Closures admin page will fail on every load with a 404.

**Fix:** Either implement the `/closures` endpoint in capacity-service, or wire the frontend to use the existing capacity endpoints.

---

### BUG-3 - CRITICAL: `adminAnalytics(window)` — wrong argument in AnalyticsPage

```typescript
// frontend/src/app/pages/admin/AnalyticsPage.tsx line 52
adminAnalytics(window)   // BUG: passing browser's window object
  .then(setKpis)
```

`adminAnalytics()` expects a time window string (`'24h'`, `'7d'`, etc.). The variable `window` here resolves to the browser global `window` object, not a time window string. The API call sends garbage and the Analytics page shows no data.

Compare with the correct call in `AdminDashboardPage.tsx`:
```typescript
adminAnalytics('24h').then(setAnalytics)  // correct
```

**Fix:** Change `adminAnalytics(window)` to `adminAnalytics(selectedWindow)` (or whatever the dropdown state variable is named in that component).

---

### BUG-4 - CRITICAL: Security headers missing from SPA responses

nginx `add_header` in a child `location` block **overrides** (does not inherit) the server-block headers. The `location /` block adds `Cache-Control` which silently drops all security headers:

```
Actual headers on GET /:
  Content-Type: text/html
  Cache-Control: max-age=3600, public, no-transform
  X-Content-Type-Options: MISSING  ← clickjacking/MIME risk
  X-Frame-Options: MISSING
  X-XSS-Protection: MISSING
```

Security headers only appear on API responses because the backend services set them directly.

**Fix:** Add security headers with `always` flag to each location block in `nginx/nginx.conf`, or use `map` + `add_header` at server level with the `always` directive:

```nginx
# Add to each location block (including location /):
add_header X-Content-Type-Options nosniff always;
add_header X-Frame-Options DENY always;
add_header X-XSS-Protection "1; mode=block" always;
```

---

### ISSUE-5 - HIGH: `POST /api/v1/map/route` returns 405

The map service registers `/api/v1/map/route` as **GET only**:
```go
r.mux.HandleFunc("/api/v1/map/route", r.mapHandler.GetRoute).Methods("GET")
```

Any frontend call using `POST /api/v1/map/route` always gets 405 Method Not Allowed.

The correct POST endpoint for route computation is:
```
POST /api/v1/routes/compute
```

**Fix:** Audit all frontend `mapApi.ts` calls and ensure `POST` uses `/api/v1/routes/compute` not `/api/v1/map/route`.

---

### ISSUE-6 - HIGH: No HTTPS on any region

All 3 cells run on plain `http://`. JWTs, passwords, and all personal data are transmitted in cleartext. Browser shows "Not secure" on every page.

**Fix:** Requires a domain name + TLS certificate. Options:
- GCP Cloud Load Balancer with Google-managed SSL cert (easiest for this setup)
- Certbot + Let's Encrypt directly on the VMs

---

### ISSUE-7 - HIGH: CORS `Allow-Origin: *` with `Allow-Credentials: true`

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Credentials: true
```

Browsers reject credentialed cross-origin requests to wildcard origins (W3C CORS spec). This will silently break any cross-origin authenticated requests.

**Fix:** Change to a specific origin:
```go
w.Header().Set("Access-Control-Allow-Origin", "https://yourdomain.com")
```
Or if wildcard is needed, remove `Allow-Credentials: true`.

---

### ISSUE-8 - MEDIUM: `/api/v1/capacity/check` requires 3 strict params

The endpoint requires all 3 parameters or returns 400:
```
segment_id          (required)
time_window_start   (required, RFC3339)
time_window_end     (required, RFC3339)
```

Easy to call incorrectly. Frontend must supply all three.

---

### ISSUE-9 - MEDIUM: JWKS key ID `"kid":"key-dev-001"` in production

The RSA signing key has a dev-labelled key ID. Signals the IAM private key was generated for development and never rotated for production. Not functionally broken but a security hygiene issue.

---

### ISSUE-10 - MEDIUM: AnalyticsPage time filter passes wrong variable

Related to BUG-3. The time period dropdown selection variable is not being passed to `adminAnalytics()`. Check what state variable holds the selected window (`'24h'`, `'7d'`, etc.) and pass that instead of `window`.

---

### ISSUE-11 - LOW: Pipeline Node.js 20 deprecation warnings

`actions/checkout@v4`, `actions/setup-node@v4`, `actions/upload-artifact@v4`, `actions/download-artifact@v4` emit deprecation warnings for Node.js 20. Non-breaking now but will eventually fail. Upgrade to `@v5` when convenient.

---

### ISSUE-12 - LOW: No DB migration rollback scripts

Migrations are append-only `.sql` files with no down/rollback scripts. A bad migration cannot be reverted without manual SQL intervention.

---

## Summary Table

| # | Severity | Issue | Owner | Status |
|---|----------|-------|-------|--------|
| 1 | CRITICAL | US + APAC show wrong region "EU" | Infra | Not fixed |
| 2 | CRITICAL | `/capacity/closures` endpoint missing - Closures page broken | Backend | Not fixed |
| 3 | CRITICAL | `adminAnalytics(window)` wrong arg - Analytics page broken | Frontend | Not fixed |
| 4 | CRITICAL | Security headers missing from SPA (nginx inheritance bug) | Infra | Not fixed |
| 5 | HIGH | `POST /map/route` returns 405 - method mismatch | Frontend/Backend | Not fixed |
| 6 | HIGH | No HTTPS on any region | Infra | Not fixed |
| 7 | HIGH | CORS `Allow-Origin: *` + `Credentials: true` conflict | Backend | Not fixed |
| 8 | MEDIUM | `/capacity/check` strict 3-param requirement | Backend | By design |
| 9 | MEDIUM | JWKS key ID `key-dev-001` in production | IAM | Not fixed |
| 10 | MEDIUM | AnalyticsPage time filter variable bug | Frontend | Not fixed |
| 11 | LOW | Node.js 20 deprecation in pipeline | CI/CD | Non-breaking |
| 12 | LOW | No migration rollback scripts | Backend | Acceptable |
| - | FIXED | nginx JWKS routing (returned HTML) | Deepika Nag | Fixed |
| - | FIXED | nginx admin/auth routing (went to journey-service) | Deepika Nag | Fixed |
| - | FIXED | `expired` status crash on Admin dashboard | Deepika Nag | Fixed |
| - | FIXED | No auth guards - unauthenticated /admin access | Deepika Nag | Fixed |
| - | FIXED | External nginx_conf override reverted fixes on redeploy | Deepika Nag | Fixed |
