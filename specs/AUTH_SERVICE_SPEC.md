# IAM / Auth Service (S1) — Complete Specification

> **Owner:** Deepika Nag
> **Language:** Go 1.22+ (gorilla/mux)
> **Database:** PostgreSQL 16 (own schema: `auth`)
> **Port:** 8082
> **Status:** Planning Phase

---

## 1. Purpose

The IAM / Auth Service is the identity and trust anchor of the Distributed Traffic Service. It is the entry point for all users — every session begins with a credential issued by this service. All other services validate the resulting JWT locally without calling IAM at runtime, meaning IAM downtime does not block the booking flow for users with existing valid tokens.

In a multi-VM deployment, all VMs run identical stacks behind a load balancer. Each VM's IAM Service independently manages its local PostgreSQL replica. PostgreSQL multi-master logical replication keeps all VMs' user and token data in sync within milliseconds. A user registered on VM A will be visible to VM B and VM C before any realistic login attempt arrives — given the ~100ms replication lag and the human time required to navigate from registration to login.

---

## 2. Responsibilities

The IAM Service is responsible for:

- Accepting driver and admin registration requests, hashing credentials securely with bcrypt, and persisting user accounts to PostgreSQL
- Validating email/password credentials and issuing signed RSA JWT access tokens and opaque refresh tokens
- Serving a JWKS endpoint (`/.well-known/jwks.json`) so that Journey, Capacity, Map, and Notification services can cache the RSA public key and validate tokens locally without calling IAM at runtime
- Rotating refresh tokens on every refresh call (refresh token rotation) and revoking them on logout or admin force-logout
- Allowing authenticated users to read and update their own profile (name, vehicle type, licence information)
- Enforcing role-based access: two roles (`driver`, `admin`), encoded as a claim in every JWT
- Bootstrapping the first admin account via a seed migration, and providing an admin-only promote endpoint for subsequent admins
- Running a background job to delete expired and revoked refresh token rows to prevent unbounded table growth

---

## 3. Architecture Context

### 3.1 Where IAM Service Sits

```
                    ┌─────────────────────────────────────┐
                    │         Load Balancer               │
                    │    (Nginx / AWS ALB)                │
                    └──────────────┬──────────────────────┘
                                   │
               ┌───────────────────┼───────────────────┐
               ▼                   ▼                   ▼
    ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
    │      VM A        │ │      VM B        │ │      VM C        │
    │  ─────────────── │ │  ─────────────── │ │  ─────────────── │
    │  IAM Service     │ │  IAM Service     │ │  IAM Service     │
    │  Journey Service │ │  Journey Service │ │  Journey Service │
    │  Capacity Service│ │  Capacity Service│ │  Capacity Service│
    │  Map Service     │ │  Map Service     │ │  Map Service     │
    │  Notification Svc│ │  Notification Svc│ │  Notification Svc│
    │  ─────────────── │ │  ─────────────── │ │  ─────────────── │
    │  PostgreSQL      │ │  PostgreSQL      │ │  PostgreSQL      │
    │  Redis           │ │  Redis           │ │  Redis           │
    └────────┬─────────┘ └────────┬─────────┘ └────────┬─────────┘
             │                    │                    │
             └─────────── Multi-Master PostgreSQL ─────┘
                          Logical Replication
```

Within a single VM, IAM Service is called directly by the frontend (via Nginx). Other services on the same VM call the JWKS endpoint only on startup and hourly refresh — never per-request:

```
  Browser / Frontend (driver or admin)
       │  POST /api/v1/auth/login      (sync — user is waiting)
       │  POST /api/v1/auth/register   (sync — user is waiting)
       │  POST /api/v1/auth/refresh    (sync — on 401)
       │  GET/PUT /api/v1/auth/profile (sync — settings page)
       ▼
  IAM Service :8082 (same VM)
       │  reads/writes auth.users, auth.refresh_tokens
       ▼
  PostgreSQL (same VM)
       │  logical replication
       ▼
  PostgreSQL on VM A, VM B, VM C

  Journey / Capacity / Map / Notification Services (same VM)
       │  GET /.well-known/jwks.json   (on startup + hourly — NOT per-request)
       ▼
  IAM Service :8082 — returns RSA public key(s)
```

### 3.2 Communication Pattern Summary

| From → To | Protocol | Sync/Async | Why |
|-----------|----------|------------|-----|
| Frontend → IAM (login / register / refresh) | REST POST | **Sync** | The user is waiting for a token or an error. There is no useful work to do until authentication completes. Fire-and-forget is not appropriate for a security gate. |
| Frontend → IAM (profile read/update) | REST GET/PUT | **Sync** | Settings pages are blocking interactions. The user sees a loading state until the response arrives. |
| Other services → IAM (JWKS) | REST GET (poll) | **Async / scheduled** | Consuming services cache the public key in memory and refresh on a timer. No runtime call happens per driver request. This decouples all booking traffic from IAM availability. |
| PostgreSQL VM-A ↔ VM-B ↔ VM-C | Logical Replication | **Async** | Multi-master sync. Handled at the infrastructure layer. Services do not implement replication logic. |

### 3.3 Why These Choices

**Login and registration are sync because the user is at a keyboard waiting.**
These are infrequent, high-stakes operations. The caller must know whether they succeeded before doing anything else. There is no intermediate state that is meaningful to expose. An async login would require a pending state, a polling mechanism, and a more complex frontend — all for no benefit.

**JWKS is polled, not called per-request.**
If Journey Service called IAM on every booking to validate the JWT, an IAM outage would block all bookings. Instead, consuming services cache the RSA public key in process memory on startup and refresh every hour. JWT validation is then a local cryptographic operation taking microseconds, with no network dependency. Existing access tokens remain valid for up to one hour during a complete IAM outage. Only new logins are affected. This is the primary fault-isolation mechanism.

**Refresh token revocation uses eventual consistency.**
When a user logs out or an admin force-logs out a user, the refresh token row is soft-revoked in PostgreSQL. This propagates to all VMs within ~100ms via logical replication. The access JWT, however, remains valid until its `exp` claim — at most one hour. This is an accepted trade-off: eliminating the per-request IAM call is worth a bounded window of continued access. Short access token TTL (1 hour) is the control mechanism.

**IAM publishes no events to Redis Streams.**
No other service needs to react in real time to user registration, login, or profile changes for the prototype. Adding event publishing would add infrastructure coupling without delivering value. If audit logging is required in future, IAM can publish to an `iam.events` stream.

---

## 4. API Contract

### 4.1 POST /api/v1/auth/register

Register a new driver account. Admin accounts are not created through this endpoint — they are bootstrapped via seed migration (§7.4) or the promote endpoint (§4.9).

**Headers:** `Content-Type: application/json`

