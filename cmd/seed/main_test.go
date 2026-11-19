package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
)

func TestSeedCreatesPersistedOfflineDataset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "waim.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	if err := seed(ctx, st, path); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	if settings.Scan.RunOnStart || settings.Scan.IntervalMinutes != 0 || len(settings.Sources) != 3 {
		t.Fatal("offline fixture must not automatically contact media servers")
	}
	catalog, err := source.Catalog(ctx, st, settings, false, nil)
	if err != nil || len(catalog.Warnings) != 0 || len(catalog.Items) != len(seriesTitles)+len(movieTitles)+2 {
		t.Fatalf("seed catalog is inconsistent: %d items, %v", len(catalog.Items), err)
	}
	entries, revision, err := st.VirtualEntries(ctx)
	if err != nil || len(entries) != 3 {
		t.Fatal("seed virtual membership missing")
	}
	run, err := st.LatestSuccessfulRun(ctx)
	if err != nil || run == nil || len(run.Media) != len(catalog.Items) || run.Metadata.Revision != revision || run.Metadata.SourcesToken != settings.SourcesToken() {
		t.Fatalf("seed scan publication inconsistent: %v", err)
	}
	for _, item := range catalog.Items {
		if item.WatchOnly && (len(item.References) != 1 || item.References[0].Type != media.Virtual) {
			t.Fatal("watch-only fixture invented a real owner")
		}
	}
	if findings, err := st.FindingsForRun(ctx, run.ID); err != nil || len(findings) == 0 {
		t.Fatal("seed did not persist findings")
	}
	if len(run.Upcoming) == 0 {
		t.Fatal("seed has no release examples")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := seed(ctx, st, path); err == nil {
		t.Fatal("seed ignored a database error")
	}
}
