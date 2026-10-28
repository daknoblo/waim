package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func TestSetupNoticeIsCentralAndLocalized(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(10)
	settings := s.cfg.Get()
	settings.TMDB.APIKey = ""
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		setTestLocale(t, s, locale)
		tr := s.catalog.For(locale)
		for _, path := range []string{"/", "/stats", "/collection", "/suggestions"} {
			t.Run(locale+path, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, req)
				html := w.Body.String()
				if w.Code != http.StatusOK {
					t.Fatalf("unexpected response %d: %s", w.Code, html)
				}
				notice := tr.T("sources.tmdbRequired")
				if strings.Count(html, notice) != 1 || !strings.Contains(html, `href="/settings?tab=metadata"`) || !strings.Contains(html, tr.T("setup.metadataAction")) {
					t.Fatal("expected one metadata setup notice linking to its settings tab")
				}
				if strings.Count(html, `data-setup-category=`) != 2 || !strings.Contains(html, `data-setup-category="sources.title"`) || !strings.Contains(html, `href="/settings?tab=media"`) || !strings.Contains(html, tr.T("setup.mediaMissing")) {
					t.Fatal("fresh installation must also show the media-sources setup category")
				}
				for _, unwanted := range []string{tr.T("common.stateUnconfigured"), tr.T("suggestions.notConfigured"), tr.T("dashboard.lastError"), "tmdb api key is not configured"} {
					if strings.Contains(html, unwanted) {
						t.Fatalf("duplicate or erroneous setup message: %s", unwanted)
					}
				}
			})
		}
		status := s.statusView(context.Background(), tr)
		if !status.SetupRequired || status.StateLabel != tr.T("dashboard.state.setup") {
			t.Fatalf("incorrect setup status: %+v", status)
		}
		for _, path := range []string{"/partials/status", "/partials/findings"} {
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			html := w.Body.String()
			if strings.Contains(html, "tmdb api key is not configured") || strings.Contains(html, "common.stateUnconfigured") {
				t.Fatal("polling reintroduced setup error")
			}
			if path == "/partials/status" && !strings.Contains(html, " disabled") {
				t.Fatal("manual scan should be disabled before setup")
			}
		}
	}
	settings.TMDB.APIKey = "fixture"
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	notices := s.layout(req, web.NavDashboard).SetupNotices
	if len(notices) != 1 || notices[0].CategoryKey != "sources.title" {
		t.Fatal("only the media-sources notice should remain after configuring TMDB")
	}
	status := s.statusView(context.Background(), s.catalog.For("en"))
	if status.SetupRequired {
		t.Fatal("manual scan did not become available after setup")
	}
}

func TestSetupNoticeReadinessAndVirtualAlternative(t *testing.T) {
	source := config.Source{
		ID: "one", Type: media.Jellyfin, Name: "Server", Enabled: true,
		Jellyfin:  config.JellyfinSettings{URL: "https://fixture.invalid", APIKey: "fixture"},
		Libraries: []config.Library{{ID: "movies", Name: "Movies", Enabled: true}},
	}
	for _, tc := range []struct {
		name    string
		mutate  func(*config.Source)
		entries int
		message string
	}{
		{"ready source", func(*config.Source) {}, 0, ""},
		{"disabled source", func(s *config.Source) { s.Enabled = false }, 0, "setup.mediaMissing"},
		{"missing address", func(s *config.Source) { s.Jellyfin.URL = "" }, 0, "setup.mediaConnection"},
		{"missing key", func(s *config.Source) { s.Jellyfin.APIKey = "" }, 0, "setup.mediaConnection"},
		{"unreadable key", func(s *config.Source) { s.KeyUnreadable = true }, 0, "setup.mediaConnection"},
		{"libraries not fetched", func(s *config.Source) { s.Libraries = nil }, 0, "setup.mediaLibraries"},
		{"libraries not selected", func(s *config.Source) { s.Libraries[0].Enabled = false }, 0, "setup.mediaLibraries"},
		{"virtual instead", func(s *config.Source) { s.Enabled = false }, 1, ""},
		{"unknown virtual storage", func(s *config.Source) { s.Enabled = false }, -1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := config.Defaults()
			settings.TMDB.APIKey = "fixture"
			settings.Sources = append(settings.Sources, source)
			settings = settings.Clone()
			tc.mutate(&settings.Sources[1])
			notices := setupNotices(settings, tc.entries)
			if tc.message == "" {
				if len(notices) != 0 {
					t.Fatalf("unexpected setup warning: %+v", notices)
				}
			} else if len(notices) != 1 || notices[0].MessageKey != tc.message || !notices[0].VirtualAlternative {
				t.Fatalf("incorrect media setup instruction: %+v", notices)
			}
		})
	}
}

func TestVirtualCollectionDoesNotRequireMediaServer(t *testing.T) {
	s := featureServer(t)
	if err := s.store.MutateVirtual(context.Background(), store.VirtualEntry{Type: media.Movie, TMDBID: 42, Title: "Virtual film"}, false); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/collection", nil)
	if notices := s.layout(req, "collection").SetupNotices; len(notices) != 0 {
		t.Fatalf("virtual-only use incorrectly requires a media server: %+v", notices)
	}
	settings := s.cfg.Get()
	settings.TMDB.APIKey = ""
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	notices := s.layout(req, "collection").SetupNotices
	if len(notices) != 1 || notices[0].CategoryKey != "settings.tab.metadata" {
		t.Fatal("virtual-only use should only request missing metadata credentials")
	}
}

func TestStatusCardStillDisplaysRealErrors(t *testing.T) {
	s := featureServer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.render(w, req, web.StatusCard(s.catalog.For("en"), web.StatusView{LastError: "real database failure"}))
	if !strings.Contains(w.Body.String(), "real database failure") {
		t.Fatal("real scan errors must remain visible")
	}
}
