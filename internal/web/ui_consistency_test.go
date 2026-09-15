package web

import (
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestVirtualLibraryDisplayNamesAreLocalizedWithoutMutatingData(t *testing.T) {
	// Legacy virtual names must render the new name; user-defined names stay intact.
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScanRun{
		Libraries: []store.LibrarySummary{
			{ID: media.VirtualID, Name: "Watch collection"},
			{ID: "real/library", Name: "Watch collection"},
		},
		Media: []store.MediaStat{
			{Type: store.MediaSeries, Title: "Tracked show", LibraryID: media.VirtualID, LibraryName: "Watch collection"},
			{Type: store.MediaMovie, Title: "Owned film", LibraryID: "real/library", LibraryName: "Watch collection"},
		},
	}
	findings := []store.Finding{
		{Kind: store.KindMissingMovie, Title: "Tracked film", LibraryID: media.VirtualID, LibraryName: "Watch collection"},
		{Kind: store.KindMissingSeason, Title: "Tracked show", LibraryID: media.VirtualID, LibraryName: "Watch collection"},
		{Kind: store.KindMissingCollection, Title: "Tracked collection", LibraryID: media.VirtualID, LibraryName: "Watch collection"},
		{Kind: store.KindMissingMovie, Title: "Owned film", LibraryID: "real/library", LibraryName: "Watch collection"},
	}
	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			tr := catalog.For(locale)
			want := tr.T("sources.collection")
			expected := map[string]string{"en": "Virtual collection", "de": "Virtuelle Sammlung"}[locale]
			if want != expected {
				t.Fatalf("collection label = %q, want %q", want, expected)
			}
			localRun, localFindings := localizedLibraryData(tr, run, findings)
			if localRun.Libraries[0].Name != want || localRun.Media[0].LibraryName != want || localFindings[0].LibraryName != want {
				t.Fatal("virtual library display names were not localized")
			}
			if localRun.Libraries[1].Name != "Watch collection" || localRun.Media[1].LibraryName != "Watch collection" {
				t.Fatal("user-provided library name was translated")
			}
			for _, row := range BuildFindingRows(tr, findings, "") {
				if row.Library != LibraryDisplayName(tr, row.LibraryID, "Watch collection") {
					t.Fatalf("unlocalized finding row: %+v", row)
				}
			}
			flow := buildSeriesFlow(tr, store.MediaStat{
				Title: "Show", LibraryID: media.VirtualID, LibraryName: "Watch collection",
				Episodes: 1, Seasons: []store.SeasonStat{{Number: 1, Episodes: 1, Total: 2}},
			})
			if flow.Library != want {
				t.Fatalf("series flow library = %q, want %q", flow.Library, want)
			}
		})
	}
	if run.Libraries[0].Name != "Watch collection" || run.Media[0].LibraryName != "Watch collection" || findings[0].LibraryName != "Watch collection" {
		t.Fatal("display localization mutated input/persisted names")
	}
}

func TestUpcomingMovieContextAndTrackedDescription(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []store.UpcomingItem{
		{Kind: store.UpcomingMovie, MediaType: store.MediaMovie, Title: "Future Film", SourceTitle: "Future Film", TMDBID: 1, ReleaseDate: "2026-01-03"},
		{Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie, Title: "Collection Film", SourceTitle: "Film Collection", TMDBID: 2, ReleaseDate: "2026-01-03"},
	}
	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			tr := catalog.For(locale)
			u := buildUpcoming(tr, items, now, NormalizeUpcomingQuery("", "all", ""))
			var standalone, collection UpcomingEntry
			for _, group := range u.Groups {
				for _, entry := range group.Items {
					if entry.Title == "Future Film" {
						standalone = entry
					} else {
						collection = entry
					}
				}
			}
			if standalone.Sub != tr.T("sources.movie") || strings.Contains(standalone.Hint, tr.T("stats.upcomingPartOf", "Future Film")) {
				t.Fatalf("standalone movie attributes itself as context: %+v", standalone)
			}
			if collection.Sub != tr.T("stats.upcomingPartOf", "Film Collection") {
				t.Fatalf("collection attribution lost: %+v", collection)
			}
			for _, marker := range u.Timeline.Markers {
				if strings.Contains(marker.Title, tr.T("stats.upcomingPartOf", "Future Film")) {
					t.Fatal("timeline retained standalone self-context")
				}
			}
			trackedWord := "tracked"
			if locale == "de" {
				trackedWord = "beobachteten"
			}
			if !strings.Contains(tr.T("stats.upcomingHint"), trackedWord) {
				t.Fatal("upcoming description excludes tracked titles")
			}
		})
	}
}
