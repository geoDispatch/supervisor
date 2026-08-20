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
│   │   ├── postgres.go              ← DB connector (mock mode supported)
│   │   ├── shelters.go              ← nearest shelter query hooks
│   │   ├── devices.go               ← device/phone DB operations
│   │   └── logs.go                  ← alert logs, event history
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
│   ├── 001_init.sql                 ← schema initialization
│   └── 002_events.sql               ← event tracking tables
│
├── scripts/
│   ├── mock_camara.go               ← local mock CAMARA server (location/reachability/qos)
│   ├── mock_agent.go                ← local mock AI decision server (zone-aware)
│   ├── simulate_disaster.go         ← sends fake sensor events for testing
│   └── seed_shelters.sql            ← populates shelters DB with MENA coordinates
│
├── config/
│   └── config.go                    ← loads env and exposes typed config
│
├── docs/
│   ├── CHANGELOGS.md                ← v0.3.0 → v0.3.1 release notes
│   ├── CONTRIBUTING.md              ← contribution guidelines
│   ├── ERRORDOCS.md                 ← error handling policy
│   ├── LICENSE
│   ├── README.md
│   └── imgs/
│       ├── prototype_schema_01.png
│       ├── testing_prototype.jpeg
│       └── time_prediction.png
│
├── .env.example                     ← mock-first env template
├── .gitignore
├── docker-compose.yml               ← containerization (v0.3.1+)
├── Dockerfile                       ← supervisor container image
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
| `internal/database` | PostgreSQL connectivity, queries (shelters, devices, logs) |
| `internal/sensor` | HTTP request parsing for incoming sensor payloads |
| `internal/population` | Population data sourcing (devices, affected persons) |
| `contracts/` | Locked inter-service JSON schema definitions |
| `migrations/` | Database schema versioning (Flyway-style SQL) |
| `scripts/` | Local simulation stack (mock CAMARA, mock AI, disaster simulation, data seeding) |
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

- Go 1.18+
- Python 3.11+ (for optional real AI agent)
- PostgreSQL/PostGIS (optional in `DATABASE_URL=mock` mode)
- Docker & Docker Compose (optional for containerized deployment)
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
# Edit values only if needed — defaults are mock-first
```

---

### Option B: Containerized (v0.3.1+, scaffold mode)

Docker Compose support is introduced as a scaffold baseline. Currently the `docker-compose.yml` is minimal/intentional to allow incremental service definitions.

Future containerization will enable:
```bash
docker-compose up
# Single command to run supervisor + mocks + optional database
```

---

## Production runtime (skeleton)

To run the production entrypoint:

First, Add the API_KEY in the .env file

```bash
go run cmd/supervisor/main.go
```

Current status:
- pipeline orchestration is in place
- dispatch/DB/CAMARA contain mock-safe and scaffolded logic
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
NOKIA_NAC_API_KEY=              # leave empty for mock

# How old (seconds) a cached device location may be (600 = 10 min)
CAMARA_LOCATION_MAX_AGE_SEC=600

# AI AGENT
AGENT_URL=http://localhost:5000/decide

# DATABASE
DATABASE_URL=postgres://user:pass@localhost:5432/geodispatch

# SMS GATEWAY (Africa's Talking)
AFRICASTALKING_API_KEY=your_sandbox_key_here
AFRICASTALKING_USERNAME=sandbox

# LOCAL SERVICES
OLLAMA_URL=http://localhost:11434

# CONCURRENCY LIMITS
CAMARA_CONCURRENCY=50
```

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