package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

func TestRetrospectiveMovieContextAfterFindingConversion(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	marshal := func(value any) string {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	past := store.Finding{
		Kind: store.KindMissingMovie, MediaType: store.MediaMovie,
		Title: "Past Film", TMDBID: 1,
		Details: marshal(map[string]string{"releaseDate": now.AddDate(0, 0, -7).Format("2006-01-02")}),
	}
	collection := store.Finding{
		Kind: store.KindMissingCollection, MediaType: store.MediaMovie,
		Title: "Original Collection", TMDBID: 10,
		Details: marshal(map[string]any{"missingParts": []map[string]any{
			{"tmdbId": 2, "title": "Collection Film", "releaseDate": now.AddDate(0, 0, -14).Format("2006-01-02")},
		}}),
	}
	run := &store.ScanRun{Upcoming: []store.UpcomingItem{{
		Kind: store.UpcomingMovie, MediaType: store.MediaMovie,
		Title: "Future Film", SourceTitle: "Future Film", TMDBID: 3,
		ReleaseDate: now.AddDate(0, 0, 7).Format("2006-01-02"),
	}}}
	for _, locale := range []string{"en", "de"} {
		for _, withCollection := range []bool{false, true} {
			name := locale + "/standalone"
			if withCollection {
				name += "-and-collection"
			}
			t.Run(name, func(t *testing.T) {
				tr := catalog.For(locale)
				findings := []store.Finding{past}
				expected := 1
				if withCollection {
					findings = append(findings, collection)
					expected++
				}
				converted, undated := pastItemsFromFindings(findings)
				if undated != 0 || len(converted) != expected || converted[0].Kind != store.UpcomingMovie {
					t.Fatalf("incorrect retrospective conversion: %+v, undated=%d", converted, undated)
				}
				u := BuildUpcomingSection(tr, run, findings, NormalizeUpcomingQuery(UpcomingPast, "all", ""))
				if !u.Past || u.Movies != expected || u.Total != expected {
					t.Fatalf("incorrect retrospective counts: %+v", u)
				}
				pastTiles, futureTiles := 0, 0
				for _, group := range u.Groups {
					for _, tile := range group.Items {
						switch tile.Title {
						case "Past Film":
							pastTiles++
							if tile.Sub != tr.T("sources.movie") || strings.Contains(tile.Hint, tr.T("stats.upcomingPartOf", "Past Film")) {
								t.Fatalf("retrospective self-context: %+v", tile)
							}
						case "Future Film":
							futureTiles++
						case "Collection Film":
							if tile.Sub != tr.T("stats.upcomingPartOf", "Original Collection") {
								t.Fatalf("collection attribution lost: %+v", tile)
							}
						}
					}
				}
				if pastTiles != 1 || futureTiles != 0 {
					t.Fatalf("pastTiles=%d, futureTiles=%d", pastTiles, futureTiles)
				}
				if len(u.Timeline.Markers) != expected {
					t.Fatalf("timeline markers=%d, want %d", len(u.Timeline.Markers), expected)
				}
				for _, marker := range u.Timeline.Markers {
					if strings.Contains(marker.Title, tr.T("stats.upcomingPartOf", "Past Film")) {
						t.Fatal("retrospective timeline retains standalone self-context")
					}
					if strings.Contains(marker.Title, "Collection Film") && !strings.Contains(marker.Title, tr.T("stats.upcomingPartOf", "Original Collection")) {
						t.Fatal("retrospective timeline lost collection attribution")
					}
				}
			})
		}
	}
}
