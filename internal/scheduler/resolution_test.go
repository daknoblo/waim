package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type resolutionTMDB struct{ scanner.TMDBAPI }

func (resolutionTMDB) SearchMovie(context.Context, string, int) ([]tmdb.MovieSearchResult, error) {
	return []tmdb.MovieSearchResult{{ID: 7, Title: "Movie", ReleaseDate: "2020-01-01"}}, nil
}
func (resolutionTMDB) Movie(context.Context, int64) (tmdb.Movie, error) {
	return tmdb.Movie{ID: 7, Title: "Movie", ReleaseDate: "2020-01-01"}, nil
}

func TestRecomputationPersistsProviderlessOccurrenceResolution(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "fake"
	settings.Sources = append(settings.Sources, config.Source{ID: "a", Type: media.Jellyfin, Name: "A", Enabled: true})
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	src, _ := cfg.Get().Source("a")
	item := media.Item{ID: "a/movie", Type: media.Movie, Name: "Movie", ProductionYear: 2020, References: []media.Reference{{ID: "a", Type: media.Jellyfin}}}
	if err := st.SaveSourceAttempt(ctx, "a", src.Fingerprint(), &media.Snapshot{Items: []media.Item{item}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 7, Title: "Movie"}, false); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, st, nil)
	s.tmdbFactory = func(config.Settings) scanner.TMDBAPI { return resolutionTMDB{} }
	s.runScan(ctx, false)
	if s.Status().LastError != "" {
		t.Fatal(s.Status().LastError)
	}
	catalog, err := source.Catalog(ctx, st, cfg.Get(), false, nil)
	if err != nil || len(catalog.Items) != 1 || catalog.Items[0].WatchOnly || len(catalog.Items[0].References) != 2 {
		t.Fatalf("scan resolution was not reusable by live catalog consumers: %+v, %v", catalog, err)
	}
}
