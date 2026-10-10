-- Offline eval store (private, lives in the data root as adskip.db).
-- Gold labels are raw per-contributor intervals, never merged here.
-- Runs / predictions / metrics tables are added when the detector and scorer exist.

CREATE TABLE IF NOT EXISTS videos (
    provider   TEXT NOT NULL,
    native_id  TEXT NOT NULL,
    duration   REAL,                 -- seconds; NULL until known (product DB or info.json)
    PRIMARY KEY (provider, native_id)
);

CREATE TABLE IF NOT EXISTS contributors (
    id          TEXT PRIMARY KEY,    -- salted hash, never a product user id
    is_assessor INTEGER NOT NULL DEFAULT 0,
    rating      INTEGER,
    banned      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS labels (
    id          INTEGER PRIMARY KEY,
    provider    TEXT NOT NULL,
    native_id   TEXT NOT NULL,
    contributor TEXT NOT NULL REFERENCES contributors(id),
    category    TEXT NOT NULL,       -- sponsor | intro | self_promotion | credits
    start       REAL NOT NULL,
    "end"       REAL NOT NULL,
    created_at  TEXT,                -- RFC 3339 UTC, when the human made the label
    source      TEXT NOT NULL DEFAULT 'markup',  -- markup | vote | verified
    FOREIGN KEY (provider, native_id) REFERENCES videos(provider, native_id)
);

-- Idempotent import: the same human interval is never stored twice.
CREATE UNIQUE INDEX IF NOT EXISTS labels_unique
    ON labels (provider, native_id, contributor, category, start, "end", source);
CREATE INDEX IF NOT EXISTS labels_video ON labels (provider, native_id);
