package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daknoblo/waim/internal/config"
)

func TestAddSourceDiscoversLibrariesAndPreservesSelectionOnRefresh(t *testing.T) {
	s := featureServer(t)
	var calls atomic.Int32
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/Library/MediaFolders" || r.Header.Get("X-Emby-Token") != "fixture-key" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"Items":[{"Id":"movies","Name":"Films","CollectionType":"movies"},{"Id":"series","Name":"Shows","CollectionType":"tvshows"}]}`))
	}))
	defer jf.Close()
	form := url.Values{"name": {"New server"}, "url": {jf.URL}, "key": {"fixture-key"}}
	req := httptest.NewRequest("POST", "/sources", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || calls.Load() != 1 {
		t.Fatalf("discovery not performed on add: status=%d calls=%d", w.Code, calls.Load())
	}
	src := s.cfg.Get().Sources[1]
	if len(src.Libraries) != 2 || src.Libraries[0].ID != "movies" || src.Libraries[0].Name != "Films" || src.Libraries[1].Type != "tvshows" {
		t.Fatalf("libraries not persisted: %+v", src.Libraries)
	}
	for _, lib := range src.Libraries {
		if lib.Enabled {
			t.Fatal("discovery must not silently opt libraries into scans")
		}
	}
	if err := s.cfg.UpdateSource(src.ID, src.Revision, func(src *config.Source) error {
		src.Libraries[0].Enabled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	src, _ = s.cfg.Get().Source(src.ID)
	if err := s.refreshSourceLibraries(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := s.cfg.Get().Source(src.ID)
	if !refreshed.Libraries[0].Enabled || refreshed.Libraries[1].Enabled || calls.Load() != 2 {
		t.Fatal("refresh changed existing selections")
	}
	if err := s.refreshSourceLibraries(context.Background(), src); err == nil {
		t.Fatal("stale discovery revision overwrote a newer source")
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/settings?tab=media", nil))
	if !strings.Contains(w.Body.String(), `value="`+strconv.FormatInt(refreshed.Revision, 10)+`"`) || !strings.Contains(w.Body.String(), "Films") {
		t.Fatal("discovered libraries not visible in returned source form")
	}
}

func TestAddSourceDiscoveryFailureKeepsOneSavedSourceAndExplainsRetry(t *testing.T) {
	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			s := featureServer(t)
			jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private-upstream-response", http.StatusUnauthorized)
			}))
			defer jf.Close()
			form := url.Values{"name": {"Offline"}, "url": {jf.URL}, "key": {"fixture-key"}}
			req := httptest.NewRequest("POST", "/sources", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if len(s.cfg.Get().Sources) != 2 {
				t.Fatal("failed discovery lost or duplicated the source")
			}
			html := w.Body.String()
			if !strings.Contains(html, s.catalog.For(locale).T("sources.addedLibrariesFailed")) || strings.Contains(html, "private-upstream-response") {
				t.Fatal("missing safe, localized failure and retry explanation")
			}
			src := s.cfg.Get().Sources[1]
			if !strings.Contains(html, "/sources/"+src.ID+"/libraries") || len(src.Libraries) != 0 {
				t.Fatal("saved source cannot retry discovery")
			}
		})
	}
}
