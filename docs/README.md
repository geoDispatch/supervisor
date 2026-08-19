# GeoDispatch

Autonomous crisis dispatcher — detects natural disasters, locates people via CAMARA network APIs, triages them into red/orange/green zones, and fires personalised survival SMS in under 4 seconds.

## Clarification

This is only the Go-Supervisor server-side part, Other parts will be coded elsewhere.

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
│   ├── supervisor/
│   │   └── main.go                  ← production supervisor runtime
│   └── testing/
│       └── main.go                  ← local simulation runtime (mock-first)
│
├── internal/
│   ├── models/
│   │   └── models.go                ← all shared structs + constants (contracts)
│   │
│   ├── camara/
│   │   ├── client.go                ← Nokia NaC HTTP client + auth hooks
│   │   ├── location.go              ← Location Retrieval API helpers
│   │   ├── reachability.go          ← Device Reachability API helpers
│   │   ├── geofencing.go            ← Geofencing API helpers
│   │   ├── qos.go                   ← QoS on Demand helpers
│   │   ├── congestion.go            ← Congestion insights helpers
│   │   └── batch.go                 ← CAMARA batch/concurrency orchestration
│   │
│   ├── zones/
│   │   ├── haversine.go             ← distance calculation
│   │   ├── assign.go                ← red/orange/green assignment logic
│   │   └── heap.go                  ← distance-priority batching
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
│   │   └── logs.go                  ← alert logs, event history
│   │
│   └── sensor/
│       └── handler.go               ← HTTP parser for incoming sensor POST
│
├── contracts/
│   ├── README.md                    ← contract rules for all team members
│   ├── sensor_input.json            ← sensor payload schema
│   ├── camara_device.json           ← CAMARA response schemas
│   ├── ai_request.json              ← Go → AI request schema
│   ├── ai_response.json             ← AI → Go response schema
│   └── ws_update.json               ← websocket update schema
│
├── scripts/
│   ├── mock_camara.go               ← local mock CAMARA server (location/reachability/qos)
│   ├── mock_agent.go                ← local mock AI decision server
│   └── simulate_disaster.go         ← sends fake sensor events for testing
│
├── config/
│   └── config.go                    ← loads env and exposes typed config
│
├── docs/
│   ├── CHANGELOGS.md
│   ├── ERRORDOCS.md
│   ├── LICENSE
│   ├── README.md
│   └── imgs/
│       ├── prototype_schema_01.png
│       ├── testing_prototype.jpeg
│       └── time_prediction.png
│
├── .env.example                     ← mock-first env template
├── .gitignore
├── go.mod
└── go.sum
```

---

## What each package owns

| Package | Responsibility |
|---|---|
| `cmd/supervisor` | Boots the runtime and orchestrates the production pipeline |
| `cmd/testing` | Runs the same orchestration in local mock mode for rapid validation |
| `internal/models` | Single source of truth for all contracts, enums, and typed payloads |
| `internal/camara` | CAMARA API integration layer (location/reachability/qos/geofencing) |
| `internal/zones` | Pure geo logic + priority ordering (haversine/assign/heap) |
| `internal/agent` | AI transport and request/response decoding only |
| `internal/dispatch` | Executes SMS/rescue actions from AI decisions |
| `internal/dashboard` | WebSocket client hub and dashboard message broadcasting |
| `internal/database` | DB connectivity + query hooks (mock-safe path available) |
| `internal/sensor` | Sensor input parsing and request boundary validation |
| `contracts/` | Locked inter-service schema definitions |
| `scripts/` | Local simulation stack (mock CAMARA, mock AI, fake sensor events) |
| `config/` | Env config + defaults + concurrency tuning |

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

## Local simulation (recommended for current phase)

### Terminal A — start mock CAMARA
```bash
go run scripts/mock_camara.go
```

### Terminal B — start mock AI agent
```bash
go run scripts/mock_agent.go
```

### Terminal C — start supervisor in testing mode
```bash
go run cmd/testing/main.go
```

### Terminal D — trigger a disaster event
```bash
go run scripts/simulate_disaster.go --type earthquake --magnitude 6.8 --lat 33.5731 --lng -7.5898
```

You should see:
- per-batch device decision tables in terminal output
- zone summaries and narrative pushes
- websocket updates on `/ws`

---

## Production runtime (skeleton)

To run the production entrypoint:

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
# Server
SERVER_PORT=8080

# Mock services (local)
MOCK_CAMARA_PORT=8081
MOCK_AGENT_PORT=5000

# External service URLs (point to mocks by default)
NOKIA_NAC_BASE_URL=http://localhost:8081
NOKIA_NAC_TOKEN=mock_token_for_testing
AGENT_URL=http://localhost:5000/decide

# Database
DATABASE_URL=mock

# SMS Gateway
AFRICASTALKING_API_KEY=your_sandbox_key_here
AFRICASTALKING_USERNAME=sandbox

# Local services
OLLAMA_URL=http://localhost:11434

# Concurrency
CAMARA_CONCURRENCY=50
SMS_CONCURRENCY=100
```

---

## Data contracts

All inter-service communication is defined under `contracts/`:

- `sensor_input.json`
- `camara_device.json`
- `ai_request.json`
- `ai_response.json`
- `ws_update.json`

Notable contract update:
- `nearest_shelter` → `nearest_shelters` in AI request schema (array, up to 3).

---

## Error handling

Error semantics are centralized in:

- `docs/ERRORDOCS.md`
- typed codes in `internal/models/models.go` (`ErrorCode`)

Primary codes:
- `CAMARA_TIMEOUT`
- `AGENT_ERROR`
- `SMS_FAILED`
- `DB_ERROR`
- `QOS_FAILED`

---

## Changelog

Detailed release history:
- `docs/CHANGELOGS.md`

Latest tracked version:
- **v0.3.0** (mock-driven end-to-end simulation + pipeline expansion)

---

## Team

| Person | Role | Owns |
|---|---|---|
| ilias | Systems Lead | Go supervisor, CAMARA orchestration, architecture |
| yassine | AI Engineer | Python agent decision layer |
| ayoub / saad | Frontend & Dashboard | WebSocket dashboard and live visualization |
| houssam | DevOps & Integration | Testing, integration, runtime workflows |

---

## License

MIT — see `docs/LICENSE`