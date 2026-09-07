CREATE TABLE IF NOT EXISTS rooms (
    code       TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS items (
    id         UUID PRIMARY KEY,
    room_code  TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    file_name  TEXT NOT NULL DEFAULT '',
    mime_type  TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    width      INTEGER NOT NULL DEFAULT 0,
    height     INTEGER NOT NULL DEFAULT 0,
    device     TEXT NOT NULL DEFAULT '',
    blob_path  TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS items_room_created_idx ON items (room_code, created_at DESC);
CREATE INDEX IF NOT EXISTS items_expires_idx ON items (expires_at);

-- Payload storage for deployments without a durable filesystem (see
-- internal/blob/postgres.go). Every chunk but the last is exactly 1 MiB, which
-- is what lets the reader seek by arithmetic instead of a lookup.
CREATE TABLE IF NOT EXISTS blob_chunks (
    blob_id    TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    data       BYTEA NOT NULL,
    -- created_at exists so orphan cleanup can leave in-flight uploads alone:
    -- chunks are written before the item row that references them.
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blob_id, seq)
);

-- ─────────────────────────────────────────────────────────────
-- Accounts & Secret Store
-- ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '',
    github_id     TEXT UNIQUE,
    github_user   TEXT NOT NULL DEFAULT '',
    avatar_url    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_idx ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS secrets (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'password',
    username        TEXT NOT NULL DEFAULT '',
    url             TEXT NOT NULL DEFAULT '',
    encrypted_value TEXT NOT NULL,
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS secrets_user_created_idx ON secrets (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS secrets_user_url_idx ON secrets (user_id, url);
