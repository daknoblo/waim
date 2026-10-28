package suggest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
)

type recommendationTransport func(*http.Request) (*http.Response, error)

func (f recommendationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRecommendationsFilterResolvedRealOwnershipButNotWatchOnly(t *testing.T) {
	original := http.DefaultTransport
	http.DefaultTransport = recommendationTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.themoviedb.org" {
			return nil, fmt.Errorf("unexpected request (no real networking allowed): %s", r.URL.Host)
		}
		body := `{"results":[]}`
		switch r.URL.Path {
		case "/3/search/movie":
			body = `{"results":[{"id":7,"title":"Resolved Movie","release_date":"2020-01-01"}]}`
		case "/3/search/tv":
			body = `{"results":[{"id":42,"name":"Resolved Show","first_air_date":"2021-01-01"}]}`
		case "/3/trending/movie/week":
			body = `{"results":[{"id":7,"title":"Owned movie"},{"id":8,"title":"Only watched"}]}`
		case "/3/trending/tv/week":
			body = `{"results":[{"id":42,"name":"Owned show"},{"id":43,"name":"Unowned show"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = original }()
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "fake"
	settings.Scan.TMDBRateLimitRPS = 50
	for _, id := range []string{"a", "b"} {
		settings.Sources = append(settings.Sources, config.Source{ID: id, Type: media.Jellyfin, Name: id, Enabled: true})
	}
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	season, first, second := 1, 1, 2
	movie := media.Item{ID: "a/movie", Type: media.Movie, Name: "Resolved Movie", ProductionYear: 2020, References: []media.Reference{{ID: "a", Type: media.Jellyfin}}}
	show := media.Item{ID: "a/show", Type: media.Series, Name: "Resolved Show", ProductionYear: 2021, References: []media.Reference{{ID: "a", Type: media.Jellyfin}}, Episodes: []media.Item{{ParentIndexNumber: &season, IndexNumber: &first}}}
	other := media.Item{ID: "b/show", Type: media.Series, Name: "Resolved Show", ProviderIDs: map[string]string{"Tmdb": "42"}, References: []media.Reference{{ID: "b", Type: media.Jellyfin}}, Episodes: []media.Item{{ParentIndexNumber: &season, IndexNumber: &second}}}
	for id, items := range map[string][]media.Item{"a": {movie, show}, "b": {other}} {
		src, _ := cfg.Get().Source(id)
		if err := st.SaveSourceAttempt(ctx, id, src.Fingerprint(), &media.Snapshot{Items: items}, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{7, 8} {
		if err := st.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: id, Title: "Watched"}, false); err != nil {
			t.Fatal(err)
		}
	}
	service := New(cfg, st, nil)
	defer service.Close()
	result := service.build(ctx)
	if len(result.Errors) != 0 {
		t.Fatalf("recommendation generation failed: %v", result.Errors)
	}
	foundWatchOnly := false
	for _, item := range result.Trending {
		if (item.MediaType == store.MediaMovie && item.TMDBID == 7) || (item.MediaType == store.MediaSeries && item.TMDBID == 42) {
			t.Fatalf("resolved real-owned title was recommended: %+v", item)
		}
		foundWatchOnly = foundWatchOnly || item.MediaType == store.MediaMovie && item.TMDBID == 8
	}
	if !foundWatchOnly {
		t.Fatal("watch-only title was incorrectly filtered as owned")
	}
	live, err := source.Catalog(ctx, st, cfg.Get(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	foundSeries := false
	for _, item := range live.Items {
		if item.Type == media.Series && item.TMDBID() == 42 {
			foundSeries = true
			if len(item.Episodes) != 2 || len(item.References) != 2 {
				t.Fatalf("recommendations and live catalog disagree on episode union: %+v", item)
			}
		}
	}
	if !foundSeries {
		t.Fatal("resolved series absent from live catalog")
	}
}
