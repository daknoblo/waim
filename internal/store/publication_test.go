package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPublicationFailureRollsBackAndPartialRefreshIsNotGrowth(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	id, err := st.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `CREATE TRIGGER reject_findings BEFORE INSERT ON findings BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	f := Finding{Kind: KindMissingMovie, MediaType: MediaMovie, Title: "Fixture", Summary: "missing"}
	meta := RunMetadata{Basis: "owned-v1", Mode: "refresh"}
	if err := st.PublishScan(ctx, id, []Finding{f}, nil, nil, nil, meta); err == nil {
		t.Fatal("database error was hidden")
	}
	if run, err := st.LatestSuccessfulRun(ctx); err != nil || run != nil {
		t.Fatal("failed publication reported success")
	}
	if fs, err := st.FindingsForRun(ctx, id); err != nil || len(fs) != 0 {
		t.Fatal("partial findings escaped rollback")
	}
	if _, err := st.db.ExecContext(ctx, `DROP TRIGGER reject_findings`); err != nil {
		t.Fatal(err)
	}
	meta.Warnings = []string{"unknown inventory"}
	if err := st.PublishScan(ctx, id, nil, nil, nil, nil, meta); err != nil {
		t.Fatal(err)
	}
	if history, err := st.SuccessfulRunTotals(ctx, 10); err != nil || len(history) != 0 {
		t.Fatal("partial inventory counted as growth")
	}
	if err := st.PublishScan(ctx, id, nil, nil, nil, nil, meta); err == nil {
		t.Fatal("published result overwritten")
	}
}
