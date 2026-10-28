package scheduler

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
)

func TestSchedulerLoopRoutesDueManualAndGlobalRequests(t *testing.T) {
	cfg := intervalConfig(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	s := New(cfg, st, nil)
	calls := map[string]int{}
	s.sourceFactory = func(src config.Source) (source.Adapter, error) {
		return intervalAdapter{src: src, calls: calls}, nil
	}
	s.tmdbFactory = func(config.Settings) scanner.TMDBAPI { return intervalTMDB{} }
	start := time.Now()
	s.sourceSchedules["a"] = sourceSchedule{minutes: 10, deadline: start.Add(-time.Second)}
	s.sourceSchedules["b"] = sourceSchedule{minutes: 30, deadline: start.Add(30 * time.Minute)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	waitForRun := func(wantID int64) {
		t.Helper()
		timeout := time.NewTimer(5 * time.Second)
		defer timeout.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			run, err := st.LatestSuccessfulRun(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if run != nil && run.ID >= wantID && !s.Running() {
				return
			}
			select {
			case <-timeout.C:
				t.Fatalf("scheduler did not complete run %d: %+v", wantID, s.Status())
			case <-tick.C:
			}
		}
	}
	waitForRun(1) // Only a's existing overdue deadline fires at startup.
	assertDeadline(t, s, "b", start.Add(30*time.Minute))
	if err := s.TriggerSource("manual"); err != nil {
		t.Fatal(err)
	}
	waitForRun(2)
	s.Recompute()
	waitForRun(3)
	s.Trigger()
	waitForRun(4)
	cancel()
	<-done
	if !reflect.DeepEqual(calls, map[string]int{"a": 2, "b": 1, "manual": 2}) {
		t.Fatalf("loop refreshed wrong sources or dropped a request: %v", calls)
	}
}
