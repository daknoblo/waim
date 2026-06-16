-- Per-title media statistics stored as JSON on each run.
ALTER TABLE scan_runs ADD COLUMN media_json TEXT;
