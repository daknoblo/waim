package scanner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type fakeJF struct {
	items    map[string][]jellyfin.Item
	episodes map[string][]jellyfin.Item
}

func (f *fakeJF) ResolveUserID(_ context.Context, _ string) (string, error) { return "user", nil }
func (f *fakeJF) ItemsInLibrary(_ context.Context, _, libID string) ([]jellyfin.Item, error) {
	return f.items[libID], nil
}
func (f *fakeJF) Episodes(_ context.Context, _, seriesID string) ([]jellyfin.Item, error) {
	return f.episodes[seriesID], nil
}

type fakeTMDB struct {
	movies      map[int64]tmdb.Movie
	collections map[int64]tmdb.Collection
	tv          map[int64]tmdb.TVShow
	seasons     map[string]tmdb.Season
}

func (f *fakeTMDB) Movie(_ context.Context, id int64) (tmdb.Movie, error) {
	m, ok := f.movies[id]
	if !ok {
		return tmdb.Movie{}, tmdb.ErrNotFound
	}
	return m, nil
}
func (f *fakeTMDB) Collection(_ context.Context, id int64) (tmdb.Collection, error) {
	c, ok := f.collections[id]
	if !ok {
		return tmdb.Collection{}, tmdb.ErrNotFound
	}
	return c, nil
}
func (f *fakeTMDB) TV(_ context.Context, id int64) (tmdb.TVShow, error) {
	t, ok := f.tv[id]
	if !ok {
		return tmdb.TVShow{}, tmdb.ErrNotFound
	}
	return t, nil
}
func (f *fakeTMDB) Season(_ context.Context, tvID int64, n int) (tmdb.Season, error) {
	s, ok := f.seasons[fmt.Sprintf("%d-%d", tvID, n)]
	if !ok {
		return tmdb.Season{}, tmdb.ErrNotFound
	}
	return s, nil
}
func (f *fakeTMDB) SearchMovie(_ context.Context, _ string, _ int) ([]tmdb.MovieSearchResult, error) {
	return nil, nil
}
func (f *fakeTMDB) SearchTV(_ context.Context, _ string, _ int) ([]tmdb.TVSearchResult, error) {
	return nil, nil
}

func intptr(i int) *int { return &i }

func TestScanFindsGaps(t *testing.T) {
	jf := &fakeJF{
		items: map[string][]jellyfin.Item{
			"lib1": {
				{ID: "m1", Name: "Movie X", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "200"}},
				{ID: "s1", Name: "Show A", Type: "Series", ProviderIDs: map[string]string{"Tmdb": "100"}},
			},
		},
		episodes: map[string][]jellyfin.Item{
			"s1": {
				{Type: "Episode", ParentIndexNumber: intptr(1), IndexNumber: intptr(1)},
				{Type: "Episode", ParentIndexNumber: intptr(1), IndexNumber: intptr(2)},
				// season 1 episode 3 missing; entire season 2 missing
			},
		},
	}
	td := &fakeTMDB{
		movies: map[int64]tmdb.Movie{
			200: {ID: 200, Title: "Movie X", BelongsToCollection: &tmdb.CollectionRef{ID: 500, Name: "X Collection"}},
		},
		collections: map[int64]tmdb.Collection{
			500: {ID: 500, Name: "X Collection", Parts: []tmdb.CollectionPart{
				{ID: 200, Title: "Movie X", ReleaseDate: "2018-01-01"},
				{ID: 201, Title: "Movie X 2", ReleaseDate: "2020-01-01"}, // missing, released
				{ID: 202, Title: "Movie X 3", ReleaseDate: "2030-01-01"}, // future, ignored
			}},
		},
		tv: map[int64]tmdb.TVShow{
			100: {ID: 100, Name: "Show A", Seasons: []tmdb.SeasonSummary{
				{SeasonNumber: 0, EpisodeCount: 2}, // specials, excluded by default
				{SeasonNumber: 1, EpisodeCount: 3},
				{SeasonNumber: 2, EpisodeCount: 2},
			}},
		},
		seasons: map[string]tmdb.Season{
			"100-1": {SeasonNumber: 1, Episodes: []tmdb.Episode{
				{EpisodeNumber: 1, AirDate: "2020-01-01"},
				{EpisodeNumber: 2, AirDate: "2020-01-08"},
				{EpisodeNumber: 3, AirDate: "2020-01-15"},
			}},
			"100-2": {SeasonNumber: 2, Episodes: []tmdb.Episode{
				{EpisodeNumber: 1, AirDate: "2021-01-01"},
				{EpisodeNumber: 2, AirDate: "2021-01-08"},
			}},
		},
	}

	settings := config.Settings{
		Libraries: []config.Library{{ID: "lib1", Name: "Mixed", Enabled: true}},
		Scan:      config.ScanSettings{IncludeSpecials: false},
	}

	s := New(jf, td, settings, nil)
	s.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	res, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ItemsScanned != 2 {
		t.Errorf("ItemsScanned = %d, want 2", res.ItemsScanned)
	}

	var gotSeason, gotEpisodes, gotCollection int
	for _, f := range res.Findings {
		switch f.Kind {
		case store.KindMissingSeason:
			gotSeason++
			if f.SeasonNumber == nil || *f.SeasonNumber != 2 {
				t.Errorf("missing season: unexpected season %v", f.SeasonNumber)
			}
		case store.KindMissingEpisodes:
			gotEpisodes++
			if f.SeasonNumber == nil || *f.SeasonNumber != 1 {
				t.Errorf("missing episodes: unexpected season %v", f.SeasonNumber)
			}
		case store.KindMissingCollection:
			gotCollection++
			if f.TMDBID != 500 {
				t.Errorf("missing collection: tmdb id %d, want 500", f.TMDBID)
			}
		}
	}
	if gotSeason != 1 {
		t.Errorf("missing-season findings = %d, want 1", gotSeason)
	}
	if gotEpisodes != 1 {
		t.Errorf("missing-episodes findings = %d, want 1", gotEpisodes)
	}
	if gotCollection != 1 {
		t.Errorf("missing-collection findings = %d, want 1", gotCollection)
	}
}
