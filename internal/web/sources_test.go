package web

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestActionsUseCurrentMembershipAndSafeLinks(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	v := store.VirtualEntry{Type: media.Movie, TMDBID: 1, Title: "Film"}.Item()
	real := v
	real.WatchOnly = false
	real.References = []media.Reference{{ID: "one", Type: media.Jellyfin, Name: "Server one", URL: "https://one.example/web/#/details?id=same"}}
	ctx := WithProvenance(context.Background(), media.Catalog{Items: media.Merge([]media.Item{real, v})}, nil)
	for _, locale := range []string{"en", "de"} {
		var b bytes.Buffer
		if err := MediaActions(cat.For(locale), WatchURL(media.Movie, 1)).Render(ctx, &b); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		if !strings.Contains(html, `aria-label="Server one"`) || !strings.Contains(html, "/collection/remove") || !strings.Contains(html, "https://one.example") {
			t.Fatalf("missing instance actions: %s", html)
		}
	}
	run := &store.ScanRun{Media: []store.MediaStat{{Type: store.MediaMovie, TMDBID: 1, Provenance: store.Provenance{References: real.References}}}}
	ctx = WithProvenance(context.Background(), media.Catalog{Items: []media.Item{v}}, run)
	if got := MediaHref(ctx, real.References[0].URL); got != WatchURL(media.Movie, 1) {
		t.Fatal("removed source still linked")
	}
	ctx = WithProvenance(context.Background(), media.Catalog{}, run)
	if Target(ctx, WatchURL(media.Movie, 1)).Watching {
		t.Fatal("removed virtual membership leaked")
	}
	if Target(ctx, "https://www.themoviedb.org/collection/1").ID != 0 {
		t.Fatal("collection offered movie watch action")
	}
}

func TestWatchOnlyExcludedFromOwnedStatsAndIncludedInRetrospective(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScanRun{Metadata: store.RunMetadata{Basis: "owned-v1"}, ItemsScanned: 1, Libraries: []store.LibrarySummary{{ID: "a", Total: 1, Scanned: 1}, {ID: media.VirtualID, Total: 1, Scanned: 1}}, Media: []store.MediaStat{
		{Type: store.MediaMovie, Title: "Owned", Runtime: 90, Rating: 8, TMDBID: 1, LibraryID: "a"},
		{Type: store.MediaMovie, Title: "Watched", Runtime: 999, Rating: 9, TMDBID: 2, LibraryID: media.VirtualID, Provenance: store.Provenance{WatchOnly: true}},
	}}
	f := store.Finding{Kind: store.KindMissingMovie, MediaType: store.MediaMovie, Title: "Watched", TMDBID: 2, Provenance: store.Provenance{WatchOnly: true}, Details: `{"releaseDate":"2026-01-01"}`}
	sd := BuildStats(cat.For("en"), StatsInput{Run: run, Findings: []store.Finding{f}})
	if sd.MoviesScanned != 1 || sd.TrackingTitles != 1 || sd.TrackingMissing != 1 || len(sd.LongestMovies) != 1 || sd.LongestMovies[0].Title != "Owned" || sd.TotalGaps != 0 {
		t.Fatalf("watch-only polluted ownership: %+v", sd)
	}
	items, _ := pastItemsFromFindings([]store.Finding{f})
	up := buildUpcoming(cat.For("en"), items, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), NormalizeUpcomingQuery("past", "all", "all"))
	if up.Movies != 1 {
		t.Fatal("standalone movie missing from retrospective")
	}
}
