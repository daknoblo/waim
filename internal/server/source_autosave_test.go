package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
)

func autosaveSource(s *Server, id string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/sources/"+id, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Waim-Source-Autosave", "true")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func autosaveFixture(t *testing.T) (*Server, config.Source, url.Values) {
	t.Helper()
	s := featureServer(t)
	interval := 45
	if err := s.cfg.AddSource(config.Source{ID: "fixture", Name: "Home", Type: media.Jellyfin, Enabled: true, ScanIntervalMinutes: &interval,
		Jellyfin:  config.JellyfinSettings{URL: "https://original.example", APIKey: "private-original-key"},
		Libraries: []config.Library{{ID: "movies", Name: "Movies", Enabled: true}, {ID: "series", Name: "Series"}}}); err != nil {
		t.Fatal(err)
	}
	src, _ := s.cfg.Get().Source("fixture")
	values := url.Values{"revision": {strconv.FormatInt(src.Revision, 10)}, "name": {src.Name}, "url": {src.Jellyfin.URL}, "enabled": {"on"}, "scan_interval": {"45"}, "library": {"movies"}}
	return s, src, values
}

func TestSourceAutosaveReturnsRevisionWithoutSecretsOrRedirect(t *testing.T) {
	s, src, values := autosaveFixture(t)
	values.Set("name", "Renamed")
	values.Set("scan_interval", "0")
	w := autosaveSource(s, src.ID, values)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "application/json") || w.Header().Get("Location") != "" || w.Header().Get("X-Waim-Save") != "ok" {
		t.Fatalf("invalid autosave response %d %s", w.Code, w.Body.String())
	}
	var result sourceSaveResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Revision != src.Revision+1 || result.Name != "Renamed" || result.ScanInterval != 0 || result.LibrariesChanged || result.ScanSummary != s.catalog.For("en").T("sources.manualScan") {
		t.Fatalf("wrong saved response: %+v", result)
	}
	if strings.Contains(w.Body.String(), "private-original-key") {
		t.Fatal("autosave echoed credential")
	}
	saved, _ := s.cfg.Get().Source(src.ID)
	if saved.Jellyfin.APIKey != src.Jellyfin.APIKey || saved.Fingerprint() != src.Fingerprint() || saved.ScanInterval(60) != 0 {
		t.Fatal("autosave discarded credentials/snapshot identity or interval")
	}
	values.Set("revision", strconv.FormatInt(result.Revision, 10))
	values.Set("scan_interval", "30")
	w = autosaveSource(s, src.ID, values)
	if w.Code != http.StatusOK {
		t.Fatalf("next revision failed: %s", w.Body.String())
	}
}

func TestSourceAutosaveRejectsInvalidConflictingAndUnsafeEdits(t *testing.T) {
	s, src, values := autosaveFixture(t)
	values.Set("url", "https://new.example")
	w := autosaveSource(s, src.ID, values)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), s.catalog.For("en").T("sources.addressNeedsKey")) {
		t.Fatal("new address inherited old key")
	}
	values.Set("key", "private-new-key")
	w = autosaveSource(s, src.ID, values)
	var saved sourceSaveResult
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !saved.LibrariesChanged || strings.Contains(w.Body.String(), "private-new-key") {
		t.Fatal("URL/key change did not signal safe library reset")
	}
	source, _ := s.cfg.Get().Source(src.ID)
	if source.Jellyfin.APIKey != "private-new-key" || len(source.Libraries) != 0 {
		t.Fatal("URL autosave did not persist replacement key/reset libraries")
	}
	w = autosaveSource(s, src.ID, values)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), s.catalog.For("en").T("sources.sourceConflict")) {
		t.Fatal("stale revision was not rejected")
	}
	values.Set("revision", strconv.FormatInt(saved.Revision, 10))
	values.Set("scan_interval", "invalid")
	w = autosaveSource(s, src.ID, values)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), s.catalog.For("en").T("sources.invalidInterval")) {
		t.Fatal("invalid interval saved")
	}
	current, _ := s.cfg.Get().Source(src.ID)
	if current.Revision != saved.Revision {
		t.Fatal("failed save advanced revision")
	}
	req := httptest.NewRequest("POST", "/sources/"+src.ID, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Waim-Source-Autosave", "true")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatal("autosave lost CSRF protection")
	}
}

func TestConcurrentSourceAutosavesCannotSilentlyOverwrite(t *testing.T) {
	s, src, values := autosaveFixture(t)
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"First", "Second"} {
		form := url.Values{}
		for key, vals := range values {
			form[key] = append([]string(nil), vals...)
		}
		form.Set("name", name)
		wg.Add(1)
		go func() { defer wg.Done(); responses <- autosaveSource(s, src.ID, form) }()
	}
	wg.Wait()
	close(responses)
	ok, conflict := 0, 0
	for w := range responses {
		switch w.Code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected %d", w.Code)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal("concurrent edits were not revision guarded")
	}
}
