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
