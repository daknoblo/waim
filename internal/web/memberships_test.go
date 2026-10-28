package web

import (
	"testing"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestSharedSeriesGapsAndRatingsUseEveryLibraryMembership(t *testing.T) {
	refs := []media.Reference{
		{ID: "a", LibraryID: "a/tv", LibraryName: "A"},
		{ID: "b", LibraryID: "b/tv", LibraryName: "B"},
		{ID: "b", LibraryID: "b/tv", LibraryName: "B", ItemID: "duplicate edition"},
	}
	stat := store.MediaStat{
		Type: store.MediaSeries, Title: "TMDB Show", TMDBID: 42, Rating: 8,
		LibraryID: "a/tv", LibraryName: "A", Episodes: 2, TotalEpisodes: 3,
		Provenance: store.Provenance{References: refs},
	}
	run := &store.ScanRun{
		Metadata: store.RunMetadata{Basis: "owned-v1"}, ItemsScanned: 1,
		Libraries: []store.LibrarySummary{
			{ID: "a/tv", Name: "A", Total: 1, Scanned: 1, Missing: 1},
			{ID: "b/tv", Name: "B", Total: 1, Scanned: 1, Missing: 1},
		},
		Media: []store.MediaStat{stat, stat},
	}
	finding := store.Finding{
		Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries, Title: "Server Show", TMDBID: 42,
		LibraryID: "a/tv", LibraryName: "A", Provenance: store.Provenance{References: refs},
		Details: `{"seasonNumber":1,"episodeCount":3,"missingEpisodes":[3]}`,
	}
	stats := BuildStats(testTranslator(t), StatsInput{Run: run, Findings: []store.Finding{finding}})
	if len(stats.Libraries) != 2 {
		t.Fatal("shared library membership missing")
	}
	for _, lib := range stats.Libraries {
		if lib.ItemsWithGaps != 1 || lib.Completeness != 0 || lib.MissingUnits != 1 {
			t.Fatalf("incorrect shared-library completeness: %+v", lib)
		}
	}
	if len(stats.LibraryRatings) != 2 || len(stats.SeriesFindings) != 2 {
		t.Fatalf("shared ratings omitted: owned=%+v missing=%+v", stats.LibraryRatings, stats.SeriesFindings)
	}
	for _, groups := range [][]StatsLibraryRatings{stats.LibraryRatings, stats.SeriesFindings} {
		for _, group := range groups {
			if len(group.Top) != 1 || len(group.Lowest) != 1 || group.Top[0].Rating != "8.0" {
				t.Fatalf("duplicate or absent per-library rating: %+v", group)
			}
		}
	}
	if stats.ItemsScanned != 1 || stats.SeriesScanned != 1 || stats.SeriesEpisodes != 2 || stats.TotalGaps != 1 || stats.MissingUnits != 1 {
		t.Fatalf("memberships inflated global counts: titles=%d series=%d episodes=%d gaps=%d missing=%d", stats.ItemsScanned, stats.SeriesScanned, stats.SeriesEpisodes, stats.TotalGaps, stats.MissingUnits)
	}
}

func TestMissingCollectionRatingsDeduplicateWithinEachMembership(t *testing.T) {
	f := store.Finding{
		Kind: store.KindMissingCollection, TMDBID: 100, LibraryID: "a/movies", LibraryName: "A",
		Provenance: store.Provenance{ContextReferences: []media.Reference{
			{ID: "a", LibraryID: "a/movies", LibraryName: "A"},
			{ID: "b", LibraryID: "b/movies", LibraryName: "B"},
		}},
		Details: `{"missingParts":[{"tmdbId":99,"title":"Part","year":"2020","rating":7}]}`,
	}
	expanded := membershipFindings([]store.Finding{f, f}, nil)
	ratings := buildFindingRatings(expanded)
	if len(ratings) != 2 {
		t.Fatalf("missing collection omitted a membership: %+v", ratings)
	}
	for _, group := range ratings {
		if len(group.Top) != 1 || group.Top[0].Title != "Part" {
			t.Fatalf("duplicate per-library collection rating: %+v", group)
		}
	}
}
