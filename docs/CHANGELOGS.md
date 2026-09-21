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

────────────────────────────────────────────────────────────────
  v0.6.1 => v0.7.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + CAMARA congestion orchestration redesigned (subscription-based flow)
      - Reworked `internal/camara/congestion.go` to use:
          create subscription → fetch congestion → cleanup subscription
      - Added explicit subscription payload with:
          `device.phoneNumber`
          `webhook.notificationUrl`
          `webhook.notificationAuthToken`
          `subscriptionExpireTime`
      - Added deferred best-effort subscription deletion after congestion fetch
      - Added graceful 429 handling in congestion fetch path (returns unknown instead of hard-failing)
      - Updated congestion retrieval call in pipeline to include device context (`phones[0]`)

  + Congestion webhook configuration introduced
      - Extended `config/config.go` with:
          `CongestionWebhookURL`
          `CongestionWebhookToken`
      - Wired new env vars:
          `CONGESTION_WEBHOOK_URL`
          `CONGESTION_WEBHOOK_TOKEN`

  + Pipeline execution order hardened in supervisor runtime
      - Moved device lookup (`PhonesNearEpicenter`) earlier in `cmd/supervisor/main.go`
      - Added early fatal handling for DB lookup failures before downstream CAMARA fan-out
      - Added early stop when no phones are found near epicenter
      - Improves determinism by ensuring downstream stages only run with a valid target set

  · CAMARA auth/header strategy simplified across services
      - Removed client-credentials token dependency from:
          `internal/camara/location.go`
          `internal/camara/reachability.go`
          `internal/camara/qos.go`
      - Introduced shared `rapidAPIHeaders(req, cfg)` helper
      - Standardized Accept/Content-Type + RapidAPI headers in one place
      - Unauthorized responses now reported as invalid API key errors (clearer operational signal)

  · Endpoint and request-path alignment updates
      - Updated reachability endpoint to:
          `/device-status/device-reachability-status/v1/retrieve`
      - Adjusted congestion request path from retrieve-style flow to fetch/subscription flow
      - Normalized CAMARA request construction and error wording for consistency

  + Simulation tooling expansion
      - Added `scripts/simulation/simulate_disaster_camara.go`
      - New simulator supports CLI flags (`--host`, `--event`, `--severity`)
      - Prints structured payload + response timing to speed local/operator testing
      - Added clearer accepted/error terminal feedback for pipeline observation
      - Renamed simulation asset:
          `scripts/simulation/simulate_disaster_morocco.go` (renamed in this release set)

  · Seed data profile refreshed for test geography
      - Reworked `scripts/seed/seed_devices.sql` with a new 40-device dataset
      - New coordinates are centered around Budapest-style test zones:
          RED (~0-5km), ORANGE (~5-10km), GREEN (~10-15km)
      - Preserved previous dataset as commented historical reference block

  / Internal refactor and codebase cleanup
      - Consolidated helper utilities in CAMARA modules (phone normalization + header utilities)
      - Reduced duplicated request header/auth code paths
      - Applied formatting/readability cleanup across QoS and CAMARA call sites
      - Improved maintainability for subsequent real-network integration iterations

────────────────────────────────────────────────────────────────
  v0.7.1 => v0.8.0                                      [MINOR]
