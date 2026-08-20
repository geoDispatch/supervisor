CREATE EXTENSION IF NOT EXISTS postgis;

-- ── devices ──────────────────────────────────────────────────────────────────
-- One row per registered SIM / phone number.
-- location is nullable: a device may be registered before its first GPS fix.
-- The CAMARA pipeline will query live location for devices where this is NULL.

CREATE TABLE IF NOT EXISTS devices (
    id          SERIAL PRIMARY KEY,
    phone       TEXT NOT NULL UNIQUE,           -- E.164 format (+212XXXXXXXXX)
    location    GEOGRAPHY(POINT, 4326),         -- last known position, nullable
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS devices_location_idx
    ON devices USING GIST (location);

-- ── shelters ──────────────────────────────────────────────────────────────────
-- Populated by scripts/seed_shelters.sql after migration runs.
-- location is NOT NULL — every shelter must have a fixed GPS position.

CREATE TABLE IF NOT EXISTS shelters (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    address     TEXT NOT NULL,
    capacity    INT  NOT NULL DEFAULT 0,
    location    GEOGRAPHY(POINT, 4326) NOT NULL
);

CREATE INDEX IF NOT EXISTS shelters_location_idx
    ON shelters USING GIST (location);