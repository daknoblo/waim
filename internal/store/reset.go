package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/daknoblo/waim/internal/media"
)

type ResetScope string

const (
	ResetMetadata ResetScope = "metadata"
	ResetMedia    ResetScope = "media"
	ResetFactory  ResetScope = "factory"
)

func (s ResetScope) Valid() bool { return s == ResetMetadata || s == ResetMedia || s == ResetFactory }

type ResetState struct {
	Epoch, FactoryEpoch int64
	FactoryPending      bool
}

func (s *Store) ResetState(ctx context.Context) (ResetState, error) {
	var state ResetState
	err := s.db.QueryRowContext(ctx, `SELECT epoch,factory_epoch,factory_pending FROM reset_state WHERE id=1`).Scan(&state.Epoch, &state.FactoryEpoch, &state.FactoryPending)
	return state, err
}

// ResetData requires exclusive application admission. FULL synchronous mode
// makes the journal durable before a factory reset can replace config.json.
func (s *Store) ResetData(ctx context.Context, scope ResetScope) (result ResetState, resultErr error) {
	if !scope.Valid() {
		return ResetState{}, errors.New("unknown reset scope")
	}
	conn, tx, err := s.beginDurableReset(ctx)
	if err != nil {
		return ResetState{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings; DELETE FROM scan_runs; DELETE FROM kv; UPDATE catalog_revision SET revision=revision+1 WHERE id=1`); err != nil {
		return ResetState{}, err
	}
	if scope == ResetMetadata || scope == ResetFactory {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tmdb_cache`); err != nil {
			return ResetState{}, err
		}
	}
	if scope == ResetMedia || scope == ResetFactory {
		if _, err := tx.ExecContext(ctx, `DELETE FROM source_snapshots`); err != nil {
			return ResetState{}, err
		}
	} else {
		rows, err := tx.QueryContext(ctx, `SELECT source_id,snapshot_json FROM source_snapshots WHERE snapshot_json IS NOT NULL`)
		if err != nil {
			return ResetState{}, err
		}
		type snapshotRow struct {
			id       string
			snapshot media.Snapshot
		}
		var snapshots []snapshotRow
		for rows.Next() {
			var row snapshotRow
			var raw string
			if err := rows.Scan(&row.id, &raw); err != nil {
				_ = rows.Close()
				return ResetState{}, err
			}
			if err := json.Unmarshal([]byte(raw), &row.snapshot); err != nil {
				_ = rows.Close()
				return ResetState{}, err
			}
			snapshots = append(snapshots, row)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return ResetState{}, err
		}
		var clearAliases func(*media.Item)
		clearAliases = func(item *media.Item) {
			item.ResolvedTMDBID, item.ResolutionInput = 0, ""
			for i := range item.Episodes {
				clearAliases(&item.Episodes[i])
			}
		}
		for _, row := range snapshots {
			for i := range row.snapshot.Items {
				clearAliases(&row.snapshot.Items[i])
			}
			raw, err := json.Marshal(row.snapshot)
			if err != nil {
				return ResetState{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE source_snapshots SET snapshot_json=? WHERE source_id=?`, string(raw), row.id); err != nil {
				return ResetState{}, err
			}
		}
	}
	if scope == ResetFactory {
		if _, err := tx.ExecContext(ctx, `DELETE FROM virtual_entries; UPDATE reset_state SET factory_pending=1,factory_epoch=epoch+1 WHERE id=1`); err != nil {
			return ResetState{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reset_state SET epoch=epoch+1 WHERE id=1`); err != nil {
		return ResetState{}, err
	}
	var state ResetState
	if err := tx.QueryRowContext(ctx, `SELECT epoch,factory_epoch,factory_pending FROM reset_state WHERE id=1`).Scan(&state.Epoch, &state.FactoryEpoch, &state.FactoryPending); err != nil {
		return ResetState{}, err
	}
	if err := tx.Commit(); err != nil {
		return ResetState{}, err
	}
	return state, nil
}

// FinishFactoryReset is its own durable boundary: startup recovery uses a fresh
// NORMAL connection and does not run ResetData again before clearing the marker.
func (s *Store) FinishFactoryReset(ctx context.Context) (resultErr error) {
	conn, tx, err := s.beginDurableReset(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE reset_state SET factory_pending=0 WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}

// Keep the pragma and commit on one pinned connection. FULL remains in effect
// for that connection, matching ResetData; reopening Store still defaults NORMAL.
func (s *Store) beginDurableReset(ctx context.Context) (*sql.Conn, *sql.Tx, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA synchronous=FULL`); err != nil {
		return nil, nil, errors.Join(err, conn.Close())
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, errors.Join(err, conn.Close())
	}
	return conn, tx, nil
}
