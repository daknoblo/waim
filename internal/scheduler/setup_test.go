package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/store"
)

func TestMissingTMDBWaitsForSetupWithoutRecordingFailure(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	s := New(cfg, st, nil)
	ctx := context.Background()
	for _, refresh := range []bool{true, false} {
		s.updateNextRun()
		s.runScan(ctx, refresh)
		status := s.Status()
		if status.LastError != "" || status.NextRun != nil || status.State != StateIdle || s.Running() {
			t.Fatalf("missing setup treated as a scan failure: %+v", status)
		}
	}
	if run, err := st.LatestRun(ctx); err != nil || run != nil {
		t.Fatalf("setup attempt created scan history: %+v %v", run, err)
	}
	s.setStatus(func(status *Status) { status.LastError = "real persistence failure" })
	s.runScan(ctx, true)
	if s.Status().LastError != "real persistence failure" {
		t.Fatal("waiting for setup must not erase a real previous failure")
	}

	settings := cfg.Get()
	settings.TMDB.APIKey = "fixture"
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	s.updateNextRun()
	if s.Status().NextRun == nil {
		t.Fatal("configured scheduler did not resume scheduling")
	}
}
