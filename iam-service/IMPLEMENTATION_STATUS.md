# IAM Auth Service - Implementation Status

> **Owner:** Deepika Nag
> **Port:** 8082
> **Stack:** Go 1.22, gorilla/mux, PostgreSQL (`auth` schema), RSA-256 JWTs, `golang.org/x/crypto/bcrypt`

---

## What Is Done

### Core Auth Flow
- **`POST /api/v1/auth/register`** - Validates input (name, email, password strength, vehicle type, license number), hashes password with `golang.org/x/crypto/bcrypt` at the cost factor from config (clamped to `bcrypt.MinCost`–`bcrypt.MaxCost`), creates user with `usr_` prefixed UUID, issues access + refresh token pair. Returns `409` on duplicate email (case-insensitive).
- **`POST /api/v1/auth/login`** - Looks up user by lowercased email, verifies bcrypt hash. Timing-safe: runs `bcrypt.CompareHashAndPassword` against a real pre-computed cost-10 hash when email is not found, so response time is indistinguishable from a wrong-password path. Returns `401` for any credential failure (no distinction between missing email / wrong password).
- **`POST /api/v1/auth/refresh`** - Validates and atomically revokes old refresh token, issues new access + refresh token pair (rotation). Returns `401` on replay (second caller gets zero rows from the `UPDATE`).
- **`POST /api/v1/auth/logout`** - Requires `Authorization: Bearer` + `refresh_token` in body. Verifies the token belongs to the calling user before revoking. Returns `204`.

