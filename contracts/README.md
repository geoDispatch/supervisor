# GeoDispatch — Data Contracts

JSON Schema contracts for all inter-service boundaries in the disaster response pipeline.

These contracts are LOCKED once published.
Do not change without notifying all team members.

## Architecture boundaries

```
[Disaster Sensor]
      │  POST /sensor
      ▼
[Go Supervisor]  ──── CAMARA APIs ────▶  Nokia NaC Network
      │  POST /decide
      ▼
[Python AI Agent]
      │  AgentResponse
      ▼
[Go Supervisor]
      ├──▶  SMS Dispatch (Africa Service / operator)
      ├──▶  Rescue Dispatch Worker
      └──▶  WebSocket  ──▶  [Dashboard]
```

## Files

| File | Schema | Direction | Consumed by |
|---|---|---|---|
| `sensor_input.json` | `SensorInput` | Sensor → Go | `internal/sensor/handler.go` |
| `camara_device.json` | `CAMARALocation/Reachability/Verification/Congestion/TriagedDevice` | Nokia NaC → Go | `internal/camara/*.go` |
| `ai_request.json` | `AgentRequest` | Go → Python AI | `internal/agent/client.go` + Python agent |
| `ai_response.json` | `AgentResponse` + `DeviceDecision` | Python AI → Go | `internal/agent/client.go` |
| `ws_update.json` | `WSUpdate` + all payloads | Go → SolidJS | `internal/dashboard/broadcast.go` + SolidJS |

---

## Critical rules (from `models.go`)

> **Go calculates zones. AI decides actions. Never reversed.**

- All timestamps: **Unix milliseconds** (`int64`)
- All phone numbers: **E.164** (`+212XXXXXXXXX`)
- All zones: **lowercase** `"red"` | `"orange"` | `"green"`
- `zone` field in `TriagedDevice` is **always set by Go haversine** — AI may escalate (`zone_escalated: true`) but never sets it initially
- `reasoning` in `DeviceDecision` is **internal audit only** — never shown to users, never sent in SMS

---

## WebSocket message flow

React should handle messages in this order:

1. `event_start` → initialise map, draw epicenter + impact radius circle
2. `device_update` → add/update dot on map (colour by zone)
3. `zone_summary` → update sidebar counters (sent after each zone batch)
4. `narrative_update` → update AI situation report text panel
5. `error` → show warning banner; if `fatal: true`, show critical alert and halt UI updates

---

## Zone definitions (set by Go haversine)

| Zone | Distance from epicenter | Risk level |
|---|---|---|
| `red` | ≤ 33% of `radius_km` | Critical — immediate danger |
| `orange` | 33%–66% of `radius_km` | High — evacuation recommended |
| `green` | 66%–100% of `radius_km` | Moderate — alert and monitor |

---

## AI action types

| Action | SMS sent | Rescue flagged |
|---|---|---|
| `sms` | ✅ | ❌ |
| `rescue_flag` | ❌ | ✅ |
| `both` | ✅ | ✅ |
| `none` | ❌ | ❌ |

`rescue_priority`: `0` = not flagged, `1–10` = flagged (1 = highest urgency, dispatched first)

---

## Batch processing order

Batches are sent to the AI agent in priority order:

```
batch_index 0 → zone "red"   (process first — highest risk)
batch_index 1 → zone "orange"
batch_index 2 → zone "green"
```

The AI agent must respond to each batch before Go sends the next.

---

## Error codes

| Code | Source | Fatal? |
|---|---|---|
| `CAMARA_TIMEOUT` | `internal/camara/` | Usually false — device skipped |
| `AGENT_ERROR` | `internal/agent/client.go` | Usually true — pipeline halted |
| `SMS_FAILED` | `internal/dispatch/sms.go` | False — logged, rescue may still run |
| `DB_ERROR` | `internal/database/` | True — cannot log or query shelters |
| `QOS_FAILED` | `internal/camara/qos.go` | False — fallback to standard QoS |


## Who owns what
- Ilias (Go)     → produces ai_request.json, consumes ai_response.json
- Yassine (Python) → consumes ai_request.json, produces ai_response.json
- Saad / Ayoub (Dashboard)  → consumes ws_update.json
- Houssam (DevOps) → validates all contracts in integration tests