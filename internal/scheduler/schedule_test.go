package scheduler

import (
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
)

type recordedTimer struct {
	resets []time.Duration
	stops  int
}

func (t *recordedTimer) Stop() bool {
	t.stops++
	return true
}
func (t *recordedTimer) Reset(d time.Duration) bool {
	t.resets = append(t.resets, d)
	return true
}

func TestSchedulePreservesDeadlineAcrossRecomputationsAndRearmsChanges(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "fixture"
	settings.Scan.IntervalMinutes = 60
	settings.Sources = append(settings.Sources, config.Source{ID: "real", Name: "Real", Type: media.Jellyfin, Enabled: true})
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, nil, nil)
	timer := &recordedTimer{}
	schedule := scanSchedule{}
	start := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	s.syncSchedule(timer, &schedule, start, nil)
	want := start.Add(time.Hour)
	for _, elapsed := range []time.Duration{10 * time.Minute, 50 * time.Minute, 59 * time.Minute} {
		s.syncSchedule(timer, &schedule, start.Add(elapsed), nil)
		if s.Status().NextRun == nil || !s.Status().NextRun.Equal(want) || !schedule.deadline.Equal(want) || len(timer.resets) != 1 {
			t.Fatal("recomputation changed the displayed or actual scan deadline")
		}
	}
	minutes := 1
	settings.Sources[1].ScanIntervalMinutes = &minutes
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	changedAt := start.Add(50 * time.Minute)
	s.syncSchedule(timer, &schedule, changedAt, nil)
	if len(timer.resets) != 2 || timer.resets[1] != time.Minute || !s.Status().NextRun.Equal(changedAt.Add(time.Minute)) {
		t.Fatal("changed scan interval did not rearm and advertise the same timer")
	}
	finished := changedAt.Add(3 * time.Minute)
	s.syncSchedule(timer, &schedule, finished, []string{"real"})
	if len(timer.resets) != 3 || !s.Status().NextRun.Equal(finished.Add(time.Minute)) {
		t.Fatal("completed real scan did not start a fresh interval")
	}
	minutes = 0
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	s.syncSchedule(timer, &schedule, finished, nil)
	if !schedule.deadline.IsZero() || s.Status().NextRun != nil || len(timer.resets) != 3 {
		t.Fatal("disabled scheduling retained an armed deadline")
	}
	minutes = 60
	settings.TMDB.APIKey = ""
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	s.syncSchedule(timer, &schedule, finished, nil)
	if s.Status().NextRun != nil || len(timer.resets) != 3 {
		t.Fatal("unconfigured scheduler advertised a runnable scan")
	}
}

func TestResetAndRecomputeNotifyScheduleWithoutQueuingScans(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(cfg, nil, nil)
	s.Recompute()
	select {
	case <-s.scheduleCh:
	default:
		t.Fatal("settings recomputation did not notify scheduler")
	}
	lease, err := cfg.Gate().TryReset()
	if err != nil {
		t.Fatal(err)
	}
	s.ResetState()
	lease.Finish(1, 0, false)
	select {
	case <-s.resetScheduleCh:
	default:
		t.Fatal("reset did not request a new schedule")
	}
	select {
	case <-s.triggerCh:
		t.Fatal("reset unexpectedly queued a source scan")
	default:
	}
	select {
	case <-s.recomputeCh:
		t.Fatal("reset left a stale recomputation queued")
	default:
	}
}
