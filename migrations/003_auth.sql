-- ── users ──────────────────────────────────────────────────────────────────
-- Adds user accounts and API key support.
-- Depends on: 001_init.sql, 002_events.sql

CREATE TABLE IF NOT EXISTS users (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT        UNIQUE NOT NULL,
    password   TEXT        NOT NULL,   -- bcrypt hash, never plaintext
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

-- Each user may have multiple API keys (machine-to-machine access).
-- The actual key is shown once on creation; only its bcrypt hash is stored.
CREATE TABLE IF NOT EXISTS api_keys (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key_hash   TEXT        NOT NULL,   -- bcrypt hash of the raw key
    label      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys (user_id);