# GeoDispatch

Autonomous crisis dispatcher — detects natural disasters, locates people via CAMARA network APIs, triages them into red/orange/green zones, and fires personalised survival SMS in under 4 seconds.

## Clarification

This repository contains only the Go Supervisor server-side component. The AI agent, dashboard, and deployment integrations may be maintained separately.

## How it works

```text
Disaster sensor
      ↓
Go Supervisor — fetches CAMARA APIs in parallel
      ↓
Zone assignment — haversine math in Go, never in AI
      ↓
Python AI Agent — decides action and crafts SMS per zone batch
      ↓
Go Supervisor — validates decisions, dispatches SMS/rescue actions,
                 and streams updates to the government dashboard
```

---

## Project structure

```text
supervisor/
├── cmd/
│   └── supervisor/
│       └── main.go                  ← supervisor runtime and shutdown wiring
│
├── internal/
│   ├── auth/                        ← JWT, API-key, password, and rate limiting
│   │   ├── jwt.go
│   │   ├── middleware.go
│   │   ├── password.go
│   │   └── ratelimit.go
│   │
│   ├── models/                      ← shared contracts, enums, payloads, ORM models
│   │   ├── models.go
│   │   ├── models_test.go
│   │   ├── orm.go
│   │   ├── phone.go
│   │   └── ws.go
│   │
│   ├── camara/                      ← Nokia NaC and mock CAMARA clients
│   │   ├── batch.go
│   │   ├── client.go
│   │   ├── congestion.go
│   │   ├── location.go
│   │   ├── qos.go
│   │   └── reachability.go
│   │
│   ├── zones/                       ← geographic and device-priority logic
│   │   ├── assign.go
│   │   ├── haversine.go
│   │   └── heap.go
│   │
│   ├── agent/                       ← AI HTTP client and request shaping
│   │   ├── client.go
│   │   └── prompt.go
│   │
│   ├── dispatch/                    ← SMS and rescue dispatch
│   │   ├── rescue.go
│   │   ├── sms.go
│   │   └── worker.go
│   │
│   ├── dashboard/                   ← WebSocket hub, replay, and broadcasting
│   │   ├── broadcast.go
│   │   ├── hub.go
│   │   ├── hub_test.go
│   │   └── server.go
│   │
│   ├── database/                    ← PostgreSQL, PostGIS, and GORM access
│   │   ├── devices.go
│   │   ├── logs.go
│   │   ├── orm.go
│   │   ├── postgres.go
│   │   └── shelters.go
│   │
│   ├── httpapi/                     ← HTTP routes and API handlers
│   │   └── httpapi.go
│   │
│   ├── origin/                      ← browser-origin policy
│   │   └── origin.go
│   │
│   ├── pipeline/                    ← incident lifecycle and orchestration
│   │   ├── batch.go
│   │   ├── manager.go
│   │   ├── pipeline.go
│   │   ├── run.go
│   │   └── validate.go
│   │
│   ├── population/
│   │   └── provider.go              ← population data sourcing helpers
│   │
│   └── sensor/
│       ├── decode.go                ← sensor payload decoding and validation
│       └── decode_test.go
│
├── contracts/
│   ├── README.md                    ← contract synchronization rules
│   ├── sensor_input.json            ← sensor payload schema
│   ├── camara_device.json           ← CAMARA response schemas
│   ├── ai_request.json              ← Go → AI request schema
│   ├── ai_response.json             ← AI → Go response schema
│   └── ws_update.json               ← WebSocket v2 schema
│
├── migrations/
│   ├── 001_init.sql                 ← devices and shelters schema
│   ├── 002_events.sql               ← events, logs, and rescue flags
│   └── 003_auth.sql                 ← users and API keys
│
├── scripts/
│   ├── agent/
│   │   ├── Dockerfile
│   │   ├── mock_agent.go
│   │   └── mock_agent_test.go
│   │
│   ├── camara/
│   │   ├── areas.go                 ← development fixture areas
│   │   ├── areas_test.go
│   │   ├── Dockerfile
│   │   ├── fixtures.json
│   │   ├── fixtures_test.go
│   │   └── mock_camara.go
│   │
│   ├── seed/
│   │   ├── seed_demo_areas.sql
│   │   ├── seed_devices.sql
│   │   └── seed_shelters.sql
│   │
│   └── simulation/
│       ├── sensor/
│       │   ├── main.go
│       │   └── main_test.go
│       └── wswatch/
│           ├── main.go
│           └── main_test.go
│
├── config/
│   ├── config.go
│   └── config_test.go
│
├── docs/
│   ├── CHANGELOGS.md
│   ├── CONTRIBUTING.md
│   ├── ERRORDOCS.md
│   ├── LICENSE
│   └── imgs/
│
├── .env.example
├── .gitignore
├── .dockerignore
├── docker-compose.yml
├── docker-compose.dev.yml
├── docker-compose.standalone.yml
├── Dockerfile
├── go.mod
└── go.sum
```

