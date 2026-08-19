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

════════════════════════════════════════════════════════════════
                        CURRENT RELEASE
════════════════════════════════════════════════════════════════

  BUILD STATUS:     ⚠️ FOUNDATION PHASE (SKELETON IN PLACE)
  VERSION:          v0.4.0
  RELEASE DATE:     August 19, 2026
  FOCUS:            Pipeline orchestration, contracts, and infra scaffolding

════════════════════════════════════════════════════════════════
  Legend:  + Added          · Changed             / Fixed
════════════════════════════════════════════════════════════════
```