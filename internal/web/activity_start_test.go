package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
)

func TestActivityManualActionsAreLocalizedAndDisabledUnlessAllowed(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, allowed := range []bool{false, true} {
			views := BuildActivities(activity.New().Snapshot(), true, time.Now())
			for i := range views {
				views[i].CanStart = allowed
			}
			var b strings.Builder
			if err := ActivityPanel(tr, views).Render(WithMutationEpoch(context.Background(), "test-epoch"), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			for _, job := range []string{"scan", "cache", "suggestions"} {
				if !strings.Contains(html, `action="/activity/`+job+`/start"`) || !strings.Contains(html, tr.T("activity.startHint."+job)) {
					t.Fatal("missing manual start action/description for " + job)
				}
			}
			if strings.Count(html, `name="_epoch" value="test-epoch"`) != 3 || !strings.Contains(html, `hx-disabled-elt="find button"`) {
				t.Fatal("manual forms lost epoch or submit-disabling behavior")
			}
			if strings.Count(html, ` disabled`) != map[bool]int{false: 3, true: 0}[allowed] {
				t.Fatal("button enablement does not match worker availability")
			}
		}
	}
}
