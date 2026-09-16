package server

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestCurrentOwnershipDropsDisabledSourceBeforeRecompute(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	src := config.Source{ID: "a", Type: media.Jellyfin, Name: "A", Enabled: true, Jellyfin: config.JellyfinSettings{URL: "https://a.invalid"}, Libraries: []config.Library{{ID: "lib", Enabled: true}}}
	if err := s.cfg.AddSource(src); err != nil {
		t.Fatal(err)
	}
	src, _ = s.cfg.Get().Source("a")
	item := media.Item{ID: "a/title", Type: media.Movie, Name: "Title", ProviderIDs: map[string]string{"Tmdb": "42"}, References: []media.Reference{{ID: "a", Type: media.Jellyfin, LibraryID: "a/lib", URL: "https://a.invalid/web/#/details?id=title"}}}
	if err := s.store.SaveSourceAttempt(ctx, "a", src.Fingerprint(), &media.Snapshot{Items: []media.Item{item}}, ""); err != nil {
		t.Fatal(err)
	}
	v := store.VirtualEntry{Type: media.Movie, TMDBID: 42, Title: "Title"}
	if err := s.store.MutateVirtual(ctx, v, false); err != nil {
		t.Fatal(err)
	}
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PublishScan(ctx, id, nil, nil, []store.MediaStat{{Type: store.MediaMovie, Title: "Title", TMDBID: 42, Provenance: store.Provenance{CatalogID: item.ID, References: item.References}}}, nil, store.RunMetadata{Basis: "owned-v1", Mode: "refresh", Revision: 1, SourcesToken: s.cfg.Get().SourcesToken()}); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.UpdateSource("a", src.Revision, func(src *config.Source) error { src.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	run, err := s.currentRun(ctx)
	if err != nil || len(run.Media) != 1 || !run.Media[0].WatchOnly || run.ItemsScanned != 0 || !run.Metadata.Pending || len(run.Media[0].References) != 1 || run.Media[0].References[0].ID != media.VirtualID {
		t.Fatalf("disabled source leaked current ownership: %+v %v", run, err)
	}
	if err := s.store.MutateVirtual(ctx, v, true); err != nil {
		t.Fatal(err)
	}
	run, err = s.currentRun(ctx)
	if err != nil || len(run.Media) != 0 {
		t.Fatal("removed observation remained in current results")
	}
}
