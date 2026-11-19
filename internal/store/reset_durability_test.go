package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCompletionPromotesReopenedConnectionAndRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reset.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ResetData(ctx, ResetFactory); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var mode int
	if err := st.db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&mode); err != nil || mode != 1 {
		t.Fatalf("reopening must retain the ordinary NORMAL default: mode=%d err=%v", mode, err)
	}
	if _, err := st.db.ExecContext(ctx, `CREATE TRIGGER reject_completion BEFORE UPDATE OF factory_pending ON reset_state
		WHEN NEW.factory_pending=0 BEGIN SELECT RAISE(ABORT,'fixture completion failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishFactoryReset(ctx); err == nil {
		t.Fatal("marker write failure was hidden")
	}
	state, err := st.ResetState(ctx)
	if err != nil || !state.FactoryPending {
		t.Fatal("failed completion cleared the recovery marker")
	}
	if _, err := st.db.ExecContext(ctx, `DROP TRIGGER reject_completion`); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishFactoryReset(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&mode); err != nil || mode != 2 {
		t.Fatalf("completion did not retain FULL on its connection: mode=%d err=%v", mode, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&mode); err != nil || mode != 1 {
		t.Fatalf("reset changed the ordinary reopen default: mode=%d err=%v", mode, err)
	}
	state, err = st.ResetState(ctx)
	if err != nil || state.FactoryPending || state.Epoch != 1 {
		t.Fatalf("completion did not survive reopen: %+v %v", state, err)
	}
}
