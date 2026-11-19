CREATE TABLE virtual_entries (
    media_type TEXT NOT NULL CHECK(media_type IN ('Movie', 'Series')),
    tmdb_id INTEGER NOT NULL CHECK(tmdb_id > 0),
    title TEXT NOT NULL,
    year INTEGER NOT NULL DEFAULT 0,
    poster TEXT NOT NULL DEFAULT '',
    added_at TEXT NOT NULL,
    PRIMARY KEY(media_type, tmdb_id)
);
CREATE TABLE catalog_revision (id INTEGER PRIMARY KEY CHECK(id = 1), revision INTEGER NOT NULL);
INSERT INTO catalog_revision VALUES(1, 0);
CREATE TABLE source_snapshots (
    source_id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    snapshot_json TEXT,
    attempted_at TEXT NOT NULL,
    succeeded_at TEXT,
    error TEXT NOT NULL DEFAULT ''
);
ALTER TABLE scan_runs ADD COLUMN metadata_json TEXT;
ALTER TABLE findings ADD COLUMN provenance_json TEXT;
