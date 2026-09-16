package scheduler

import "time"

type scheduleTimer interface {
	Stop() bool
	Reset(time.Duration) bool
}

type scanSchedule struct {
	initialized bool
	minutes     int
	configured  bool
	deadline    time.Time
}

// The UI deadline and the actual timer are updated together. Ordinary
// recomputations leave both untouched; completed real scans start a new interval.
func (s *Scheduler) syncSchedule(timer scheduleTimer, schedule *scanSchedule, now time.Time, force bool) {
	settings := s.cfg.Get()
	minutes, configured := settings.Scan.IntervalMinutes, settings.TMDB.APIKey != ""
	if !force && schedule.initialized && schedule.minutes == minutes && schedule.configured == configured {
		return
	}
	// Go 1.25 timers cannot deliver a stale value after Stop/Reset returns.
	timer.Stop()
	schedule.initialized, schedule.minutes, schedule.configured = true, minutes, configured
	schedule.deadline = time.Time{}
	if minutes > 0 && configured {
		delay := time.Duration(minutes) * time.Minute
		schedule.deadline = now.Add(delay)
		timer.Reset(delay)
	}
	deadline := schedule.deadline
	s.setStatus(func(status *Status) {
		status.NextRun = nil
		if !deadline.IsZero() {
			status.NextRun = &deadline
		}
	})
}
