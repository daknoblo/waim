package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type collectionTMDB struct {
	scanner.TMDBAPI
	failSeason, futureOnly bool
	calls                  map[int]int
}

func (*collectionTMDB) TV(context.Context, int64) (tmdb.TVShow, error) {
	return tmdb.TVShow{ID: 42, Name: "Series", Seasons: []tmdb.SeasonSummary{
		{SeasonNumber: 0, EpisodeCount: 1}, {SeasonNumber: 1, EpisodeCount: 3}, {SeasonNumber: 2, EpisodeCount: 2},
	}}, nil
}

func (*collectionTMDB) Movie(context.Context, int64) (tmdb.Movie, error) {
	return tmdb.Movie{ID: 42, Title: "Owned film", ReleaseDate: "2020-01-01"}, nil
}
func (f *collectionTMDB) Season(_ context.Context, _ int64, sn int) (tmdb.Season, error) {
	f.calls[sn]++
	if f.failSeason && sn == 2 {
		return tmdb.Season{}, errors.New("season unavailable")
	}
	date := "2020-01-01"
	if f.futureOnly {
		date = "2999-01-01"
	}
	eps := []tmdb.Episode{{EpisodeNumber: 1, SeasonNumber: sn, AirDate: date}}
	if sn == 1 {
		eps = append(eps, tmdb.Episode{EpisodeNumber: 2, SeasonNumber: sn, AirDate: date}, tmdb.Episode{EpisodeNumber: 3, SeasonNumber: sn, AirDate: "2999-01-01"})
	}
	if sn == 2 {
		eps = append(eps, tmdb.Episode{EpisodeNumber: 2, SeasonNumber: sn})
	}
	return tmdb.Season{SeasonNumber: sn, Episodes: eps}, nil
}

