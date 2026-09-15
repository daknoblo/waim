package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/logbuf"
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
				if strings.Count(html, notice) != 1 || !strings.Contains(html, `<a href="/settings?tab=metadata">`+notice+`</a>`) {
					t.Fatal("expected exactly one setup notice linking to settings")
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
	if s.layout(req, web.NavDashboard).SetupRequired {
		t.Fatal("setup notice did not clear after configuring TMDB")
	}
	status := s.statusView(context.Background(), s.catalog.For("en"))
	if status.SetupRequired {
		t.Fatal("manual scan did not become available after setup")
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
