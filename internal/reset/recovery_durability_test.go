package reset

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/store"
)

func TestReopenedFactoryRecoveryCommitsMarkerWithFullDurability(t *testing.T) {
	cfg, st := fixture(t)
	ctx := context.Background()
	dir, dbPath := filepath.Dir(cfg.Path()), st.Path()
	lease, err := cfg.Gate().TryReset()
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.ResetData(ctx, store.ResetFactory)
	lease.Finish(state.Epoch, state.FactoryEpoch, true)
	if err != nil {
		t.Fatal(err)
	}

	// The trigger observes the actual writer connection inside the completion
	// transaction. SQLite cannot change synchronous mode within a transaction.
	observer, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = observer.Exec(`
		CREATE TABLE recovery_commit_audit (sync_mode INTEGER NOT NULL);
		CREATE TRIGGER audit_factory_completion
		AFTER UPDATE OF factory_pending ON reset_state
		WHEN OLD.factory_pending=1 AND NEW.factory_pending=0
		BEGIN
			INSERT INTO recovery_commit_audit(sync_mode)
			SELECT synchronous FROM pragma_synchronous;
		END;`)
	closeErr := observer.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("install durability observer: %v; close: %v", err, closeErr)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen both components; reusing the old Store would retain FULL and mask
	// the bug. Config is file-backed and has no Close method.
	cfg, err = config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := New(cfg, st).Recover(ctx); err != nil {
		t.Fatal(err)
	}

	observer, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var mode, commits int
	err = observer.QueryRow(`SELECT min(sync_mode),count(*) FROM recovery_commit_audit`).Scan(&mode, &commits)
	closeErr = observer.Close()
	if err != nil || closeErr != nil || mode != 2 || commits != 1 {
		t.Fatalf("completion commit did not use FULL: mode=%d commits=%d err=%v close=%v", mode, commits, err, closeErr)
	}
	release, err := cfg.Gate().Enter()
	if err != nil {
		t.Fatal("recovery did not reopen ordinary admission")
	}
	_, err = cfg.UpdateGlobals(func(s *config.Settings) error {
		s.Locale = "de"
		s.TMDB.APIKey = "post-recovery-fixture-key"
		return nil
	})
	release()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err = config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(cfg, st).Recover(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = st.ResetState(ctx)
	if err != nil || state.FactoryPending || state.Epoch != 1 {
		t.Fatalf("completion marker returned after restart: %+v %v", state, err)
	}
	if got := cfg.Get(); got.Locale != "de" || got.TMDB.APIKey != "post-recovery-fixture-key" {
		t.Fatal("later restart erased post-recovery settings")
	}
}
