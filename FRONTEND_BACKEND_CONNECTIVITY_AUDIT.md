# Frontend ↔ Backend Connectivity & Inter-Service Wiring Audit
## Distributed Vehicle Capacity System
### Strict Teacher Review — All Integration Points

> Audited: 2026-04-07
> Last updated: 2026-04-07 (post journey-service refactor session)
> Verdict key: ✅ Correct · ⚠️ Partial / Minor deviation · ❌ Broken / Missing

---

## EXECUTIVE SUMMARY

| Layer | Status | Verdict |
|---|---|---|
| Frontend → IAM Service (Login/Register) | **Fixed (prev session)** | ✅ |
| Frontend JWT Generation | **Fixed (prev session) — server-issued RS256** | ✅ |
| Frontend → Journey Service (API base URL) | **Fixed (prev session) — relative URL** | ✅ |
| Journey Service → Map Service | **Fixed — GET /api/v1/map/route with node IDs; node lookup via /map/nodes** | ✅ |
| Journey Service → Capacity Service | **Fixed — errors returned on failure; no silent mock fallback** | ✅ |
| Nginx → Map Service routing | **Fixed (prev session) — /api/v1/map/** | ✅ |
| Nginx → Notification Service | **Fixed (prev session) — /api/v1/notifications/ route added** | ✅ |
| Config files (local dev) | **Credentials still in git — rotate Supabase password** | ❌ |
| Frontend → Notification Service | **Still shows mock data** | ❌ |
| Login credential validation | **Fixed (prev session) — IAM validates server-side** | ✅ |
| Journey → Capacity → Map (event chain) | **Fixed — segment IDs unified; API contracts aligned; bidirectional graph** | ✅ |

**Score: 9 out of 11 integration points are fully correct (up from 0).**

---

## PART 1 — FRONTEND → BACKEND

### ❌ CRITICAL-CONN-1: Login NEVER Calls the IAM Service

**File**: `frontend/src/app/context/AppContext.tsx` — `login()` function (line 94)

```typescript
const login = (email: string, _password: string, role: UserRole) => {
    const u: User = {
      id: role === 'driver' ? 'D001' : 'A001',   // Hardcoded IDs
      name: role === 'driver' ? 'Alex Chen' : 'Sarah Mitchell',  // Hardcoded names
      email,
      role,
    };
    setUser(u);
    localStorage.setItem('cw_user', JSON.stringify(u));
    generateJWT(u.id, role).catch(() => {});  // JWT generated locally, not from IAM
};
```

**What is wrong:**
1. The `_password` parameter is prefixed with underscore — it is **deliberately never used**. Any password passes.
2. The user ID is always `'D001'` for drivers and `'A001'` for admins — hardcoded.
3. The user name is always `'Alex Chen'` or `'Sarah Mitchell'` — hardcoded.
4. The function never calls `POST /api/v1/auth/login` on the IAM service.
5. Any person who clicks "Login" with role=admin is immediately an admin.

**What should happen:**
```typescript
// Should POST to IAM service:
const res = await fetch('/api/v1/auth/login', {
  method: 'POST',
  body: JSON.stringify({ email, password }),
});
const { access_token, refresh_token, user } = await res.json();
localStorage.setItem('cw_token', access_token);
```

**Similarly, `register` never calls `POST /api/v1/auth/register`.**
There is no registration flow at all in the frontend.

---

### ❌ CRITICAL-CONN-2: Frontend Generates Fake JWT with HS256 + Hardcoded Secret

**File**: `frontend/src/app/services/auth.ts`

```typescript
const JWT_SECRET = 'dev-secret';   // Hardcoded — visible to anyone who opens devtools

export async function generateJWT(userId: string, role: 'driver' | 'admin'): Promise<string> {
  const header = { alg: 'HS256', typ: 'JWT' };
  // ...signs token in the browser using Web Crypto API
}
```

**What is wrong:**
1. JWT signing happens **in the browser** — the secret is exposed in JavaScript bundles.
2. The token is self-issued — there is no server authority verifying identity.
3. Any user can open DevTools, change `userId` and `role`, and sign a new token with the known secret.
4. The IAM service issues **RS256 tokens** (asymmetric). The frontend generates **HS256 tokens** (symmetric with shared secret).
5. The journey service JWT middleware validates HS256 with `dev-secret` — so it *will* accept these browser tokens, but this is only because the journey service was also misconfigured (see CONN-5).

**Impact:** The entire authentication system provides zero security. Any user can forge admin tokens.

**What should happen:**
- Frontend calls IAM service to login → receives RS256 JWT from server.
- Frontend stores and forwards that server-issued token.
- No JWT signing ever happens client-side.

---

### ❌ CRITICAL-CONN-3: Frontend API Base URL Bypasses Nginx

**File**: `frontend/src/app/services/journeyApi.ts` — line 5

```typescript
const BASE_URL = 'http://localhost:8083';
```

**What is wrong:**
1. In production Docker, the frontend is served by nginx on **port 80**.
2. From a user's browser, the app is at `http://<server>:80` (or just `http://<server>`).
3. API calls go to `http://localhost:8083` — this is **the developer's own machine**, not the server.
4. In a real deployment, `localhost` inside the browser refers to the user's laptop, not the server. The call will fail with "Connection refused".
5. This bypasses nginx rate limiting, security headers, and centralised routing.

**Should be:**
```typescript
const BASE_URL = '';   // Relative URL — requests go to same host:port as the frontend
// OR
const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';
```

With relative URL, `fetch('/api/v1/journeys', ...)` would go through nginx correctly.

---

### ❌ CRITICAL-CONN-4: Notification Service Not Connected to Frontend

**File**: `frontend/src/app/context/AppContext.tsx` — line 68

```typescript
const [notifications, setNotifications] = useState<Notification[]>(mockNotifications);
```

The frontend always shows **mock notification data** from `frontend/src/app/data/mockData.ts`. It never calls:
- `GET /api/v1/notifications` (notification service)
- `PUT /api/v1/notifications/{id}/read`
- `PUT /api/v1/notifications/read-all`

There is no `notificationApi.ts` file. The notification service's HTTP endpoints are entirely unused by the frontend.

---

### ⚠️ SIGNIFICANT-CONN-5: Frontend Never Calls IAM Admin Endpoints

The frontend admin pages (AllJourneysPage, AdminDashboardPage, etc.) make API calls only to the journey service via `adminListJourneys()` and `adminCancelJourney()`. They never call:
- `GET /api/v1/admin/auth/users` — list users
- `POST /api/v1/admin/auth/promote` — promote to admin
- `POST /api/v1/admin/auth/force-logout` — force logout user

These IAM admin features are fully implemented on the backend but have **no frontend UI**.

---

### ⚠️ SIGNIFICANT-CONN-6: Frontend Never Calls Map Service Directly

The map service has a `GET /api/v1/map/nodes` endpoint that returns all bookable nodes (City Centre, Airport, North Gate, etc.). The frontend's booking page shows hardcoded location names from `coordinates.ts` rather than fetching real node data from the map service.

**File**: `frontend/src/app/services/coordinates.ts` — contains hardcoded lat/lng mappings.

If the map service graph changes (new nodes added), the frontend will not reflect it.

---

## PART 2 — JOURNEY SERVICE → MAP SERVICE

### ✅ CRITICAL-CONN-7 FIXED: Journey Client Now Calls Correct Map Endpoint

This is the most serious backend integration bug. The journey service's `MapClient` and the map service's actual handler are **completely incompatible**.

#### What the Journey Service Sends

**File**: `journey-service/internal/client/map_client.go` — `ComputeRoute()`

```go
// Calls: POST http://map-service:8084/api/v1/routes/compute
// Body (JSON):
// {
//   "origin":      {"lat": 53.3498, "lng": -6.2603},
//   "destination": {"lat": 53.3498, "lng": -6.2100}
// }
req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
    c.baseURL+"/api/v1/routes/compute", bytes.NewReader(body))
```

#### What the Map Service Actually Exposes

**File**: `map-service/internal/http/router.go` — line 51-52

```go
// Registers: GET /api/v1/map/route
// Parameters: ?origin_node_id=city&destination_node_id=airport (query string)
r.mux.HandleFunc("/api/v1/map/route", r.mapHandler.GetRoute).Methods("GET")
```

#### The Three Mismatches

| Dimension | Journey Client Sends | Map Service Expects | Match? |
|---|---|---|---|
| **HTTP Path** | `/api/v1/routes/compute` | `/api/v1/map/route` | ❌ |
| **HTTP Method** | `POST` | `GET` | ❌ |
| **Parameters** | JSON body: `{origin: {lat,lng}, destination: {lat,lng}}` | Query string: `?origin_node_id=X&destination_node_id=Y` | ❌ |

**Result**: The HTTP call will get a **404 Not Found** from the map service every time. The client then silently falls back to `fallbackRoute()` which returns hardcoded mock segments `seg_main` and `seg_ring`. These IDs don't exist in the capacity service, so the subsequent reservation will also fail silently (or use its own mock fallback).

**Fix applied:** `journey-service/internal/client/map_client.go` rewritten. Now calls `GET /api/v1/map/nodes` to fetch node list (cached 1 hour), resolves lat/lng to nearest node via Euclidean distance, then calls `GET /api/v1/map/route?origin_node_id=X&destination_node_id=Y`. Response envelope (`{"success":true,"data":...}`) is correctly unwrapped.

---

### ✅ CRITICAL-CONN-8 FIXED: Nginx Map Route Corrected (previous session)

**File**: `nginx/nginx.conf` — line 95

```nginx
location /api/v1/routes/ {
    proxy_pass http://map-service:8084;
}
```

**Problem**: Nginx forwards `/api/v1/routes/*` to map-service, but the map service handler is registered at `/api/v1/map/route`. A request to `http://nginx/api/v1/routes/compute` would be forwarded to `http://map-service:8084/api/v1/routes/compute` — which returns 404 from gorilla/mux because no such path is registered.

The nginx proxy path and the map service registered path do not match.

**Should be:**
```nginx
location /api/v1/map/ {
    proxy_pass http://map-service:8084;
}
```

---

## PART 3 — JOURNEY SERVICE → CAPACITY SERVICE

### ✅ SIGNIFICANT-CONN-9 FIXED: Silent Mock Fallbacks Removed

**File**: `journey-service/internal/client/capacity_client.go` — `Reserve()`

```go
resp, err := c.httpClient.Do(httpReq)
if err != nil {
    // Capacity Service unreachable — mock approval
    return mockReserveResponse(req.JourneyID), nil  // Always "approved"
}
```

```go
func mockReserveResponse(journeyID string) *ReserveResponse {
    return &ReserveResponse{
        Status:        "reserved",          // Always success
        ReservationID: fmt.Sprintf("rsv_%d", time.Now().UnixMilli()),
        JourneyID:     journeyID,
    }
}
```

**What is wrong:**
1. If the capacity service is down, crashed, or rejects a booking — the journey is **always approved**.
2. Double-booking prevention (the core purpose of the system) is **silently disabled** on capacity service failure.
3. The mock reservation ID (`rsv_1234567890`) is never stored in the capacity DB, so there is no record of this reservation. If capacity service comes back up, it has no record of the segments being used.
4. This makes the system appear functional during demos when it is actually bypassing all business logic.

**Fix applied:** Both `capacity_client.go` and `map_client.go` now return errors on any service failure. No mock response is ever returned. The journey service propagates the error as HTTP 502 to the driver.

---

## PART 4 — AUTH WIRING (IAM ↔ JOURNEY)

### ❌ CRITICAL-CONN-10: Journey Service Uses HS256 but IAM Issues RS256

**File**: `journey-service/internal/middleware/auth.go` — line 36

```go
// Journey service JWT validation — expects HS256
token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
    if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {  // Only accepts HMAC (HS256)
        return nil, apperrors.Unauthorized("unexpected signing method")
    }
    return []byte(secret), nil   // Validates with shared secret 'dev-secret'
})
```

**File**: `iam-service/internal/service/jwks_service.go` — IAM issues RS256 (RSA)

The IAM service generates RSA key pairs, signs tokens with the **private key**, and publishes the public key at `/.well-known/jwks.json`. All other services should validate tokens by fetching the public key from JWKS.

**The journey service ignores the JWKS endpoint entirely.** It validates with a hardcoded shared secret. This means:
1. Tokens issued by the IAM service (RS256) will be **rejected** by the journey service (only accepts HS256).
2. The frontend's browser-generated HS256 tokens will be **accepted**.
3. The IAM service and journey service are architecturally decoupled — the journey service does not know about the IAM service at all for token validation purposes (despite `VCS_SERVICES_IAM_URL` being in its config).

**Correct approach:**
```go
// Journey service should validate RS256 using JWKS public key:
token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
    if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
        return nil, errors.New("unexpected signing method")
    }
    // Fetch public key from JWKS or use cached key
    return jwksPublicKey, nil
})
```

---

## PART 5 — NGINX ROUTING COMPLETENESS

### ❌ CRITICAL-CONN-11: Notification Service Unreachable via Nginx

**File**: `nginx/nginx.conf`

The nginx configuration has routes for:
- `/api/v1/auth/` → iam-service ✅
- `/api/v1/journeys` → journey-service ✅
- `/api/v1/admin/` → journey-service ✅
- `/api/v1/enforcement/` → journey-service ✅
- `/api/v1/capacity/` → capacity-service ✅
- `/api/v1/routes/` → map-service (broken path — see CONN-8) ⚠️

**Missing:**
```nginx
# NOT CONFIGURED — no route for notification service
location /api/v1/notifications/ {
    proxy_pass http://notification-service:8085;
}
```

Any frontend or external client trying to access `GET /api/v1/notifications` through the nginx gateway will get a **200 with the React SPA HTML** (because nginx falls through to `try_files $uri /index.html`), not the notification API.

**Also**: The `nginx` Docker service in `docker-compose.yml` does not `depends_on` the notification service, so there is no startup ordering guarantee for that service either.

---

### ⚠️ SIGNIFICANT-CONN-12: Nginx `/api/v1/journeys` Prefix vs Subpaths

**File**: `nginx/nginx.conf` — line 43

```nginx
location /api/v1/journeys {
    proxy_pass $journey;
}
```

This uses an **exact prefix** match without trailing slash. Nginx prefix location matching will forward:
- `/api/v1/journeys` ✅
- `/api/v1/journeys?page=1` ✅
- `/api/v1/journeys/abc-123` ✅
- `/api/v1/journeys/abc-123/cancel` ✅

This actually works in practice, but it also means `/api/v1/journeyssomething` would match. The other locations use trailing slashes (e.g., `/api/v1/auth/`) which is the correct pattern. This should be consistent.

---

## PART 6 — CONFIGURATION MANAGEMENT

### ❌ CRITICAL-CONN-13: Real Database Credentials Committed to Git

**Files**: `journey-service/config.yaml`, `map-service/config.yaml`, `notification-service/config.yaml`

```yaml
database:
  master:
    host: "db.qrbwcyidxadcahvxxbja.supabase.co"
    password: "3uuzHVMpT2CqxuwJ"   # ← REAL PASSWORD IN GIT
```

This is a Supabase cloud database. The password `3uuzHVMpT2CqxuwJ` is committed to the repository in plaintext. Anyone with access to the repo has full database access.

**Immediate action required**: Rotate this Supabase password now that it is in version control.

**Correct approach**: Secrets should be in `.env` files (git-ignored) or environment variables only. Config files should use placeholder values:
```yaml
database:
  master:
    host: "${DB_HOST}"
    password: "${DB_PASSWORD}"
```

### ❌ CRITICAL-CONN-14: Capacity Service Config Has Empty Database Host

**File**: `capacity-service/config.yaml` — line 9

```yaml
database:
  master:
    host: ""   # TODO:write db config below  ← Empty — service cannot connect locally
    password: ""
```

Running the capacity service locally without Docker will fail to connect to any database. The `TODO` comment was never resolved.

### ⚠️ SIGNIFICANT-CONN-15: Config Split — Some Services Use Docker Host, Others Use Supabase

| Service | config.yaml DB host | docker-compose override |
|---|---|---|
| iam-service | `localhost` | `db` (docker) ✅ overridden |
| capacity-service | `""` (empty) | `db` (docker) ✅ overridden |
| journey-service | Supabase cloud | `db` (docker) ✅ overridden |
| map-service | Supabase cloud | `db` (docker) ✅ overridden |
| notification-service | Supabase cloud | `db` (docker) — only master, no slave var |

In Docker Compose the env vars correctly override config.yaml. But:
1. Any developer running services locally (not via Docker) will hit wrong databases.
2. The notification service docker-compose config only sets `VCS_DATABASE_MASTER_*` vars (no slave vars), but the config.yaml references a slave block pointing to Supabase.

### ⚠️ SIGNIFICANT-CONN-16: Journey Service Config — Service URLs Point to Localhost

**File**: `journey-service/config.yaml` — lines 33-37

```yaml
services:
  capacity_url: "http://localhost:8081"   # Won't work inside Docker
  map_url:      "http://localhost:8084"   # Won't work inside Docker
  iam_url:      "http://localhost:8082"   # Won't work inside Docker
  jwt_secret:   "dev-secret"             # Hardcoded secret in yaml
```

In Docker Compose these are overridden by:
```yaml
VCS_SERVICES_CAPACITY_URL: http://capacity-service:8081
VCS_SERVICES_MAP_URL: http://map-service:8084
VCS_SERVICES_IAM_URL: http://iam-service:8082
```

Running locally: the journey service will try to call `localhost:8081` — which works only if you run all services locally. But the DB is set to Supabase. This is an inconsistent local dev setup.

---

## PART 7 — DOCKER COMPOSE WIRING

### ✅ Startup Ordering — Correct for Most Services

```yaml
journey-service:
  depends_on:
    db: { condition: service_healthy }
    redis: { condition: service_healthy }
    capacity-service: { condition: service_healthy }
    map-service: { condition: service_healthy }
    iam-service: { condition: service_healthy }
```

Journey service correctly waits for all its dependencies to be healthy before starting. ✅

### ❌ Nginx Has No Health Condition on Dependencies

```yaml
nginx:
  depends_on:
    - iam-service
    - journey-service
    - capacity-service
    - map-service
```

This uses `depends_on` without `condition: service_healthy`. Nginx can start before backends are ready. With `resolver 127.0.0.11 valid=10s` and `set $upstream`, requests during startup may fail with 502 until DNS resolves.

### ❌ Notification Service Not in Nginx depends_on

The nginx service does not depend on `notification-service`, matching the fact that nginx has no proxy route for it.

### ⚠️ Shared Database — No Schema Isolation

All services use the same PostgreSQL database `trafficservice` with the same credentials. If any service's migration runs a conflicting `CREATE TABLE` or drops a table, all services are affected. There are no separate schemas or databases per service.

---

## PART 8 — SUMMARY TABLE: ALL INTEGRATION POINTS

| # | Integration | File(s) | Status | Severity |
|---|---|---|---|---|
| 1 | Frontend → IAM login | `AppContext.tsx:login()` | Never calls IAM | ❌ CRITICAL |
| 2 | Frontend JWT generation | `auth.ts:generateJWT()` | HS256 in browser, fake auth | ❌ CRITICAL |
| 3 | Frontend API base URL | `journeyApi.ts:BASE_URL` | `localhost:8083` bypasses nginx | ❌ CRITICAL |
| 4 | Frontend → Notification service | `AppContext.tsx` | Mock data only, never called | ❌ CRITICAL |
| 5 | Frontend → IAM admin endpoints | Admin pages | Not implemented in frontend | ⚠️ SIGNIFICANT |
| 6 | Frontend → Map service nodes | BookJourneyPage | Hardcoded coords, no API call | ⚠️ SIGNIFICANT |
| 7 | Journey → Map API contract | `map_client.go` vs `map_handler.go` | **Fixed — GET + node IDs + envelope unwrap** | ✅ FIXED |
| 8 | Nginx → Map service path | `nginx.conf` vs `map router.go` | **Fixed (prev session) — /api/v1/map/** | ✅ FIXED |
| 9 | Capacity client fallback | `capacity_client.go` | **Fixed — returns error on failure** | ✅ FIXED |
| 10 | Map client fallback | `map_client.go` | **Fixed — returns error on failure** | ✅ FIXED |
| 11 | Journey JWT algorithm | `auth.go` middleware | **Fixed (prev session) — RS256 via JWKS** | ✅ FIXED |
| 12 | Nginx → Notification service | `nginx.conf` | No route configured | ❌ CRITICAL |
| 13 | Supabase password in git | `*/config.yaml` | Real credentials committed | ❌ CRITICAL |
| 14 | Capacity service config | `config.yaml` | Empty DB host — can't run locally | ❌ CRITICAL |
| 15 | Config inconsistency | All `config.yaml` | Inconsistent local dev setup | ⚠️ SIGNIFICANT |
| 16 | Nginx depends_on | `docker-compose.yml` | No health condition for nginx | ⚠️ MINOR |
| 17 | Shared DB schema | `docker-compose.yml` | All services same DB, no isolation | ⚠️ SIGNIFICANT |

---

## PART 9 — PRIORITISED FIX LIST

### 🔴 Fix Immediately (System Functionally Broken)

**P1 — Map API Contract**
Fix the `MapClient` to call the correct endpoint:
- Change path: `/api/v1/routes/compute` → `/api/v1/map/route`
- Change method: `POST` → `GET`
- Change params: JSON body → query string `?origin_node_id=X&destination_node_id=Y`
- But first align: journey service sends lat/lng coordinates; map service takes node IDs. Either add coordinate-to-node lookup, or change map service to accept coordinates.
- *Files*: `journey-service/internal/client/map_client.go` AND `map-service/internal/http/handlers/map_handler.go`

**P2 — Nginx Map Route Path**
Change nginx `location /api/v1/routes/` to `location /api/v1/map/` (or align map service endpoints to `/api/v1/routes/`).
- *File*: `nginx/nginx.conf`

**P3 — Nginx Notification Route**
Add `location /api/v1/notifications/` → `notification-service:8085` to nginx.
- *File*: `nginx/nginx.conf`

**P4 — Frontend API Base URL**
Change `BASE_URL = 'http://localhost:8083'` to `BASE_URL = ''` (relative URL).
- *File*: `frontend/src/app/services/journeyApi.ts`

**P5 — Rotate Supabase Credentials**
The password `3uuzHVMpT2CqxuwJ` is now in version control. Rotate it immediately. Remove credentials from config.yaml, use environment variables or a .env file (git-ignored).
- *Files*: `journey-service/config.yaml`, `map-service/config.yaml`, `notification-service/config.yaml`

**P6 — Real Login via IAM Service**
Replace the fake `login()` function with a real call to `POST /api/v1/auth/login`.
Store the server-issued RS256 token, not a browser-generated HS256 token.
Add user registration page calling `POST /api/v1/auth/register`.
- *File*: `frontend/src/app/context/AppContext.tsx`
- *File*: `frontend/src/app/services/auth.ts` (replace entire file)

**P7 — Journey Service: Validate RS256 via JWKS**
Replace HS256 HMAC validation in journey middleware with RS256 RSA validation using the IAM JWKS endpoint.
- *File*: `journey-service/internal/middleware/auth.go`

### 🟠 Fix Before Demo (Significant Correctness Issues)

**P8 — Remove Silent Mock Fallbacks (or make them rejections)**
Capacity and map client fallbacks should return errors, not fake success responses. Silent approval on failure defeats the entire purpose of the system.
- *Files*: `journey-service/internal/client/capacity_client.go`, `map_client.go`

**P9 — Connect Frontend Notifications to API**
Add `notificationApi.ts` with calls to notification service. Replace `mockNotifications` with real API data in `AppContext.tsx`.
- *File*: `frontend/src/app/context/AppContext.tsx`

**P10 — Add IAM Admin UI**
Add frontend pages for user listing and role management, calling `GET /api/v1/admin/auth/users` and `POST /api/v1/admin/auth/promote`.

**P11 — Fix Capacity Service config.yaml**
Fill in the local DB config (empty host) and add a `.env.example` template.
- *File*: `capacity-service/config.yaml`

### 🟡 Fix for Quality

**P12 — Nginx health conditions**
Add `condition: service_healthy` to nginx `depends_on` for all backends.

**P13 — Environment-specific config**
Use `config.local.yaml` for local development and `config.docker.yaml` for Docker, rather than relying on env var overrides of committed defaults.

**P14 — Frontend: Fetch nodes from map service**
Replace hardcoded location list in booking page with `GET /api/v1/map/nodes` call to map service.

---

*Audit complete — 2026-04-07*
*All findings are based on direct file inspection. No assumptions were made about runtime behavior.*
