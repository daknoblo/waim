package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/daknoblo/waim/internal/media"
)

func snapshotInTx(ctx context.Context, tx *sql.Tx, id, fingerprint string) (*media.Snapshot, error) {
	var payload sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT snapshot_json FROM source_snapshots WHERE source_id=? AND fingerprint=?`, id, fingerprint).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil || !payload.Valid {
		return nil, err
	}
	var snapshot media.Snapshot
	if err := json.Unmarshal([]byte(payload.String), &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// carryResolutions only reuses verified aliases for exactly matching original
// identity metadata. The caller additionally enforces the source fingerprint.
func carryResolutions(snapshot *media.Snapshot, resolved []media.Item) media.Snapshot {
	out := *snapshot
	out.Items = append([]media.Item(nil), snapshot.Items...)
	identities := map[string]int64{}
	for _, item := range resolved {
		if item.ResolvedTMDBID > 0 && item.TMDBID() == item.ResolvedTMDBID {
			identities[item.ResolutionInput] = item.ResolvedTMDBID
		}
	}
	for i, item := range out.Items {
		if item.TMDBID() == 0 {
			if id := identities[item.IdentityInput()]; id > 0 {
				out.Items[i] = item.WithResolvedID(id)
			}
		}
	}
	return out
}

// RememberSourceResolutions updates only matching occurrences in the current
// fingerprint-bound snapshot. It neither refreshes inventory nor changes its
// success/attempt timestamps, and does not retain an unbounded alias history.
func (s *Store) RememberSourceResolutions(ctx context.Context, id, fingerprint string, resolved []media.Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot, err := snapshotInTx(ctx, tx, id, fingerprint)
	if err != nil {
		return err
	}
	if snapshot == nil {
		return ErrCatalogChanged
	}
	updated := carryResolutions(snapshot, resolved)
	current := map[string]int64{}
	for _, item := range updated.Items {
		current[item.IdentityInput()] = item.TMDBID()
	}
	for _, item := range resolved {
		if item.ResolvedTMDBID <= 0 || item.ResolutionInput != item.IdentityInput() || current[item.IdentityInput()] != item.ResolvedTMDBID {
			return ErrCatalogChanged
		}
	}
	payload, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE source_snapshots SET snapshot_json=? WHERE source_id=? AND fingerprint=?`, string(payload), id, fingerprint); err != nil {
		return err
	}
	return tx.Commit()
}
