package server

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func TestLateResolutionDeduplicatesOldUnresolvedAndWatchedStats(t *testing.T) {
	for _, canonicalFirst := range []bool{false, true} {
		name := "unresolved-first"
		if canonicalFirst {
			name = "canonical-first"
		}
		t.Run(name, func(t *testing.T) {
			s := featureServer(t)
			ctx := context.Background()
			if err := s.cfg.AddSource(config.Source{ID: "a", Type: media.Jellyfin, Name: "A", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			src, _ := s.cfg.Get().Source("a")
			item := media.Item{ID: "a/movie", Name: "Resolved Movie", Type: media.Movie, ProductionYear: 2020, References: []media.Reference{{ID: "a", Type: media.Jellyfin, LibraryID: "a/library"}}}
			if err := s.store.SaveSourceAttempt(ctx, "a", src.Fingerprint(), &media.Snapshot{Items: []media.Item{item}}, ""); err != nil {
				t.Fatal(err)
			}
			entry := store.VirtualEntry{Type: media.Movie, TMDBID: 7, Title: item.Name}
			if err := s.store.MutateVirtual(ctx, entry, false); err != nil {
				t.Fatal(err)
			}
			unknown := store.MediaStat{Type: store.MediaMovie, Title: item.Name, Provenance: store.Provenance{CatalogID: item.ID, References: item.References}}
			known := store.MediaStat{Type: store.MediaMovie, Title: "Canonical metadata", TMDBID: 7, Rating: 8, Runtime: 90, Provenance: store.Provenance{WatchOnly: true, References: entry.Item().References}}
			stats := []store.MediaStat{unknown, known}
			if canonicalFirst {
				stats = []store.MediaStat{known, unknown}
			}
			id, err := s.store.StartScanRun(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.store.PublishScan(ctx, id, nil, nil, stats, nil, store.RunMetadata{Basis: "owned-v1", Mode: "recompute", Revision: 1, SourcesToken: s.cfg.Get().SourcesToken()}); err != nil {
				t.Fatal(err)
			}
			catalog, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := source.ResolveSavedCatalog(ctx, s.store, s.cfg.Get(), catalog, resolvedCatalogTMDB{}); err != nil {
				t.Fatal(err)
			}
			run, err := s.currentRun(ctx)
			if err != nil || run == nil || len(run.Media) != 1 || run.ItemsScanned != 1 || !run.Metadata.Pending {
				t.Fatalf("late resolution duplicated or hid ownership: %+v, %v", run, err)
			}
			stat := run.Media[0]
			if stat.WatchOnly || stat.TMDBID != 7 || stat.Rating != 8 || stat.Runtime != 90 || len(stat.References) != 2 {
				t.Fatalf("late resolution did not preserve canonical metadata and all membership: %+v", stat)
			}
			if s.dataState(ctx) != web.DataIncomplete {
				t.Fatal("old derived evaluations were not marked pending")
			}
		})
	}
}

func TestKnownIdentityDoesNotFollowReusedLocalID(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	if err := s.cfg.AddSource(config.Source{ID: "a", Type: media.Jellyfin, Name: "A", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	src, _ := s.cfg.Get().Source("a")
	item := media.Item{ID: "a/movie", Name: "Original", Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": "7"}, References: []media.Reference{{ID: "a", Type: media.Jellyfin}}}
	if err := s.store.SaveSourceAttempt(ctx, "a", src.Fingerprint(), &media.Snapshot{Items: []media.Item{item}}, ""); err != nil {
		t.Fatal(err)
	}
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stats := []store.MediaStat{{Type: store.MediaMovie, Title: "Original", TMDBID: 7, Provenance: store.Provenance{CatalogID: item.ID, References: item.References}}}
	if err := s.store.PublishScan(ctx, id, nil, nil, stats, nil, store.RunMetadata{Basis: "owned-v1", Mode: "refresh", SourcesToken: s.cfg.Get().SourcesToken()}); err != nil {
		t.Fatal(err)
	}
	item.Name, item.ProviderIDs = "Different movie", map[string]string{"Tmdb": "9"}
	if err := s.store.SaveSourceAttempt(ctx, "a", src.Fingerprint(), &media.Snapshot{Items: []media.Item{item}}, ""); err != nil {
		t.Fatal(err)
	}
	run, err := s.currentRun(ctx)
	if err != nil || run == nil || len(run.Media) != 0 || !run.Metadata.Pending {
		t.Fatalf("old canonical metadata followed a reused local ID: %+v, %v", run, err)
	}
}