────────────────────────────────────────────────────────────────
  + Dashboard websocket delivery made concurrency-safe
      - Reworked `internal/dashboard/hub.go` to use a dedicated buffered message channel (`msgCh`)
      - Implemented single-writer broadcast loop in `Hub.Run()` to avoid concurrent websocket writes
      - Added automatic stale-client cleanup on write failure
      - Added non-blocking enqueue strategy (drop-on-full) to protect pipeline throughput under load

  + Per-device state persistence across pipeline stages
      - Added `deviceInfo` map in `cmd/supervisor/main.go` to persist:
          latitude,
          longitude,
          reachability
      - Second broadcast phase now reuses stored coordinates instead of emitting zero-value lat/lng
      - Reachability status now preserved end-to-end from triage phase to dispatch updates

  + Reachability timeout hardening in device triage
      - Added per-device timeout guard for reachability lookups (`~1s`)
      - On timeout/error, device is explicitly downgraded to `NOT_CONNECTED` fallback
      - Error stream now reports reachability timeout context with phone-scoped diagnostics

  · QoS request model aligned with newer CAMARA/NAC shape
      - Extended QoS session create payload with `device` object and IPv4 addressing block
      - Updated request signature:
          `RequestQoS(ctx, cfg, epicenter, phone)`
      - Updated QoS base path usage:
          `/qod/v0/sessions`
          `/qod/v0/sessions/{id}/extend`
      - Stored phone value in QoS session cache for recovery/recreate continuity

  · Congestion query flow refined for provider response variance
      - Refactored congestion body generation through shared constructor (`newCongestionBody`)
      - Added defaults for webhook URL/token when env values are missing (defensive fallback)
      - Switched fetch endpoint to query-style path:
          `/congestion-insights/v0/query`
      - Added support for both object and array response formats
      - Returns first item safely when array payload is received

  · Event payload typing normalized for websocket contracts
      - Updated `internal/models/models.go` `EventStart` fields:
          `DisasterType` and `AftershockRisk` now serialized as strings
      - Updated `ErrorUpdate.Phone` with `omitempty` JSON behavior
      - Applied formatting/consistency cleanup in model definitions

  · Seed profile rotation for regional simulation context
      - `scripts/seed/seed_devices.sql` switched active dataset back to Morocco coordinates/phones
      - Prior Budapest-shaped dataset kept as commented reference block for alternate testing

  / Supervisor runtime and observability cleanup
      - Improved internal readability/formatting in pipeline and batch rendering helpers
      - Clarified broadcast stage intent (triage broadcast vs dispatch broadcast)
      - Reduced ambiguity in map-update and timing tracking sections

────────────────────────────────────────────────────────────────
  v0.8.2 => v0.9.0                                      [MINOR]
────────────────────────────────────────────────────────────────
  + Docker Compose deployment alignment
      - Updated PostgreSQL credentials to use:
          POSTGRES_USER=geodispatch
          POSTGRES_PASSWORD=geodispatch
      - Updated PostgreSQL health checks to match the new credentials
      - Added automatic seed mounts for:
          scripts/seed/seed_shelters.sql
          scripts/seed/seed_devices.sql

  · Environment file resolution improved
      - Updated Compose services to load environment variables from:
          ../.env
      - Better aligned supervisor execution with the surrounding deploy
        repository layout

  / Development stack simplified
      - Removed the embedded mock AI agent from `docker-compose.dev.yml`
      - Removed the supervisor dependency on the containerized mock agent
      - Prepared the stack to use an externally managed AI service

────────────────────────────────────────────────────────────────
  v0.9.0 => v1.0.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + Deterministic large-scale disaster simulation
      - Replaced the fixed 40-device CAMARA fixture with a generated
        300-device simulation dataset
      - Added deterministic random generation using a fixed seed
      - Generated device locations across three operational zones:
          RED     — 150 devices within approximately 0–5 km
          ORANGE  — 100 devices within approximately 5–10 km
          GREEN   — 50 devices within approximately 10–15 km
      - Generated zone-specific reachability distributions:
          RED     — high proportion of disconnected devices
          ORANGE  — mixed SMS/data reachability
          GREEN   — predominantly data-connected devices

  + Database seed profile expanded
      - Replaced the previous 40-device seed with 300 generated device
        records
      - Preserved the Casablanca/Morocco disaster simulation geography
      - Maintained PostGIS point-based device locations

  · Mock CAMARA service modernization
      - Converted hardcoded location and reachability maps into generated
        fixtures
      - Added deterministic coordinate generation around the epicenter
      - Updated mock response timestamps for the September 2026 simulation
      - Kept mock behavior reproducible across container restarts

  / Development Compose cleanup
      - Removed the mock CAMARA service from `docker-compose.dev.yml`
      - Prepared the supervisor stack for externally supplied CAMARA data
        or a separate mock deployment

