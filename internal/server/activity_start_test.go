package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/refresher"
	"github.com/daknoblo/waim/internal/reset"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
)

func activityServer(t *testing.T) *Server {
	t.Helper()
	s := featureServer(t)
	s.activities = activity.New()
	s.logs = logbuf.New(10)
	s.sched = scheduler.New(s.cfg, s.store, s.log, s.activities)
	s.suggest.Close()
	s.suggest = suggest.New(s.cfg, s.store, s.log, s.activities)
	return s.WithRefresher(refresher.New(s.cfg, s.store, s.log, s.activities))
}

func startActivity(s *Server, job, token string, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/activity/"+job+"/start", strings.NewReader(url.Values{"_epoch": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func TestActivityStartButtonsQueueWorkersAndPreservePartialEpoch(t *testing.T) {
	s := activityServer(t)
	for _, job := range []string{"scan", "cache"} {
		w := startActivity(s, job, s.cfg.Gate().Token(), true)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<html") || strings.Contains(w.Body.String(), `id="log"`) {
			t.Fatalf("%s did not update only activity tiles: %d", job, w.Code)
		}
		if !strings.Contains(w.Body.String(), `name="_epoch" value="`+s.cfg.Gate().Token()+`"`) {
			t.Fatal("polled activity form lost reset/restart protection token")
		}
		for _, view := range s.activityViews(context.Background()) {
			if string(view.Job) == job && view.CanStart {
				t.Fatal("queued task still has an enabled Start button")
			}
		}
		if again := startActivity(s, job, s.cfg.Gate().Token(), true); again.Code != http.StatusConflict {
			t.Fatal("duplicate queued manual task not rejected")
		}
	}
}

func TestActivityStartSetupCSRFAndResetProtection(t *testing.T) {
	s := activityServer(t)
	for _, job := range []string{"scan", "cache", "suggestions"} {
		req := httptest.NewRequest("POST", "/activity/"+job+"/start", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatal("activity start accepted cross-site mutation")
		}
	}
	if w := startActivity(s, "unknown", s.cfg.Gate().Token(), false); w.Code != http.StatusBadRequest {
		t.Fatal("unknown manual task accepted")
	}
	old := s.cfg.Gate().Token()
	if err := reset.New(s.cfg, s.store).Apply(context.Background(), store.ResetMetadata, old, s.sched.ResetState); err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{"scan", "cache", "suggestions"} {
		if w := startActivity(s, job, old, true); w.Code != http.StatusConflict {
			t.Fatal("pre-reset activity form accepted")
		}
	}
	partial := diagRequest(s, "/partials/activity", "en", "")
	if !strings.Contains(partial.Body.String(), `name="_epoch" value="`+s.cfg.Gate().Token()+`"`) {
		t.Fatal("fresh polling response lacks current epoch")
	}
	settings := s.cfg.Get()
	settings.TMDB.APIKey = ""
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{"scan", "cache", "suggestions"} {
		if w := startActivity(s, job, s.cfg.Gate().Token(), true); w.Code != http.StatusConflict {
			t.Fatal("task started before metadata setup")
		}
	}
	for _, view := range s.activityViews(context.Background()) {
		if view.CanStart {
			t.Fatal("start button enabled without setup")
		}
	}
}

func TestActivitySuggestionStartReusesServiceAndNativeRedirect(t *testing.T) {
	s := activityServer(t)
	settings := s.cfg.Get()
	settings.Scan.TMDBRateLimitRPS = 50
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	http.DefaultTransport = suggestionCacheTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[]}`)), Request: r}, nil
	})
	defer func() { s.suggest.Close(); http.DefaultTransport = old }()
	w := startActivity(s, "suggestions", s.cfg.Gate().Token(), false)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/logs" {
		t.Fatal("native task start did not redirect back to activity log")
	}
	waitSuggestionRefresh(t, s.suggest)
	if result, _ := s.suggest.Result(); result == nil || result.GeneratedAt.IsZero() {
		t.Fatal("manual suggestion tile did not execute the existing generation service")
	}
	if s.activities.Snapshot()[2].Status != activity.Completed {
		t.Fatal("generation did not report progress/completion on shared tracker")
	}
}