### JWKS
- **`GET /.well-known/jwks.json`** - Loads RSA-2048 private key from `keys/private.pem` at startup (supports both PKCS#1 and PKCS#8 PEM formats). Builds JWK response with `kty`, `use`, `alg`, `kid`, `n`, `e`. Supports serving a previous key simultaneously for zero-downtime rotation (configure `iam.previous_kid` + `iam.previous_pub_key_pem` in `config.yaml`). `Cache-Control: public, max-age=3600`.

### Profile
- **`GET /api/v1/auth/profile`** - Returns full user record for the authenticated user (id, name, email, role, vehicle_type, license_info, created_at, updated_at).
- **`PUT /api/v1/auth/profile`** - Partial update: only fields present in the request body are changed. Validates `name` length and `vehicle_type` enum. `email` and `role` cannot be changed through this endpoint.

### Admin
- **`GET /api/v1/admin/auth/users`** - Lists users with optional `role` filter, `page`, and `limit` (max 100, default 20). Returns `users` array + `pagination` object.
- **`GET /api/v1/admin/auth/users/{id}`** - Get a single user by ID.
- **`POST /api/v1/admin/auth/promote`** - Promote/demote user role. Guards against demoting the sole remaining admin (`CANNOT_DEMOTE_SOLE_ADMIN`).
- **`POST /api/v1/admin/auth/force-logout`** - Revokes all active refresh tokens for a given user across all devices. Access JWT remains valid until its `exp` (max 1 hour - known limitation of stateless JWTs).

### Health
- **`GET /health`** - Returns `{ status, db, uptime_seconds }`. DB status comes from a live `PingContext`. Never returns 503 (degraded DB is still reported but service stays up).
- **`GET /ready`** - Returns `503` if the DB ping fails, `200` otherwise. Used by Docker Swarm to gate traffic.

### JWT Middleware
- `internal/middleware/jwt_middleware.go` - Parses `Authorization: Bearer <token>`, validates RSA signature against the current public key, checks `iss == "traffic-iam"`. Injects `*model.Claims` into request context.
- `RequireRole(roles...)` - Role-enforcement middleware. Returns `403` if the claim role is not in the allowed set.

### Background Cleanup Job
- `CleanupService.Start(ctx)` - Runs on a configurable ticker (default 24h). Deletes expired and revoked refresh tokens older than `token_retention_days` (default 7). Runs as a goroutine started in `main.go`; exits cleanly when the context is cancelled on shutdown.

### Database Migrations
| File | What it creates |
|------|-----------------|
| `migrations/001_create_users.sql` | `auth` schema, `auth.users` table with `CHECK` constraints and indexes |
| `migrations/002_create_refresh_tokens.sql` | `auth.refresh_tokens` table with cascade delete and indexes |
| `migrations/003_seed_admin.sql` | Template for the initial admin account - **fill in a real bcrypt hash before running** |

### Configuration
- `pkg/config/config.go` - Full `Config` struct: server, database (master/slave), redis, iam, logging sections. Loaded from `config.yaml`, overrideable via `VCS_*` env vars.
- `config.yaml` - Default values for local development. **Database credentials are placeholders - override via env vars in production.**

### Project Layout
```
iam-service/
├── cmd/server/main.go
├── config.yaml
├── internal/
│   ├── http/
│   │   ├── middleware.go          (logging, CORS)
│   │   ├── router.go              (all routes wired)
│   │   └── handlers/
│   │       ├── auth_handler.go
│   │       ├── profile_handler.go
│   │       ├── jwks_handler.go
│   │       ├── admin_handler.go
│   │       └── health_handler.go
│   ├── middleware/
│   │   └── jwt_middleware.go      (JWTMiddleware + RequireRole)
│   ├── model/
│   │   ├── user.go
│   │   └── token.go
│   ├── repository/
│   │   ├── user_repo.go
│   │   └── token_repo.go
│   └── service/
│       ├── auth_service.go
│       ├── profile_service.go
│       ├── admin_service.go
│       ├── jwks_service.go
│       └── cleanup_service.go
├── migrations/
│   ├── 001_create_users.sql
│   ├── 002_create_refresh_tokens.sql
│   └── 003_seed_admin.sql
├── pkg/
│   ├── config/config.go           (replaced skeleton)
│   ├── errors/errors.go           (skeleton - unchanged)
│   ├── logger/logger.go           (skeleton - unchanged)
│   ├── postgres/connection.go     (skeleton - unchanged)
│   ├── response/response.go       (skeleton - unchanged)
│   └── tracing/middleware.go      (skeleton - unchanged)
└── keys/
    └── private.pem                (.gitignored - generate with openssl)
```

---

## What Is NOT Done (Gaps vs. Spec)

### 1. Redis Rate Limiting (spec §8)
**Status: Not implemented.**
The spec calls for an application-layer Redis-backed per-IP rate limiter on `/login` (5 failures → `429`, 60-second window). This is defence-in-depth beneath Nginx.

What is missing:
- `internal/middleware/ratelimit.go` - Redis counter middleware
- Redis client initialisation in `main.go`
- Wiring the rate limit middleware onto `/login` and `/register` routes
- `pkg/redis/client.go` (or equivalent) - connection setup

The `redis` section already exists in `config.yaml` and `Config` struct, so the plumbing is ready. Only the implementation is missing.

**To implement:**
```go
// pkg/redis/client.go  - add go-redis dependency
import "github.com/redis/go-redis/v9"

func NewClient(cfg config.RedisConfig) *redis.Client {
    return redis.NewClient(&redis.Options{
        Addr:     cfg.Host,
        Password: cfg.Password,
        DB:       cfg.DB,
    })
}
```
Then in the rate limit middleware: `INCR iam:ratelimit:login:{ip}` with 60s TTL; return `429` if count > 5.

---

### 2. Admin Get-User Uses Full Table Scan (minor)
**Status: Functional but inefficient.**
`AdminHandler.GetUser` calls `admin.ListUsers(ctx, "", 1, 10000)` then iterates in Go to find the matching ID. This works but is O(n) in the database.

**Fix:** Add a `GetByID` method to `AdminService` (and expose the existing `UserRepo.GetByID` through it), then call it directly. One-line change in `admin_handler.go`.

---

### 3. Swagger / OpenAPI Annotations (spec §14 `docs/`)
**Status: Skeleton docs exist, but new handlers have no `// @Summary` annotations.**
The `docs/docs.go`, `swagger.json`, and `swagger.yaml` are the original skeleton files covering only `/health` and `/ready`. None of the new endpoints (register, login, refresh, logout, profile, JWKS, admin) have Swagger annotations.

**To implement:** Add `// @Summary`, `// @Param`, `// @Success`, `// @Failure` comments to each handler function, then run:
```
swag init -g cmd/server/main.go -o docs
```

---

### 4. `003_seed_admin.sql` Should Not Be Committed (spec §7.4)
**Status: File is committed with a placeholder hash.**
The spec explicitly states this file must not be committed - it should live in the deployment runbook only.

**Action:** The placeholder file is safe (the hash value `$2a$12$REPLACE_THIS...` will never match any real password), but it should be moved to a deployment runbook before production. The real file with the actual bcrypt hash should never enter the repository.

---

### 5. Password Reset Flow (spec §10, E10)
**Status: Out of scope for prototype.**
There is no self-service password reset. Recovery requires an admin to directly update `password_hash` in the database.

If needed later: requires SMTP/SES for email delivery and a time-limited reset token endpoint. The token table could be extended with a `type` column to support this without a new table.

---

### 6. Nginx Rate Limiting (spec §4.1, §4.2, §4.3)
**Status: Not part of this service - handled at infrastructure layer.**
The spec references Nginx rate limits (10 req/IP/min for register/login, 30 req/IP/min for refresh, IP block after 5 consecutive failures). These are Nginx `limit_req_zone` rules in `nginx/nginx.conf`, not Go code. IAM has no control over this.

---

## Quick-Start Checklist

```bash
# 1. Generate RSA key (one-time, per environment)
openssl genrsa -out keys/private.pem 2048

# 2. Apply migrations (requires a running PostgreSQL with trafficservice DB)
psql -U postgres -d trafficservice -f migrations/001_create_users.sql
psql -U postgres -d trafficservice -f migrations/002_create_refresh_tokens.sql
# Edit 003_seed_admin.sql: replace the hash placeholder, then run:
psql -U postgres -d trafficservice -f migrations/003_seed_admin.sql

# 3. Fetch dependencies
go mod tidy

# 4. Build and run
go run ./cmd/server

# 5. Verify
curl http://localhost:8082/health
curl http://localhost:8082/.well-known/jwks.json
```

---

## Known Limitations (Acknowledged in Spec)

| # | Limitation | Ref |
|---|-----------|-----|
| L1 | Access JWTs remain valid up to 1 hour after force-logout or admin role demotion - no per-request revocation check | E4 |
| L2 | Login on a VM that hasn't yet received a just-registered user row (~100ms replication lag) will fail | E2 |
| L3 | No self-service password reset | E10 |
| L4 | Application-layer Redis rate limiting is not implemented (Nginx-only for now) | §8 |
