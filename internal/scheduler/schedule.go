package scheduler

import (
	"time"

	"github.com/daknoblo/waim/internal/media"
)

type scheduleTimer interface {
	Stop() bool
	Reset(time.Duration) bool
}

type scanSchedule struct {
	initialized bool
	deadline    time.Time
}

type sourceSchedule struct {
	minutes  int
	deadline time.Time
}

// SourceNextRun returns a copy of the source's next automatic scan deadline.
// Disabled, manual-only and unsupported sources have no deadline.
func (s *Scheduler) SourceNextRun(id string) *time.Time {
	settings := s.cfg.Get()
	src, exists := settings.Source(id)
	if !exists || !src.Enabled || src.Type != media.Jellyfin || src.ScanInterval(settings.Scan.IntervalMinutes) <= 0 || settings.TMDB.APIKey == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.sourceSchedules[id]
	if !ok || entry.minutes != src.ScanInterval(settings.Scan.IntervalMinutes) {
		return nil
	}
	deadline := entry.deadline
	return &deadline
}

// Only interval/eligibility changes and refreshed sources start a new interval.
// The timer always points at the minimum; unrelated edits leave it untouched.
func (s *Scheduler) syncSchedule(timer scheduleTimer, schedule *scanSchedule, now time.Time, refreshed []string) {
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		// Reset notifications may arrive before the exclusive lease finishes.
		// Retry admission locally without letting a consumed timer strand scans.
		timer.Stop()
		timer.Reset(100 * time.Millisecond)
		schedule.initialized = false
		return
	}
	defer release()
	settings := s.cfg.Get()
	s.mu.Lock()
	defer s.mu.Unlock()
	rearm := make(map[string]bool, len(refreshed))
	for _, id := range refreshed {
		rearm[id] = true
	}
	next := make(map[string]sourceSchedule)
	var deadline time.Time
	for _, src := range settings.Sources {
		minutes := src.ScanInterval(settings.Scan.IntervalMinutes)
		if !src.Enabled || src.Type != media.Jellyfin || minutes <= 0 || settings.TMDB.APIKey == "" {
			continue
		}
		entry, exists := s.sourceSchedules[src.ID]
		if !exists || entry.minutes != minutes || rearm[src.ID] {
			entry = sourceSchedule{minutes: minutes, deadline: now.Add(time.Duration(minutes) * time.Minute)}
		}
		next[src.ID] = entry
		if deadline.IsZero() || entry.deadline.Before(deadline) {
			deadline = entry.deadline
		}
	}
	s.sourceSchedules = next
	s.status.NextRun = nil
	if !deadline.IsZero() {
		s.status.NextRun = &deadline
	}
	if schedule.initialized && schedule.deadline.Equal(deadline) {
		return
	}
	timer.Stop()
	schedule.initialized, schedule.deadline = true, deadline
	if !deadline.IsZero() {
		// Keep overdue sources due, but avoid spinning if persistence fails
		// before any provider can be refreshed and therefore rearmed.
		timer.Reset(max(time.Second, deadline.Sub(now)))
	}
}

func (s *Scheduler) dueSources(now time.Time) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	for id, entry := range s.sourceSchedules {
		if !entry.deadline.After(now) {
			ids = append(ids, id)
		}
	}
	return ids
}
