-- ── events ────────────────────────────────────────────────────────────────────
-- One row per SensorInput received on POST /sensor.
-- EventID is the natural PK (comes from USGS / sensor source).

CREATE TABLE IF NOT EXISTS events (
    id              TEXT PRIMARY KEY,               -- SensorInput.EventID
    disaster_type   TEXT        NOT NULL,
    severity        FLOAT       NOT NULL,
    epicenter_lat   FLOAT       NOT NULL,
    epicenter_lng   FLOAT       NOT NULL,
    radius_km       FLOAT       NOT NULL,
    aftershock_risk TEXT        NOT NULL,
    tsunami_risk    BOOLEAN     NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- ── device_logs ───────────────────────────────────────────────────────────────
-- One row per DeviceDecision returned by the AI agent.
-- Captures the full outcome: zone, action, SMS text, shelter assigned, etc.

CREATE TABLE IF NOT EXISTS device_logs (
    id              SERIAL PRIMARY KEY,
    event_id        TEXT        NOT NULL REFERENCES events(id),
    phone           TEXT        NOT NULL,
    zone            TEXT        NOT NULL,           -- "red" | "orange" | "green"
    action          TEXT        NOT NULL,           -- ActionType value
    sms_message     TEXT,
    shelter_name    TEXT,
    rescue_priority INT         DEFAULT 0,
    confidence      FLOAT,
    zone_escalated  BOOLEAN     DEFAULT FALSE,
    logged_at       TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS device_logs_event_idx
    ON device_logs (event_id);

CREATE INDEX IF NOT EXISTS device_logs_phone_idx
    ON device_logs (phone);

-- ── rescue_flags ──────────────────────────────────────────────────────────────
-- Written by dispatch.FlagRescue for devices needing physical intervention.
-- Used by command-centre dashboard to show rescue queue ordered by priority.

CREATE TABLE IF NOT EXISTS rescue_flags (
    id              SERIAL PRIMARY KEY,
    event_id        TEXT        NOT NULL REFERENCES events(id),
    phone           TEXT        NOT NULL,
    zone            TEXT        NOT NULL,
    rescue_priority INT         DEFAULT 0,
    flagged_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (event_id, phone)                        -- ON CONFLICT DO NOTHING safe
);

CREATE INDEX IF NOT EXISTS rescue_flags_event_idx
    ON rescue_flags (event_id, rescue_priority DESC);