---

## What each package owns

| Package | Responsibility |
|---|---|
| `cmd/supervisor` | Boots the runtime and wires the server components |
| `internal/auth` | JWT, API-key, bcrypt password, and rate-limit middleware |
| `internal/models` | Shared contracts, enums, payloads, WebSocket models, and ORM models |
| `internal/camara` | CAMARA integration: location, reachability, QoS, congestion, and batching |
| `internal/zones` | Haversine distance, zone assignment, and priority helpers |
| `internal/agent` | AI HTTP client, health checks, and response decoding |
| `internal/dispatch` | Executes SMS and rescue actions from AI decisions |
| `internal/dashboard` | WebSocket hub, replay snapshots, heartbeats, and broadcasts |
| `internal/database` | PostgreSQL/PostGIS queries and GORM-backed API persistence |
| `internal/httpapi` | Sensor, health, capabilities, authentication, and REST routes |
| `internal/origin` | Browser-origin allowlisting for HTTP and WebSocket access |
| `internal/pipeline` | Incident lifecycle, triage, AI batches, validation, and shutdown |
| `internal/sensor` | Sensor payload decoding and validation |
| `internal/population` | Population and affected-device data helpers |
| `contracts/` | Locked inter-service JSON schema definitions |
| `migrations/` | Database schema versioning |
| `scripts/` | Mock services, seed data, and local simulation tools |
| `config/` | Environment loading, typed configuration, and defaults |

---

## Golden rules

- **Go calculates zones** — AI never performs coordinate calculations or haversine math.
- **AI crafts decisions and messages** — Go validates, executes, and dispatches them.
- **All timestamps** — Unix milliseconds, represented as `int64` at transport boundaries.
- **All phone numbers** — E.164 format, for example `+212XXXXXXXXX`.
- **All zone values** — `"red"`, `"orange"`, or `"green"` in lowercase.
- **Contracts are locked** — update canonical contracts only with team-wide agreement.
- **Errors are typed** — use `ErrorCode` constants rather than raw string codes.
- **SMS state is explicit** — if no gateway is configured, an SMS must be reported as `not_configured`, never as sent.
- **WebSocket frames are versioned** — dashboard clients must support the WebSocket contract version advertised by the frame.

---

## Prerequisites

- Go 1.22 or newer
- PostgreSQL 15 or newer with PostGIS
- Docker and Docker Compose
- Python 3.11 or newer for the optional external AI agent
- CAMARA credentials for real-network mode
- SMS gateway credentials for production SMS dispatch

---

## Setup

### 1. Clone the repository

```bash
git clone https://github.com/geoDispatch/supervisor
cd supervisor
```

### 2. Install Go dependencies

```bash
go mod tidy
```

### 3. Configure the environment

```bash
cp .env.example .env
```

Edit `.env` when using real CAMARA, AI, database, authentication, or SMS services.

