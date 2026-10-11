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

-- One detector run: a model x prompt version over an eval set. Aggregates go
-- to runs/<id>/ in the repo; everything per video stays here.
CREATE TABLE IF NOT EXISTS runs (
    id             TEXT PRIMARY KEY,   -- <date>-<model>-<prompt version>, same as the runs/ directory
    created_at     TEXT NOT NULL,      -- RFC 3339 UTC
    model          TEXT NOT NULL,
    effort         TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    prompt_sha     TEXT NOT NULL,
    evalset        TEXT NOT NULL,
    config_json    TEXT NOT NULL       -- the full config.json, for reproducibility
);

-- One model call per video per run, successful or not.
CREATE TABLE IF NOT EXISTS calls (
    run_id             TEXT NOT NULL REFERENCES runs(id),
    provider           TEXT NOT NULL,
    native_id          TEXT NOT NULL,
    status             TEXT NOT NULL,  -- ok | no_transcript | api_error | invalid_output
    error              TEXT,
    input_tokens       INTEGER NOT NULL DEFAULT 0,
    output_tokens      INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    latency_ms         INTEGER NOT NULL DEFAULT 0,
    cost_usd           REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (run_id, provider, native_id)
);

-- Raw predictions, unfiltered: min-duration and confidence thresholds are
-- applied by the scorer, so one run can be scored under several settings.
CREATE TABLE IF NOT EXISTS predictions (
    id          INTEGER PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs(id),
    provider    TEXT NOT NULL,
    native_id   TEXT NOT NULL,
    category    TEXT NOT NULL,
    start       REAL NOT NULL,
    "end"       REAL NOT NULL,
    first_cue   INTEGER NOT NULL,
    last_cue    INTEGER NOT NULL,
    confidence  TEXT NOT NULL,
    quote       TEXT NOT NULL,
    quote_ok    INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS predictions_run_video ON predictions (run_id, provider, native_id);
