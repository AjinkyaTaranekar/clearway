# Distributed Vehicle Capacity System

A microservices-based system for managing vehicle journey bookings across road segments.

## Live Deployments

| Region | App | API Docs | Grafana | CockroachDB UI |
|--------|-----|----------|---------|----------------|
| EU (eu-west1) | [35.187.121.12](http://35.187.121.12) | [/docs/iam/](http://35.187.121.12/docs/iam/) · [/docs/journey/](http://35.187.121.12/docs/journey/) · [/docs/capacity/](http://35.187.121.12/docs/capacity/) · [/docs/map/](http://35.187.121.12/docs/map/) | [35.187.121.12:3000](http://35.187.121.12:3000) | [35.187.121.12:8080](http://35.187.121.12:8080) |
| US (us-east1) | [34.138.242.217](http://34.138.242.217) | [/docs/iam/](http://34.138.242.217/docs/iam/) · [/docs/journey/](http://34.138.242.217/docs/journey/) · [/docs/capacity/](http://34.138.242.217/docs/capacity/) · [/docs/map/](http://34.138.242.217/docs/map/) | [34.138.242.217:3000](http://34.138.242.217:3000) | [34.138.242.217:8080](http://34.138.242.217:8080) |
| APAC (asia-east1) | [34.80.180.64](http://34.80.180.64) | [/docs/iam/](http://34.80.180.64/docs/iam/) · [/docs/journey/](http://34.80.180.64/docs/journey/) · [/docs/capacity/](http://34.80.180.64/docs/capacity/) · [/docs/map/](http://34.80.180.64/docs/map/) | [34.80.180.64:3000](http://34.80.180.64:3000) | [34.80.180.64:8080](http://34.80.180.64:8080) |

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
└── specs/                  # Service specification documents
```