---

## Quick start

### Full standalone development stack

The standalone Compose file runs PostgreSQL/PostGIS, the mock CAMARA service, the mock AI agent, and the supervisor.

```bash
docker compose -f docker-compose.standalone.yml up -d --build
```

The services are available at:

| Service | Address |
|---|---|
| Supervisor | `http://localhost:8080` |
| Mock CAMARA | `http://localhost:8081` |
| Mock AI agent | `http://localhost:8082` |
| PostgreSQL | `localhost:5432` |

### Watch the WebSocket stream

In another terminal:

```bash
go run ./scripts/simulation/wswatch
```

### Send a simulated disaster event

```bash
go run ./scripts/simulation/sensor -h
```

The database initialization scripts run when the PostgreSQL volume is created for the first time. To recreate the database and rerun the seed scripts:

```bash
docker compose -f docker-compose.standalone.yml down -v
docker compose -f docker-compose.standalone.yml up -d --build
```

### Production Compose

The production Compose file runs PostgreSQL and the supervisor. External CAMARA and AI services are expected.

```bash
docker compose up
```

---

## Run locally without Docker

After configuring PostgreSQL and the required service URLs:

```bash
go run ./cmd/supervisor
```

Run the test suite:

```bash
go test ./...
```

Run tests with the race detector:

```bash
go test -race ./...
```

---

## HTTP and WebSocket endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/sensor` | Accepts a disaster sensor event |
| `GET` | `/health` | Readiness and dependency health status |
| `GET` | `/livez` | Lightweight liveness probe |
| `GET` | `/capabilities` | Reports supported disaster capabilities |
| `GET` | `/ws` | Dashboard WebSocket stream |
| `POST` | `/auth/register` | Registers an operator account |
| `POST` | `/auth/login` | Issues a JWT access token |
| `*` | `/api/*` | Protected REST API routes |

Protected API routes accept either a JWT:

```http
Authorization: Bearer <jwt>
```

or an API key:

```http
X-API-Key: <api-key>
```

---

## WebSocket behavior

The dashboard WebSocket uses contract version 2.

Supported frame types include:

- `snapshot_begin`
- `snapshot_end`
- `heartbeat`
- `event_start`
- `event_context`
- `device_update`
- `zone_summary`
- `narrative_update`
- `error`
- `event_complete`

The WebSocket hub provides:

- Per-event sequence numbers
- Reconnection snapshots
- Replay of the latest event state
- Heartbeats and ping/pong handling
- Per-client buffered queues
- Slow-client detection
- Origin allowlisting
- Graceful shutdown behavior

Dashboard clients should use `seq` to detect missing live frames. A reconnect should rebuild state from the received snapshot.

---

## Environment variables

The complete environment template is maintained in `.env.example`.

Important settings include:

```env
# SERVER
SERVER_PORT=8080
GEODISPATCH_ENV=development
ALLOWED_ORIGINS=

# CAMARA
MOCK_CAMARA_PORT=8081
NOKIA_NAC_BASE_URL=https://network-as-code.nokia.rapidapi.com
MOCK_NOKIA_NAC_BASE_URL=http://mock_camara:8081
NOKIA_NAC_HOST=network-as-code.nokia.rapidapi.com
NOKIA_NAC_API_KEY=
CAMARA_LOCATION_MAX_AGE_SEC=600
CAMARA_TIMEOUT_MS=5000
CAMARA_REACHABILITY_TIMEOUT_MS=1000
CAMARA_CONCURRENCY=50

# AI AGENT
AGENT_URL=http://mock_agent:8082/decide
AGENT_TIMEOUT_SEC=120
AGENT_BATCH_SIZE=20

# DATABASE
DATABASE_URL="postgres://geodispatch:geodispatch@postgres:5432/geodispatch?sslmode=disable"

# AUTHENTICATION
JWT_SECRET=change-me-in-production
RATE_LIMIT_RPS=10

# PIPELINE
PIPELINE_TIMEOUT_SEC=900
SENSOR_MAX_BODY_BYTES=16384

# WEBSOCKET
WS_CLIENT_QUEUE_LIMIT=65536
WS_WRITE_TIMEOUT_SEC=10
WS_HEARTBEAT_SEC=15

# SMS
SMS_GATEWAY=
```

