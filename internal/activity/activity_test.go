package activity

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentJobsAndSnapshotIsolation(t *testing.T) {
	tracker := New()
	var wg sync.WaitGroup
	for _, job := range []Job{Scan, Cache, Suggestions, Sources} {
		run := tracker.Start(job)
		run.Phase(Metadata, 100)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				run.Current(fmt.Sprintf("Title %d", i))
				run.Advance(false, false)
				copy := tracker.Snapshot()
				copy[0].Current = "mutated snapshot"
			}
			run.Finish(context.Background(), Completed, 0)
		}()
	}
	wg.Wait()
	states := tracker.Snapshot()
	if len(states) != 4 {
		t.Fatalf("unbounded states: %d", len(states))
	}
	for _, state := range states {
		if state.Current != "Title 99" || state.Status != Completed || state.Done != 100 || state.Remaining() != 0 {
			t.Fatalf("job interference: %+v", state)
		}
	}
}

func TestStaleAndFinishedHandlesCannotOverwrite(t *testing.T) {
	tracker := New()
	old := tracker.Start(Scan)
	newer := tracker.Start(Scan)
	newer.Phase(Identity, 1)
	newer.Current("New title")
	old.Phase(Metadata, 200)
	old.Finish(context.Background(), Failed, 9)
	if got := tracker.Snapshot()[0]; got.Current != "New title" || got.Phase != Identity || got.Status != Running {
		t.Fatalf("old handle overwrote new run: %+v", got)
	}
	newer.Advance(false, false)
	newer.Finish(context.Background(), Completed, 0)
	newer.Current("late update")
	newer.Finish(context.Background(), Failed, 1)
	if got := tracker.Snapshot()[0]; got.Current != "New title" || got.Status != Completed {
		t.Fatalf("finished run changed: %+v", got)
	}
}

func TestKnownPhaseRemainingAndUnknownTotals(t *testing.T) {
	tracker := New()
	run := tracker.Start(Scan)
	run.Phase(Metadata, 3)
	run.Advance(false, false)
	s := tracker.Snapshot()[0]
	if !s.Known || s.Percent() != 33 || s.RemainingPercent() != 67 || s.Remaining() != 2 {
		t.Fatalf("incorrect current-phase share: %+v", s)
	}
	run.Advance(true, false)
	run.Advance(false, true)
	run.Advance(false, false)
	if got := tracker.Snapshot()[0]; got.Done != 3 || got.Remaining() != 0 {
		t.Fatalf("counter overflow: %+v", got)
	}
	run.Phase(Persistence, -1)
	s = tracker.Snapshot()[0]
	if s.Known || s.Done != 0 || s.Remaining() != 0 || s.Percent() != 0 || s.Failures != 1 || s.Skipped != 1 {
		t.Fatalf("unknown phase retained fake percentage or lost diagnostics: %+v", s)
	}
	run.Finish(context.Background(), Completed, 0)
	if tracker.Snapshot()[0].Status != Partial {
		t.Fatal("failed/skipped units became green")
	}
}

func TestCompletionOutcomesAndCancellation(t *testing.T) {
	for _, status := range []Status{Completed, Failed, Partial, Cancelled, Waiting} {
		t.Run(string(status), func(t *testing.T) {
			tracker := New()
			run := tracker.Start(Scan)
			run.Finish(context.Background(), status, 0)
			got := tracker.Snapshot()[0]
			if got.Status != status || got.EndedAt.IsZero() {
				t.Fatalf("bad terminal status: %+v", got)
			}
		})
	}
	for _, warnings := range []int{0, 3} {
		tracker := New()
		run := tracker.Start(Scan)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		run.Finish(ctx, Completed, warnings)
		if tracker.Snapshot()[0].Status != Cancelled {
			t.Fatal("cancelled work reported complete")
		}
	}
	tracker := New()
	run := tracker.Start(Scan)
	run.Phase(Metadata, 3)
	run.Advance(false, false)
	run.Finish(context.Background(), Completed, 0)
	if tracker.Snapshot()[0].Status != Partial {
		t.Fatal("incomplete known scope reported complete")
	}
	run = tracker.Start(Scan)
	run.Finish(context.Background(), Completed, 2)
	if tracker.Snapshot()[0].Status != Partial {
		t.Fatal("stale inventory warnings reported complete")
	}
}

func TestLabelsAreBoundedAndQueriesAreNeverRetained(t *testing.T) {
	tracker := New()
	run := tracker.Start(Cache)
	run.Current(strings.Repeat("ö", 300) + " https://user:password@example.com/?key=secret")
	run.Subject("Server https://example.com/?api_key=secret")
	run.Endpoint("/search/movie?query=private&api_key=secret")
	got := tracker.Snapshot()[1]
	if len([]rune(got.Current)) > 180 || strings.Contains(fmt.Sprint(got), "secret") || got.Query != "/search/movie" {
		t.Fatalf("unsafe activity label: %+v", got)
	}
	run.Endpoint("/movie/credential")
	if tracker.Snapshot()[1].Query != "" {
		t.Fatal("unrecognised path reflected")
	}
	var absent *Tracker
	FromContext(context.Background()).Phase(Metadata, 1)
	absent.Start(Scan).Finish(context.Background(), Completed, 0)
	if len(absent.Snapshot()) != 4 {
		t.Fatal("nil tracker must give stable idle jobs")
	}
	if tracker.Start(Job("unbounded-key")) != nil || len(tracker.Snapshot()) != 4 {
		t.Fatal("unknown jobs grew the tracker")
	}
}
