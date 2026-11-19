package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
)

type suggestionCacheTransport func(*http.Request) (*http.Response, error)

func (f suggestionCacheTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func waitSuggestionRefresh(t *testing.T, service *suggest.Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for service.Running() {
		if time.Now().After(deadline) {
			t.Fatal("suggestion refresh did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSuggestionPageVisitsAndWatchEditsUseWarmCache(t *testing.T) {
	s := featureServer(t)
	settings := s.cfg.Get()
	settings.Scan.TMDBRateLimitRPS = 50
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	old := http.DefaultTransport
	http.DefaultTransport = suggestionCacheTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := `{"results":[{"id":900,"title":"Cached recommendation","name":"Cached recommendation"}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = old }()
	s.suggest.Generate()
	waitSuggestionRefresh(t, s.suggest)
	before := calls.Load()
	if err := s.store.MutateVirtual(context.Background(), store.VirtualEntry{Type: media.Series, TMDBID: 900, Title: "Cached recommendation"}, false); err != nil {
		t.Fatal(err)
	}
	s.suggest.Invalidate()
	for _, locale := range []string{"en", "de"} {
		for _, path := range []string{"/suggestions", "/partials/suggestions", "/suggestions"} {
			w := diagRequest(s, path, locale, "")
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Cached recommendation") || strings.Contains(w.Body.String(), `aria-busy="true"`) {
				t.Fatalf("%s did not immediately render warm suggestions", path)
			}
			if !strings.Contains(w.Body.String(), s.catalog.For(locale).T("sources.alreadyTracked")) {
				t.Fatal("cached results did not reflect current virtual membership")
			}
		}
	}
	if calls.Load() != before {
		t.Fatal("opening the page or adding to the collection regenerated suggestions")
	}
}

func TestNativeRefreshRedirectsAndPersistedErrorsReachDiagnostics(t *testing.T) {
	s := featureServer(t)
	s.activities = activity.New()
	s.suggest.Close()
	s.suggest = suggest.New(s.cfg, s.store, s.log, s.activities)
	settings := s.cfg.Get()
	settings.Scan.TMDBRateLimitRPS = 50
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	http.DefaultTransport = suggestionCacheTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = old }()
	req := httptest.NewRequest("POST", "/suggestions/generate", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/suggestions" {
		t.Fatal("non-HTMX refresh did not redirect to a full page")
	}
	waitSuggestionRefresh(t, s.suggest)
	s.suggest.Close()
	s.activities = activity.New()
	s.suggest = suggest.New(s.cfg, s.store, s.log, s.activities)
	for _, locale := range []string{"en", "de"} {
		html := diagRequest(s, "/partials/health-indicator", locale, "").Body.String()
		if !strings.Contains(html, `data-health="error"`) {
			t.Fatal("persisted suggestion failure disappeared after restart")
		}
		html = diagRequest(s, "/partials/diagnostics", locale, "").Body.String()
		if !strings.Contains(html, s.catalog.For(locale).T("diagnostics.reason.suggestionsUnavailable")) || strings.Contains(html, fmt.Sprint(http.StatusUnauthorized)) {
			t.Fatal("persisted diagnostics lost the localized safe reason")
		}
	}
}