**Rate limit (Nginx):** 10 requests per IP per minute.

**Request Body:**
```json
{
  "name": "Alice Byrne",
  "email": "alice@example.com",
  "password": "securePass1",
  "vehicle_type": "car",
  "license_info": {
    "license_number": "D12345678",
    "expiry_date": "2030-06-01",
    "class": "B",
    "issuing_jurisdiction": "Ireland"
  }
}
```

**Field validation:**

| Field | Required | Rules |
|-------|----------|-------|
| `name` | Yes | Non-empty, max 100 characters |
| `email` | Yes | Valid format, unique (case-insensitive) |
| `password` | Yes | Min 8 characters, at least one letter and one digit |
| `vehicle_type` | Yes | One of: `car`, `van`, `motorcycle`, `truck` |
| `license_info.license_number` | Yes | Non-empty string |
| `license_info.expiry_date` | No | ISO 8601 date `YYYY-MM-DD` |
| `license_info.class` | No | Free string |
| `license_info.issuing_jurisdiction` | No | Free string |

**Response (201 Created):**
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6ImtleS0yMDI2MDEifQ...",
  "refresh_token": "rt_c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f",
  "user": {
    "id": "usr_a1b2c3d4e5f6g7h8",
    "name": "Alice Byrne",
    "email": "alice@example.com",
    "role": "driver",
    "vehicle_type": "car"
  }
}
```

**Error Responses:**
- `400` — Validation failure. Body includes a `details` array listing each failing field.
- `409` — Email already registered (case-insensitive match). Do not reveal whether the account exists with a specific role.
- `429` — Nginx rate limit exceeded.
- `500` — Database or hashing failure.

**Validation error body example:**
```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "One or more fields are invalid.",
    "details": [
      { "field": "email",    "message": "Invalid email format." },
      { "field": "password", "message": "Must be at least 8 characters with 1 letter and 1 digit." }
    ]
  }
}
```

---

### 4.2 POST /api/v1/auth/login

Authenticate with email and password and receive a JWT access token and refresh token.

**Headers:** `Content-Type: application/json`

**Rate limit (Nginx):** 10 requests per IP per minute. After 5 consecutive failures within 1 minute, block the IP for 5 minutes.

**Request Body:**
```json
{
  "email": "alice@example.com",
  "password": "securePass1"
}
```

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6ImtleS0yMDI2MDEifQ...",
  "refresh_token": "rt_c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f",
  "user": {
    "id": "usr_a1b2c3d4e5f6g7h8",
    "name": "Alice Byrne",
    "email": "alice@example.com",
    "role": "driver",
    "vehicle_type": "car"
  }
}
```

> **Critical:** `role` is always lowercase (`"driver"` or `"admin"`). The frontend role-toggle and redirect logic depends on this exact casing.

**JWT Claims (decoded access token payload):**
```json
{
  "sub":   "usr_a1b2c3d4e5f6g7h8",
  "role":  "driver",
  "email": "alice@example.com",
  "iss":   "traffic-iam",
  "iat":   1745305201,
  "exp":   1745308801
}
```

The `kid` (key ID) is in the JWT **header**, not the payload:
```json
{ "alg": "RS256", "kid": "key-202601", "typ": "JWT" }
```

**Error Responses:**
- `400` — Missing email or password field.
- `401` — Incorrect credentials. Do not distinguish between "email not found" and "wrong password" — prevents user enumeration.
- `429` — Rate limit exceeded.

**Timing-safe comparison:** When the email is not found, IAM compares the submitted password against a dummy bcrypt hash to ensure a constant-time response. This prevents attackers from distinguishing valid from invalid emails via response latency.

---

### 4.3 POST /api/v1/auth/refresh

Exchange a valid, non-revoked refresh token for a new access token. The old refresh token is immediately revoked and a new one is issued (refresh token rotation). If the same refresh token is replayed, it returns `401`.

**Headers:** `Content-Type: application/json`

**Rate limit (Nginx):** 30 requests per IP per minute.

**Request Body:**
```json
{
  "refresh_token": "rt_c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f"
}
```

**Response (200 OK):**
```json
{
  "access_token":  "eyJhbGciOiJSUzI1NiIsImtpZCI6ImtleS0yMDI2MDEifQ...",
  "refresh_token": "rt_new_f2a391c87d4e5f91a820b734c9e0d27381f6a1e5b4c8d092"
}
```

**Error Responses:**
- `400` — Missing `refresh_token` field.
- `401` — Token not found, already revoked, or expired.
- `429` — Rate limit exceeded.

**Atomicity:** The revoke-old / issue-new operation executes in a single PostgreSQL transaction: `UPDATE auth.refresh_tokens SET revoked_at = NOW() WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id`. If two concurrent refresh calls present the same token, only one UPDATE returns a row. The other receives `401`.

---

### 4.4 POST /api/v1/auth/logout

Revoke the current session's refresh token. The access token remains valid until its `exp` (at most 1 hour).

**Headers:** `Authorization: Bearer <access_token>`, `Content-Type: application/json`

**Request Body:**
```json
{
  "refresh_token": "rt_c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f"
}
```

**Response:** `204 No Content` (empty body)

**Error Responses:**
- `400` — Missing `refresh_token`.
- `401` — Missing or invalid access token.
- `404` — Refresh token not found for this user.

---

### 4.5 GET /api/v1/auth/profile

Retrieve the authenticated user's full profile.

**Headers:** `Authorization: Bearer <access_token>`

**Response (200 OK):**
```json
{
  "id":           "usr_a1b2c3d4e5f6g7h8",
  "name":         "Alice Byrne",
  "email":        "alice@example.com",
  "role":         "driver",
  "vehicle_type": "car",
  "license_info": {
    "license_number":       "D12345678",
    "expiry_date":          "2030-06-01",
    "class":                "B",
    "issuing_jurisdiction": "Ireland"
  },
  "created_at": "2026-04-01T09:00:00Z",
  "updated_at": "2026-04-01T09:00:00Z"
}
```

**Error Responses:**
- `401` — Missing, expired, or invalid JWT.

---

### 4.6 PUT /api/v1/auth/profile

Update the authenticated user's profile. Partial updates are supported — only provided fields are changed. `email` and `role` cannot be changed through this endpoint.

**Headers:** `Authorization: Bearer <access_token>`, `Content-Type: application/json`

**Request Body (partial update example):**
```json
{
  "name":         "Alice B. Byrne",
  "vehicle_type": "van",
  "license_info": {
    "license_number":       "D12345678",
    "expiry_date":          "2032-06-01",
    "class":                "B",
    "issuing_jurisdiction": "Ireland"
  }
}
```

**Response (200 OK):** Same shape as `GET /api/v1/auth/profile`, reflecting updated values.

