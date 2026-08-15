# GeoDispatch

Autonomous crisis dispatcher — detects natural disasters, locates people via CAMARA network APIs, triages them into red/orange/green zones, and fires personalised survival SMS in under 4 seconds.

## Clarification

This is only the Go-Supervisor server-side part, Other parts will be coded elsewhere.

## How it works

```
Disaster sensor
      ↓
Go Supervisor — fetches CAMARA APIs in parallel (50 concurrent goroutines)
      ↓
Zone assignment — haversine math in Go, never in AI
      ↓
Python AI Agent (Ollama, local) — decides action + crafts SMS per zone batch
      ↓
Go Supervisor — fires SMS in parallel + streams to gov dashboard via WebSocket
```

---

## Project structure

```
geodispatch/
├── cmd/
│   └── supervisor/
│       └── main.go                  ← entry point, boots everything
│
├── internal/
│   ├── models/
│   │   └── models.go                ← all shared structs + constants (contracts)
│   │
│   ├── camara/
│   │   ├── client.go                ← Nokia NaC HTTP client + auth
│   │   ├── location.go              ← Location Retrieval API
│   │   ├── reachability.go          ← Device Reachability API
│   │   ├── geofencing.go            ← Geofencing API
│   │   ├── qos.go                   ← QoS on Demand API
│   │   ├── congestion.go            ← Congestion Insights API
│   │   └── batch.go                 ← semaphore, 50 concurrent goroutines
│   │
│   ├── zones/
│   │   ├── haversine.go             ← distance calculation
│   │   └── assign.go                ← red/orange/green assignment logic
│   │
│   ├── agent/
│   │   ├── client.go                ← HTTP client that calls Python agent
│   │   └── prompt.go                ← builds AgentRequest per zone batch
│   │
│   ├── dispatch/
│   │   ├── sms.go                   ← Africa's Talking / Twilio SMS sender
│   │   ├── rescue.go                ← rescue flag logger + dashboard push
│   │   └── worker.go                ← goroutine pool for parallel dispatch
│   │
│   ├── dashboard/
│   │   ├── server.go                ← WebSocket server (gorilla/websocket)
│   │   ├── hub.go                   ← manages connected clients
│   │   └── broadcast.go             ← pushes WSUpdate to all clients
│   │
│   ├── database/
│   │   ├── postgres.go              ← connection pool setup
│   │   ├── shelters.go              ← PostGIS nearest shelter query
│   │   └── logs.go                  ← alert logs, event history
│   │
│   └── sensor/
│       └── handler.go               ← HTTP handler for incoming sensor POST
│
├── contracts/
│   ├── README.md                    ← contract rules for all team members
│   ├── sensor_input.json            ← example sensor payload
│   ├── camara_device.json           ← example CAMARA device data
│   ├── ai_request.json              ← example Go → Python payload
│   ├── ai_response.json             ← example Python → Go payload
│   └── ws_update.json               ← example WebSocket message
│
├── scripts/
│   ├── mock_server.go               ← fake server for team development
│   ├── simulate_disaster.go         ← sends fake sensor events for testing
│   └── seed_shelters.sql            ← populates PostGIS with MENA shelter data
│
├── config/
│   └── config.go                    ← loads .env, exposes typed config struct
│
├── docs/
│   ├── CHANGELOGS.md
│   ├── LICENSE
│   └── README.md
│
├── .env.example                     ← template for environment variables
├── .env                             ← real keys (gitignored)
└── .gitignore
```

---

## What each package owns

