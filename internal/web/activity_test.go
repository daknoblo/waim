package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
)

func TestActivityLastFinishedUsesEndNotStart(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, locale := range []string{"de", "en"} {
		tr := catalog.For(locale)
		for _, age := range []time.Duration{7 * time.Minute, 3 * time.Hour} {
			ended := now.Add(-age)
			state := activity.State{Job: activity.Cache, Status: activity.Completed, StartedAt: ended.Add(-2 * time.Hour), EndedAt: ended}
			var b strings.Builder
			if err := ActivityPanel(tr, BuildActivities([]activity.State{state}, true, now)).Render(t.Context(), &b); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(b.String(), tr.T("activity.lastFinished", FormatRelative(tr, &ended))) || !strings.Contains(b.String(), ended.Format(time.RFC3339)) {
				t.Fatalf("missing localized completion time (%s, %v)", locale, age)
			}
		}
	}
}

func TestActivitySeparatesProgressFromTitleDiagnostics(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	tr := catalog.For("de")
	tracker := activity.New()
	run := tracker.Start(activity.Cache)
	run.Phase(activity.Refresh, 7)
	run.Advance(true, false)
	run.Report(activity.Diagnostic{Reason: activity.CacheUnavailable, Severity: activity.Error, Query: "/tv/42/season/2?api_key=private", Current: "Example series"})
	var card, panel strings.Builder
	if err := ActivityPanel(tr, BuildActivities(tracker.Snapshot(), true, time.Now())).Render(t.Context(), &card); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(card.String(), "Aufgabe 2 von 7") || !strings.Contains(card.String(), "Jetzt starten") ||
		strings.Contains(card.String(), `data-diagnostics-link`) || strings.Contains(card.String(), "Example series") {
		t.Fatal("tile must show phase progress without diagnostic detail")
	}
	d := tracker.Snapshot()[1].Diagnostics[0]
	if err := DiagnosticsPanel(tr, DiagnosticsData{Items: []DiagnosticView{{Diagnostic: d}}}).Render(t.Context(), &panel); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(panel.String(), "Example series") || !strings.Contains(panel.String(), `href="https://www.themoviedb.org/tv/42/season/2"`) || strings.Contains(panel.String(), "private") {
		t.Fatal("diagnostic must identify the title without disclosing query parameters")
	}
}

func TestActivityModeUsesLocalizedLabelWithoutChangingJobIdentity(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		for _, mode := range []activity.Mode{activity.Recompute, activity.SourceRefresh} {
			tracker := activity.New()
			run := tracker.Start(activity.Scan)
			run.Mode(mode)
			run.Phase(activity.Inventory, -1)
			run.Phase(activity.Metadata, 10)
			views := BuildActivities(tracker.Snapshot(), true, time.Now())
			tr := catalog.For(locale)
			var html strings.Builder
			if err := ActivityPanel(tr, views).Render(context.Background(), &html); err != nil {
				t.Fatal(err)
			}
			key := "activity.job.scan"
			if mode == activity.Recompute {
				key = "activity.job.recompute"
			}
			if !strings.Contains(html.String(), `data-job="scan"`) || strings.Contains(html.String(), `data-job="recompute"`) || !strings.Contains(html.String(), `<h3 class="min-w-0 text-sm font-semibold text-slate-100">`+tr.T(key)+`</h3>`) {
				t.Fatalf("%s mode %s has incorrect job label/identity: %s", locale, mode, html.String())
			}
		}
	}
}