**Error Responses:**
- `400` — Invalid `vehicle_type` value, malformed `license_info`, or attempt to update a non-updatable field (`email`, `role`).
- `401` — Missing or invalid JWT.

---

### 4.7 GET /.well-known/jwks.json

Serve the RSA public key(s) in JWKS format. Consumed by all other services on startup and hourly refresh to prime their local JWT validation cache.

**Authentication:** None required. This endpoint is intentionally unauthenticated — other services need it before they have a token.

**Cache-Control:** `public, max-age=3600`

**Response (200 OK):**
```json
{
  "keys": [
    {
      "kty": "RSA",
      "use": "sig",
      "alg": "RS256",
      "kid": "key-202601",
      "n":   "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4...",
      "e":   "AQAB"
    }
  ]
}
```

During key rotation, both the old and new public keys are present in the `keys` array simultaneously. Consumers match `kid` from the JWT header to select the correct key. IAM guarantees both keys are served for at least the duration of the access token TTL (1 hour) after a rotation.

**Error Responses:**
- `500` — IAM failed to load its private key at startup; JWKS cannot be served.

---

### 4.8 GET /api/v1/admin/auth/users

List all registered users. Admin only.

**Headers:** `Authorization: Bearer <access_token>` (role: `admin`)

**Query Parameters:**

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `role` | string | — | Filter by `driver` or `admin` |
| `page` | int | `1` | Page number |
| `limit` | int | `20` | Results per page (max 100) |

**Response (200 OK):**
```json
{
  "users": [
    {
      "id":           "usr_a1b2c3d4e5f6g7h8",
      "name":         "Alice Byrne",
      "email":        "alice@example.com",
      "role":         "driver",
      "vehicle_type": "car",
      "created_at":   "2026-04-01T09:00:00Z"
    }
  ],
  "pagination": {
    "page":  1,
    "limit": 20,
    "total": 145
  }
}
```

**Error Responses:**
- `401` — Not authenticated.
- `403` — Authenticated but not an admin.

---

### 4.9 POST /api/v1/admin/auth/promote

Promote a driver to admin, or demote an admin back to driver. Admin only. Cannot demote the sole remaining admin (§10, E11).

**Headers:** `Authorization: Bearer <access_token>` (role: `admin`), `Content-Type: application/json`

**Request Body:**
```json
{
  "user_id": "usr_a1b2c3d4e5f6g7h8",
  "role":    "admin"
}
```

**Response (200 OK):**
```json
{
  "user_id":    "usr_a1b2c3d4e5f6g7h8",
  "new_role":   "admin",
  "updated_at": "2026-04-03T10:00:00Z"
}
```

**Error Responses:**
- `400` — Invalid `role` value, or attempt to demote the sole remaining admin.
- `401` — Not authenticated.
- `403` — Caller is not an admin.
- `404` — `user_id` does not exist.

---

### 4.10 POST /api/v1/admin/auth/force-logout

Revoke all refresh tokens for a given user across all devices. Admin only. The user's access JWT remains valid until `exp`.

**Headers:** `Authorization: Bearer <access_token>` (role: `admin`), `Content-Type: application/json`

**Request Body:**
```json
{
  "user_id": "usr_a1b2c3d4e5f6g7h8"
}
```

**Response:** `204 No Content`

**Error Responses:**
- `401` — Not authenticated.
- `403` — Caller is not an admin.
- `404` — `user_id` does not exist.

---

### 4.11 GET /health

```json
{
  "status":         "healthy",
  "db":             "connected",
  "uptime_seconds": 3600
}
```

Returns `200` when healthy, `503` if the database is unreachable.

---

### 4.12 GET /ready

Returns `200 OK` only when the PostgreSQL connection pool is established and the RSA private key has been loaded successfully. Returns `503` during startup. Used by Docker Swarm to gate traffic routing.

---

## 5. What IAM Service Provides to Other Services

### To Journey Service (S2, Ajinkya) — JWKS

```
GET /.well-known/jwks.json
```

Journey Service fetches this on startup and refreshes every hour. It maintains an in-memory map of `kid → rsa.PublicKey`. On each inbound request it reads the JWT header, extracts `kid`, looks up the key, and validates signature and claims locally. No network call to IAM occurs per request.

**Go snippet for Journey Service (illustrative):**
```go
// On startup
resp, _ := http.Get(os.Getenv("IAM_JWKS_URL"))
var jwks jose.JSONWebKeySet
json.NewDecoder(resp.Body).Decode(&jwks)
keyCache = buildKeyMap(jwks) // map[kid]*rsa.PublicKey

// On each inbound request
token, _ := jwt.ParseWithClaims(rawToken, &Claims{}, func(t *jwt.Token) (interface{}, error) {
    kid := t.Header["kid"].(string)
    key, ok := keyCache[kid]
    if !ok {
        // Unknown kid: re-fetch JWKS once before failing (handles key rotation)
        keyCache = refreshJWKS()
        key, ok = keyCache[kid]
        if !ok {
            return nil, fmt.Errorf("unknown kid: %s", kid)
        }
    }
    return key, nil
})
```

### To All Services — JWT Claims Contract

Every JWT issued by IAM contains these claims that consuming services depend on:

| Claim | Location | Type | Value |
|-------|----------|------|-------|
| `kid` | JWT header | string | Key ID — used to select public key from JWKS cache |
| `sub` | JWT payload | string | User ID (`usr_` prefix) |
| `role` | JWT payload | string | `"driver"` or `"admin"` (always lowercase) |
| `email` | JWT payload | string | User's email address |
| `iss` | JWT payload | string | `"traffic-iam"` |
| `iat` | JWT payload | int64 | Unix timestamp — issued at |
| `exp` | JWT payload | int64 | Unix timestamp — expiry |

Consuming services MUST validate: `exp > now`, `iss == "traffic-iam"`, RSA signature against the key for `kid`.

---

## 6. What IAM Service Needs from Other Services

**IAM has no runtime dependencies on any other service.**

On startup it reads the RSA private key from the path configured in `IAM_PRIVATE_KEY_PATH`. This is a file read, not a network call. IAM never calls Journey, Capacity, Map, or Notification services.

This isolation means:
- IAM can be developed and tested independently.
- An IAM outage does not cascade to booking traffic (existing tokens remain valid).
- Other service outages do not affect IAM's ability to authenticate users.

---

## 7. Database Schema

IAM owns the `auth` schema. No other service reads or writes to this schema directly.

