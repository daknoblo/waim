package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/daknoblo/waim/internal/media"
)

var ErrCatalogChanged = errors.New("catalog changed during computation")

type VirtualEntry struct {
	Type    string    `json:"type"`
	TMDBID  int64     `json:"tmdbId"`
	Title   string    `json:"title"`
	Year    int       `json:"year"`
	Poster  string    `json:"poster"`
	AddedAt time.Time `json:"addedAt"`
}

func (v VirtualEntry) Item() media.Item {
	kind := "movie"
	if v.Type == media.Series {
		kind = "tv"
	}
	return media.Item{ID: media.Qualify(media.VirtualID, media.Key(v.Type, v.TMDBID)), Name: v.Title, Type: v.Type, ProductionYear: v.Year, ProviderIDs: map[string]string{"Tmdb": strconv.FormatInt(v.TMDBID, 10)}, WatchOnly: true, References: []media.Reference{{ID: media.VirtualID, Type: media.Virtual, Name: media.VirtualName, LibraryID: media.VirtualID, LibraryName: media.VirtualName, URL: fmt.Sprintf("https://www.themoviedb.org/%s/%d", kind, v.TMDBID)}}}
}

func (s *Store) VirtualEntries(ctx context.Context) ([]VirtualEntry, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM catalog_revision WHERE id=1`).Scan(&revision); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT media_type,tmdb_id,title,year,poster,added_at FROM virtual_entries ORDER BY added_at,title`)
	if err != nil {
		return nil, 0, err
	}
	out := []VirtualEntry{}
	for rows.Next() {
		var v VirtualEntry
		var added string
		if err := rows.Scan(&v.Type, &v.TMDBID, &v.Title, &v.Year, &v.Poster, &added); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		v.AddedAt, _ = time.Parse(timeLayout, added)
		out = append(out, v)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return out, revision, tx.Commit()
}

func (s *Store) MutateVirtual(ctx context.Context, v VirtualEntry, remove bool) error {
	if (v.Type != media.Movie && v.Type != media.Series) || v.TMDBID <= 0 {
		return errors.New("invalid media identity")
	}
	if !remove && v.Title == "" {
		return errors.New("title is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var result sql.Result
	if remove {
		result, err = tx.ExecContext(ctx, `DELETE FROM virtual_entries WHERE media_type=? AND tmdb_id=?`, v.Type, v.TMDBID)
	} else {
		result, err = tx.ExecContext(ctx, `INSERT INTO virtual_entries(media_type,tmdb_id,title,year,poster,added_at) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, v.Type, v.TMDBID, v.Title, v.Year, v.Poster, time.Now().UTC().Format(timeLayout))
	}
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE catalog_revision SET revision=revision+1 WHERE id=1`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type SourceSnapshot struct {
	SourceID    string
	Fingerprint string
	Snapshot    *media.Snapshot
	AttemptedAt time.Time
	SucceededAt *time.Time
	Error       string
}

func (s *Store) SourceSnapshot(ctx context.Context, id, fingerprint string) (SourceSnapshot, error) {
	out := SourceSnapshot{SourceID: id, Fingerprint: fingerprint}
	var raw, success sql.NullString
	var attempted string
	err := s.db.QueryRowContext(ctx, `SELECT snapshot_json,attempted_at,succeeded_at,error FROM source_snapshots WHERE source_id=? AND fingerprint=?`, id, fingerprint).Scan(&raw, &attempted, &success, &out.Error)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.AttemptedAt, _ = time.Parse(timeLayout, attempted)
	if success.Valid {
		t, _ := time.Parse(timeLayout, success.String)
		out.SucceededAt = &t
	}
	if raw.Valid {
		var snap media.Snapshot
		if err := json.Unmarshal([]byte(raw.String), &snap); err != nil {
			return out, err
		}
		out.Snapshot = &snap
	}
	return out, nil
}

// SaveSourceAttempt never replaces a good same-identity snapshot on failure.
// A changed identity invalidates the old payload even if its first fetch fails.
func (s *Store) SaveSourceAttempt(ctx context.Context, id, fingerprint string, snapshot *media.Snapshot, message string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var payload any
	var success any
	now := time.Now().UTC().Format(timeLayout)
	if snapshot != nil {
		previous, err := snapshotInTx(ctx, tx, id, fingerprint)
		if err != nil {
			var syntaxError *json.SyntaxError
			var typeError *json.UnmarshalTypeError
			if !errors.As(err, &syntaxError) && !errors.As(err, &typeError) {
				return err
			}
			slog.Warn("replacing corrupt source snapshot without reusing identity bindings", "sourceId", id, "err", err)
		}
		if previous != nil {
			updated := carryResolutions(snapshot, previous.Items)
			snapshot = &updated
		}
		b, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		payload, success = string(b), now
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_snapshots(source_id,fingerprint,snapshot_json,attempted_at,succeeded_at,error) VALUES(?,?,?,?,?,?)
	ON CONFLICT(source_id) DO UPDATE SET
	snapshot_json=CASE WHEN excluded.snapshot_json IS NOT NULL THEN excluded.snapshot_json WHEN source_snapshots.fingerprint=excluded.fingerprint THEN source_snapshots.snapshot_json ELSE NULL END,
	succeeded_at=CASE WHEN excluded.succeeded_at IS NOT NULL THEN excluded.succeeded_at WHEN source_snapshots.fingerprint=excluded.fingerprint THEN source_snapshots.succeeded_at ELSE NULL END,
	fingerprint=excluded.fingerprint,attempted_at=excluded.attempted_at,error=excluded.error`, id, fingerprint, payload, now, success, message)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type RunMetadata struct {
	Pending      bool     `json:"pending,omitempty"`
	SourcesToken string   `json:"sourcesToken,omitempty"`
	Basis        string   `json:"basis"`
	Mode         string   `json:"mode"`
	Revision     int64    `json:"revision"`
	Warnings     []string `json:"warnings,omitempty"`
}

// PublishScan commits findings, summaries and success together, conditional on
// the virtual revision still being current.
func (s *Store) PublishScan(ctx context.Context, id int64, fs []Finding, libraries []LibrarySummary, stats []MediaStat, upcoming []UpcomingItem, metadata RunMetadata) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM catalog_revision WHERE id=1`).Scan(&revision); err != nil {
		return err
	}
	if revision != metadata.Revision {
		return ErrCatalogChanged
	}
	now := time.Now().UTC().Format(timeLayout)
	for _, f := range fs {
		refs, err := json.Marshal(f.Provenance)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO findings(scan_run_id,kind,media_type,library_id,library_name,title,tmdb_id,jellyfin_id,season_number,summary,details,created_at,provenance_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, f.Kind, f.MediaType, f.LibraryID, f.LibraryName, f.Title, nullInt64(f.TMDBID), nullString(f.JellyfinID), nullIntPtr(f.SeasonNumber), f.Summary, nullString(f.Details), now, string(refs))
		if err != nil {
			return err
		}
	}
	lb, err := json.Marshal(libraries)
	if err != nil {
		return err
	}
	mb, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	ub, err := json.Marshal(upcoming)
	if err != nil {
		return err
	}
	meta, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	owned := 0
	for _, m := range stats {
		if !m.WatchOnly {
			owned++
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE scan_runs SET finished_at=?,status='success',error=NULL,libraries_scanned=?,items_scanned=?,missing_count=?,libraries_json=?,media_json=?,upcoming_json=?,metadata_json=? WHERE id=? AND status='running'`, now, len(libraries), owned, len(fs), string(lb), string(mb), string(ub), string(meta), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("scan run is missing or already published")
	}
	return tx.Commit()
}
