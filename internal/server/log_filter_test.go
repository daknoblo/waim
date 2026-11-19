package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func TestLogLevelFilterAppliesToPageAndPollingWithoutChangingBuffer(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(20)
	at := time.Date(2026, 10, 28, 12, 4, 5, 0, time.UTC)
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for _, level := range levels {
		s.logs.Add(logbuf.Entry{Time: at, Level: level, Message: "unique-" + level})
	}
	for _, locale := range []string{"en", "de"} {
		for _, selected := range append([]string{""}, levels...) {
			for _, path := range []string{"/logs", "/partials/log"} {
				w := diagRequest(s, path+"?level="+selected, locale, "")
				html := w.Body.String()
				if w.Code != http.StatusOK || !strings.Contains(html, web.FormatTimeValue(at)) {
					t.Fatal("filtered log must render dates and times")
				}
				for _, level := range levels {
					if strings.Contains(html, "unique-"+level) != (selected == "" || level == selected) {
						t.Fatalf("path %s level %s incorrectly includes/excludes %s", path, selected, level)
					}
				}
				if path == "/logs" && (!strings.Contains(html, `hx-include="#log-level"`) || strings.Count(html, `id="log-level"`) != 1) {
					t.Fatal("polling must use a stable, single filter outside the swapped log contents")
				}
			}
		}
		first := diagRequest(s, "/partials/log?level=ERROR", locale, "")
		s.logs.Add(logbuf.Entry{Time: at, Level: "INFO", Message: "new-info"})
		if got := diagRequest(s, "/partials/log?level=ERROR", locale, first.Header().Get(viewTagHeader)); got.Code != http.StatusNoContent {
			t.Fatal("unrelated log level should not change filtered polling response")
		}
		s.logs.Add(logbuf.Entry{Time: at, Level: "ERROR", Message: "new-error"})
		if got := diagRequest(s, "/partials/log?level=ERROR", locale, first.Header().Get(viewTagHeader)); got.Code != http.StatusOK {
			t.Fatal("new matching log entry should update filtered content")
		}
	}
	if len(s.logs.Entries()) != 8 {
		t.Fatal("filter must not remove entries from the shared buffer")
	}
	for _, path := range []string{"/logs?level=INVALID", "/partials/log?level=WARN%3Cscript%3E"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatal("invalid log filter must be explicitly rejected")
		}
	}
	s.logs = logbuf.New(1)
	if html := diagRequest(s, "/partials/log?level=WARN", "de", "").Body.String(); !strings.Contains(html, s.catalog.For("de").T("logs.noMatches")) {
		t.Fatal("empty filtered log needs a localized explanation")
	}
}

func TestDiagnosticDatesAreStableForLiveAndPersistedWarnings(t *testing.T) {
	s := featureServer(t)
	s.activities = activity.New()
	exact := time.Date(2026, 10, 27, 8, 4, 3, 0, time.UTC)
	run := s.activities.Start(activity.Cache)
	run.Report(activity.Diagnostic{Reason: activity.CacheUnavailable, Severity: activity.Error, OccurredAt: exact, Query: "/movie/42"})
	run.Finish(context.Background(), activity.Completed, 0)
	id, err := s.store.StartScanRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PublishScan(context.Background(), id, nil, nil, nil, nil, store.RunMetadata{
		Basis: "owned-v1", SourcesToken: s.cfg.Get().SourcesToken(), Warnings: []string{"Unresolved title: Legacy title"},
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := s.store.LatestSuccessfulRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		first := diagRequest(s, "/partials/diagnostics", locale, "")
		html := first.Body.String()
		for _, text := range []string{exact.Format(time.RFC3339Nano), web.FormatTimeValue(exact),
			persisted.FinishedAt.Format(time.RFC3339Nano), s.catalog.For(locale).T("diagnostics.recordedAt")} {
			if !strings.Contains(html, text) {
				t.Fatalf("diagnostic timestamp/fallback missing: %s", text)
			}
		}
		if got := diagRequest(s, "/partials/diagnostics", locale, first.Header().Get(viewTagHeader)); got.Code != http.StatusNoContent {
			t.Fatal("polling must not restamp diagnostics with the current time")
		}
	}
}
