package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func diagRequest(s *Server, path, locale, tag string) *httptest.ResponseRecorder {
	settings := s.cfg.Get()
	if settings.Locale != locale {
		settings.Locale = locale
		if err := s.cfg.Save(settings); err != nil {
			panic(err)
		}
	}
	r := httptest.NewRequest("GET", path, nil)
	r.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
	r.Header.Set(viewTagHeader, tag)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestDiagnosticsLocalizeAndStayOutsideFastActivityPoll(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(10)
	s.activities = activity.New()
	run := s.activities.Start(activity.Scan)
	run.Phase(activity.Identity, 2)
	for _, name := range []string{"Lost movie", "Lost series"} {
		run.UnresolvedTitle(name, "Server", name)
		run.Advance(false, true)
	}
	run.Finish(context.Background(), activity.Completed, 1)
	for _, locale := range []string{"en", "de"} {
		html := diagRequest(s, "/logs", locale, "").Body.String()
		for _, text := range []string{"Lost movie", "Lost series", s.catalog.For(locale).T("diagnostics.reason.unresolved"), `id="diagnostics"`, `id="diagnostic-content"`, `data-diagnostics-link="true"`} {
			if !strings.Contains(html, text) {
				t.Fatalf("%s logs lack %s", locale, text)
			}
		}
		partial := diagRequest(s, "/partials/activity", locale, "").Body.String()
		if strings.Contains(partial, `id="diagnostics"`) || strings.Contains(partial, "Lost series") {
			t.Fatal("expanded diagnostics placed inside fast-changing activity swap")
		}
		first := diagRequest(s, "/partials/diagnostics", locale, "")
		run = s.activities.Start(activity.Cache)
		run.Phase(activity.Refresh, 10)
		run.Advance(false, false)
		if got := diagRequest(s, "/partials/diagnostics", locale, first.Header().Get(viewTagHeader)); got.Code != 204 {
			t.Fatal("ordinary progress re-rendered diagnostic details")
		}
		s.activities.Start(activity.Cache).Finish(context.Background(), activity.Completed, 0)
	}
}

func TestHealthIndicatorPriorityRecoveryAndCounterOnlyDetails(t *testing.T) {
	s := featureServer(t)
	s.activities = activity.New()
	s.logs = logbuf.New(10)
	s.logs.Add(logbuf.Entry{Level: "ERROR", Message: "old log alone must not latch the indicator"})
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); html != "" {
		t.Fatalf("clean indicator visible: %s", html)
	}
	warn := s.activities.Start(activity.Suggestions)
	warn.Warnings(2)
	warn.Finish(context.Background(), activity.Completed, 2)
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); !strings.Contains(html, `data-health="warning"`) {
		t.Fatal("warnings did not show amber")
	}
	if html := diagRequest(s, "/partials/diagnostics", "en", "").Body.String(); !strings.Contains(html, s.catalog.For("en").T("diagnostics.unavailable")) {
		t.Fatal("old counters invented explanatory details")
	}
	errRun := s.activities.Start(activity.Cache)
	errRun.Report(activity.Diagnostic{Reason: activity.CacheUnavailable, Severity: activity.Error, Query: "/search/movie?query=private&api_key=secret"})
	errRun.Advance(true, false)
	errRun.Finish(context.Background(), activity.Completed, 0)
	first := diagRequest(s, "/partials/health-indicator", "de", "")
	if !strings.Contains(first.Body.String(), `data-health="error"`) || !strings.Contains(first.Body.String(), s.catalog.For("de").T("diagnostics.indicator.error")) {
		t.Fatal("error precedence/accessibility missing")
	}
	if got := diagRequest(s, "/partials/health-indicator", "de", first.Header().Get(viewTagHeader)); got.Code != 204 {
		t.Fatal("unchanged health did not return 204")
	}
	errRun = s.activities.Start(activity.Cache)
	if html := diagRequest(s, "/partials/diagnostics", "en", "").Body.String(); !strings.Contains(html, s.catalog.For("en").T("diagnostics.retrying")) {
		t.Fatal("retry hid prior failure")
	}
	errRun.Finish(context.Background(), activity.Completed, 0)
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); !strings.Contains(html, `data-health="warning"`) {
		t.Fatal("successful retry failed to remove red")
	}
	s.activities.Start(activity.Suggestions).Finish(context.Background(), activity.Completed, 0)
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); html != "" {
		t.Fatal("successful retries did not hide indicator")
	}
}