func TestCollectionCompletenessUsesReleasedEpisodeUnion(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		specials, ownSpecial bool
		missing, failed      bool
		futureOnly           bool
		want                 string
	}{
		{name: "split across sources", want: "complete"},
		{name: "specials ignored", ownSpecial: true, want: "complete"},
		{name: "specials missing", specials: true, want: "partial"},
		{name: "specials included", specials: true, ownSpecial: true, want: "complete"},
		{name: "equal count wrong episode", missing: true, want: "partial"},
		{name: "failed season", failed: true, want: "unverified"},
		{name: "no released episodes", futureOnly: true, want: "unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := featureServer(t)
			ctx := context.Background()
			settings := s.cfg.Get()
			settings.Scan.IncludeSpecials = tc.specials
			if err := s.cfg.Save(settings); err != nil {
				t.Fatal(err)
			}
			entry := store.VirtualEntry{Type: media.Series, TMDBID: 42, Title: "Series"}
			if err := s.store.MutateVirtual(ctx, entry, false); err != nil {
				t.Fatal(err)
			}
			item := media.Item{ID: "audit/series", Type: media.Series, Name: "Series", ProviderIDs: map[string]string{"Tmdb": "42"}, Episodes: []media.Item{auditEpisode(1, 1), auditEpisode(2, 1)}}
			if tc.ownSpecial {
				item.Episodes = append(item.Episodes, auditEpisode(0, 1))
			}
			auditSource(t, s, []media.Item{item})
			if err := s.cfg.AddSource(config.Source{ID: "second", Type: media.Jellyfin, Name: "Second", Enabled: true, Libraries: []config.Library{{ID: "lib", Name: "Series", Enabled: true}}}); err != nil {
				t.Fatal(err)
			}
			second, _ := s.cfg.Get().Source("second")
			episode := 2
			if tc.missing {
				episode = 99
			}
			item.ID, item.Episodes = "second/series", []media.Item{auditEpisode(1, episode)}
			item.References = []media.Reference{{ID: second.ID, Type: media.Jellyfin, Name: second.Name, LibraryID: "second/lib", LibraryName: "Series", ItemID: item.ID}}
			if err := s.store.SaveSourceAttempt(ctx, second.ID, second.Fingerprint(), &media.Snapshot{Items: []media.Item{item}, Libraries: []media.Library{{ID: "second/lib", Name: "Series"}}}, ""); err != nil {
				t.Fatal(err)
			}
			c, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
			if err != nil {
				t.Fatal(err)
			}
			td := &collectionTMDB{failSeason: tc.failed, futureOnly: tc.futureOnly, calls: map[int]int{}}
			result, err := scanner.New(c, td, s.cfg.Get(), nil).Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			auditPublish(t, s, c, result)
			for sn, count := range td.calls {
				if count != 1 {
					t.Fatalf("availability added redundant season %d requests: %d", sn, count)
				}
			}
			for _, locale := range []string{"en", "de"} {
				w := diagRequest(s, "/collection", locale, "")
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `data-collection-ownership="`+tc.want+`"`) {
					t.Fatalf("%s expected %s: %s", locale, tc.want, w.Body.String())
				}
			}
			// Reopen the actual persisted database rather than reusing an in-memory result.
			reopened, err := store.Open(filepath.Clean(s.store.Path()))
			if err != nil {
				t.Fatal(err)
			}
			saved, err := reopened.LatestSuccessfulRun(ctx)
			_ = reopened.Close()
			if err != nil || saved == nil || len(saved.Media) != 1 {
				t.Fatalf("persisted assessment missing: %v", err)
			}
			availability := saved.Media[0].LibraryAvailability
			if tc.failed {
				if availability != nil {
					t.Fatal("failed metadata produced a verified assessment")
				}
			} else if availability == nil || availability.Complete != (tc.want == "complete") {
				t.Fatalf("incorrect persisted availability: %+v", availability)
			}
			if tc.want != "complete" {
				return
			}
			changed := s.cfg.Get()
			changed.Scan.IncludeSpecials = !tc.specials
			if err := s.cfg.Save(changed); err != nil {
				t.Fatal(err)
			}
			if html := diagRequest(s, "/collection", "en", "").Body.String(); strings.Contains(html, `data-collection-ownership="complete"`) {
				t.Fatal("changed specials policy retained an incompatible completion assessment")
			}
			changed.Scan.IncludeSpecials = tc.specials
			if err := s.cfg.Save(changed); err != nil {
				t.Fatal(err)
			}
			if err := s.store.SaveSourceAttempt(ctx, second.ID, second.Fingerprint(), nil, "offline"); err != nil {
				t.Fatal(err)
			}
			if html := diagRequest(s, "/collection", "en", "").Body.String(); strings.Contains(html, `data-collection-ownership="complete"`) || !strings.Contains(html, `data-collection-ownership="unverified"`) {
				t.Fatal("stale source kept confirmed ownership")
			}
			form := url.Values{"kind": {media.Series}, "id": {"42"}}
			req := httptest.NewRequest("POST", "/collection/remove", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Referer", "http://example.com/collection")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusSeeOther {
				t.Fatal("manual removal failed")
			}
			entries, _, err := s.store.VirtualEntries(ctx)
			if err != nil || len(entries) != 0 {
				t.Fatal("virtual membership retained after removal")
			}
			c, err = source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
			if err != nil || len(c.Items) != 1 || c.Items[0].WatchOnly || len(c.Items[0].References) != 2 || len(c.Items[0].Episodes) < 3 {
				t.Fatal("removing virtual entry changed real-source tracking")
			}
		})
	}
}

func TestCollectionOwnedMovieDoesNotRequireEpisodeCounts(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	if err := s.store.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 42, Title: "Owned film"}, false); err != nil {
		t.Fatal(err)
	}
	auditSource(t, s, []media.Item{{ID: "audit/movie", Type: media.Movie, Name: "Owned film", ProviderIDs: map[string]string{"Tmdb": "42"}}})
	c, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.New(c, &collectionTMDB{}, s.cfg.Get(), nil).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	auditPublish(t, s, c, result)
	if html := diagRequest(s, "/collection", "en", "").Body.String(); !strings.Contains(html, `data-collection-ownership="complete"`) {
		t.Fatal("owned movie should not require series episode counters")
	}
}

func TestSuggestionAddPersistsVerifiedSeriesAndReturnsToSuggestions(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	if err := s.store.TMDBCachePut(ctx, "/tv/42?language=en-US", []byte(`{"id":42,"name":"Verified series","first_air_date":"2020-01-01"}`)); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"kind": {media.Series}, "id": {"42"}, "title": {"Forged title"}}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/collection/add", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", "http://example.com/suggestions")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/suggestions" {
			t.Fatalf("suggestion add did not return to suggestions: %d %s", w.Code, w.Body.String())
		}
	}
	entries, revision, err := s.store.VirtualEntries(ctx)
	if err != nil || len(entries) != 1 || revision != 1 || entries[0].Title != "Verified series" || entries[0].Type != media.Series {
		t.Fatalf("add did not persist canonical, unique series: %+v %v", entries, err)
	}
}
