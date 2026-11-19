package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/store"
)

func TestMaintenanceDiscardsQueuedAndNewScans(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "fixture"
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	s := New(cfg, st, nil)
	s.Trigger()
	oldEpoch := <-s.triggerCh
	lease, err := cfg.Gate().TryReset()
	if err != nil {
		t.Fatal(err)
	}
	s.Trigger()
	s.Recompute()
	s.runScan(context.Background(), false)
	s.ResetState()
	lease.Finish(1, 0, false)
	s.runScan(context.Background(), false, oldEpoch)
	if run, err := st.LatestRun(context.Background()); err != nil || run != nil {
		t.Fatal("work arriving before/during reset refilled afterwards")
	}
	select {
	case <-s.triggerCh:
		t.Fatal("scan queued during reset")
	default:
	}
	select {
	case <-s.recomputeCh:
		t.Fatal("recompute queued during reset")
	default:
	}
}
