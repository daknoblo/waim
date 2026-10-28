package activity

import (
	"context"
	"testing"
)

func TestRunModeSurvivesPhaseChangesButNotNewRuns(t *testing.T) {
	tracker := New()
	run := tracker.Start(Scan)
	run.Mode(Recompute)
	for _, phase := range []Phase{Inventory, Identity, Metadata, Persistence} {
		run.Phase(phase, -1)
		if state := tracker.Snapshot()[0]; state.Mode != Recompute || state.Job != Scan {
			t.Fatalf("phase cleared mode or changed logical job: %+v", state)
		}
	}
	run.Finish(context.Background(), Completed, 0)
	if tracker.Snapshot()[0].Mode != Recompute {
		t.Fatal("completion cleared recalculation mode")
	}
	next := tracker.Start(Scan)
	next.Mode(SourceRefresh)
	run.Mode(Recompute)
	if state := tracker.Snapshot()[0]; state.Mode != SourceRefresh || state.Job != Scan {
		t.Fatalf("refresh inherited stale recalculation mode: %+v", state)
	}
}
