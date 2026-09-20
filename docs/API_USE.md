# GeoDispatch Supervisor — API Documentation

Base URL: `http://localhost:8080` (development)

Interactive UI: `http://localhost:8080/swagger/`

---

## Authentication

The API supports two authentication methods:

| Method | Header | Use case |
|--------|--------|----------|
| JWT token | `Authorization: Bearer <token>` | Human operators, dashboard |
| API key | `X-API-Key: <key>` | Scripts, agents, machine-to-machine |

All `/api/*` routes require one of the two. `/auth/*` routes are public.

---

## 1. Auth

### POST /auth/register

Creates a new operator account.

**Request**
```json
{
  "email": "admin@geodispatch.io",
  "password": "supersecret"
}
```

**Rules**
- `email` — required, must be unique
- `password` — required, minimum 8 characters

**Responses**

`201 Created`
```json
{
  "id": "a1b2c3d4-...",
  "email": "admin@geodispatch.io",
  "created_at": "2026-09-20T22:00:00Z"
}
```

`400 Bad Request`
```json
{ "error": "email_and_password_required" }
{ "error": "password_too_short", "min": 8 }
{ "error": "invalid_json" }
```

`409 Conflict`
```json
{ "error": "email_already_registered" }
```

---

### POST /auth/login

Validates credentials and returns a signed JWT (24h TTL).

**Request**
```json
{
  "email": "admin@geodispatch.io",
  "password": "supersecret"
}
```

**Responses**

`200 OK`
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

`400 Bad Request`
```json
{ "error": "invalid_json" }
```

`401 Unauthorized`
```json
{ "error": "invalid_credentials" }
```

