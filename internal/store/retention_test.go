package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPruneRunsPreservesRefreshGrowthAcrossRecomputations(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	publish := func(mode string, count int) {
		t.Helper()
		id, err := st.StartScanRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		stats := make([]MediaStat, count)
		if err := st.PublishScan(ctx, id, nil, nil, stats, nil, RunMetadata{Basis: "owned-v1", Mode: mode}); err != nil {
			t.Fatal(err)
		}
		if err := st.PruneRuns(ctx, 20); err != nil {
			t.Fatal(err)
		}
	}
	publish("refresh", 1)
	publish("refresh", 2)
	before, err := st.SuccessfulRunTotals(ctx, 20)
	if err != nil || len(before) != 2 {
		t.Fatal("refresh history missing")
	}
	for i := 0; i < 25; i++ {
		publish("recompute", 2)
	}
	history, err := st.SuccessfulRunTotals(ctx, 20)
	if err != nil || len(history) != 2 || history[0] != before[0] || history[1] != before[1] {
		t.Fatalf("virtual edits erased real growth history: %+v, %v", history, err)
	}
	runs, err := st.RecentRuns(ctx, 100)
	if err != nil || len(runs) != 22 {
		t.Fatalf("retention is not independently bounded: %d runs, %v", len(runs), err)
	}
	for i := 0; i < 25; i++ {
		publish("refresh", 3)
	}
	history, err = st.SuccessfulRunTotals(ctx, 100)
	if err != nil || len(history) != 20 {
		t.Fatalf("real refresh history is not bounded: %d, %v", len(history), err)
	}
}
