package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/logbuf"
)

func TestActivityLogsAndIndependentPartialLocalized(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(10)
	s.activities = activity.New()
	scan := s.activities.Start(activity.Scan)
	scan.Phase(activity.Metadata, 10)
	scan.Current("Distinctive current title")
	scan.Advance(false, false)
	cache := s.activities.Start(activity.Cache)
	cache.Phase(activity.Refresh, 2)
	cache.Advance(true, false)
	suggestions := s.activities.Start(activity.Suggestions)
	suggestions.Phase(activity.AI, -1)
	for _, locale := range []string{"en", "de"} {
		setTestLocale(t, s, locale)
		tr := s.catalog.For(locale)
		for _, path := range []string{"/logs", "/partials/activity"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			html := w.Body.String()
			if w.Code != 200 || !strings.Contains(html, tr.T("activity.heading")) {
				t.Fatalf("%s %s: %d %s", locale, path, w.Code, html)
			}
			for _, text := range []string{
				tr.T("activity.job.scan"), tr.T("activity.job.cache"), tr.T("activity.job.suggestions"),
				tr.T("activity.currentPhase"), tr.T("activity.phase.ai"), "Distinctive current title",
				`aria-valuenow="10"`, `aria-valuenow="50"`, `aria-valuetext="` + tr.T("activity.indeterminate") + `"`,
				`data-job="scan"`, `data-job="cache"`, `data-job="suggestions"`,
			} {
				if !strings.Contains(html, text) {
					t.Errorf("%s %s lacks %q", locale, path, text)
				}
			}
			if strings.Count(html, `aria-valuenow=`) != 2 || strings.Contains(html, `style=`) {
				t.Fatal("indeterminate progress or strict CSP broken")
			}
			if strings.Contains(html, `aria-live=`) {
				t.Fatal("activity repeatedly announces history")
			}
			if path == "/logs" {
				if strings.Index(html, `id="activity"`) > strings.Index(html, `id="log"`) || !strings.Contains(html, `hx-get="/partials/activity"`) || !strings.Contains(html, `hx-trigger="every 2s"`) {
					t.Fatal("activity not independently polled above logs")
				}
			} else if strings.Contains(html, `id="log"`) || strings.Contains(html, "<html") {
				t.Fatal("activity partial contains whole page/log")
			}
		}
	}
	scan.Finish(context.Background(), activity.Completed, 0)
	cache.Finish(context.Background(), activity.Completed, 0)
	suggestions.Finish(context.Background(), activity.Completed, 0)
	request := func(path, tag string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set(viewTagHeader, tag)
		s.Handler().ServeHTTP(w, r)
		return w
	}
	first := request("/partials/activity", "")
	tag := first.Header().Get(viewTagHeader)
	if tag == "" || request("/partials/activity", tag).Code != http.StatusNoContent {
		t.Fatal("unchanged activity did not return 204")
	}
	logTag := request("/partials/log", "").Header().Get(viewTagHeader)
	s.activities.Start(activity.Scan).Phase(activity.Inventory, -1)
	if request("/partials/activity", tag).Code != http.StatusOK {
		t.Fatal("activity change did not change its fingerprint")
	}
	if request("/partials/log", logTag).Code != http.StatusNoContent {
		t.Fatal("activity update changed the log fingerprint")
	}
}

func TestActivityWaitingSetupAndIdleLocalization(t *testing.T) {
	s := featureServer(t)
	s.activities = activity.New()
	s.logs = logbuf.New(10)
	for _, configured := range []bool{false, true} {
		settings := s.cfg.Get()
		settings.TMDB.APIKey = ""
		if configured {
			settings.TMDB.APIKey = "fixture"
		}
		if err := s.cfg.Save(settings); err != nil {
			t.Fatal(err)
		}
		for _, locale := range []string{"en", "de"} {
			setTestLocale(t, s, locale)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/partials/activity", nil)
			r.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
			s.Handler().ServeHTTP(w, r)
			key := "activity.status.waiting"
			if configured {
				key = "activity.status.idle"
			}
			if strings.Count(w.Body.String(), s.catalog.For(locale).T(key)) != 3 || strings.Contains(w.Body.String(), "aria-valuenow") {
				t.Fatalf("bad %s idle/setup state: %s", locale, w.Body.String())
			}
			if strings.Contains(w.Body.String(), `href="/settings"`) {
				t.Fatal("activity repeated the global setup instruction")
			}
			if !configured {
				w = httptest.NewRecorder()
				r = httptest.NewRequest("GET", "/logs", nil)
				r.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
				s.Handler().ServeHTTP(w, r)
				if strings.Count(w.Body.String(), s.catalog.For(locale).T("sources.tmdbRequired")) != 1 {
					t.Fatal("logs must retain exactly one global TMDB setup instruction")
				}
			}
		}
	}
}
