# GeoDispatch

Autonomous crisis dispatcher — detects natural disasters, locates people via CAMARA network APIs, triages them into red/orange/green zones, and fires personalised survival SMS in under 4 seconds.

## Clarification

This is only the Go-Supervisor server-side part. Other parts will be coded elsewhere.

## How it works

```text
Disaster sensor
      ↓
Go Supervisor — fetches CAMARA APIs in parallel (50 concurrent goroutines)
      ↓
Zone assignment — haversine math in Go, never in AI
      ↓
Python AI Agent — decides action + crafts SMS per zone batch
      ↓
Go Supervisor — fires SMS/rescue in parallel + streams to gov dashboard via WebSocket
```

---

## Project structure

```text
geodispatch/
├── cmd/
│   └── supervisor/
│       └── main.go                  ← production/mock supervisor runtime
│
├── internal/
│   ├── models/
│   │   └── models.go                ← all shared structs + constants (contracts)
│   │
│   ├── camara/
│   │   ├── client.go                ← Nokia NaC HTTP client + auth hooks
│   │   ├── location.go              ← Location Retrieval API
│   │   ├── reachability.go          ← Device Reachability API
│   │   ├── qos.go                   ← QoS on Demand API
│   │   ├── congestion.go            ← Congestion Insights API
│   │   └── batch.go                 ← semaphore + concurrency orchestration
│   │
│   ├── zones/
│   │   ├── haversine.go             ← distance calculation (km from epicenter)
│   │   ├── assign.go                ← red/orange/green assignment logic
│   │   └── heap.go                  ← distance-priority batching + streaming
│   │
│   ├── agent/
│   │   ├── client.go                ← HTTP client that calls Python agent
│   │   └── prompt.go                ← Agent request shaping
│   │
│   ├── dispatch/
│   │   ├── sms.go                   ← SMS sender (stub/mock-safe, gateway-ready)
│   │   ├── rescue.go                ← rescue flag logger + dashboard push
│   │   └── worker.go                ← goroutine pool for parallel dispatch
│   │
│   ├── dashboard/
│   │   ├── server.go                ← websocket server wrappers
│   │   ├── hub.go                   ← connected-client hub + broadcasting
│   │   └── broadcast.go             ← WSUpdate emit helpers
│   │
│   ├── database/
│   │   ├── postgres.go              ← PostgreSQL connection pool + health check
│   │   ├── shelters.go              ← PostGIS nearest-shelter queries
│   │   ├── devices.go               ← device location queries + upsert
│   │   └── logs.go                  ← event/device_log/rescue_flag writes
│   │
│   ├── sensor/
│   │   └── handler.go               ← HTTP parser for incoming sensor POST
│   │
│   └── population/
│       └── provider.go              ← population data sourcing helpers
│
├── contracts/
│   ├── README.md                    ← contract rules for all team members
│   ├── sensor_input.json            ← sensor payload schema
│   ├── camara_device.json           ← CAMARA response schemas
│   ├── ai_request.json              ← Go → AI request schema
│   ├── ai_response.json             ← AI → Go response schema
│   └── ws_update.json               ← websocket update schema
│
├── migrations/
│   ├── 001_init.sql                 ← devices & shelters schema (PostGIS)
│   └── 002_events.sql               ← events, device_logs, rescue_flags schema
│
├── scripts/
│   ├── agent/
│   │   ├── Dockerfile               ← mock AI agent service container
│   │   └── mock_agent.go            ← zone-aware AI decision mock server
│   ├── camara/
│   │   ├── Dockerfile               ← mock CAMARA service container
│   │   └── mock_camara.go           ← location/reachability/qos mock server
│   ├── seed/
│   │   ├── seed_shelters.sql        ← MENA shelter coordinates (geo data)
│   │   └── seed_devices.sql         ← 40 test phones across red/orange/green zones
│   └── simulate_disaster.go         ← sends fake sensor events to supervisor
│
├── config/
│   └── config.go                    ← loads env and exposes typed config
│
├── docs/
│   ├── CHANGELOGS.md                ← v0.3.0 → v0.5.0 release notes
│   ├── CONTRIBUTING.md              ← contribution guidelines
│   ├── ERRORDOCS.md                 ← error handling policy
│   ├── LICENSE
│   ├── README.md
│   └── imgs/
│       ├── prototype_schema_01.png
│       ├── testing_prototype.jpeg
│       └── time_prediction.png
│
├── .env                             ← environment configuration (local)
├── .env.example                     ← environment template
├── .gitignore
├── .dockerignore                    ← Docker build context exclusions
├── docker-compose.yml               ← production Compose (Postgres + Supervisor)
├── docker-compose.dev.yml           ← full-stack dev Compose (all services + health checks)
├── Dockerfile                       ← supervisor container image (multi-stage)
├── go.mod
└── go.sum
```

---

## What each package owns

| Package | Responsibility |
|---|---|
| `cmd/supervisor` | Boots runtime, orchestrates production pipeline, HTTP routes |
| `internal/models` | Single source of truth for all contracts, enums, and typed payloads |
| `internal/camara` | CAMARA API integration (location, reachability, QoS, congestion, batching) |
| `internal/zones` | Pure geo logic: haversine distance, zone assignment, priority heap |
| `internal/agent` | AI HTTP client and request/response decoding only |
| `internal/dispatch` | Executes SMS/rescue actions from AI decisions |
| `internal/dashboard` | WebSocket hub and real-time message broadcasting to clients |
| `internal/database` | PostgreSQL connectivity, PostGIS queries (shelters, devices, logs, events) |
| `internal/sensor` | HTTP request parsing for incoming sensor payloads |
| `internal/population` | Population data sourcing (devices, affected persons) |
| `contracts/` | Locked inter-service JSON schema definitions |
| `migrations/` | Database schema versioning (SQL migrations) |
| `scripts/` | Containerized services (mock CAMARA, mock AI, disaster simulator, data seeders) |
| `config/` | Environment loading, typed config struct, defaults |

