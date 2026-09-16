package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/media"
)

func TestVirtualRevisionAndAtomicPublication(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	v := VirtualEntry{Type: media.Movie, TMDBID: 42, Title: "Movie"}
	if err := st.MutateVirtual(ctx, v, false); err != nil {
		t.Fatal(err)
	}
	if err := st.MutateVirtual(ctx, v, false); err != nil {
		t.Fatal(err)
	}
	entries, revision, err := st.VirtualEntries(ctx)
	if err != nil || len(entries) != 1 || revision != 1 {
		t.Fatalf("%v %d %v", entries, revision, err)
	}
	id, err := st.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MutateVirtual(ctx, v, true); err != nil {
		t.Fatal(err)
	}
	f := Finding{Kind: KindMissingMovie, MediaType: MediaMovie, Title: "Movie", LibraryID: "virtual", LibraryName: "Watch", Summary: "not owned", TMDBID: 42, Provenance: Provenance{WatchOnly: true, References: v.Item().References}}
	err = st.PublishScan(ctx, id, []Finding{f}, nil, nil, nil, RunMetadata{Basis: "owned-v1", Mode: "recompute", Revision: revision})
	if !errors.Is(err, ErrCatalogChanged) {
		t.Fatalf("want revision conflict: %v", err)
	}
	fs, err := st.FindingsForRun(ctx, id)
	if err != nil || len(fs) != 0 {
		t.Fatal("conflicting computation partially published")
	}
	if run, err := st.LatestSuccessfulRun(ctx); err != nil || run != nil {
		t.Fatal("false successful run")
	}
	err = st.PublishScan(ctx, id, []Finding{f}, nil, []MediaStat{{Type: MediaMovie, Provenance: Provenance{WatchOnly: true}}}, nil, RunMetadata{Basis: "owned-v1", Mode: "recompute", Revision: 2})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.LatestSuccessfulRun(ctx)
	if err != nil || run.ItemsScanned != 0 || run.Metadata.Revision != 2 {
		t.Fatalf("incorrect published state: %+v %v", run, err)
	}
	fs, err = st.FindingsForRun(ctx, id)
	if err != nil || !fs[0].WatchOnly || len(fs[0].References) != 1 {
		t.Fatal("provenance not persisted")
	}
	history, err := st.SuccessfulRunTotals(ctx, 10)
	if err != nil || len(history) != 0 {
		t.Fatal("recomputation counted as growth")
	}
}

func TestSourceSnapshotsBoundedByIdentity(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = st.Close() }()
	ctx := context.Background()
	snap := &media.Snapshot{Items: []media.Item{{ID: "a", Name: "Owned"}}}
	if err := st.SaveSourceAttempt(ctx, "a", "one", snap, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSourceAttempt(ctx, "a", "one", nil, "offline"); err != nil {
		t.Fatal(err)
	}
	got, err := st.SourceSnapshot(ctx, "a", "one")
	if err != nil || got.Snapshot == nil || got.Error != "offline" {
		t.Fatal("last good snapshot lost")
	}
	if err := st.SaveSourceAttempt(ctx, "a", "two", nil, "offline"); err != nil {
		t.Fatal(err)
	}
	got, err = st.SourceSnapshot(ctx, "a", "two")
	if err != nil || got.Snapshot != nil || got.SucceededAt != nil {
		t.Fatal("unrelated identity reused")
	}
}
