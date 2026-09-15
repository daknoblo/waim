package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
)

func featureServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	var err error
	s.store, err = store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	s.catalog, err = i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	s.sched = scheduler.New(s.cfg, s.store, s.log)
	s.suggest = suggest.New(s.cfg, s.store, s.log)
	t.Cleanup(func() { s.suggest.Close(); _ = s.store.Close() })
	settings := s.cfg.Get()
	settings.TMDB.APIKey = "local-fake"
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCollectionEndpointsUseVerifiedMetadataAndProtectMutations(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	if err := s.store.TMDBCachePut(ctx, "/movie/42?language=en-US", []byte(`{"id":42,"title":"Verified title","release_date":"2020-01-01","vote_average":8}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.store.TMDBCachePut(ctx, "/search/movie?include_adult=false&language=en-US&page=1&query=Verified", []byte(`{"results":[{"id":42,"title":"Verified title","release_date":"2020-01-01"}]}`)); err != nil {
		t.Fatal(err)
	}
	post := func(path string, form url.Values, foreign bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", "http://example.com/collection")
		if foreign {
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		return w
	}
	form := url.Values{"kind": {media.Movie}, "id": {"42"}, "title": {"Forged"}, "owned": {"true"}}
	if w := post("/collection/add", form, true); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site mutation accepted: %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := post("/collection/add", form, false); w.Code != 303 {
			t.Fatalf("add failed: %d %s", w.Code, w.Body.String())
		}
	}
	entries, revision, err := s.store.VirtualEntries(ctx)
	if err != nil || len(entries) != 1 || revision != 1 || entries[0].Title != "Verified title" {
		t.Fatalf("browser metadata trusted or duplicate: %+v", entries)
	}
	for _, locale := range []string{"en", "de"} {
		req := httptest.NewRequest("GET", "/collection?q=Verified", nil)
		req.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Verified title") || !strings.Contains(w.Body.String(), "/collection/remove") {
			t.Fatalf("collection not immediately visible: %s", w.Body.String())
		}
	}
	if w := post("/collection/add", url.Values{"kind": {"collection"}, "id": {"42"}}, false); w.Code != 400 {
		t.Fatal("collection ID accepted as movie")
	}
	if w := post("/collection/remove", form, false); w.Code != 303 {
		t.Fatal("remove failed")
	}
	entries, _, err = s.store.VirtualEntries(ctx)
	if err != nil || len(entries) != 0 {
		t.Fatal("remove did not persist")
	}
	if w := post("/sources/virtual/remove", url.Values{"revision": {"0"}, "confirm": {"yes"}}, false); !strings.Contains(w.Body.String(), "cannot be removed") {
		t.Fatal("permanent collection protection missing")
	}
}

func TestSourceFormsAreIndependent(t *testing.T) {
	s := featureServer(t)
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Library/MediaFolders" {
			t.Errorf("unexpected discovery request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"Items":[]}`))
	}))
	defer jf.Close()
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		return w
	}

	for _, name := range []string{"A", "B"} {
		if w := post("/sources", url.Values{"name": {name}, "url": {jf.URL}, "key": {"key-" + name}}); w.Code != 303 {
			t.Fatal(w.Body.String())
		}
	}
	a, b := s.cfg.Get().Sources[1], s.cfg.Get().Sources[2]
	revision := strconv.FormatInt(a.Revision, 10)
	if w := post("/sources/"+a.ID, url.Values{"revision": {revision}, "name": {"Renamed"}, "url": {a.Jellyfin.URL}, "enabled": {"on"}}); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	bNow, _ := s.cfg.Get().Source(b.ID)
	if bNow.Jellyfin.APIKey != "key-B" || bNow.Revision != b.Revision {
		t.Fatal("independent source overwritten")
	}
	if w := post("/sources/"+a.ID, url.Values{"revision": {revision}, "name": {"Old form"}, "url": {a.Jellyfin.URL}}); !strings.Contains(w.Body.String(), "reload") {
		t.Fatal("stale form accepted")
	}
	aNow, _ := s.cfg.Get().Source(a.ID)
	if aNow.Name != "Renamed" {
		t.Fatal("stale form overwrote name")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/settings?tab=media", nil))
	if strings.Contains(w.Body.String(), "key-A") || strings.Contains(w.Body.String(), "key-B") {
		t.Fatal("source form leaked keys")
	}
}