────────────────────────────────────────────────────────────────
  v1.0.0 => v1.1.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + Dashboard simulation and WebSocket contract v2 foundation
      - Reworked the dashboard transport around structured WebSocket v2
        envelopes
      - Added contract versioning, sequence numbers, replay flags, and
        event lifecycle metadata
      - Added support for:
          snapshot_begin
          snapshot_end
          heartbeat
          event_start
          event_context
          device_update
          zone_summary
          narrative_update
          error
          event_complete

  + Reliable dashboard event replay
      - Added an in-memory event recorder for the currently held incident
      - Added late-client snapshots with deterministic replay ordering
      - Preserved the latest state for:
          device updates
          event context
          zone summaries
          zone narratives
      - Added bounded replay storage for the latest 100 error frames
      - Retained completed events until the next event begins

  + WebSocket reliability and lifecycle handling
      - Added per-client buffered queues and dedicated writer goroutines
      - Added heartbeat frames and WebSocket ping/pong handling
      - Added slow-client detection and 1013 backpressure disconnects
      - Added graceful WebSocket shutdown with close code 1001
      - Added origin allowlisting for browser WebSocket upgrades
      - Added WebSocket health statistics:
          connected clients
          published frames
          slow-client disconnects
          maximum queue depth

  + Supervisor runtime refactor
      - Reduced `cmd/supervisor/main.go` to application wiring and lifecycle
        management
      - Moved pipeline orchestration into `internal/pipeline`
      - Moved HTTP handling into `internal/httpapi`
      - Added coordinated shutdown for:
          HTTP server
          running pipelines
          WebSocket clients
          database connections

  + Configuration and environment hardening
      - Added development and production environment profiles
      - Added configurable browser origin allowlists
      - Added configurable CAMARA, agent, pipeline, sensor, and WebSocket
        timeouts
      - Added agent batch-size clamping to the supported range of 1–20
      - Added configurable WebSocket queue and heartbeat limits
      - Added explicit SMS gateway configuration state
      - Changed the local mock agent port from 5000 to 8082 to avoid macOS
        AirPlay Receiver conflicts

  + CAMARA and AI client reliability improvements
      - Added shared HTTP clients with configured timeouts
      - Added response body size limits
      - Added strict JSON decoding and unknown-field rejection
      - Added structured client errors with timeout and cancellation support
      - Added redacted upstream error snippets to prevent phone-number leakage
      - Added CAMARA response validation for:
          location area type
          coordinate ranges
          radius values
          reachability status
          congestion levels
      - Added agent health probing through the sibling `/health` endpoint

  + QoS and congestion client consolidation
      - Unified CAMARA mock and real-network client behavior
      - Added per-client QoS session storage
      - Added QoS session extension and recreation after expiry
      - Added best-effort congestion subscription cleanup
      - Added support for both object and array congestion responses
      - Added safe fallback behavior for unknown congestion states

  + Database and pipeline safeguards
      - Added database readiness checks before executing operations
      - Added nil-database protection
      - Added database test coverage for event insertion, rescue flags, and
        device logs
      - Added phone redaction to database and network error messages
      - Added strict tests for timeouts, malformed responses, body limits,
        replay behavior, reconnects, origin policy, and slow clients

  + Standalone development stack
      - Added `docker-compose.standalone.yml`
      - Added a self-contained local stack containing:
          PostgreSQL/PostGIS
          mock CAMARA
          mock AI agent
          supervisor
      - Added paced mock latency for dashboard demonstrations
      - Added development fixture seed support through:
          scripts/seed/seed_demo_areas.sql

  · Contract governance updated
      - Replaced the previous standalone contract documentation with
        synchronized canonical contract guidance
      - Added contract v2 references and synchronization instructions
      - Expanded JSON schemas with stricter validation rules for sensor,
        CAMARA, AI, and WebSocket payloads

