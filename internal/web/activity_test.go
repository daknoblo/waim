package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
)

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
