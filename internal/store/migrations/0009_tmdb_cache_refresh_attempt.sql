-- Failed refreshes keep the last successful payload but yield their queue slot.
ALTER TABLE tmdb_cache ADD COLUMN last_attempt_at TEXT;
CREATE INDEX idx_tmdb_cache_refresh_order
    ON tmdb_cache(COALESCE(last_attempt_at, fetched_at), cache_key);
DROP INDEX idx_tmdb_cache_fetched_at;