---

## Golden rules

- **Go calculates zones** — AI never touches coordinates or haversine math
- **AI crafts decisions/messages** — Go executes, validates, and dispatches
- **All timestamps** — Unix milliseconds `int64` at transport boundaries
- **All phone numbers** — E.164 format `+212XXXXXXXXX`
- **All zone values** — `"red"` · `"orange"` · `"green"` (lowercase, always)
- **Contracts are locked** — update contracts only with team-wide agreement
- **Errors are typed** — use `ErrorCode` constants, avoid raw string codes

---

## Prerequisites

- Go 1.22+
- PostgreSQL 15+ with PostGIS extension
- Docker & Docker Compose 3.9+
- Python 3.11+ (for optional real AI agent)
- CAMARA credentials (optional for local mocks)
- SMS gateway credentials (optional for local mocks)

---

## Setup

### 1. Clone and enter the project
```bash
git clone https://github.com/geoDispatch/supervisor
cd supervisor
```

### 2. Install Go dependencies
```bash
go mod tidy
```

### 3. Configure environment
```bash
cp .env.example .env
# Edit values only if needed — defaults point to localhost services
```

---

## Quick Start (Containerized — Recommended)

### Full development stack (all services + database)

```bash
# Start everything
docker-compose -f docker-compose.dev.yml up

# In another terminal, trigger a disaster event
go run scripts/simulate_disaster.go --event evt-001 --severity 6.8
```

This boots:
- PostgreSQL 16 + PostGIS (with automatic seed data)
- Mock CAMARA server (location/reachability/QoS)
- Mock AI Agent server (zone-based decisions)
- Supervisor (ready to receive events on `http://localhost:8080/sensor`)

### Production Compose (Postgres + Supervisor only)

```bash
docker-compose up
# Supervisor connects to real DB; external CAMARA/AI expected
```

---
## Production runtime

To run the production entrypoint:

```bash
go run cmd/supervisor/main.go
```

Current status:
- pipeline orchestration is in place
- full database integration with PostgreSQL + PostGIS
- real Docker Compose support for reproducible deployments
- intended for iterative hardening toward full production readiness

---

## Environment variables

```env
# SERVER CONFIGURATION
SERVER_PORT=8080

# MOCK SERVER PORTS (Local Testing)
MOCK_CAMARA_PORT=8081
MOCK_AGENT_PORT=5000

# NOKIA NETWORK AS CODE — CAMARA
NOKIA_NAC_BASE_URL=https://network-as-code.nokia.rapidapi.com
MOCK_NOKIA_NAC_BASE_URL=http://localhost:8081

NOKIA_NAC_HOST=network-as-code.nokia.rapidapi.com

# leave empty for mock
NOKIA_NAC_API_KEY=

# How old (seconds) a cached device location may be (600 = 10 min)
CAMARA_LOCATION_MAX_AGE_SEC=600

# AI AGENT
AGENT_URL=http://localhost:5000/decide

# DATABASE
DATABASE_URL=postgres://geodispatch:geodispatch@localhost:5432/geodispatch

# SMS GATEWAY (Africa's Talking)
AFRICASTALKING_API_KEY=your_sandbox_key_here
AFRICASTALKING_USERNAME=sandbox

# LOCAL SERVICES
OLLAMA_URL=http://localhost:11434

# CONCURRENCY LIMITS
CAMARA_CONCURRENCY=50
```

---

## Database schema

### `devices` table
Registered phones and their last-known location (populated from seed scripts or CAMARA live lookups).

### `shelters` table
Fixed disaster shelters with capacity and location (PostGIS geography for accurate distance ordering).

### `events` table
Top-level disaster event records (one per SensorInput).

### `device_logs` table
Audit trail: one row per DeviceDecision (phone, zone, action, SMS text, shelter assigned, AI confidence).

### `rescue_flags` table
Rescue queue: devices flagged for physical intervention, ordered by priority and timestamp.

---

## Error handling

Error semantics are centralized in:

- `docs/ERRORDOCS.md` — error handling policy and escalation rules
- typed codes in `internal/models/models.go` (`ErrorCode` constants)

Primary codes:
- `CAMARA_TIMEOUT` — Nokia NaC API timeout or failure
- `AGENT_ERROR` — Python AI agent crash or invalid response
- `SMS_FAILED` — SMS gateway rejection
- `DB_ERROR` — Database critical failure
- `QOS_FAILED` — QoS upgrade request failed

---

## Contributing

See `docs/CONTRIBUTING.md` for development guidelines, code standards, and PR workflow.

---

## Team

| Person | Role | Owns |
|---|---|---|
| ilias | Systems Lead | Go supervisor, CAMARA orchestration, architecture |
| yassine | AI Engineer | Python agent decision layer |
| ayoub / saad | Frontend & Dashboard | WebSocket dashboard and live visualization |
| houssam | DevOps & Integration | Testing, integration, runtime workflows, containerization |

---

## License

MIT — see `docs/LICENSE`