package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/media"
)

func TestSnapshotResolutionsAreDurableAndIdentityBound(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	raw := media.Item{ID: "a/local", Type: media.Movie, Name: "Movie", ProductionYear: 2020}
	snapshot := &media.Snapshot{Items: []media.Item{raw}}
	if err := st.SaveSourceAttempt(ctx, "a", "original", snapshot, ""); err != nil {
		t.Fatal(err)
	}
	resolved := raw.WithResolvedID(7)
	if err := st.RememberSourceResolutions(ctx, "a", "original", []media.Item{resolved}); err != nil {
		t.Fatal(err)
	}
	read := func(fingerprint string) media.Item {
		t.Helper()
		saved, err := st.SourceSnapshot(ctx, "a", fingerprint)
		if err != nil || saved.Snapshot == nil {
			t.Fatalf("snapshot unavailable: %v", err)
		}
		return saved.Snapshot.Items[0]
	}
	if got := read("original"); got.TMDBID() != 7 || len(got.ProviderIDs) != 0 {
		t.Fatal("resolution was lost or overwrote original provider metadata")
	}
	if err := st.RememberSourceResolutions(ctx, "a", "original", []media.Item{raw.WithResolvedID(8)}); !errors.Is(err, ErrCatalogChanged) {
		t.Fatal("competing resolution diverged from the durable identity")
	}
	if err := st.SaveSourceAttempt(ctx, "a", "original", snapshot, ""); err != nil {
		t.Fatal(err)
	}
	if read("original").TMDBID() != 7 {
		t.Fatal("refresh of unchanged occurrence dropped the resolution")
	}
	changed := raw
	changed.Name = "Different title using the same local ID"
	if err := st.SaveSourceAttempt(ctx, "a", "original", &media.Snapshot{Items: []media.Item{changed}}, ""); err != nil {
		t.Fatal(err)
	}
	if read("original").TMDBID() != 0 {
		t.Fatal("local-ID reuse inherited an unrelated identity")
	}
	if err := st.RememberSourceResolutions(ctx, "a", "original", []media.Item{resolved}); !errors.Is(err, ErrCatalogChanged) {
		t.Fatalf("outdated resolution accepted after a concurrent refresh: %v", err)
	}
	if err := st.SaveSourceAttempt(ctx, "a", "original", snapshot, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.RememberSourceResolutions(ctx, "a", "original", []media.Item{resolved}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSourceAttempt(ctx, "a", "different-server", snapshot, ""); err != nil {
		t.Fatal(err)
	}
	if read("different-server").TMDBID() != 0 {
		t.Fatal("resolution crossed the source configuration fingerprint")
	}
	if err := st.RememberSourceResolutions(ctx, "a", "original", []media.Item{resolved}); !errors.Is(err, ErrCatalogChanged) {
		t.Fatal("old-source resolution accepted")
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE source_snapshots SET snapshot_json='{' WHERE source_id='a'`); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSourceAttempt(ctx, "a", "different-server", snapshot, ""); err != nil {
		t.Fatalf("a successful refresh could not replace corrupt old alias data: %v", err)
	}
	if got := read("different-server"); got.Name != raw.Name || got.TMDBID() != 0 {
		t.Fatal("corrupt snapshot replacement reused an unverified identity")
	}
}
