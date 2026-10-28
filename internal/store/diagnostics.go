package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// DiagnosticBasis avoids loading scan media/results or source inventory JSON.
// It is suitable for polling; full warning payloads are read only on changes.
type DiagnosticRun struct {
	ID                        int64
	Status, Started, Finished string
	HasError                  bool
}
type DiagnosticSource struct {
	ID, Fingerprint, Attempted, Succeeded string
	Failed                                bool
}
type DiagnosticBasis struct {
	Latest, Finished, Successful, Unfinished DiagnosticRun
	Sources                                  []DiagnosticSource
	Revision                                 int64
}

func (s *Store) DiagnosticBasis(ctx context.Context) (DiagnosticBasis, error) {
	var out DiagnosticBasis
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []struct {
		condition string
		target    *DiagnosticRun
	}{
		{"", &out.Latest}, {" WHERE status!='running'", &out.Finished}, {" WHERE status='success'", &out.Successful},
		{" WHERE status='running' AND id < (SELECT max(id) FROM scan_runs)", &out.Unfinished},
	} {
		err := tx.QueryRowContext(ctx, `SELECT id,status,started_at,COALESCE(finished_at,''),COALESCE(error,'')!='' FROM scan_runs`+q.condition+` ORDER BY id DESC LIMIT 1`).Scan(&q.target.ID, &q.target.Status, &q.target.Started, &q.target.Finished, &q.target.HasError)
		if err != nil && err != sql.ErrNoRows {
			return out, err
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM catalog_revision WHERE id=1`).Scan(&out.Revision); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT source_id,fingerprint,attempted_at,COALESCE(succeeded_at,''),error!='' FROM source_snapshots ORDER BY source_id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var src DiagnosticSource
		if err := rows.Scan(&src.ID, &src.Fingerprint, &src.Attempted, &src.Succeeded, &src.Failed); err != nil {
			_ = rows.Close()
			return out, err
		}
		out.Sources = append(out.Sources, src)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) DiagnosticRunMetadata(ctx context.Context, id int64) (RunMetadata, error) {
	var raw sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT metadata_json FROM scan_runs WHERE id=?`, id).Scan(&raw); err != nil {
		return RunMetadata{}, err
	}
	var out RunMetadata
	if raw.Valid && raw.String != "" {
		if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Store) DiagnosticSourceWarnings(ctx context.Context, id, fingerprint string) ([]string, error) {
	var raw sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT snapshot_json FROM source_snapshots WHERE source_id=? AND fingerprint=?`, id, fingerprint).Scan(&raw); err != nil {
		return nil, err
	}
	var out struct {
		Warnings []string `json:"warnings"`
	}
	if raw.Valid {
		if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
			return nil, err
		}
	}
	return out.Warnings, nil
}