For production, replace development defaults with strong secrets and externally managed service URLs.

---

## Database schema

### `devices`

Registered phone numbers and their last-known locations. Locations are stored using PostGIS geography data.

### `shelters`

Fixed disaster shelters with names, addresses, capacities, and geographic coordinates.

### `events`

Top-level disaster event records. Each accepted sensor event creates one event record.

### `device_logs`

Audit records for AI decisions, including:

- Event ID
- Phone number
- Assigned zone
- Action
- SMS message
- Shelter name
- Rescue priority
- AI confidence
- Escalation state

### `rescue_flags`

Rescue queue entries for devices requiring physical intervention.

### `users`

Operator accounts with bcrypt password hashes.

### `api_keys`

Machine-to-machine credentials. Only bcrypt hashes are persisted.

---

## Authentication and security

The supervisor supports:

- JWT access tokens signed with `JWT_SECRET`
- 24-hour JWT token lifetime
- Bcrypt password hashing
- API-key authentication
- Per-IP token-bucket rate limiting
- Browser-origin allowlisting
- Redacted phone numbers in operational error messages
- Strict JSON decoding for upstream AI responses
- Response-body size limits for CAMARA and AI requests

Never commit `.env` files, production JWT secrets, API keys, passwords, or SMS credentials.

---

## Error handling

Error semantics are centralized in:

- `docs/ERRORDOCS.md`
- typed codes in `internal/models/models.go`
- WebSocket error frames in `contracts/ws_update.json`

Primary error codes include:

- `CAMARA_TIMEOUT` — CAMARA request timeout or lookup failure
- `CAMARA_ERROR` — CAMARA request or response failure
- `AGENT_ERROR` — AI agent failure
- `AGENT_INVALID_RESPONSE` — invalid or malformed AI response
- `SMS_FAILED` — SMS gateway rejection
- `DB_ERROR` — database failure
- `QOS_FAILED` — QoS request or upgrade failure
- `INTERNAL_ERROR` — unexpected supervisor failure

Non-fatal errors are reported to the dashboard when processing can continue. Fatal errors stop the current pipeline and are followed by an `event_complete` frame with a failed status.

---

## Contracts

The `contracts/` directory contains the inter-service schemas used by the supervisor.

Important contracts include:

- `sensor_input.json` — sensor payload accepted by `POST /sensor`
- `camara_device.json` — CAMARA response and triage shapes
- `ai_request.json` — supervisor-to-agent request
- `ai_response.json` — agent-to-supervisor response
- `ws_update.json` — WebSocket v2 frame definitions

Contract changes must be coordinated with the AI, dashboard, and deployment components.

---

## Contributing

See [`docs/CONTRIBUTING.md`](CONTRIBUTING.md) for:

- Development workflow
- Code style
- Testing expectations
- Commit conventions
- Pull request requirements

Before opening a pull request:

```bash
gofmt -w .
go test ./...
go test -race ./...
```

---

## Team

| Person | Role | Owns |
|---|---|---|
| [@ilias](https://github.com/iliassovic2003) | Systems Lead | Go supervisor, CAMARA orchestration, architecture |
| [@yassine](https://github.com/yassinsl) | AI Engineer | Python agent decision layer |
| [@ayoub](https://github.com/AelElz) / [@saad](https://github.com/saadzaoual) | Frontend and Documentation | WebSocket dashboard and live visualization |
| [@houssam](https://github.com/macrovvave) | DevOps and Integration | Testing, integration, runtime workflows, and containerization |

---

## License

MIT — see [`docs/LICENSE`](LICENSE).