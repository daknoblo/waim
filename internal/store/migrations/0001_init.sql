-- Initial schema for waim.

CREATE TABLE IF NOT EXISTS kv (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS scan_runs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at        TEXT NOT NULL,
    finished_at       TEXT,
    status            TEXT NOT NULL,                  -- running | success | error
    error             TEXT,
    libraries_scanned INTEGER NOT NULL DEFAULT 0,
    items_scanned     INTEGER NOT NULL DEFAULT 0,
    missing_count     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS findings (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    scan_run_id   INTEGER NOT NULL REFERENCES scan_runs(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,        -- missing_season | missing_episodes | missing_collection
    media_type    TEXT NOT NULL,        -- series | movie
    library_id    TEXT NOT NULL,
    library_name  TEXT NOT NULL,
    title         TEXT NOT NULL,
    tmdb_id       INTEGER,
    jellyfin_id   TEXT,
    season_number INTEGER,
    summary       TEXT NOT NULL,
    details       TEXT,                  -- JSON payload
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_findings_scan ON findings(scan_run_id);
CREATE INDEX IF NOT EXISTS idx_scan_runs_status ON scan_runs(status);
