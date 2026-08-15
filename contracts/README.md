# GeoDispatch — Data Contracts

These contracts are LOCKED once published.
Do not change without notifying all team members.

## Golden rules
- Go calculates zones — AI never touches coordinates or math
- AI crafts messages — Go never writes SMS text
- All timestamps: Unix milliseconds (int64)
- All phones: E.164 format (+212XXXXXXXXX)
- All zones: "red" | "orange" | "green" (lowercase, always)

## Endpoints

### Sensor → Go Supervisor
POST http://localhost:8080/sensor
Body: see contracts/sensor_input.json

### Go Supervisor → Python AI Agent
POST http://localhost:5000/decide
Body: see contracts/ai_request.json
Response: see contracts/ai_response.json

### Go Supervisor → React Dashboard
WS ws://localhost:8080/ws
Messages: see contracts/ws_update.json

## Who owns what
- Ilias (Go)     → produces ai_request.json, consumes ai_response.json
- Yassine (Python) → consumes ai_request.json, produces ai_response.json
- Saad / Ayoub (React)  → consumes ws_update.json
- Houssam (DevOps) → validates all contracts in integration tests