**Usage**
```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

> The token expires after 24 hours. Call `/auth/login` again to get a new one.

---

## 2. Devices

All routes require `Authorization: Bearer <token>` or `X-API-Key: <key>`.

### GET /api/devices

Returns all registered SIM/phone devices ordered by id.

**Response** `200 OK`
```json
[
  {
    "id": 1,
    "phone": "+212600000001",
    "updated_at": "2026-09-20T22:00:00Z"
  },
  {
    "id": 2,
    "phone": "+212600000002",
    "updated_at": "2026-09-20T22:00:00Z"
  }
]
```

> `location` (PostGIS geography) is not included in the REST response.
> Use the WebSocket stream (`GET /ws`) for live device positions.

---

### POST /api/devices

Registers a new device by phone number.

**Request**
```json
{
  "phone": "+212600000001"
}
```

**Rules**
- `phone` — required, E.164 format (`+` followed by country code and number)
- Must be unique

**Responses**

`201 Created`
```json
{
  "id": 1,
  "phone": "+212600000001",
  "updated_at": "2026-09-20T22:00:00Z"
}
```

`400 Bad Request`
```json
{ "error": "phone_required" }
```

`409 Conflict`
```json
{ "error": "device_already_registered" }
```

---

### PUT /api/devices/{id}

Updates a device's phone number. `id` is the integer id from the list.

**Request**
```json
{
  "phone": "+212600000099"
}
```

**Responses**

`200 OK`
```json
{
  "id": 1,
  "phone": "+212600000099",
  "updated_at": "2026-09-20T22:05:00Z"
}
```

`400 Bad Request`
```json
{ "error": "invalid_id" }
{ "error": "invalid_json" }
{ "error": "nothing_to_update" }
```

`404 Not Found`
```json
{ "error": "device_not_found" }
```

`409 Conflict`
```json
{ "error": "phone_already_exists" }
```

---

### DELETE /api/devices/{id}

Removes a device permanently. `id` is the integer id from the list.

**Responses**

`204 No Content` — success, no body

`400 Bad Request`
```json
{ "error": "invalid_id" }
```

`404 Not Found`
```json
{ "error": "device_not_found" }
```

---

## 3. Events

### GET /api/events

Returns all disaster events, newest first.

**Response** `200 OK`
```json
[
  {
    "id": "us7000abcd",
    "disaster_type": "earthquake",
    "severity": 7.2,
    "epicenter_lat": 33.5731,
    "epicenter_lng": -7.5898,
    "radius_km": 50.0,
    "aftershock_risk": "high",
    "tsunami_risk": false,
    "created_at": "2026-09-20T22:00:00Z"
  }
]
```

> Events are created by `POST /sensor` — they cannot be created via the REST API.

---

### GET /api/events/{id}

Returns a single event and all its device logs.
`id` is the string event ID from the USGS/sensor source (e.g. `us7000abcd`).

**Response** `200 OK`
```json
{
  "event": {
    "id": "us7000abcd",
    "disaster_type": "earthquake",
    "severity": 7.2,
    "epicenter_lat": 33.5731,
    "epicenter_lng": -7.5898,
    "radius_km": 50.0,
    "aftershock_risk": "high",
    "tsunami_risk": false,
    "created_at": "2026-09-20T22:00:00Z"
  },
  "logs": [
    {
      "id": 1,
      "event_id": "us7000abcd",
      "phone": "+212600000001",
      "zone": "red",
      "action": "evacuate",
      "sms_message": "URGENT: Evacuate immediately.",
      "shelter_name": "Shelter Hay Hassani",
      "rescue_priority": 3,
      "confidence": 0.95,
      "zone_escalated": false,
      "logged_at": "2026-09-20T22:01:00Z"
    }
  ]
}
```

`zone` values: `"red"` | `"orange"` | `"green"`

`404 Not Found`
```json
{ "error": "event_not_found" }
```

---

## 4. Shelters

### GET /api/shelters

Returns all shelters ordered by id.

**Response** `200 OK`
```json
[
  {
    "id": 1,
    "name": "Shelter Hay Hassani",
    "address": "Rue des Orangers, Casablanca",
    "capacity": 500
  }
]
```

> `location` (PostGIS geography) is not included. Coordinates are available
> via the WebSocket stream or a direct database query with `ST_AsGeoJSON`.

---

## 5. Rescue Flags

### GET /api/rescue-flags

Returns all rescue flags ordered by priority descending (highest urgency first),
then by time flagged ascending (oldest first within same priority).

**Response** `200 OK`
```json
[
  {
    "id": 1,
    "event_id": "us7000abcd",
    "phone": "+212600000001",
    "zone": "red",
    "rescue_priority": 5,
    "flagged_at": "2026-09-20T22:01:00Z"
  }
]
```

> Rescue flags are written by the pipeline when a device needs physical
> intervention. They cannot be created via the REST API.

---

## 6. API Keys

API keys allow machine-to-machine access without a JWT login flow.
The raw key is shown **once** on creation — it is never retrievable again.

### GET /api/keys

Returns all API keys in the system. `key_hash` is never included.
Requires JWT auth.

**Response** `200 OK`
```json
[
  {
    "id": "f1e2d3c4-...",
    "user_id": "a1b2c3d4-...",
    "label": "python-agent",
    "created_at": "2026-09-20T22:00:00Z"
  }
]
```

---

### POST /api/keys

Creates a new API key for the authenticated user.
**Requires JWT** — cannot be called with an API key.

**Request** (body is optional)
```json
{
  "label": "python-agent"
}
```

**Response** `201 Created`
```json
{
  "id": "f1e2d3c4-...",
  "key": "a3f9e2b1...64hexchars...",
  "label": "python-agent",
  "created_at": "2026-09-20T22:00:00Z"
}
```

> Save the `key` value immediately — it is shown once and never stored.

`401 Unauthorized`
```json
{ "error": "jwt_required" }
```

---

### DELETE /api/keys/{id}

Permanently revokes an API key. `id` is the UUID from the list.

**Responses**

`204 No Content` — success, no body

`400 Bad Request`
```json
{ "error": "invalid_id" }
```

`404 Not Found`
```json
{ "error": "key_not_found" }
```

---

## 7. Existing Pipeline Endpoints

These endpoints existed before the REST API and have no auth requirement.

### POST /sensor

Starts a disaster pipeline. See `contracts/sensor_input.json` for the full schema.

### GET /ws

WebSocket stream. Emits real-time pipeline frames to the dashboard.
See `contracts/ws_update.json` for the frame schema.

### GET /health

Readiness probe. Returns `200` when all dependencies are up, `503` otherwise.

### GET /livez

Liveness probe. Always returns `200 ok` if the process is running.

### GET /capabilities

Returns the API contract version, supported disaster types, and limits.

---

## Error format

All errors follow the same shape:

```json
{ "error": "snake_case_error_code" }
```

| HTTP code | Meaning |
|-----------|---------|
| 400 | Bad request — missing or invalid field |
| 401 | Unauthorized — missing, expired, or invalid token/key |
| 404 | Not found |
| 409 | Conflict — duplicate value |
| 429 | Rate limit exceeded — slow down and retry |
| 500 | Server error — check supervisor logs |

---

## Rate limiting

All `/api/*` routes are rate-limited per IP.
Default: **10 requests/second** (configurable via `RATE_LIMIT_RPS`).

When exceeded:
```
HTTP 429 Too Many Requests
Retry-After: 1
{ "error": "rate_limit_exceeded" }
```