────────────────────────────────────────────────────────────────
  v1.1.0 => v1.2.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + ORM and authentication persistence foundation
      - Added GORM PostgreSQL integration alongside the existing database
        connection layer
      - Added `internal/database/orm.go` with pooled GORM connections
      - Preserved the existing raw SQL pipeline database path
      - Added ORM models for:
          users
          api_keys
          devices
          shelters
          events
          device_logs
          rescue_flags

  + Authentication database migration
      - Added `migrations/003_auth.sql`
      - Created the `users` table for operator accounts
      - Created the `api_keys` table for machine-to-machine access
      - Added UUID identifiers and foreign-key cascade behavior
      - Added indexes for user email and API-key ownership

  + Authentication configuration
      - Added `JWT_SECRET` configuration
      - Added configurable per-IP API rate limiting through
        `RATE_LIMIT_RPS`
      - Updated `.env.example` with authentication and rate-limit settings
      - Added startup warnings when JWT configuration is missing

  + Supervisor database lifecycle integration
      - Opened a dedicated GORM database handle during supervisor startup
      - Passed the GORM handle into the HTTP API layer
      - Added graceful GORM connection shutdown
      - Preserved the existing PostgreSQL pool for pipeline operations

  + Configuration system expansion
      - Added environment normalization for development and production modes
      - Added explicit origin-list parsing
      - Added configurable:
          CAMARA timeouts
          agent timeout
          agent batch size
          pipeline timeout
          sensor body limit
          WebSocket queue limit
          WebSocket write timeout
          WebSocket heartbeat interval
      - Added network-source reporting for mock CAMARA versus Nokia NaC
      - Added SMS gateway configuration status reporting

  + Agent and CAMARA integration hardening
      - Added strict agent response validation
      - Added malformed-response and oversized-body protection
      - Added structured timeout and cancellation errors
      - Added shared CAMARA client infrastructure for mock and real modes
      - Added response validation and phone-redacted error handling
      - Added test coverage for network failures, authentication failures,
        malformed payloads, timeouts, and response limits

  + Contract v2 alignment
      - Added stricter sensor input validation:
          event ID format and length
          timestamp bounds
          severity limits
          radius limits
          depth limits
      - Added a maximum AI batch size of 20 devices
      - Expanded CAMARA contract schemas with explicit `oneOf` response
        definitions
      - Updated WebSocket contract documentation for replay, lifecycle,
        error, dispatch, and completion states