| Package | Responsibility |
|---|---|
| `cmd/supervisor` | Boots the HTTP server, wires all packages together, nothing else |
| `internal/models` | Single source of truth for all structs — nobody defines types outside this file |
| `internal/camara` | One file per CAMARA API — `batch.go` is the semaphore that prevents rate limiting |
| `internal/zones` | Pure math, no external dependencies, fully unit testable in isolation |
| `internal/agent` | Knows how to talk to Python and how to build the prompt — nothing else |
| `internal/dispatch` | Fires SMS and rescue flags — never decides who gets what, only executes |
| `internal/dashboard` | WebSocket only — no business logic, just pushes what Go tells it to push |
| `internal/database` | PostGIS queries only — `shelters.go` holds real MENA shelter coordinates |
| `internal/sensor` | One handler — receives POST from sensor, kicks off the entire pipeline |
| `contracts/` | JSON examples for every team member — Person 1 and Person 3 build against these |
| `scripts/` | `simulate_disaster.go` lets Person 5 run end-to-end tests without a real sensor |
| `config/` | One place for all env vars — everyone imports this instead of calling `os.Getenv` directly |

---

## Golden rules

- **Go calculates zones** — AI never touches coordinates or math
- **AI crafts messages** — Go never writes SMS text
- **All timestamps** — Unix milliseconds `int64`
- **All phone numbers** — E.164 format `+212XXXXXXXXX`
- **All zone values** — `"red"` · `"orange"` · `"green"` (lowercase, always)
- **Contracts are locked** — do not change `internal/models/models.go` without notifying all team members

---

## Prerequisites

- Go 1.23+
- Python 3.11+ (AI agent — separate repo)
- PostgreSQL 15+ with PostGIS extension
- Ollama with Llama 3.1 8B pulled locally
- Nokia NaC account — [networkascode.nokia.io](https://networkascode.nokia.io)
- Africa's Talking account (SMS gateway)

---

## Setup

**1. Clone and enter the project**
```bash
git clone https://github.com/yourteam/geodispatch
cd geodispatch
```

**2. Install Go dependencies**
```bash
go mod tidy
```

**3. Configure environment**
```bash
cp .env.example .env
# Edit .env with your real credentials
```

**4. Set up the database**
```bash
createdb geodispatch
psql geodispatch -c "CREATE EXTENSION postgis;"
psql geodispatch -f scripts/seed_shelters.sql
```

**5. Pull Ollama model**
```bash
ollama pull llama3.1
ollama create geodispatch-earthquake -f ai-agent/modelfiles/Modelfile.earthquake
ollama create geodispatch-flood      -f ai-agent/modelfiles/Modelfile.flood
ollama create geodispatch-heatwave   -f ai-agent/modelfiles/Modelfile.heatwave
```

**6. Start the Python AI agent** (separate terminal)
```bash
cd ../geodispatch-agent
pip install -r requirements.txt
python main.py
```

**7. Run GeoDispatch supervisor**
```bash
go run cmd/supervisor/main.go
```

**8. Simulate a disaster** (separate terminal, for testing)
```bash
go run scripts/simulate_disaster.go --type earthquake --magnitude 6.8 --lat 31.6295 --lng -7.9811
```

---

## Environment variables

```env
# Nokia NaC
NOKIA_NAC_TOKEN=your_token_here
NOKIA_NAC_BASE_URL=https://network-as-code.nokia.com/api

# Python AI Agent
AGENT_URL=http://localhost:5000/decide

# Database
DATABASE_URL=postgres://user:pass@localhost:5432/geodispatch

# SMS Gateway
AFRICASTALKING_API_KEY=your_key_here
AFRICASTALKING_USERNAME=your_username

# Server
WS_PORT=8080
SENSOR_PORT=8080
```

---

## Team

| Person | Role | Owns |
|---|---|---|
| ilias | Systems Lead | This repo — Go supervisor, CAMARA integration, architecture |
| yassine | AI Engineer | Python agent, Ollama Modelfiles, Nokia NaC SDK |
| ayoub | Frontend & Docs | React gov dashboard, Mapbox map, WebSocket client |
| saad | Frontend & Pitch Lead | Presentation, live demo, frontend support |
| houssam | Integration & DevOps | Testing, deployment, Nokia NaC simulators, DB setup |

---

## Data contracts

All inter-service communication is defined in `contracts/`. See `contracts/README.md` for the full contract specification and endpoint list.

---

## License

MIT — see `LICENSE`