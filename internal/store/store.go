// Package store implements SQLite-backed persistence for scan runs and the
// gaps ("findings") discovered when comparing Jellyfin against TMDB.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (registered as "sqlite")
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const timeLayout = time.RFC3339Nano

// Store wraps a SQLite database connection.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path, applies pending
// migrations, and returns a ready-to-use Store.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: serialise writes to avoid lock contention
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version    INTEGER PRIMARY KEY,
        applied_at TEXT NOT NULL
    )`); err != nil {
		return fmt.Errorf("store: create migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for i, name := range names {
		version := i + 1
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&exists); err != nil {
			return fmt.Errorf("store: check migration %d: %w", version, err)
		}
		if exists > 0 {
			continue
		}
		sqlBytes, rerr := migrationsFS.ReadFile("migrations/" + name)
		if rerr != nil {
			return fmt.Errorf("store: read migration %s: %w", name, rerr)
		}
		tx, terr := s.db.BeginTx(ctx, nil)
		if terr != nil {
			return fmt.Errorf("store: begin migration %s: %w", name, terr)
		}
		if _, eerr := tx.ExecContext(ctx, string(sqlBytes)); eerr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: apply migration %s: %w", name, eerr)
		}
		if _, eerr := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			version, time.Now().UTC().Format(timeLayout),
		); eerr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: record migration %s: %w", name, eerr)
		}
		if cerr := tx.Commit(); cerr != nil {
			return fmt.Errorf("store: commit migration %s: %w", name, cerr)
		}
	}
	return nil
}

// --- Key/value helpers ---

// SetKV stores or updates a key/value pair.
func (s *Store) SetKV(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO kv(key, value, updated_at) VALUES(?, ?, ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("store: set kv: %w", err)
	}
	return nil
}

// GetKV returns the value for key and whether it was found.
func (s *Store) GetKV(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	switch err {
	case nil:
		return v, true, nil
	case sql.ErrNoRows:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("store: get kv: %w", err)
	}
}

// --- Scan runs ---

// StartScanRun inserts a new run in the "running" state and returns its ID.
func (s *Store) StartScanRun(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO scan_runs(started_at, status) VALUES(?, ?)`,
		time.Now().UTC().Format(timeLayout), StatusRunning)
	if err != nil {
		return 0, fmt.Errorf("store: start scan run: %w", err)
	}
	return res.LastInsertId()
}

// FinishScanRun marks a run as completed (success or error) with summary counts.
func (s *Store) FinishScanRun(ctx context.Context, id int64, status, errMsg string, libs, items, missing int, summaries []LibrarySummary, media []MediaStat) error {
	var libsJSON, mediaJSON any
	if len(summaries) > 0 {
		if b, err := json.Marshal(summaries); err == nil {
			libsJSON = string(b)
		}
	}
	if len(media) > 0 {
		if b, err := json.Marshal(media); err == nil {
			mediaJSON = string(b)
		}
	}
	_, err := s.db.ExecContext(ctx, `
        UPDATE scan_runs
        SET finished_at = ?, status = ?, error = ?, libraries_scanned = ?, items_scanned = ?, missing_count = ?, libraries_json = ?, media_json = ?
        WHERE id = ?`,
		time.Now().UTC().Format(timeLayout), status, nullString(errMsg), libs, items, missing, libsJSON, mediaJSON, id)
	if err != nil {
		return fmt.Errorf("store: finish scan run: %w", err)
	}
	return nil
}

// AddFindings bulk-inserts findings for a scan run within a transaction.
func (s *Store) AddFindings(ctx context.Context, runID int64, fs []Finding) error {
	if len(fs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin findings tx: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO findings(scan_run_id, kind, media_type, library_id, library_name,
            title, tmdb_id, jellyfin_id, season_number, summary, details, created_at)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: prepare findings: %w", err)
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(timeLayout)
	for _, f := range fs {
		if _, err := stmt.ExecContext(ctx, runID, f.Kind, f.MediaType, f.LibraryID,
			f.LibraryName, f.Title, nullInt64(f.TMDBID), nullString(f.JellyfinID),
			nullIntPtr(f.SeasonNumber), f.Summary, nullString(f.Details), now); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: insert finding: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit findings: %w", err)
	}
	return nil
}

// LatestRun returns the most recent scan run regardless of status.
func (s *Store) LatestRun(ctx context.Context) (*ScanRun, error) {
	return s.queryRun(ctx, `SELECT id, started_at, finished_at, status, error,
        libraries_scanned, items_scanned, missing_count, libraries_json, media_json
        FROM scan_runs ORDER BY id DESC LIMIT 1`)
}

// LatestSuccessfulRun returns the most recent successfully completed run.
func (s *Store) LatestSuccessfulRun(ctx context.Context) (*ScanRun, error) {
	return s.queryRun(ctx, `SELECT id, started_at, finished_at, status, error,
        libraries_scanned, items_scanned, missing_count, libraries_json, media_json
        FROM scan_runs WHERE status = 'success' ORDER BY id DESC LIMIT 1`)
}

func (s *Store) queryRun(ctx context.Context, query string, args ...any) (*ScanRun, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	run, err := scanRunRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: query run: %w", err)
	}
	return run, nil
}

