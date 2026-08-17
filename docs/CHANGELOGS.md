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


════════════════════════════════════════════════════════════════
                        CURRENT RELEASE
════════════════════════════════════════════════════════════════

  BUILD STATUS:     ⚠️ FOUNDATION PHASE (SKELETON IN PLACE)
  VERSION:          v0.2.0
  RELEASE DATE:     August 16, 2026
  FOCUS:            Pipeline orchestration, contracts, and infra scaffolding

════════════════════════════════════════════════════════════════
  Legend:  + Added          · Changed             / Fixed
════════════════════════════════════════════════════════════════
```