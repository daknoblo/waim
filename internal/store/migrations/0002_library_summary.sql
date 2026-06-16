-- Per-library scan breakdown stored as JSON on each run.
ALTER TABLE scan_runs ADD COLUMN libraries_json TEXT;
