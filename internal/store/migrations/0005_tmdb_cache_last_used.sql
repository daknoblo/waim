-- Track when each cached TMDB entry was last actually used (read by a scan or
-- suggestion). The background refresher updates fetched_at but not last_used_at,
-- so entries for media removed from the library age out and can be pruned.
ALTER TABLE tmdb_cache ADD COLUMN last_used_at TEXT;
UPDATE tmdb_cache SET last_used_at = fetched_at WHERE last_used_at IS NULL;
CREATE INDEX idx_tmdb_cache_last_used_at ON tmdb_cache(last_used_at);