```sql
CREATE SCHEMA IF NOT EXISTS auth;

-- User accounts
CREATE TABLE auth.users (
    id              VARCHAR(40)  PRIMARY KEY,      -- 'usr_' + nanoid(16)
    name            VARCHAR(100) NOT NULL,
    email           VARCHAR(254) NOT NULL,
    email_lower     VARCHAR(254) NOT NULL,          -- lowercased copy for case-insensitive unique check
    password_hash   VARCHAR(255) NOT NULL,          -- bcrypt, cost factor 12
    role            VARCHAR(10)  NOT NULL DEFAULT 'driver',
    vehicle_type    VARCHAR(15)  NOT NULL,
    license_info    JSONB        NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_role         CHECK (role IN ('driver', 'admin')),
    CONSTRAINT chk_vehicle_type CHECK (vehicle_type IN ('car', 'van', 'motorcycle', 'truck'))
);

-- Case-insensitive unique email
CREATE UNIQUE INDEX idx_users_email_lower ON auth.users (email_lower);

-- Admin user listing filter
CREATE INDEX idx_users_role ON auth.users (role);


-- Refresh token store
-- Stores only a SHA-256 hash of the opaque token — never the token itself.
CREATE TABLE auth.refresh_tokens (
    id          BIGSERIAL    PRIMARY KEY,
    user_id     VARCHAR(40)  NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    token_hash  VARCHAR(64)  NOT NULL,              -- SHA-256 hex, 64 chars
    expires_at  TIMESTAMPTZ  NOT NULL,
    revoked_at  TIMESTAMPTZ,                        -- NULL = active
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    user_agent  TEXT,                               -- optional, for audit
    ip_address  INET                                -- optional, for audit

    CONSTRAINT chk_token_hash_len CHECK (char_length(token_hash) = 64)
);

-- Fast token lookup on refresh (primary access pattern)
CREATE UNIQUE INDEX idx_refresh_tokens_hash    ON auth.refresh_tokens (token_hash);

-- Fast "revoke all tokens for user" query
CREATE INDEX idx_refresh_tokens_user_id        ON auth.refresh_tokens (user_id);

-- Background cleanup scan
CREATE INDEX idx_refresh_tokens_expires_at     ON auth.refresh_tokens (expires_at);
```

### `license_info` JSONB shape

```json
{
  "license_number":       "D12345678",
  "expiry_date":          "2030-06-01",
  "class":                "B",
  "issuing_jurisdiction": "Ireland"
}
```

Only `license_number` is required. All other keys are optional and stored as-is.

### 7.4 Seed Data (003_seed_admin.sql)

The initial admin account is inserted by a migration script. **This file must not be committed to the repository.** It lives in the deployment runbook only.

```sql
-- 003_seed_admin.sql  (deployment runbook — do NOT commit to git)
INSERT INTO auth.users (id, name, email, email_lower, password_hash, role, vehicle_type, license_info)
VALUES (
    'usr_admin000000000000000000000001',
    'System Admin',
    'admin@traffic.ie',
    'admin@traffic.ie',
    '$2a$12$<bcrypt_hash_of_initial_password_here>',
    'admin',
    'car',
    '{"license_number": "ADMIN001"}'
);
```

Operators must change the admin password after first login, or regenerate this seed for non-production environments. Recovery for a forgotten admin password: direct database update of `password_hash` (bcrypt hash of new password, cost 12).

---

## 8. Redis Usage

IAM uses the per-VM Redis instance for one optional purpose: **application-level rate limiting** as a defence-in-depth layer beneath Nginx.

```
Key:   iam:ratelimit:login:{ip_address}
Type:  String (integer counter)
TTL:   60 seconds
Value: Number of failed login attempts in the current window
```

If the counter reaches 5, IAM returns `429 Too Many Requests` with `Retry-After: 60` regardless of whether Nginx has already acted. This is an optional layer — if Nginx fully enforces rate limits, this code path is inactive.

IAM does **not** use Redis Streams. It does not publish or consume events.

IAM does **not** cache user records in Redis. Login reads go directly to PostgreSQL. Login is low-frequency and a direct database read is appropriate. Caching would introduce invalidation complexity (e.g., role changes would need cache purge logic).

---

## 9. Key Management

### RSA Key Pair

IAM signs JWTs using an RSA-2048 private key. The corresponding public key is served via JWKS. Each key is identified by a `kid` string (e.g., `key-202601`).

**Private key storage:** Mounted as a Docker Secret at the path configured in `IAM_PRIVATE_KEY_PATH`. Never stored in environment variables in production. Never committed to the repository.

**In-process:** The private key is loaded once at startup into process memory. It is never written to disk after the initial mount.

### Key Rotation (Manual, Prototype)

1. Generate a new RSA key pair.
2. Update the Docker Secret with the new private key.
3. Update `IAM_SIGNING_KID` to the new key ID (e.g., `key-202602`).
4. Set `IAM_PREVIOUS_KID` and `IAM_PREVIOUS_PUBLIC_KEY_PEM` so IAM continues to serve the old public key in JWKS during the overlap period.
5. Rolling-restart IAM instances via Docker Swarm.
6. Wait at least `IAM_KEY_OVERLAP_SECONDS` (≥ `IAM_ACCESS_TOKEN_TTL`, default 3600) before removing the old key from configuration.

During the overlap period both old and new public keys are present in the JWKS `keys` array. Consumers match by `kid` so in-flight tokens signed with the old key continue to validate correctly.

Scheduled rotation (e.g., every 90 days) is noted as a production follow-up; it is not required for the prototype.

---

## 10. Edge Cases

