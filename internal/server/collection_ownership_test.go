package server

import (
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func TestCollectionOwnershipRequiresVerifiedCurrentInventory(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*store.ScanRun, *store.ScanRun)
		want   string
	}{
		{"complete", func(_, _ *store.ScanRun) {}, web.CollectionComplete},
		{"missing episode", func(r, _ *store.ScanRun) { r.Media[0].LibraryAvailability.Complete = false }, web.CollectionPartial},
		{"no released episodes", func(r, _ *store.ScanRun) { r.Media[0].LibraryAvailability.ReleasedEpisodes = 0 }, web.CollectionUnverified},
		{"legacy assessment", func(r, _ *store.ScanRun) { r.Media[0].LibraryAvailability = nil }, web.CollectionUnverified},
		{"legacy run", func(r, _ *store.ScanRun) { r.Metadata.Basis = "" }, web.CollectionUnverified},
		{"pending", func(r, _ *store.ScanRun) { r.Metadata.Pending = true }, web.CollectionUnverified},
		{"metadata failed", func(r, _ *store.ScanRun) { r.Media[0].MetadataUnavailable = true }, web.CollectionUnverified},
		{"warning", func(r, _ *store.ScanRun) { r.Metadata.Warnings = []string{"unresolved"} }, web.CollectionUnverified},
		{"uncertain", func(r, _ *store.ScanRun) { r.Media[0].Unconfirmed = true }, web.CollectionUnverified},
		{"stale source", func(r, _ *store.ScanRun) { r.Media[0].References[0].Stale = true }, web.CollectionUnverified},
		{"specials changed", func(r, _ *store.ScanRun) { r.Media[0].LibraryAvailability.IncludeSpecials = true }, web.CollectionUnverified},
		{"latest failed", func(_, latest *store.ScanRun) { latest.ID++; latest.Status = store.StatusError }, web.CollectionUnverified},
		{"latest running", func(_, latest *store.ScanRun) { latest.ID++; latest.Status = store.StatusRunning }, web.CollectionUnverified},
		{"virtual only", func(r, _ *store.ScanRun) { r.Media[0].WatchOnly = true }, ""},
		{"no real reference", func(r, _ *store.ScanRun) { r.Media[0].References[0].Type = media.Virtual }, ""},
		{"new episode due", func(r, _ *store.ScanRun) {
			r.Upcoming = []store.UpcomingItem{{Kind: store.UpcomingEpisode, TMDBID: 42, ReleaseDate: "2026-09-16"}}
		}, web.CollectionUnverified},
		{"future episode", func(r, _ *store.ScanRun) {
			r.Upcoming = []store.UpcomingItem{{Kind: store.UpcomingEpisode, TMDBID: 42, ReleaseDate: "2026-09-17"}}
		}, web.CollectionComplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finished := now.Add(-time.Hour)
			run := &store.ScanRun{ID: 1, Status: store.StatusSuccess, FinishedAt: &finished, Metadata: store.RunMetadata{Basis: "owned-v1"}, Media: []store.MediaStat{{
				Type: store.MediaSeries, TMDBID: 42,
				Provenance:          store.Provenance{References: []media.Reference{{ID: "real", Type: media.Jellyfin}}},
				LibraryAvailability: &store.LibraryAvailability{Complete: true, ReleasedEpisodes: 3},
			}}}
			latest := *run
			tc.change(run, &latest)
			entries := []store.VirtualEntry{{Type: media.Series, TMDBID: 42}, {Type: media.Movie, TMDBID: 42}}
			got := collectionOwnership(entries, run, &latest, false, false, now)
			if got[media.Key(media.Series, 42)] != tc.want || got[media.Key(media.Movie, 42)] != "" {
				t.Fatalf("unexpected ownership: %+v, want %s for series only", got, tc.want)
			}
			if tc.want == web.CollectionComplete && collectionOwnership(entries, run, &latest, false, true, now)[media.Key(media.Series, 42)] != web.CollectionUnverified {
				t.Fatal("active recomputation should not confirm completeness")
			}
		})
	}
}
