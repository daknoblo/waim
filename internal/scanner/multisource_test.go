package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

func TestMixedSeasonOwnershipAndVirtualMembership(t *testing.T) {
	td := &fakeTMDB{tv: map[int64]tmdb.TVShow{42: {ID: 42, Name: "Show", Seasons: []tmdb.SeasonSummary{{SeasonNumber: 1, EpisodeCount: 2}, {SeasonNumber: 2, EpisodeCount: 1}}}}, seasons: map[string]tmdb.Season{
		"42-1": {Episodes: []tmdb.Episode{{EpisodeNumber: 1, AirDate: "2020-01-01"}, {EpisodeNumber: 2, AirDate: "2020-01-02"}}},
		"42-2": {Episodes: []tmdb.Episode{{EpisodeNumber: 1, AirDate: "2021-01-01"}}},
	}}
	item := func(src string, eps ...media.Item) media.Item {
		return media.Item{ID: media.Qualify(src, "same"), Type: media.Series, Name: "Show", ProviderIDs: map[string]string{"Tmdb": "42"}, References: []media.Reference{{ID: src, Type: media.Jellyfin, LibraryID: src}}, Episodes: eps}
	}
	a := item("a", media.Item{ParentIndexNumber: intptr(1), IndexNumber: intptr(1)})
	b := item("b", media.Item{ParentIndexNumber: intptr(1), IndexNumber: intptr(1)}, media.Item{ParentIndexNumber: intptr(1), IndexNumber: intptr(2)}, media.Item{ParentIndexNumber: intptr(2), IndexNumber: intptr(1)})
	v := store.VirtualEntry{Type: media.Series, TMDBID: 42, Title: "Show"}.Item()
	c := media.Catalog{Items: []media.Item{v, a, b}, Libraries: []media.Library{{ID: "a"}, {ID: "b"}, {ID: media.VirtualID}}}
	res, err := New(c, td, config.Defaults(), nil).Run(context.Background())
	if err != nil || len(res.Findings) != 0 || len(res.Media) != 1 || res.Media[0].Episodes != 3 || len(res.Media[0].References) != 3 || res.Media[0].WatchOnly {
		t.Fatalf("wrong cross-source ownership: %+v %v", res, err)
	}
	c.Items = []media.Item{v}
	res, err = New(c, td, config.Defaults(), nil).Run(context.Background())
	if err != nil || len(res.Findings) != 2 || res.Media[0].Episodes != 0 || !res.Media[0].WatchOnly {
		t.Fatalf("watch-only series invented ownership: %+v %v", res, err)
	}
	c.Warnings = []string{"unknown source"}
	res, err = New(c, td, config.Defaults(), nil).Run(context.Background())
	if err != nil || !res.Findings[0].Unconfirmed {
		t.Fatal("unknown source yielded confirmed gaps")
	}
}

func TestWatchedMoviesStandaloneAndCollectionDedup(t *testing.T) {
	td := &fakeTMDB{movies: map[int64]tmdb.Movie{
		1: {ID: 1, Title: "Past", ReleaseDate: "2020-01-01", Runtime: 120, VoteAverage: 8},
		2: {ID: 2, Title: "Future", ReleaseDate: "2030-01-01", Runtime: 100, VoteAverage: 7},
		3: {ID: 3, Title: "Owned", ReleaseDate: "2020-01-01", BelongsToCollection: &tmdb.CollectionRef{ID: 100, Name: "Saga"}},
	}, collections: map[int64]tmdb.Collection{100: {ID: 100, Name: "Saga", Parts: []tmdb.CollectionPart{{ID: 1, Title: "Past", ReleaseDate: "2020-01-01"}, {ID: 2, Title: "Future", ReleaseDate: "2030-01-01"}, {ID: 3, Title: "Owned", ReleaseDate: "2020-01-01"}}}}}
	c := media.Catalog{Items: []media.Item{store.VirtualEntry{Type: media.Movie, TMDBID: 1, Title: "Past"}.Item(), store.VirtualEntry{Type: media.Movie, TMDBID: 2, Title: "Future"}.Item()}, Libraries: []media.Library{{ID: media.VirtualID}}}
	scan := func() Result {
		s := New(c, td, config.Defaults(), nil)
		s.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
		res, err := s.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := scan()
	if len(res.Findings) != 1 || res.Findings[0].Kind != store.KindMissingMovie || len(res.Upcoming) != 1 || res.Upcoming[0].Kind != store.UpcomingMovie || len(res.Media) != 2 || !res.Media[0].WatchOnly {
		t.Fatalf("standalone watch failed: %+v", res)
	}
	c.Items = append(c.Items, media.Item{ID: "real", Name: "Owned", Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": "3"}})
	res = scan()
	if len(res.Findings) != 1 || res.Findings[0].Kind != store.KindMissingCollection || len(res.Upcoming) != 1 || len(res.Upcoming[0].References) != 1 {
		t.Fatalf("duplicate collection/watch units: %+v", res)
	}
	c.Items = append(c.Items, media.Item{ID: "acquired", Name: "Past", Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": "1"}, References: []media.Reference{{ID: "a", Type: media.Jellyfin}}})
	res = scan()
	if len(res.Findings) != 0 {
		t.Fatal("acquired watched title remains missing")
	}
	for _, m := range res.Media {
		if m.TMDBID == 1 && (m.WatchOnly || len(m.References) != 2) {
			t.Fatal("ownership lost virtual membership")
		}
	}
	c.Items = c.Items[:len(c.Items)-1]
	res = scan()
	if len(res.Findings) != 1 {
		t.Fatal("disappeared real movie lost its watch tracking")
	}
}

func TestUnresolvedOwnedTitlesStaySourceLocalAndVisible(t *testing.T) {
	items := []media.Item{
		{ID: "a/same", Name: "Unknown", Type: media.Movie, References: []media.Reference{{ID: "a", LibraryID: "a"}}},
		{ID: "b/same", Name: "Unknown", Type: media.Movie, References: []media.Reference{{ID: "b", LibraryID: "b"}}},
	}

	c := media.Catalog{Items: items, Libraries: []media.Library{{ID: "a"}, {ID: "b"}}}
	res, err := New(c, &fakeTMDB{}, config.Defaults(), nil).Run(context.Background())
	if err != nil || len(res.Media) != 2 || len(res.Warnings) != 2 || res.Media[0].CatalogID == res.Media[1].CatalogID {
		t.Fatalf("unresolved ownership disappeared: %+v %v", res, err)
	}
}