| # | Scenario | Risk | Mitigation |
|---|----------|------|------------|
| E1 | Two concurrent registration requests with the same email hit VM A and VM B simultaneously | Both read no existing row, both attempt INSERT | `UNIQUE INDEX idx_users_email_lower` rejects the second insert with a constraint violation. IAM returns `409 EMAIL_ALREADY_EXISTS`. The uniqueness guarantee holds under replication: whichever VM inserts first propagates the row; the other VM's unique constraint then also blocks any replication of the loser's insert. |
| E2 | User registers on VM A, immediately logs in and the load balancer routes to VM B before replication arrives | Login on VM B fails — row not yet visible | PostgreSQL logical replication delivers within ~100ms. The human registration-to-login flow (reading a success message, navigating to login) takes seconds. In practice this window is never hit. Acknowledged as eventual consistency; no sticky sessions implemented. |
| E3 | Two concurrent refresh calls present the same refresh token (mobile + web client) | Both could be accepted, producing two new tokens | The `UPDATE SET revoked_at = NOW() WHERE token_hash = $1 AND revoked_at IS NULL` uses a row-level lock. Only one UPDATE returns a row. The other sees zero rows and receives `401 INVALID_REFRESH_TOKEN`. |
| E4 | Admin force-logout revokes all refresh tokens but the driver's access JWT is still valid | Driver continues making booking requests for up to 1 hour | Access tokens are self-contained JWTs validated locally by other services without calling IAM. Revocation does not immediately invalidate them. Max continued access = access token TTL (1 hour). Documented known limitation. Production fix: shared token revocation list in Redis checked by all services. |
| E5 | JWKS endpoint unavailable when Journey Service starts | Journey Service cannot prime its key cache, cannot validate any token | Journey Service retries JWKS fetch with exponential backoff (2s / 4s / 8s, 3 attempts). If all fail, it refuses to start (fail-fast) and Docker Swarm restarts it. IAM must be included in health check dependencies for other services' startup ordering. |
| E6 | IAM rotates to a new RSA key. Journey Service has cached the old JWKS. A new login returns a JWT with `kid: key-202602` which Journey Service does not recognise | Journey Service returns 401 for a valid token | On a `kid` cache miss, consuming services re-fetch JWKS immediately (one-time eager refresh outside the hourly schedule). If the re-fetch succeeds and the new key is present, validation proceeds. IAM guarantees the old key remains in JWKS for at least 1 hour after rotation, so all in-flight tokens signed with the old key continue to validate. |
| E7 | Brute-force login attack against a known email address | Credential stuffing or dictionary attack | Defence in depth: (1) Nginx blocks IP after 5 failures/minute; (2) IAM application-layer Redis counter returns 429 after 5 failures; (3) bcrypt cost 12 adds ~100ms per comparison; (4) timing-safe dummy hash comparison prevents email-validity oracle via response time. |
| E8 | `INSERT INTO auth.users` fails due to a transient PostgreSQL error during registration | User gets 500; retries and hits the same VM | IAM wraps the INSERT in a transaction. On failure: rollback, return 500. Retry is safe: if the first attempt actually committed (e.g., response lost in transit), the unique email constraint returns 409 on retry — the client knows the account exists. |
| E9 | Expired refresh token presented to `/api/v1/auth/refresh` | Should be rejected cleanly | The SELECT includes `AND revoked_at IS NULL AND expires_at > NOW()`. Expired rows are not matched. Response: `401 INVALID_REFRESH_TOKEN`. Row is retained for `IAM_TOKEN_RETENTION_DAYS` for audit, then deleted by the background cleanup job. |
| E10 | Driver forgets their password | No self-service recovery path in prototype | Password reset is out of scope — it requires email delivery (SMTP/SES) and a time-limited reset token flow. Documented as a known limitation. Recovery: admin deletes the account and the driver re-registers, or admin directly updates `password_hash` in the database. |
| E11 | Admin attempts to demote themselves when they are the sole admin | System left with zero admin accounts | `POST /api/v1/admin/auth/promote` counts remaining admins before applying a demotion. If `SELECT COUNT(*) FROM auth.users WHERE role = 'admin'` returns 1 and the target is that account, the request returns `400 CANNOT_DEMOTE_SOLE_ADMIN`. |
| E12 | Replication conflict: same user row updated on two VMs simultaneously (e.g., profile update on VM A and admin promote on VM B) | Conflict on replicated UPDATE | PostgreSQL logical replication uses last-writer-wins by `updated_at` timestamp. For profile updates, last-writer-wins is acceptable — the user sees whichever update was committed last. For role changes, the admin promote endpoint is an infrequent, explicit action. The probability of a simultaneous conflict is negligible. |

---

## 11. Background Jobs

### 11.1 Expired Refresh Token Cleanup

**Purpose:** Prevent unbounded growth of `auth.refresh_tokens`. Rows are retained for audit purposes for `IAM_TOKEN_RETENTION_DAYS` after expiry or revocation, then deleted.

**Frequency:** Daily (configurable: `IAM_CLEANUP_CRON`, default `0 2 * * *`)

```sql
DELETE FROM auth.refresh_tokens
WHERE
    (expires_at  < NOW() - ($1 || ' days')::interval)
 OR (revoked_at IS NOT NULL AND revoked_at < NOW() - ($1 || ' days')::interval);
-- $1 = IAM_TOKEN_RETENTION_DAYS (default: 7)
```

**Multi-VM behaviour:** All VMs run this job because all VMs run identical stacks. Each VM's DELETE operates on the same replicated data. `DELETE` on already-deleted rows is a no-op. The duplicated work is negligible at prototype scale. If it becomes a concern, a PostgreSQL advisory lock can be used to elect a single runner per execution window.

```go
func (s *Service) startCleanupJob(ctx context.Context) {
    ticker := time.NewTicker(24 * time.Hour)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            s.db.ExecContext(ctx, `
                DELETE FROM auth.refresh_tokens
                WHERE expires_at  < NOW() - ($1 || ' days')::interval
                   OR (revoked_at IS NOT NULL AND revoked_at < NOW() - ($1 || ' days')::interval)
            `, s.cfg.TokenRetentionDays)
        case <-ctx.Done():
            return
        }
    }
}
```

---

## 12. Multi-VM Behavior

### 12.1 Stateless Handler Design

Every IAM HTTP handler reads all state from PostgreSQL on each request. There is no in-memory user cache or session store. Any request can be handled by any VM. The load balancer does not need sticky sessions for correctness.

### 12.2 Request Flow (Registration + Login)

```
1. Driver fills in registration form and submits

2. Load balancer routes request to VM B (arbitrary)

3. VM B's IAM Service:
   a. Validates fields
   b. Checks email uniqueness (VM B's PostgreSQL)
   c. bcrypt hashes the password
   d. INSERT auth.users → VM B's PostgreSQL
   e. INSERT auth.refresh_tokens → VM B's PostgreSQL
   f. Signs JWT with RSA private key (in-process)
   g. Returns 201 with access_token + refresh_token + user

4. PostgreSQL logical replication propagates within ~100ms:
   - auth.users row → VM A, VM C
   - auth.refresh_tokens row → VM A, VM C

5. Driver navigates to login page (seconds later)
   Load balancer may route to VM A — the row is already replicated.
   Login succeeds.
```

### 12.3 Request Flow (JWKS Fetch by Consuming Service)

```
1. Journey Service (VM B) starts up

2. Journey Service fetches:
   GET http://iam-service:8082/.well-known/jwks.json
   (intra-VM call — same Docker network)

3. IAM Service (VM B) responds with JWKS from in-process state (loaded from key file at startup)

4. Journey Service stores kid → rsa.PublicKey in memory

5. 3600 seconds later, Journey Service re-fetches and updates its cache

6. All JWT validation from this point is local — IAM is not called per request
```

### 12.4 Key Distribution in Docker Swarm

The RSA private key is a Docker Secret mounted at `IAM_PRIVATE_KEY_PATH` on every node running the IAM service. All IAM instances use the **same private key**, so a JWT signed by IAM on VM A is validated correctly by Journey Service on VM B using the same JWKS-cached public key.

### 12.5 Replication Lag and Consistency

