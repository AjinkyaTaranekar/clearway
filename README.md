# Distributed Vehicle Capacity System

A microservices-based system for managing vehicle journey bookings across road segments.

## Live Deployments

HTTPS is terminated at the GCP global load balancer. HTTP on port 80 redirects to HTTPS.

| Region | App (HTTPS) | API Docs | Grafana | CockroachDB UI |
|--------|-------------|----------|---------|----------------|
| EU (europe-west1) | [35.244.162.92.nip.io](https://35.244.162.92.nip.io) | [/docs/iam/](https://35.244.162.92.nip.io/docs/iam/) · [/docs/journey/](https://35.244.162.92.nip.io/docs/journey/) · [/docs/capacity/](https://35.244.162.92.nip.io/docs/capacity/) · [/docs/map/](https://35.244.162.92.nip.io/docs/map/) | [35.187.121.12:3000](http://35.187.121.12:3000) | [35.187.121.12:8080](http://35.187.121.12:8080) |
| US (us-east1) | [35.227.198.68.nip.io](https://35.227.198.68.nip.io) | [/docs/iam/](https://35.227.198.68.nip.io/docs/iam/) · [/docs/journey/](https://35.227.198.68.nip.io/docs/journey/) · [/docs/capacity/](https://35.227.198.68.nip.io/docs/capacity/) · [/docs/map/](https://35.227.198.68.nip.io/docs/map/) | [34.138.242.217:3000](http://34.138.242.217:3000) | [34.138.242.217:8080](http://34.138.242.217:8080) |
| APAC (asia-east1) | [34.8.134.246.nip.io](https://34.8.134.246.nip.io) | [/docs/iam/](https://34.8.134.246.nip.io/docs/iam/) · [/docs/journey/](https://34.8.134.246.nip.io/docs/journey/) · [/docs/capacity/](https://34.8.134.246.nip.io/docs/capacity/) · [/docs/map/](https://34.8.134.246.nip.io/docs/map/) | [34.80.180.64:3000](http://34.80.180.64:3000) | [34.80.180.64:8080](http://34.80.180.64:8080) |

> HTTPS uses [nip.io](https://nip.io) wildcard DNS (no real domain required) with GCP-managed TLS certificates that auto-renew. See [`docs/https-setup.md`](docs/https-setup.md) for architecture details and the upgrade path to a real domain.

---

## Swagger Base URL Per Region

Each Go service supports a runtime Swagger base URL override via `VCS_SWAGGER_PUBLIC_BASE_URL`. This is injected automatically during deployment — Swagger "Try it out" calls point at the correct regional HTTPS gateway.

| Region | Value |
|--------|-------|
| EU | `https://35.244.162.92.nip.io` |
| US | `https://35.227.198.68.nip.io` |
| APAC | `https://34.8.134.246.nip.io` |

For local Docker Compose, it defaults to `http://localhost`.

---

## Prerequisites

| Tool | Version |
|------|---------|
| Node.js + npm | v18+ |
| Go | 1.24+ |
| PostgreSQL | 14+ (or Supabase account) |

---

## Frontend

Built with React 18, Vite, and TypeScript.

```bash
cd frontend
npm install
npm run dev
```

The dev server starts at `http://localhost:5173`.

To build for production:

```bash
npm run build
```

---

## Backend Services

All five services are written in Go and share the same Makefile structure.

| Service | Port | Responsibility |
|---------|------|----------------|
| `iam-service` | 8082 | Authentication, JWT, user/driver profiles |
| `journey-service` | 8083 | Booking lifecycle, saga orchestration |
| `capacity-service` | 8081 | Road segment slot management |
| `map-service` | 8084 | Route planning, Dijkstra shortest path |
| `notification-service` | 8085 | Push notifications via Firebase |

### First-time setup (per service)

```bash
cd <service-name>          # e.g. cd journey-service
make install-tools         # installs golangci-lint and swag
```

### Running a service

```bash
make run       # go run ./cmd/server
# or
make dev       # hot reload via air (falls back to go run if air not installed)
```

### Other useful commands

```bash
make build     # compile binary to ./bin/
make test      # run tests with coverage
make swagger   # regenerate Swagger docs in ./docs/
make check     # fmt + vet + lint + test
make tidy      # go mod tidy
```

### Configuration

Each service reads from its own `config.yaml`. Update the database section with your PostgreSQL credentials before running:

```yaml
server:
  port: 8082          # unique per service (see table above)

database:
  master:
    host: "your-db-host"
    port: 5432
    user: "postgres"
    password: "your-password"
    dbname: "postgres"
  slave:
    host: "your-db-host"
    port: 5432
    user: "postgres"
    password: "your-password"
    dbname: "postgres"
```

---

## Running with Docker

Each service has a multi-stage Dockerfile.

```bash
cd <service-name>
make docker-build      # builds image tagged vcs-<service>:latest
make docker-run        # runs container on port 8080
```

---

## Project Structure

```
.
├── frontend/               # React + Vite + TypeScript UI
├── iam-service/            # Identity & access management (port 8082)
├── journey-service/        # Journey booking orchestrator (port 8083)
├── capacity-service/       # Segment capacity manager (port 8081)
├── map-service/            # Route & map service (port 8084)
├── notification-service/   # Push notification consumer (port 8085)
├── docs/                   # Architecture and operational documentation
└── specs/                  # Service specification documents
```
