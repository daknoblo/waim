package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/reset"
	"github.com/daknoblo/waim/internal/store"
)

func TestAsyncWatchAddReturnsSavedControlsWithoutRedirect(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	if err := s.store.TMDBCachePut(ctx, "/tv/42?language=en-US", []byte(`{"id":42,"name":"Verified <series>","first_air_date":"2020-01-01"}`)); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		setTestLocale(t, s, locale)
		form := url.Values{"kind": {media.Series}, "id": {"42"}, "title": {"Untrusted title"}, "_epoch": {s.cfg.Gate().Token()}}
		req := httptest.NewRequest("POST", "/collection/add", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Waim-Collection-Async", "true")
		req.Header.Set("Referer", "http://example.com/suggestions")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK || w.Header().Get("Location") != "" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("async add must not reload the page: %d %s", w.Code, w.Body.String())
		}
		var data struct {
			Kind                string
			ID, Count, Revision int64
			EntryHTML           string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data.Kind != media.Series || data.ID != 42 || data.Count != 1 || data.Revision != 1 {
			t.Fatalf("add was not idempotent or returned wrong membership: %+v", data)
		}
		for _, expected := range []string{`data-collection-entry="series:42"`, "Verified &lt;series&gt;", `action="/collection/remove"`, s.catalog.For(locale).T("sources.unwatch"), s.cfg.Gate().Token()} {
			if !strings.Contains(data.EntryHTML, expected) {
				t.Fatal("returned entry missing " + expected)
			}
		}
		if strings.Contains(data.EntryHTML, "<series>") || strings.Contains(data.EntryHTML, "Untrusted title") || strings.Contains(data.EntryHTML, "<html") {
			t.Fatal("async entry included unsafe/unverified content or full page")
		}
	}
}

func TestAsyncWatchAddRejectsInvalidStaleAndForeignRequests(t *testing.T) {
	s := featureServer(t)
	oldToken := s.cfg.Gate().Token()
	if err := reset.New(s.cfg, s.store).Apply(context.Background(), store.ResetMetadata, oldToken, s.sched.ResetState); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, id, epoch string
		foreign         bool
		status          int
	}{
		{media.Movie, "42", oldToken, false, http.StatusConflict},
		{media.Movie, "42", s.cfg.Gate().Token(), true, http.StatusForbidden},
		{"collection", "42", s.cfg.Gate().Token(), false, http.StatusBadRequest},
	} {
		req := httptest.NewRequest("POST", "/collection/add", strings.NewReader(url.Values{"kind": {tc.kind}, "id": {tc.id}, "_epoch": {tc.epoch}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Waim-Collection-Async", "true")
		if tc.foreign {
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("expected %d, got %d", tc.status, w.Code)
		}
	}
	entries, _, err := s.store.VirtualEntries(context.Background())
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected async request mutated collection")
	}
}
