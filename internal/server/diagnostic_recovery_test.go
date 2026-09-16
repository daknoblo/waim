package server

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
)

func TestNewCleanScanRetiresOldSuggestionIdentityWarnings(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	settings := s.cfg.Get()
	settings.Scan.TMDBRateLimitRPS = 50
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	src := auditSource(t, s, []media.Item{{ID: "audit/film", Type: media.Movie, Name: "Last Boy Scout (1991)"}})
	s.activities = activity.New()
	s.logs = logbuf.New(10)
	s.suggest.Close()
	s.suggest = suggest.New(s.cfg, s.store, s.log, s.activities)
	var calls atomic.Int64
	old := http.DefaultTransport
	http.DefaultTransport = suggestionCacheTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[{"id":901,"name":"Keep cached pick","title":"Keep cached pick"}]}`)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = old }()
	s.suggest.Generate()
	waitSuggestionRefresh(t, s.suggest)
	before, _ := s.suggest.Result()
	beforeCalls := calls.Load()
	if before == nil {
		t.Fatal("suggestion cache missing")
	}
	if html := diagRequest(s, "/partials/diagnostics", "de", "").Body.String(); !strings.Contains(html, "Last Boy Scout") {
		t.Fatal("initial unresolved-title warning missing")
	}
	snapshot, err := s.store.SourceSnapshot(ctx, src.ID, src.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Snapshot.Items[0].Name = "Corrected title"
	snapshot.Snapshot.Items[0].ProviderIDs = map[string]string{"Tmdb": "42"}
	if err := s.store.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), snapshot.Snapshot, ""); err != nil {
		t.Fatal(err)
	}
	catalog, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.New(catalog, &collectionTMDB{}, s.cfg.Get(), nil).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	auditPublish(t, s, catalog, result)
	assertResolvedSuggestion(t, s)
	after, _ := s.suggest.Result()
	if before != after || calls.Load() != beforeCalls || s.suggest.NeedsRefresh(ctx) {
		t.Fatal("reconciling diagnostics regenerated, replaced or invalidated cached suggestions")
	}
	if raw := s.activities.Snapshot()[2]; raw.Status != activity.Partial || len(raw.Diagnostics) != 1 {
		t.Fatal("reconciliation mutated historical activity instead of its current presentation")
	}
	// Persisted suggestion diagnostics must use their original attempt time,
	// not startup time, when compared with the newer scan after a real reopen.
	s.suggest.Close()
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Dir(s.cfg.Path()))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(s.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	tracker := activity.New()
	service := suggest.New(cfg, st, s.log, tracker)
	defer func() { service.Close(); _ = st.Close() }()
	restarted := New(cfg, st, scheduler.New(cfg, st, s.log, tracker), service, logbuf.New(10), s.catalog, s.log, nil, tracker)
	assertResolvedSuggestion(t, restarted)
	if got, _ := service.Result(); got == nil || !got.GeneratedAt.Equal(before.GeneratedAt) || calls.Load() != beforeCalls {
		t.Fatal("restart did not keep the original cached suggestion result")
	}
	failedID, err := st.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishScanRun(ctx, failedID, store.StatusError, "later unrelated outage", 0, 0, 0, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	service.Close()
	service = suggest.New(cfg, st, s.log, tracker)
	restarted.suggest = service
	for _, locale := range []string{"en", "de"} {
		html := diagRequest(restarted, "/partials/diagnostics", locale, "").Body.String()
		if strings.Contains(html, "Last Boy Scout") || !strings.Contains(html, `data-reason="scanFailed"`) {
			t.Fatal("later failure resurrected resolved warning or hid the new failure")
		}
		if html := diagRequest(restarted, "/partials/health-indicator", locale, "").Body.String(); !strings.Contains(html, `data-health="error"`) {
			t.Fatal("new independent scan failure must still turn the header red")
		}
	}
}

func assertResolvedSuggestion(t *testing.T, s *Server) {
	t.Helper()
	for _, locale := range []string{"en", "de"} {
		for _, path := range []string{"/logs", "/partials/diagnostics", "/partials/activity"} {
			w := diagRequest(s, path, locale, "")
			if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Last Boy Scout") || strings.Contains(w.Body.String(), `data-reason="unresolved"`) {
				t.Fatalf("%s kept a superseded suggestion identity warning", path)
			}
		}
		data := s.diagnostics(context.Background())
		if data.Severity != "" || len(data.Items) != 0 {
			t.Fatalf("resolved suggestion warning still affects header: %+v", data)
		}
		if html := diagRequest(s, "/partials/health-indicator", locale, "").Body.String(); html != "" {
			t.Fatal("resolved warning kept the header indicator visible")
		}
		for _, view := range s.activityViews(context.Background()) {
			if view.Job == activity.Suggestions && (view.Status != activity.Completed || view.Resolved != 1 || view.Warnings != 0 || view.Skipped != 0 || view.Tone() != "activity-success") {
				t.Fatalf("suggestion card still reports stale warnings: %+v", view)
			}
		}
		first := diagRequest(s, "/partials/activity", locale, "")
		if got := diagRequest(s, "/partials/activity", locale, first.Header().Get(viewTagHeader)); got.Code != http.StatusNoContent {
			t.Fatal("unchanged reconciled cards should return 204")
		}
	}
}

func TestIdentityReconciliationPreservesIndependentProblems(t *testing.T) {
	ended := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name   string
		change func(*activity.State)
		clean  time.Time
		want   activity.Status
		left   int
	}{
		{"resolved", func(*activity.State) {}, time.Now(), activity.Completed, 0},
		{"scan older", func(*activity.State) {}, ended.Add(-time.Second), activity.Partial, 1},
		{"no proof", func(*activity.State) {}, time.Time{}, activity.Partial, 1},
		{"same time", func(*activity.State) {}, ended, activity.Partial, 1},
		{"still running", func(s *activity.State) { s.Status = activity.Running }, time.Now(), activity.Running, 1},
		{"truncated", func(s *activity.State) { s.DiagnosticsTruncated = true }, time.Now(), activity.Partial, 1},
		{"other counters", func(s *activity.State) { s.Warnings = 2 }, time.Now(), activity.Partial, 0},
		{"incomplete work", func(s *activity.State) { s.Known, s.Total, s.Done = true, 3, 2 }, time.Now(), activity.Partial, 0},
		{"AI error", func(s *activity.State) {
			s.Diagnostics = append(s.Diagnostics, activity.Diagnostic{Reason: activity.AIUnavailable, Severity: activity.Error})
			s.Warnings++
		}, time.Now(), activity.Partial, 1},
		{"identity request failed", func(s *activity.State) {
			s.Diagnostics[0].Reason, s.Diagnostics[0].Severity = activity.IdentityUnavailable, activity.Error
		}, time.Now(), activity.Partial, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := activity.State{Job: activity.Suggestions, Status: activity.Partial, EndedAt: ended, Warnings: 1, Skipped: 1,
				Diagnostics: []activity.Diagnostic{{Reason: activity.Unresolved, Severity: activity.Skip, Current: "Original title"}}}
			tc.change(&state)
			got := reconcileIdentityWarnings(state, tc.clean)
			if got.Status != tc.want || len(got.Diagnostics) != tc.left || state.Diagnostics[0].Current != "Original title" {
				t.Fatalf("unexpected projection: %+v", got)
			}
			if tc.name == "AI error" && got.Severity() != activity.Error {
				t.Fatal("new scan incorrectly cleared an independent AI failure")
			}
		})
	}
}

func TestNewSuggestionAttemptDoesNotReviveResolvedPriorWarnings(t *testing.T) {
	old := activity.State{Job: activity.Suggestions, Status: activity.Partial, EndedAt: time.Now().Add(-time.Hour),
		Skipped: 1, Warnings: 1, Diagnostics: []activity.Diagnostic{{Reason: activity.Unresolved, Severity: activity.Skip}}}
	running := activity.State{Job: activity.Suggestions, Status: activity.Running, PreviousSeverity: activity.Warning,
		Diagnostics: []activity.Diagnostic{{Reason: activity.Unresolved, Severity: activity.Skip, Previous: true}}}
	projected := reconcilePreviousIdentityWarnings(running, old, time.Now())
	if projected.Severity() != "" || len(projected.Diagnostics) != 0 || projected.Status != activity.Running {
		t.Fatal("new attempt revived its previously resolved warning")
	}
	running.Diagnostics = append(running.Diagnostics, activity.Diagnostic{Reason: activity.Unresolved, Severity: activity.Skip, Current: "New unresolved title"})
	projected = reconcilePreviousIdentityWarnings(running, old, time.Now())
	if projected.Severity() != activity.Warning || len(projected.Diagnostics) != 1 || projected.Diagnostics[0].Current != "New unresolved title" {
		t.Fatal("new attempt's current warning was incorrectly cleared")
	}
}

func TestUnconfirmedScanCannotRetireSuggestionWarning(t *testing.T) {
	for _, problem := range []string{"unresolved", "pending", "failed", "stale", "unknown", "legacy"} {
		t.Run(problem, func(t *testing.T) {
			s := featureServer(t)
			s.activities = activity.New()
			r := s.activities.Start(activity.Suggestions)
			r.UnresolvedTitle("old", "Server", "Still unresolved")
			r.Advance(false, true)
			r.Finish(context.Background(), activity.Completed, 1)
			src := auditSource(t, s, []media.Item{})
			catalog, err := source.Catalog(context.Background(), s.store, s.cfg.Get(), false, nil)
			if err != nil {
				t.Fatal(err)
			}
			result := scanner.Result{}
			if problem == "unresolved" {
				result.Warnings = []string{"Unresolved title: Still unresolved"}
			}
			auditPublish(t, s, catalog, result)
			switch problem {
			case "pending":
				if err := s.store.MutateVirtual(context.Background(), store.VirtualEntry{Type: media.Movie, TMDBID: 99, Title: "New"}, false); err != nil {
					t.Fatal(err)
				}
			case "failed":
				id, err := s.store.StartScanRun(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if err := s.store.FinishScanRun(context.Background(), id, store.StatusError, "failed", 0, 0, 0, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
			case "stale":
				if err := s.store.SaveSourceAttempt(context.Background(), src.ID, src.Fingerprint(), nil, "offline"); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				if err := s.cfg.AddSource(config.Source{ID: "new", Type: media.Jellyfin, Name: "New", Enabled: true}); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				id, err := s.store.StartScanRun(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if err := s.store.PublishScan(context.Background(), id, nil, nil, nil, nil, store.RunMetadata{Revision: catalog.Revision, SourcesToken: s.cfg.Get().SourcesToken()}); err != nil {
					t.Fatal(err)
				}
			}
			if html := diagRequest(s, "/partials/diagnostics", "en", "").Body.String(); !strings.Contains(html, "Still unresolved") {
				t.Fatal("unconfirmed scan retired old identity warning")
			}
		})
	}
}
