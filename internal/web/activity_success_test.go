package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
)

func TestActivitySuccessIsGreenOnlyAfterConfirmedCompletion(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		for _, job := range []activity.Job{activity.Scan, activity.Cache, activity.Suggestions} {
			for _, status := range []activity.Status{activity.Idle, activity.Running, activity.Waiting, activity.Completed, activity.Partial, activity.Failed, activity.Cancelled} {
				state := activity.State{Job: job, Status: status}
				views := BuildActivities([]activity.State{state}, false, time.Now())
				var html strings.Builder
				if err := ActivityPanel(catalog.For(locale), views).Render(context.Background(), &html); err != nil {
					t.Fatal(err)
				}
				wantOK := status == activity.Completed
				if strings.Contains(html.String(), "activity-success") != wantOK || strings.Contains(html.String(), `class="activity-status">OK</span>`) != wantOK {
					t.Fatalf("%s %s %s: incorrect success indication", locale, job, status)
				}
			}
		}
	}
	if view := (ActivityView{State: activity.State{Status: activity.Completed, Warnings: 1}}); view.Tone() == "activity-success" || view.StatusKey() == "activity.status.ok" {
		t.Fatal("inconsistent completed state with warnings must not display green OK")
	}
}
