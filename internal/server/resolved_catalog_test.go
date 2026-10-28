package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/web"
)

type resolvedCatalogTMDB struct{ scanner.TMDBAPI }

func (resolvedCatalogTMDB) SearchMovie(context.Context, string, int) ([]tmdb.MovieSearchResult, error) {
	return []tmdb.MovieSearchResult{{ID: 7, Title: "Resolved Movie", ReleaseDate: "2020-01-01"}}, nil
}
func (resolvedCatalogTMDB) SearchTV(context.Context, string, int) ([]tmdb.TVSearchResult, error) {
	return []tmdb.TVSearchResult{{ID: 42, Name: "Resolved Show", FirstAirDate: "2021-01-01"}}, nil
}
func (resolvedCatalogTMDB) Movie(context.Context, int64) (tmdb.Movie, error) {
	return tmdb.Movie{ID: 7, Title: "Resolved Movie", ReleaseDate: "2020-01-01"}, nil
}
func (resolvedCatalogTMDB) TV(context.Context, int64) (tmdb.TVShow, error) {
	return tmdb.TVShow{ID: 42, Name: "Resolved Show", VoteAverage: 8, Seasons: []tmdb.SeasonSummary{{SeasonNumber: 1, EpisodeCount: 3}}}, nil
}
func (resolvedCatalogTMDB) Season(context.Context, int64, int) (tmdb.Season, error) {
	return tmdb.Season{SeasonNumber: 1, Episodes: []tmdb.Episode{
		{EpisodeNumber: 1, AirDate: "2021-01-01"},
		{EpisodeNumber: 2, AirDate: "2021-01-02"},
		{EpisodeNumber: 3, AirDate: "2021-01-03"},
	}}, nil
}

func TestResolvedOccurrencesStayOwnedInLiveViewsAndBadges(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := s.cfg.AddSource(config.Source{
			ID: id, Type: media.Jellyfin, Name: "Server " + id, Enabled: true,
			Jellyfin:  config.JellyfinSettings{URL: "https://" + id + ".invalid"},
			Libraries: []config.Library{{ID: "library", Name: id, Enabled: true}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	episode := func(number int) media.Item {
		season := 1
		return media.Item{ParentIndexNumber: &season, IndexNumber: &number}
	}
	occurrence := func(src, local, kind, title string, year int) media.Item {
		return media.Item{
			ID: media.Qualify(src, local), Type: kind, Name: title, ProductionYear: year,
			References: []media.Reference{{ID: src, Type: media.Jellyfin, Name: "Server " + src, LibraryID: src + "/library", ItemID: local, URL: "https://" + src + ".invalid/web/#/details?id=" + local}},
		}
	}
	movie := occurrence("a", "movie", media.Movie, "Resolved Movie", 2020)
	aShow := occurrence("a", "show", media.Series, "Resolved Show", 2021)
	aShow.Episodes = []media.Item{episode(1)}
	bShow := occurrence("b", "show", media.Series, "Resolved Show", 2021)
	bShow.ProviderIDs = map[string]string{"Tmdb": "42"}
	bShow.Episodes = []media.Item{episode(2)}
	for id, items := range map[string][]media.Item{"a": {movie, aShow}, "b": {bShow}} {
		src, _ := s.cfg.Get().Source(id)
		if err := s.store.SaveSourceAttempt(ctx, id, src.Fingerprint(), &media.Snapshot{Items: items}, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []store.VirtualEntry{
		{Type: media.Movie, TMDBID: 7, Title: movie.Name},
		{Type: media.Series, TMDBID: 42, Title: aShow.Name},
	} {
		if err := s.store.MutateVirtual(ctx, entry, false); err != nil {
			t.Fatal(err)
		}
	}
	settings := s.cfg.Get()
	catalog, err := source.Catalog(ctx, s.store, settings, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	td := resolvedCatalogTMDB{}
	catalog, err = source.ResolveSavedCatalog(ctx, s.store, settings, catalog, td)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.New(catalog, td, settings, nil).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PublishScan(ctx, id, result.Findings, result.Libraries, result.Media, result.Upcoming,
		store.RunMetadata{Basis: "owned-v1", Mode: "recompute", Revision: catalog.Revision, SourcesToken: settings.SourcesToken()}); err != nil {
		t.Fatal(err)
	}
	run, err := s.currentRun(ctx)
	if err != nil || run == nil || len(run.Media) != 2 || run.ItemsScanned != 2 {
		t.Fatalf("live overlay lost resolved ownership: %+v, %v", run, err)
	}
	for _, stat := range run.Media {
		if stat.WatchOnly {
			t.Fatalf("resolved real occurrence became watch-only: %+v", stat)
		}
		if stat.Type == store.MediaSeries && (stat.Episodes != 2 || len(stat.References) != 3) {
			t.Fatalf("live series lost a resolved source's episodes: %+v", stat)
		}
	}
	r := s.provenanceRequest(httptest.NewRequest("GET", "/stats", nil))
	target := web.Target(r.Context(), web.WatchURL(media.Movie, 7))
	if target.WatchOnly || !target.Watching || len(target.References) != 2 {
		t.Fatalf("badge ownership diverged from scan ownership: %+v", target)
	}
	src, _ := s.cfg.Get().Source("a")
	if err := s.cfg.UpdateSourceWithKey("a", src.Revision, "replacement", func(src *config.Source) error {
		src.Jellyfin.URL = "https://different.invalid"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range changed.Items {
		if item.Type == media.Movie && (!item.WatchOnly || len(item.References) != 1) {
			t.Fatal("resolved alias leaked across changed source fingerprint")
		}
		if item.Type == media.Series && len(item.Episodes) != 1 {
			t.Fatal("changed source's resolved episodes remained in the catalog")
		}
	}
}