| Operation | Consistency Model | Practical Impact |
|-----------|-------------------|-----------------|
| Registration | Eventual (write on one VM, propagates to others in ~100ms) | Negligible — human navigation time far exceeds replication lag |
| Login | Reads from local PostgreSQL replica | Safe — replica is current within ~100ms of any write |
| Refresh token revocation | Eventual | Access JWT remains valid until `exp` (max 1 hour); refresh revocation propagates in ~100ms |
| Profile update | Last-write-wins on conflict | Acceptable — profile edits are infrequent and non-safety-critical |

---

## 13. Configuration (Environment Variables)

```bash
# Server
PORT=8082
ENV=production
LOG_LEVEL=info
VM_ID=vm-a                              # Used in logs

# PostgreSQL (local instance on this VM)
DB_HOST=localhost
DB_PORT=5432
DB_NAME=trafficservice
DB_SCHEMA=auth
DB_USER=iam_svc
DB_PASSWORD=<secret>
DB_MAX_CONNS=20
DB_IDLE_CONNS=5
DB_CONN_TIMEOUT_SECONDS=5

# Redis (local instance on this VM — optional, for app-layer rate limiting)
REDIS_HOST=localhost:6379
REDIS_PASSWORD=<secret>
REDIS_DB=0

# RSA key pair
IAM_PRIVATE_KEY_PATH=/run/secrets/iam_private_key    # Docker Secret mount path
IAM_SIGNING_KID=key-202601                           # kid for current signing key
IAM_PREVIOUS_KID=                                    # kid of previous key (empty = none)
IAM_PREVIOUS_PUBLIC_KEY_PEM=                         # PEM of old public key (for rotation overlap)
IAM_KEY_OVERLAP_SECONDS=3600                         # How long to serve both keys after rotation

# Token lifetimes
IAM_ACCESS_TOKEN_TTL=3600          # Access token lifetime in seconds (1 hour)
IAM_REFRESH_TOKEN_TTL=604800       # Refresh token lifetime in seconds (7 days)

# Security
IAM_BCRYPT_COST=12                 # bcrypt work factor (10–14 range)

# JWKS
IAM_JWKS_CACHE_MAX_AGE=3600        # Cache-Control max-age on JWKS responses (seconds)

# Background jobs
IAM_TOKEN_RETENTION_DAYS=7         # Days to retain expired/revoked tokens before deletion
IAM_CLEANUP_CRON=0 2 * * *         # Cron expression for cleanup job (default: 2 AM daily)

# CORS
IAM_CORS_ALLOWED_ORIGINS=http://localhost:5173    # Comma-separated; add prod domain in deployment

# Logging
IAM_ENVIRONMENT=development        # development | production (affects log format)
```

---

## 14. Project Structure

```
iam-service/
├── cmd/
│   └── server/
│       └── main.go                      # Entry point: load config, wire deps, start HTTP server + cleanup job
│
├── internal/
│   ├── config/
│   │   └── config.go                    # Reads env vars, validates required fields, exposes Config struct
│   │
│   ├── handler/
│   │   ├── auth_handler.go              # POST /register, /login, /refresh, /logout
│   │   ├── profile_handler.go           # GET/PUT /profile
│   │   ├── jwks_handler.go              # GET /.well-known/jwks.json
│   │   ├── admin_handler.go             # GET /admin/auth/users, POST /promote, /force-logout
│   │   └── health_handler.go            # GET /health, GET /ready
│   │
│   ├── middleware/
│   │   ├── jwt_middleware.go            # Validates Bearer token; injects claims into request context
│   │   ├── require_role.go              # Role enforcement: require_role("admin") wraps admin routes
│   │   ├── cors.go                      # CORS headers (reads IAM_CORS_ALLOWED_ORIGINS)
│   │   └── ratelimit.go                 # Optional Redis-backed per-IP rate limiter for /login
│   │
│   ├── model/
│   │   ├── user.go                      # User struct, Role constants, VehicleType constants
│   │   └── token.go                     # RefreshToken struct, Claims struct for JWT
│   │
│   ├── service/
│   │   ├── auth_service.go              # Register, Login, Refresh, Logout, ForceLogout logic
│   │   ├── profile_service.go           # GetProfile, UpdateProfile logic
│   │   ├── jwks_service.go              # Loads RSA key at startup, builds JWKS response, handles overlap keys
│   │   └── cleanup_service.go           # Background expired-token deletion job
│   │
│   ├── repository/
│   │   ├── user_repo.go                 # PostgreSQL CRUD for auth.users
│   │   └── token_repo.go                # PostgreSQL CRUD for auth.refresh_tokens
│   │
│   └── infrastructure/
│       ├── postgres.go                  # Connection pool setup, migration runner
│       └── redis.go                     # Redis client setup (optional — for rate limiting)
│
├── migrations/
│   ├── 001_create_users.sql             # CREATE TABLE auth.users + indexes
│   ├── 002_create_refresh_tokens.sql    # CREATE TABLE auth.refresh_tokens + indexes
│   └── 003_seed_admin.sql               # NOT committed to git — lives in deployment runbook only
│
├── keys/                                # .gitignore'd — local dev only
│   ├── private.pem                      # RSA private key (never commit)
│   └── public.pem                       # RSA public key (safe to commit for CI testing)
│
├── pkg/                                 # Shared packages (align with skeleton conventions)
│   ├── config/config.go
│   ├── errors/errors.go
│   ├── logger/logger.go
│   ├── postgres/connection.go
│   └── response/response.go
│
├── docs/
│   ├── docs.go
│   ├── swagger.json
│   └── swagger.yaml
│
├── Dockerfile
├── Makefile
├── config.yaml
└── go.mod
```

**Key dependencies (`go.mod`):**

```
github.com/gorilla/mux            v1.8.1    # HTTP routing
github.com/golang-jwt/jwt/v5      v5.2.1    # JWT signing and parsing
github.com/lestrrat-go/jwx/v2     v2.0.21   # JWKS marshalling
golang.org/x/crypto               latest    # bcrypt
github.com/jackc/pgx/v5           v5.5.5    # PostgreSQL driver
github.com/redis/go-redis/v9      v9.5.1    # Redis client (optional)
github.com/rs/zerolog             v1.32.0   # Structured logging
```

---

## 15. Sequence Diagrams

### 15.1 Registration

```
Browser          Nginx (rate limit)       IAM Service              PostgreSQL
   │                    │                      │                        │
   │  POST /api/v1/auth/register               │                        │
   ├───────────────────►│                      │                        │
   │                    │  (pass, within limit) │                        │
   │                    ├─────────────────────►│                        │
   │                    │                 validate fields                │
   │                    │                 email uniqueness check ───────►│
   │                    │                      │◄─── no existing row ───│
   │                    │                 bcrypt hash password           │
   │                    │                 INSERT auth.users ────────────►│
   │                    │                      │◄─── inserted ──────────│
   │                    │                 INSERT auth.refresh_tokens ───►│
   │                    │                      │◄─── inserted ──────────│
   │                    │                 sign JWT (RSA private key)     │
   │◄───────────────────┤◄─────────────────────┤                        │
   │  201 { access_token, refresh_token, user } │                        │
   │                    │                      │                        │
   │                    │         [PostgreSQL replication propagates     │
   │                    │          auth.users to VM B and VM C async]   │
```

