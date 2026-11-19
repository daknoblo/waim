package server

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestDashboardVirtualLibraryLocalization(t *testing.T) {
	// Keep the old name in stored fixtures to exercise upgrades without rescanning.
	s := featureServer(t)
	ctx := context.Background()
	if err := s.cfg.AddSource(config.Source{
		ID: "real", Type: media.Jellyfin, Name: "Watch collection", Enabled: true,
		Libraries: []config.Library{{ID: "library", Name: "Watch collection", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	libs := []store.LibrarySummary{
		{ID: media.VirtualID, Name: "Watch collection"},
		{ID: "real/library", Name: "Watch collection"},
	}
	if err := s.store.FinishScanRun(ctx, id, store.StatusSuccess, "", 2, 0, 0, libs, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			tr := s.catalog.For(locale)
			filters := s.libraryFilters(tr)
			foundVirtual := false
			for _, filter := range filters {
				if filter.ID == media.VirtualID {
					foundVirtual = true
					if filter.Name != tr.T("sources.collection") {
						t.Fatalf("virtual filter label = %q", filter.Name)
					}
				}
				if filter.ID == "real" && filter.Name != "Watch collection" {
					t.Fatal("user source name was translated")
				}
			}
			if !foundVirtual {
				t.Fatal("virtual filter missing")
			}
			status := s.statusView(ctx, tr)
			if status.Libraries[0].Name != tr.T("sources.collection") || status.Libraries[1].Name != "Watch collection" {
				t.Fatalf("incorrect status library labels: %+v", status.Libraries)
			}
		})
	}
	run, err := s.store.LatestSuccessfulRun(ctx)
	if err != nil || run.Libraries[0].Name != "Watch collection" {
		t.Fatal("rendering modified persisted library name")
	}
	src, _ := s.cfg.Get().Source("real")
	if src.Name != "Watch collection" {
		t.Fatal("rendering modified persisted user source name")
	}
}