// FindingsForRun returns all findings for a given run, ordered by title.
func (s *Store) FindingsForRun(ctx context.Context, runID int64) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, scan_run_id, kind, media_type, library_id, library_name, title,
            tmdb_id, jellyfin_id, season_number, summary, details, created_at
        FROM findings WHERE scan_run_id = ?
        ORDER BY media_type, title, season_number`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: query findings: %w", err)
	}
	defer rows.Close()

	var out []Finding
	for rows.Next() {
		var (
			f         Finding
			tmdbID    sql.NullInt64
			jellyfin  sql.NullString
			details   sql.NullString
			season    sql.NullInt64
			createdAt string
		)
		if err := rows.Scan(&f.ID, &f.ScanRunID, &f.Kind, &f.MediaType, &f.LibraryID,
			&f.LibraryName, &f.Title, &tmdbID, &jellyfin, &season, &f.Summary,
			&details, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan finding: %w", err)
		}
		if tmdbID.Valid {
			f.TMDBID = tmdbID.Int64
		}
		if jellyfin.Valid {
			f.JellyfinID = jellyfin.String
		}
		if details.Valid {
			f.Details = details.String
		}
		if season.Valid {
			n := int(season.Int64)
			f.SeasonNumber = &n
		}
		if t, perr := time.Parse(timeLayout, createdAt); perr == nil {
			f.CreatedAt = t
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RecentRuns returns up to limit recent runs, newest first.
func (s *Store) RecentRuns(ctx context.Context, limit int) ([]ScanRun, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, started_at, finished_at, status, error,
            libraries_scanned, items_scanned, missing_count, libraries_json, media_json
        FROM scan_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: recent runs: %w", err)
	}
	defer rows.Close()
	var out []ScanRun
	for rows.Next() {
		run, err := scanRunRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan run row: %w", err)
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

// ExportSyncState builds an exportable snapshot of the latest successful run.
func (s *Store) ExportSyncState(ctx context.Context) (SyncState, error) {
	state := SyncState{GeneratedAt: time.Now().UTC()}
	run, err := s.LatestSuccessfulRun(ctx)
	if err != nil {
		return state, err
	}
	state.Run = run
	if run != nil {
		fs, ferr := s.FindingsForRun(ctx, run.ID)
		if ferr != nil {
			return state, ferr
		}
		state.Findings = fs
	}
	return state, nil
}

// PruneRuns deletes all but the most recent keep runs (and their findings).
func (s *Store) PruneRuns(ctx context.Context, keep int) error {
	if keep < 1 {
		keep = 1
	}
	_, err := s.db.ExecContext(ctx, `
        DELETE FROM scan_runs WHERE id NOT IN (
            SELECT id FROM scan_runs ORDER BY id DESC LIMIT ?
        )`, keep)
	if err != nil {
		return fmt.Errorf("store: prune runs: %w", err)
	}
	return nil
}

// rowScanner abstracts *sql.Row and *sql.Rows for shared scanning.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRunRow(row rowScanner) (*ScanRun, error) {
	var (
		run           ScanRun
		startedAt     string
		finishedAt    sql.NullString
		errMsg        sql.NullString
		librariesJSON sql.NullString
		mediaJSON     sql.NullString
	)
	if err := row.Scan(&run.ID, &startedAt, &finishedAt, &run.Status, &errMsg,
		&run.LibrariesScanned, &run.ItemsScanned, &run.MissingCount, &librariesJSON, &mediaJSON); err != nil {
		return nil, err
	}
	if t, err := time.Parse(timeLayout, startedAt); err == nil {
		run.StartedAt = t
	}
	if finishedAt.Valid {
		if t, err := time.Parse(timeLayout, finishedAt.String); err == nil {
			run.FinishedAt = &t
		}
	}
	if errMsg.Valid {
		run.Error = errMsg.String
	}
	if librariesJSON.Valid && librariesJSON.String != "" {
		_ = json.Unmarshal([]byte(librariesJSON.String), &run.Libraries)
	}
	if mediaJSON.Valid && mediaJSON.String != "" {
		_ = json.Unmarshal([]byte(mediaJSON.String), &run.Media)
	}
	return &run, nil
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullIntPtr(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