### 15.2 Login

```
Browser          Nginx                    IAM Service              PostgreSQL
   │               │                          │                        │
   │  POST /api/v1/auth/login                  │                        │
   ├──────────────►│                          │                        │
   │               │  (pass) ────────────────►│                        │
   │               │                    SELECT user WHERE email_lower ─►│
   │               │                          │◄─── row (or miss) ─────│
   │               │                    bcrypt.CompareHashAndPassword   │
   │               │                    (if miss: compare dummy hash   │
   │               │                     for constant-time response)   │
   │               │                    INSERT new refresh_token ──────►│
   │               │                          │◄─── inserted ──────────│
   │               │                    sign access JWT                 │
   │◄──────────────┤◄─────────────────────────┤                        │
   │  200 { access_token, refresh_token, user }│                        │
```

### 15.3 Token Refresh

```
Browser                    IAM Service              PostgreSQL
   │                            │                        │
   │  POST /api/v1/auth/refresh │                        │
   ├───────────────────────────►│                        │
   │                       SHA-256 hash input token      │
   │                       UPDATE refresh_tokens         │
   │                         SET revoked_at = NOW()      │
   │                         WHERE token_hash = $1       │
   │                         AND revoked_at IS NULL      │
   │                         AND expires_at > NOW()  ───►│
   │                            │◄─── 1 row (or 0) ──────│
   │                       if 0 rows → 401               │
   │                       INSERT new refresh_token ─────►│
   │                            │◄─── inserted ──────────│
   │                       sign new access JWT            │
   │◄───────────────────────────┤                        │
   │  200 { access_token, refresh_token }                │
```

### 15.4 JWKS Fetch by Journey Service (Startup + Hourly Refresh)

```
Journey Service (VM B)           IAM Service (VM B)
        │                               │
        │  GET /.well-known/jwks.json ─►│
        │                          load key(s) from in-process state
        │◄─── 200 { keys: [...] } ──────│
        │   store in keyCache[kid]      │
        │   start 3600s refresh ticker  │
        │                               │
  [3600s later]                         │
        │  GET /.well-known/jwks.json ─►│
        │◄─── 200 { keys: [...] } ──────│
        │   update keyCache             │
        │                               │
  [Each inbound driver request]         │
        │  parse JWT header → kid       │
        │  keyCache[kid] → rsa.PublicKey│
        │  validate sig + exp + iss     │
        │  (IAM not called)             │
```

### 15.5 Admin Force-Logout

```
Admin Browser              IAM Service              PostgreSQL
      │                         │                        │
      │  POST /admin/auth/force-logout                   │
      │  { user_id: "usr_..." } │                        │
      ├────────────────────────►│                        │
      │                    validate JWT (admin role)      │
      │                    check user exists ────────────►│
      │                         │◄─── found ─────────────│
      │                    UPDATE refresh_tokens          │
      │                      SET revoked_at = NOW()       │
      │                      WHERE user_id = $1           │
      │                      AND revoked_at IS NULL ─────►│
      │                         │◄─── N rows updated ─────│
      │◄────────────────────────┤                        │
      │  204 No Content         │                        │
      │                         │                        │
      │         [PostgreSQL replication propagates        │
      │          revoked rows to VM A and VM C async]     │
      │                         │                        │
      │         [Driver's access JWT remains valid        │
      │          until exp — at most 1 hour]              │
```

---

## 16. Frontend Integration

The frontend is a React 18 PWA running at `frontend/`. It is currently fully mocked. IAM is the **only service the frontend calls for authentication and profile management.** All other services (Journey, Capacity, Map) validate the JWT themselves using JWKS-cached public keys.

### 16.1 Tech Stack (Frontend)

| Concern | Library |
|---------|---------|
| Framework | React 18.3.1 + TypeScript, Vite 6.3.5 |
| Routing | React Router 7.13.0 |
| Styling | Tailwind CSS 4.x, Radix UI, shadcn/ui |
| Forms | React Hook Form 7.x |
| State | React Context (`AppContext`) |
| HTTP | Fetch API — see §16.5 for client blueprint |

### 16.2 Application Routes That Call IAM

```
/auth                  → LoginPage (driver or admin)
/driver/settings       → Driver profile read + update
/admin/settings        → Admin profile read
/admin/auth/...        → Admin user management (promote, force-logout, list)
```

### 16.3 Page → API Mapping

| Page | Backend Call | Endpoint | Notes |
|------|-------------|----------|-------|
| `/auth` | Login | `POST /api/v1/auth/login` | Frontend reads `role` to decide redirect: `driver` → `/driver`, `admin` → `/admin` |
| `/auth` | Register | `POST /api/v1/auth/register` | Auto-logs in after success (tokens in response) |
| Any page on 401 | Refresh | `POST /api/v1/auth/refresh` | Automatic intercept before redirect |
| `/driver/settings` | Fetch profile | `GET /api/v1/auth/profile` | Shows name, vehicle type, licence info |
| `/driver/settings` | Update profile | `PUT /api/v1/auth/profile` | Partial update — send only changed fields |
| `/driver/settings` | Logout | `POST /api/v1/auth/logout` | Sends `{ refresh_token }` from localStorage |
| `/admin/settings` | Fetch profile | `GET /api/v1/auth/profile` | Admin reads own profile |
| `/admin/auth/users` | List users | `GET /api/v1/admin/auth/users` | Paginated, filterable by role |
| `/admin/auth/users/:id` | Promote | `POST /api/v1/admin/auth/promote` | Change role |
| `/admin/auth/users/:id` | Force logout | `POST /api/v1/admin/auth/force-logout` | Revoke all tokens |

### 16.4 Authentication Flow

```
1. User hits /auth → selects role (driver | admin), enters email + password

2. POST /api/v1/auth/login  (IAM Service :8082)
   Body:     { "email": "...", "password": "..." }
   Response: { "access_token": "...", "refresh_token": "...",
               "user": { "id", "name", "email", "role", "vehicle_type" } }

3. Frontend stores:
     localStorage["cw_token"]   = access_token
     localStorage["cw_refresh"] = refresh_token
     localStorage["cw_user"]    = JSON.stringify(user)

4. Redirect based on role:
     role === "driver" → /driver
     role === "admin"  → /admin

5. Every subsequent request to any service:
     Authorization: Bearer <access_token>

6. On 401 response from any service:
     POST /api/v1/auth/refresh { "refresh_token": <stored token> }
     On success: replace stored access_token + refresh_token, retry original request once
     On failure: clear localStorage, redirect to /auth

7. Logout:
     POST /api/v1/auth/logout { "refresh_token": <stored token> }
     Clear localStorage keys
     Redirect to /auth
```

