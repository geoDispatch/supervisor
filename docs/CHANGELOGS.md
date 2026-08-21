```text
════════════════════════════════════════════════════════════════
                GEODISPATCH SUPERVISOR — CHANGELOG
════════════════════════════════════════════════════════════════

────────────────────────────────────────────────────────────────
  v0.0.0 => v0.1.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + Initial architectural baseline
      - Created foundational repository structure:
          cmd/, config/, contracts/, internal/, docs/
      - Added initial technical documentation:
          docs/README.md
          docs/ERRORDOCS.md
          contracts/README.md
      - Established cross-team contract ownership boundaries (Go / AI / Dashboard / DevOps)


────────────────────────────────────────────────────────────────
  v0.1.0 => v0.1.1                                      [MINOR]
────────────────────────────────────────────────────────────────
  + Repository hygiene baseline
  · Added `.gitignore` for binaries, env files, vendor deps, and IDE artifacts
  · Prepared clean commit discipline for rapid Go iteration
  (From now on, i'll be documenting as a minor update to the project).


────────────────────────────────────────────────────────────────
  v0.1.1 => v0.2.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + Core supervisor bootstrap (Go)
      - Implemented first end-to-end orchestration skeleton in `cmd/supervisor/main.go`
      - Added HTTP routes:
          POST /sensor   — ingest disaster signal
          GET  /ws       — dashboard websocket stream
          GET  /health   — health probe
      - Introduced concurrent pipeline flow:
          CAMARA area calls + shelter lookup + per-device triage + AI batching + dashboard broadcast
  + Configuration system hardening
      - Expanded `config/config.go` with required env validation and sane defaults
      - Added typed env parsing (`mustEnv`, `getEnv`, `getEnvInt`)
      - Introduced concurrency knobs (CAMARA/SMS limits)
  + Contract-first architecture
      - Added/expanded JSON contracts:
          contracts/sensor_input.json
          contracts/camara_device.json
          contracts/ai_request.json
          contracts/ai_response.json
          contracts/ws_update.json
      - Upgraded `contracts/README.md` into full governance/spec guide
  + Go module initialization
      - Added `go.mod` / `go.sum`
      - Added dependency: `github.com/joho/godotenv`
  + Internal package scaffolding
      - Promoted empty placeholders to compilable package stubs:
          internal/agent
          internal/camara
          internal/dashboard
          internal/database
          internal/dispatch
  + Documentation assets
      - Added architecture support images under `docs/imgs/`
  / Documentation structure alignment
      - Shifted project docs ownership under `docs/` structure
      - Removed root-level starter `README.md` / `LICENSE` in favor of docs-oriented layout


────────────────────────────────────────────────────────────────
  v0.2.0 => v0.3.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + Full mock-driven testing environment introduced
      - Added `.env.example` tailored for local simulation
      - Added dedicated mock service ports and defaults:
          MOCK_CAMARA_PORT=8081
          MOCK_AGENT_PORT=5000
          DATABASE_URL=mock
  + Added end-to-end local simulation stack
      - Added `scripts/mock_camara.go` with deterministic location/reachability/QoS responses
      - Added `scripts/mock_agent.go` with zone-based AI decision simulation
      - Added `cmd/testing/main.go` as a full pipeline test harness with rich console telemetry
      - Removed obsolete `scripts/mock_server.go`
  + Pipeline dispatch stage materially expanded in supervisor runtime
      - Added parallel per-decision execution for:
          SMS dispatch
          rescue flagging
          websocket updates
      - Added post-AI batch processing with:
          zone summary aggregation
          narrative broadcast
          optional QoS upgrade trigger
      - Added final event completion logging and timing output
  + Zone processing primitives implemented
      - Added `internal/zones/heap.go` for distance-priority batching
      - Implemented `internal/zones/haversine.go` for epicenter distance math
      - Implemented `internal/zones/assign.go` for red/orange/green zone assignment
  + Core package implementations landed (from stubs to executable logic)
      - `internal/agent/client.go` now performs real HTTP JSON decision calls
      - `internal/camara/client.go` now performs location/reachability/QoS requests
      - `internal/dashboard/hub.go` now provides live websocket connection + broadcast wrappers
      - `internal/sensor/handler.go` now parses incoming sensor payloads
      - `internal/database/postgres.go` now supports safe mock-mode DB initialization
      - `internal/dispatch/sms.go` now provides compile-safe SMS/rescue dispatch stubs
  + Model and contract evolution
      - Updated `contracts/ai_request.json`:
          `nearest_shelter` → `nearest_shelters` (array, up to 3)
      - Updated internal models with stronger typed enums:
          `AftershockRisk` type introduced
          `ErrorCode` constants introduced (CAMARA_TIMEOUT / AGENT_ERROR / SMS_FAILED / DB_ERROR / QOS_FAILED)
      - Extended `DeviceDecision` with `shelter_name`
  + Documentation and compliance improvements
      - Added structured `docs/CHANGELOGS.md`
      - Significantly expanded `docs/ERRORDOCS.md` with error taxonomy and handling policy
      - Added MIT license file under `docs/LICENSE`
      - Added testing illustration asset: `docs/imgs/testing_prototype.jpeg`
  · Configuration profile simplified for local-first development
      - Refactored `config/config.go` to default to mock-friendly values
      - Reduced strict env requirements to streamline test bootstrapping
  · Dependency updates
      - Added `github.com/gorilla/websocket v1.5.3` to support dashboard realtime channel

────────────────────────────────────────────────────────────────
  v0.3.2 => v0.4.0                                      [MINOR]
────────────────────────────────────────────────────────────────
  + Containerization groundwork added
      - Introduced `docker-compose.yml` as a starter scaffold (currently minimal/empty by design).
      - Prepared the project for future containerized execution of:
          supervisor service,
          mock CAMARA,
          mock AI agent,
          optional database stack.
      - Established a path from multi-terminal local runs to one-command orchestrated startup.

  · Pipeline and runtime refinements
      - Improved consistency across the full execution flow:
          sensor ingest → CAMARA → AI decisioning → dispatch → websocket updates.
      - Reduced instability in concurrent/batch processing paths during local simulation.
      - Improved local mock behavior alignment with real runtime expectations.

  · Contract/model alignment updates
      - Applied small schema and typed-field consistency improvements between supervisor and AI boundaries.
      - Tightened internal message handling for cleaner interoperability and fewer integration mismatches.

  / Fixes and cleanup
      - Resolved simulation-time regressions found during iterative testing.
      - Reduced noisy failure behavior in batch runs.
      - Improved release-note and docs consistency for better traceability.

────────────────────────────────────────────────────────────────
  v0.4.2 => v0.5.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + Real PostgreSQL integration with PostGIS support
      - Implemented `internal/database/postgres.go` with connection pooling
      - Added context-aware DB operations with proper error handling
      - Integrated `github.com/lib/pq` PostgreSQL driver
      - Replaced mock-only DB mode with production-ready Postgres client

  + Database schema and migrations foundation
      - Created `migrations/001_init.sql`:
          devices table (phone, location, GIS index)
          shelters table (name, address, capacity, location)
      - Created `migrations/002_events.sql`:
          events table (disaster events, metadata)
          device_logs table (AI decisions, audit trail)
          rescue_flags table (rescue queue with priority)
      - All tables use PostGIS geography types for accurate distance calculations

  + Full database operations layer
      - `internal/database/devices.go` — PhonesNearEpicenter query + UpsertDeviceLocation
      - `internal/database/shelters.go` — NearestShelters with PostGIS distance ordering
      - `internal/database/logs.go` — InsertEvent, InsertDeviceLog, FlagRescue, RescueFlagsForEvent
      - All operations are context-aware and transaction-safe

  + Production-grade Docker Compose setup
      - Added `docker-compose.yml` with PostgreSQL + Supervisor services
      - Added `docker-compose.dev.yml` with full stack:
          PostgreSQL 16 + PostGIS (with health checks + seed scripts)
          Mock CAMARA service (containerized)
          Mock AI Agent service (containerized)
          Supervisor (depends_on ordering for startup safety)
      - Added `Dockerfile` — multi-stage build, optimized Alpine base, minimal image size
      - Added `.dockerignore` to keep Docker context lean

  + Service containerization and health infrastructure
      - Created `scripts/camara/Dockerfile` for mock CAMARA service
      - Created `scripts/agent/Dockerfile` for mock AI Agent service
      - Added health check endpoints:
          Mock CAMARA: `GET /qos`
          Mock AI Agent: `GET /health`
          Supervisor: `GET /health`
      - Compose health checks ensure services start in correct dependency order

  + Database seeding and test data
      - Created `scripts/seed/seed_shelters.sql` — MENA shelter coordinates
      - Created `scripts/seed/seed_devices.sql` — 40 test phones across red/orange/green zones
      - Integrated seed scripts into Compose `docker-entrypoint-initdb.d/` for automatic population

  + Supervisor runtime updates
      - Updated `cmd/supervisor/main.go` to perform real DB connection on startup
      - Connected to PostgreSQL via `database.Connect()` with pool tuning
      - Removed mock-mode bypass (DATABASE_URL=mock no longer supported)
      - Added graceful shutdown with `defer db.Close()`

  + Disaster simulation tool enhancements
      - Updated `scripts/simulate_disaster.go` to:
          Accept command-line flags (--host, --event, --severity)
          Pretty-print disaster event payload
          Show elapsed time and HTTP response details
          Provide helpful feedback for debugging

  / Dependency updates
      - Removed `github.com/joho/godotenv` (no longer needed with .env files in Compose)
      - Added `github.com/lib/pq` for PostgreSQL wire protocol
      - Upgraded Go version context to 1.22 in Dockerfiles

  / Project reorganization
      - Moved mock services to `scripts/camara/` and `scripts/agent/` for modular builds
      - Created `scripts/seed/` directory for SQL seeding scripts
      - Reorganized script structure to support per-service Dockerfiles

────────────────────────────────────────────────────────────────
  v0.5.2 => v0.6.0                                      [MINOR]
────────────────────────────────────────────────────────────────
  + Enhanced error handling and real-time broadcasting
      - Implemented comprehensive `ErrorUpdate` webhooks across entire pipeline
      - All critical operations now capture and broadcast errors to dashboard
      - Error severity classification:
          Fatal errors: DB_ERROR (database failures)
          Non-fatal errors: CAMARA_TIMEOUT, QOS_FAILED, SMS_FAILED, AGENT_ERROR
      - Added phone context to error broadcasts for targeted troubleshooting
      - Real-time error visibility on government dashboard

  + Device processing pipeline refactored for determinism
      - Replaced MinHeap concurrent streaming with deterministic slice-based approach
      - New flow: collect all devices → sort by distance → batch → process
      - Eliminates race conditions from concurrent heap mutations
      - Cleaner batch generation logic using simple slice slicing
      - Improved stability in high-device-count scenarios

  · Critical operation error handling
      - `camara.UpgradeQoS()` now returns error (signature changed)
      - `dispatch.SendSMS()` errors now captured and broadcast
      - `dispatch.FlagRescue()` errors now captured and broadcast
      - Location/Reachability lookups include phone context in error logs
      - Prevents silent failures in dispatch phase

  · Environment configuration hardened for Docker Compose
      - `.env.example` now reflects production-ready Docker networking
      - Eliminates localhost binding issues in containerized environments

  / Code quality and logging consistency
      - Standardized error log format: log.Printf("[ErrorCode] message: %v", err)
      - Removed timing-based telemetry (T+Nms references)
      - Added readability newlines in concurrent/sync blocks
      - Improved log consistency across pipeline stages

  / Docker Compose maintenance
      - Removed version: "3.9" declarations (implicit in Docker Compose v2+)
      - Keeps Compose files forward-compatible with latest tooling

════════════════════════════════════════════════════════════════
                        CURRENT RELEASE
════════════════════════════════════════════════════════════════

  BUILD STATUS:     ⚠️ FOUNDATION PHASE (SKELETON IN PLACE)
  VERSION:          v0.5.0
  RELEASE DATE:     August 21, 2026
  FOCUS:            Pipeline tests

════════════════════════════════════════════════════════════════
  Legend:  + Added          · Changed             / Fixed
════════════════════════════════════════════════════════════════
```