────────────────────────────────────────────────────────────────
  v1.2.0 => v1.3.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + JWT authentication implemented
      - Added `internal/auth/jwt.go`
      - Added signed access tokens using HMAC-SHA256
      - Added 24-hour token lifetime
      - Added user ID and email claims
      - Added signing-method validation during token verification
      - Added protection against invalid or expired tokens

  + Password security layer added
      - Added bcrypt password hashing
      - Added bcrypt password verification
      - Standardized password hashing with bcrypt cost 12
      - Ensured passwords are never serialized through ORM models

  + HTTP authentication middleware
      - Added JWT Bearer-token middleware
      - Added API-key middleware backed by the `api_keys` table
      - Added combined JWT-or-API-key authentication
      - Added request-context claim propagation
      - Added clear unauthorized responses for:
          missing_token
          invalid_token
          missing_api_key
          invalid_api_key
          server_error

  + Per-IP API rate limiting
      - Added token-bucket rate limiting through `golang.org/x/time/rate`
      - Added configurable requests-per-second limits
      - Added automatic stale client pruning
      - Added `Retry-After` responses for rejected requests
      - Added support for `X-Forwarded-For` client identification

  + HTTP API authentication wiring
      - Extended `internal/httpapi.Options` with:
          GormDB
          JWTSecret
          RateLimitRPS
      - Added authenticated route registration for:
          POST /auth/register
          POST /auth/login
          /api/*
      - Protected REST endpoints with JWT-or-API-key authentication
      - Applied rate limiting to protected API routes
      - Kept existing sensor, health, capabilities, liveness, and WebSocket
        routes available through the existing API registration flow

  + Runtime integration
      - Added GORM initialization to supervisor startup
      - Connected JWT and rate-limit configuration to the HTTP server
      - Added GORM shutdown handling alongside the existing PostgreSQL pool
      - Added startup diagnostics for missing JWT configuration

────────────────────────────────────────────────────────────────
  v1.3.1 => v1.4.0                                      [MINOR]
────────────────────────────────────────────────────────────────
  + REST API and API-key management endpoints
      - Added protected REST resources under `/api/`
      - Added device management endpoints:
          GET    /api/devices
          POST   /api/devices
          PUT    /api/devices/{id}
          DELETE /api/devices/{id}
      - Added event inspection endpoints:
          GET /api/events
          GET /api/events/{id}
      - Added shelter listing endpoint:
          GET /api/shelters
      - Added rescue queue endpoint:
          GET /api/rescue-flags
      - Added API-key management endpoints:
          GET    /api/keys
          POST   /api/keys
          DELETE /api/keys/{id}

  + Authentication flows exposed through HTTP API
      - Added `POST /auth/register` for operator account creation
      - Added `POST /auth/login` for JWT issuance
      - Enforced minimum password length of eight characters
      - Normalized registration and login email addresses
      - Added duplicate-email conflict handling
      - Preserved bcrypt password hashing and secure credential handling

  + API-key lifecycle management
      - Added cryptographically secure API-key generation
      - Returned raw API keys only once during creation
      - Persisted only bcrypt hashes of API keys
      - Added JWT-only API-key creation
      - Added safe API-key listing without exposing `key_hash`
      - Added API-key revocation by UUID

  + Swagger/OpenAPI documentation
      - Added generated Swagger documentation under `docs/swagger/`:
          docs/swagger/docs.go
          docs/swagger/swagger.json
          docs/swagger/swagger.yaml
      - Added Swagger UI at:
          GET /swagger/
      - Documented JWT Bearer authentication and X-API-Key authentication
      - Documented authentication, API-key, device, event, shelter, and
        rescue endpoints
      - Added API metadata, security definitions, request schemas, and
        response definitions

  + HTTP server integration
      - Registered Swagger UI alongside the existing health, sensor,
        capability, authentication, and REST routes
      - Added protected route wiring through the existing rate-limit and
        JWT/API-key middleware
      - Added separate JWT-only protection for API-key creation

  / Dependency and project metadata updates
      - Added Swagger dependencies:
          github.com/swaggo/http-swagger
          github.com/swaggo/swag
      - Added JWT and rate-limit dependencies to the module requirements
      - Updated Go module version context and generated dependency checksums
      - Added API documentation generation metadata to
        `cmd/supervisor/main.go`

────────────────────────────────────────────────────────────────
  v1.4.0 => v1.5.0                                      [MAJOR]
────────────────────────────────────────────────────────────────
  + Complete REST resource API
      - Expanded the protected API with device CRUD operations
      - Added event listing and event-detail views with associated device
        logs
      - Added shelter listing backed by GORM models
      - Added rescue-flag queue listing ordered by priority and timestamp
      - Added validation for integer and string resource identifiers
      - Added duplicate-device and duplicate-phone conflict handling

  + REST handler organization improved
      - Refactored `internal/httpapi/rest.go` around a dedicated
        `restHandler`
      - Centralized the GORM database handle for REST handlers
      - Extracted route registration from individual handler implementations
      - Improved separation between routing and resource operations
      - Added consistent database error logging and JSON error responses

  + Public API documentation expanded
      - Added `docs/API_USE.md` with practical API usage documentation
      - Documented:
          authentication and JWT usage
          API-key usage
          device management
          event inspection
          shelter access
          rescue queue access
          existing sensor and WebSocket endpoints
          HTTP error semantics
          API rate limiting
      - Expanded generated Swagger JSON, YAML, and Go documentation with
        device, event, shelter, rescue, and ORM response models

  + Database startup resilience
      - Added retry-based PostgreSQL startup connection handling
      - Added retry-based GORM startup connection handling
      - Added up to 10 connection attempts
      - Added three-second retry intervals
      - Added clear startup logging for:
          successful connection attempts
          failed attempts
          exhausted retries
          shutdown before connection
      - Prevents the supervisor from failing immediately when PostgreSQL
        is still starting inside Compose

════════════════════════════════════════════════════════════════
                        CURRENT RELEASE
════════════════════════════════════════════════════════════════

  BUILD STATUS:     ✅ REST API + DOCUMENTATION + STARTUP RESILIENCE
  VERSION:          v1.5.0
  RELEASE DATE:     September 20, 2026
  FOCUS:            Protected resource APIs, Swagger/OpenAPI documentation,
                    API usage guidance, database startup retries, and
                    Go 1.22 compatibility

════════════════════════════════════════════════════════════════
  Legend:  + Added          · Changed             / Fixed
════════════════════════════════════════════════════════════════
```