### 16.5 API Client Blueprint (Frontend)

```typescript
// src/app/lib/api.ts
const BASE = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost';

const SERVICES = {
  iam: `${BASE}:8082/api/v1`,
  // other services...
};

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const token = localStorage.getItem('cw_token');
  const res = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...options?.headers,
    },
  });
  if (res.status === 401) {
    const refreshToken = localStorage.getItem('cw_refresh');
    const refreshRes = await fetch(`${SERVICES.iam}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });
    if (refreshRes.ok) {
      const { access_token, refresh_token } = await refreshRes.json();
      localStorage.setItem('cw_token',   access_token);
      localStorage.setItem('cw_refresh', refresh_token);
      return request<T>(url, options); // retry original request once
    }
    localStorage.clear();
    window.location.href = '/auth';
    throw new Error('Unauthenticated');
  }
  if (!res.ok) throw await res.json();
  return res.json();
}

export const iamAPI = {
  login:      (body: { email: string; password: string }) =>
    request(`${SERVICES.iam}/auth/login`,    { method: 'POST', body: JSON.stringify(body) }),
  register:   (body: RegisterRequest) =>
    request(`${SERVICES.iam}/auth/register`, { method: 'POST', body: JSON.stringify(body) }),
  refresh:    (refresh_token: string) =>
    request(`${SERVICES.iam}/auth/refresh`,  { method: 'POST', body: JSON.stringify({ refresh_token }) }),
  logout:     (refresh_token: string) =>
    request(`${SERVICES.iam}/auth/logout`,   { method: 'POST', body: JSON.stringify({ refresh_token }) }),
  profile:    () =>
    request(`${SERVICES.iam}/auth/profile`),
  updateProfile: (body: Partial<ProfileUpdateRequest>) =>
    request(`${SERVICES.iam}/auth/profile`,  { method: 'PUT',  body: JSON.stringify(body) }),
  adminListUsers:   (params?: URLSearchParams) =>
    request(`${SERVICES.iam}/admin/auth/users?${params ?? ''}`),
  adminPromote:     (body: { user_id: string; role: string }) =>
    request(`${SERVICES.iam}/admin/auth/promote`,      { method: 'POST', body: JSON.stringify(body) }),
  adminForceLogout: (user_id: string) =>
    request(`${SERVICES.iam}/admin/auth/force-logout`, { method: 'POST', body: JSON.stringify({ user_id }) }),
};
```

### 16.6 Expected Response Types (TypeScript)

```typescript
interface LoginResponse {
  access_token:  string;
  refresh_token: string;
  user: {
    id:           string;           // "usr_..."
    name:         string;
    email:        string;
    role:         "driver" | "admin";   // ALWAYS lowercase
    vehicle_type: "car" | "van" | "motorcycle" | "truck";
  };
}

interface ProfileResponse {
  id:           string;
  name:         string;
  email:        string;
  role:         "driver" | "admin";
  vehicle_type: "car" | "van" | "motorcycle" | "truck";
  license_info: {
    license_number:        string;
    expiry_date?:          string;   // "YYYY-MM-DD"
    class?:                string;
    issuing_jurisdiction?: string;
  };
  created_at: string;  // ISO 8601
  updated_at: string;  // ISO 8601
}

interface ErrorResponse {
  error: {
    code:     string;    // e.g. "INVALID_CREDENTIALS", "EMAIL_ALREADY_EXISTS"
    message:  string;
    details?: Array<{ field: string; message: string }>;  // VALIDATION_ERROR only
  };
}
```

### 16.7 Data Model Alignment

| Frontend field | Backend field | Notes |
|----------------|---------------|-------|
| `role: "driver"` | `role: "driver"` | Always lowercase — no conversion needed |
| `role: "admin"` | `role: "admin"` | Always lowercase — no conversion needed |
| `vehicleType: "HGV"` (display label) | `vehicle_type: "truck"` | Frontend maps display label ↔ API value |
| `vehicleType: "Car"` (display label) | `vehicle_type: "car"` | Frontend lowercases on submit |
| `licenseInfo` (camelCase) | `license_info` (snake_case) | Use a `camelcase-keys` transform on all API responses |
| `createdAt` (camelCase) | `created_at` (snake_case) | Standard JSON → TypeScript camelCase convention |

**Vehicle type display map (frontend):**
```typescript
const VEHICLE_DISPLAY: Record<string, string> = {
  car:        "Car",
  van:        "Van",
  motorcycle: "Motorcycle",
  truck:      "HGV",
};
const VEHICLE_API: Record<string, string> = {
  Car:        "car",
  Van:        "van",
  Motorcycle: "motorcycle",
  HGV:        "truck",
};
```

### 16.8 CORS Configuration

IAM must include the following headers on all responses (and 204 on `OPTIONS` preflight):

```
Access-Control-Allow-Origin:  http://localhost:5173    (Vite dev; add prod domain via IAM_CORS_ALLOWED_ORIGINS)
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
Access-Control-Allow-Credentials: true
Access-Control-Max-Age:       86400
```

`OPTIONS` preflight requests must return `204 No Content` with the above headers and an empty body.

---

## 17. Interface Contract Summary (for Other Services)

All services that perform JWT validation depend on the following guarantees from IAM. IAM must not deviate from these without coordinating with all service owners.

**JWKS endpoint — always available unauthenticated:**
```
GET /.well-known/jwks.json
Cache-Control: public, max-age=3600
{ "keys": [ { "kty": "RSA", "use": "sig", "alg": "RS256", "kid": "...", "n": "...", "e": "..." } ] }
```

**JWT claims — exact field names and types:**
```
Header: { "alg": "RS256", "kid": "<current signing kid>", "typ": "JWT" }
Payload: {
  "sub":   "usr_<nanoid>",           // user ID
  "role":  "driver" | "admin",       // ALWAYS lowercase
  "email": "<email>",
  "iss":   "traffic-iam",            // MUST match this exact string
  "iat":   <unix timestamp>,
  "exp":   <unix timestamp>
}
```

**Rotation guarantee:** Old public key served in JWKS for at least `IAM_KEY_OVERLAP_SECONDS` (≥ `IAM_ACCESS_TOKEN_TTL`) after a new key is introduced.

**`kid` on cache miss:** Consuming services MUST re-fetch JWKS on a `kid` miss before failing the request, to handle key rotation transparently.

---

*Last updated: 2026-04-03*
*Service version: 0.1.0 (planning)*