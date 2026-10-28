package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
)

func TestDiagnosticWarningsSurviveFreshRuntimeAndUnfinishedScanIsVisible(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PublishScan(ctx, id, nil, nil, nil, nil, store.RunMetadata{Basis: "owned-v1", SourcesToken: s.cfg.Get().SourcesToken(), Warnings: []string{"Unresolved title: Persisted after restart"}}); err != nil {
		t.Fatal(err)
	}
	cfgPath, dbPath := s.cfg.Path(), s.store.Path()
	s.suggest.Close()
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Dir(cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sug := suggest.New(cfg, st, s.log)
	t.Cleanup(func() { sug.Close(); _ = st.Close() })
	restarted := New(cfg, st, scheduler.New(cfg, st, s.log), sug, logbuf.New(10), s.catalog, s.log, nil, activity.New())
	html := diagRequest(restarted, "/partials/diagnostics", "en", "").Body.String()
	if !strings.Contains(html, "Persisted after restart") || !strings.Contains(html, `data-reason="unresolved"`) {
		t.Fatal("new runtime lost stored scan warnings")
	}
	if _, err := st.StartScanRun(ctx); err != nil {
		t.Fatal(err)
	}
	html = diagRequest(restarted, "/partials/diagnostics", "en", "").Body.String()
	if !strings.Contains(html, `data-reason="unfinishedScan"`) {
		t.Fatal("unfinished persisted scan was presented as clean")
	}
	if _, err := st.StartScanRun(ctx); err != nil {
		t.Fatal(err)
	}
	restarted.activities.Start(activity.Scan)
	html = diagRequest(restarted, "/partials/diagnostics", "en", "").Body.String()
	if !strings.Contains(html, `data-reason="unfinishedScan"`) || !strings.Contains(html, s.catalog.For("en").T("diagnostics.retrying")) {
		t.Fatal("retry hid the earlier unfinished scan before completion")
	}
}

func TestDiagnosticsBoundedViewKeepsErrorsAheadOfWarnings(t *testing.T) {
	s := featureServer(t)
	s.activities = activity.New()
	run := s.activities.Start(activity.Scan)
	for i := 0; i < activity.MaxDiagnostics+1; i++ {
		run.Report(activity.Diagnostic{Key: fmt.Sprint(i), Reason: activity.Unresolved, Severity: activity.Skip, Current: fmt.Sprintf("Title %d", i)})
	}
	run.Finish(context.Background(), activity.Completed, 101)
	cache := s.activities.Start(activity.Cache)
	cache.Report(activity.Diagnostic{Reason: activity.CacheUnavailable, Severity: activity.Error, Query: "/movie/99"})
	cache.Finish(context.Background(), activity.Failed, 0)
	data := s.diagnostics(context.Background())
	if !data.Truncated || len(data.Items) != activity.MaxDiagnostics || data.Items[0].Reason != activity.CacheUnavailable || data.Severity != activity.Error {
		t.Fatal("bounded diagnostic view hid the current error")
	}
	html := diagRequest(s, "/partials/diagnostics", "de", "").Body.String()
	if !strings.Contains(html, s.catalog.For("de").T("diagnostics.truncated")) {
		t.Fatal("truncation not explained")
	}
}
