-- Persistent cache of raw TMDB API responses, keyed by request path+query.
-- Lets scans reuse data and a background job refresh it incrementally instead
-- of re-fetching everything from TMDB on every scan.
CREATE TABLE tmdb_cache (
    cache_key  TEXT PRIMARY KEY,
    payload    TEXT NOT NULL,
    fetched_at TEXT NOT NULL
);
CREATE INDEX idx_tmdb_cache_fetched_at ON tmdb_cache(fetched_at);