func TestPersistedWarningsAndFailuresSurviveRestartWithoutGlobalBanners(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(10)
	ctx := context.Background()
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta := store.RunMetadata{Basis: "owned-v1", SourcesToken: s.cfg.Get().SourcesToken(), Warnings: []string{"Unresolved title: Saved title", "older arbitrary upstream body secret-token"}}
	if err := s.store.PublishScan(ctx, id, nil, nil, nil, nil, meta); err != nil {
		t.Fatal(err)
	}
	s.activities = activity.New()
	for _, locale := range []string{"en", "de"} {
		html := diagRequest(s, "/logs", locale, "").Body.String()
		if !strings.Contains(html, "Saved title") || !strings.Contains(html, s.catalog.For(locale).T("diagnostics.reason.legacyWarning")) || strings.Contains(html, "secret-token") {
			t.Fatal("persisted warnings lost or unsafe legacy content exposed")
		}
		for _, page := range []string{"/", "/stats", "/collection", "/settings?tab=media"} {
			html = diagRequest(s, page, locale, "").Body.String()
			if strings.Contains(html, s.catalog.For(locale).T("sources.incomplete")) && strings.Contains(html, `border-amber-500/40 bg-amber-500/10 px-3 py-2`) {
				t.Fatal("dashboard inventory warning banner retained")
			}
			if strings.Contains(html, `mb-4 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3`) {
				t.Fatal("global inventory warning banner retained")
			}
			if !strings.Contains(html, `id="health-indicator"`) {
				t.Fatal("header indicator absent")
			}
			if page == "/" && strings.Count(html, s.catalog.For(locale).T("sources.incomplete")) > 1 {
				t.Fatal("dashboard repeats the incomplete inventory message")
			}
		}
		for _, path := range []string{"/partials/status", "/partials/findings"} {
			html = diagRequest(s, path, locale, "").Body.String()
			limit := 1
			if path == "/partials/status" {
				limit = 0
			}
			if strings.Count(html, s.catalog.For(locale).T("sources.incomplete")) > limit {
				t.Fatalf("%s restored duplicate inventory notices", path)
			}
		}
	}
	failed, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.FinishScanRun(ctx, failed, store.StatusError, "private-error https://host/?key=secret", 0, 0, 0, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); !strings.Contains(html, `data-health="error"`) {
		t.Fatal("persisted failure did not show red")
	}
	if html := diagRequest(s, "/partials/diagnostics", "en", "").Body.String(); strings.Contains(html, "private-error") || strings.Contains(html, "key=secret") {
		t.Fatal("raw persisted error exposed")
	}
	next, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta.Warnings = nil
	if err := s.store.PublishScan(ctx, next, nil, nil, nil, nil, meta); err != nil {
		t.Fatal(err)
	}
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); html != "" {
		t.Fatal("later success did not clear persisted warnings/failure")
	}
}

func TestHealthPollingUsesCachedWarningsNotCatalogOrInventory(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	src := config.Source{ID: "source", Type: media.Jellyfin, Name: "Source", Enabled: true}
	if err := s.cfg.AddSource(src); err != nil {
		t.Fatal(err)
	}
	src, _ = s.cfg.Get().Source(src.ID)
	if err := s.store.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), &media.Snapshot{Warnings: []string{"Invalid episode range (only the start episode counted): Stored series"}}, ""); err != nil {
		t.Fatal(err)
	}
	first := diagRequest(s, "/partials/health-indicator", "en", "")
	if !strings.Contains(first.Body.String(), `data-health="warning"`) {
		t.Fatal("snapshot normalization warning not visible")
	}
	db, err := sql.Open("sqlite", s.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// These would fail if the poll loads catalog/virtual inventory or re-parses
	// the full source snapshot rather than using its unchanged summary token.
	if _, err := db.Exec(`DROP TABLE virtual_entries; UPDATE source_snapshots SET snapshot_json='not-json'`); err != nil {
		t.Fatal(err)
	}
	// A different run changes the aggregate version but must not force an
	// unchanged source's potentially large warning payload to be read again.
	if _, err := s.store.StartScanRun(ctx); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&logs, nil))
	for range 3 {
		w := diagRequest(s, "/partials/health-indicator", "en", first.Header().Get(viewTagHeader))
		if w.Code != 204 {
			t.Fatalf("lightweight unchanged poll failed: %d %s", w.Code, w.Body.String())
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("poll attempted catalog presentation: %s", logs.String())
	}
	if _, err := db.Exec(`DROP TABLE catalog_revision`); err != nil {
		t.Fatal(err)
	}
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); !strings.Contains(html, `data-health="error"`) {
		t.Fatal("storage read failure not red")
	}
}

func TestSetupDoesNotBecomeOperationalErrorAndResetClearsDetails(t *testing.T) {
	s := featureServer(t)
	s.activities = activity.New()
	s.logs = logbuf.New(10)
	settings := s.cfg.Get()
	settings.TMDB.APIKey = ""
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	for _, job := range []activity.Job{activity.Scan, activity.Cache, activity.Suggestions} {
		s.activities.Start(job).Finish(context.Background(), activity.Waiting, 0)
	}
	if html := diagRequest(s, "/partials/health-indicator", "en", "").Body.String(); html != "" {
		t.Fatal("setup waiting became an operational error")
	}
	run := s.activities.Start(activity.Scan)
	run.ReportLegacy("Unresolved title: Private title", "")
	run.Finish(context.Background(), activity.Completed, 1)
	_ = s.diagnostics(context.Background())
	if w := resetPost(s, resetForm(s, "factory"), false); w.Code != http.StatusSeeOther {
		t.Fatalf("reset failed: %s", w.Body.String())
	}
	data, err := json.Marshal(s.diagnostics(context.Background()))
	if err != nil || strings.Contains(string(data), "Private title") || s.diagnostics(context.Background()).Severity != "" {
		t.Fatal("full reset retained diagnostic state")
	}
}
