// Package scheduler orchestrates scans: it runs them on startup, on a periodic
// interval and on demand, while exposing the current status for the UI.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

// State values for the scheduler.
const (
	StateIdle    = "idle"
	StateRunning = "running"
)

// Status is a snapshot of the scheduler state for display.
type Status struct {
	State        string     `json:"state"`
	LastRunID    int64      `json:"lastRunId,omitempty"`
	LastStarted  *time.Time `json:"lastStarted,omitempty"`
	LastFinished *time.Time `json:"lastFinished,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
	LastMissing  int        `json:"lastMissing"`
	NextRun      *time.Time `json:"nextRun,omitempty"`
}

// Scheduler coordinates scan execution.
type Scheduler struct {
	cfg   *config.Manager
	store *store.Store
	log   *slog.Logger

	mu        sync.RWMutex
	status    Status
	triggerCh chan struct{}
	running   atomic.Bool
}

// New creates a Scheduler.
func New(cfg *config.Manager, st *store.Store, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		cfg:       cfg,
		store:     st,
		log:       log,
		status:    Status{State: StateIdle},
		triggerCh: make(chan struct{}, 1),
	}
}

// Status returns a copy of the current status.
func (s *Scheduler) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// Running reports whether a scan is currently executing.
func (s *Scheduler) Running() bool { return s.running.Load() }

// Trigger requests an immediate scan. It is non-blocking; if a scan is already
// queued or running, the request is coalesced.
func (s *Scheduler) Trigger() {
	select {
	case s.triggerCh <- struct{}{}:
	default:
	}
}

// Run starts the scheduler loop and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s.cfg.Get().Scan.RunOnStart {
		s.Trigger()
	}
	timer := time.NewTimer(s.nextInterval())
	defer timer.Stop()

	for {
		s.updateNextRun()
		select {
		case <-ctx.Done():
			return
		case <-s.triggerCh:
			s.runScan(ctx)
			s.resetTimer(timer)
		case <-timer.C:
			if s.cfg.Get().Scan.IntervalMinutes > 0 {
				s.runScan(ctx)
			}
			s.resetTimer(timer)
		}
	}
}

func (s *Scheduler) nextInterval() time.Duration {
	m := s.cfg.Get().Scan.IntervalMinutes
	if m <= 0 {
		return time.Hour // park; periodic runs are skipped when interval is 0
	}
	return time.Duration(m) * time.Minute
}

func (s *Scheduler) resetTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(s.nextInterval())
}

func (s *Scheduler) updateNextRun() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Get().Scan.IntervalMinutes > 0 {
		next := time.Now().Add(s.nextInterval())
		s.status.NextRun = &next
	} else {
		s.status.NextRun = nil
	}
}

// runScan executes a single scan, ignoring overlapping invocations.
func (s *Scheduler) runScan(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	settings := s.cfg.Get()
	if err := validateRunnable(settings, s.cfg.CipherEnabled()); err != nil {
		s.setStatus(func(st *Status) {
			st.State = StateIdle
			st.LastError = err.Error()
		})
		s.log.Warn("scan skipped", "reason", err)
		return
	}

	started := time.Now()
	runID, err := s.store.StartScanRun(ctx)
	if err != nil {
		s.log.Error("failed to start scan run", "err", err)
		return
	}
	s.setStatus(func(st *Status) {
		st.State = StateRunning
		st.LastRunID = runID
		st.LastStarted = &started
		st.LastError = ""
	})
	s.log.Info("scan started", "runId", runID)

	jf := jellyfin.New(settings.Jellyfin.URL, settings.Jellyfin.APIKey)
	td := tmdb.New(settings.TMDB.APIKey, settings.TMDB.Language, settings.TMDB.Region, settings.Scan.TMDBRateLimitRPS)
	sc := scanner.New(jf, td, settings, s.log)

	result, scanErr := sc.Run(ctx)
	finished := time.Now()

	if scanErr != nil {
		_ = s.store.FinishScanRun(ctx, runID, store.StatusError, scanErr.Error(),
			result.LibrariesScanned, result.ItemsScanned, 0)
		s.setStatus(func(st *Status) {
			st.State = StateIdle
			st.LastFinished = &finished
			st.LastError = scanErr.Error()
		})
		s.log.Error("scan failed", "runId", runID, "err", scanErr)
		return
	}

	if err := s.store.AddFindings(ctx, runID, result.Findings); err != nil {
		s.log.Error("failed to persist findings", "runId", runID, "err", err)
	}
	if err := s.store.FinishScanRun(ctx, runID, store.StatusSuccess, "",
		result.LibrariesScanned, result.ItemsScanned, len(result.Findings)); err != nil {
		s.log.Error("failed to finish scan run", "runId", runID, "err", err)
	}
	_ = s.store.PruneRuns(ctx, 20)

	s.setStatus(func(st *Status) {
		st.State = StateIdle
		st.LastFinished = &finished
		st.LastMissing = len(result.Findings)
		st.LastError = ""
	})
	s.log.Info("scan finished", "runId", runID, "libraries", result.LibrariesScanned,
		"items", result.ItemsScanned, "missing", len(result.Findings))
}

func (s *Scheduler) setStatus(mut func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mut(&s.status)
}

func validateRunnable(s config.Settings, cipherEnabled bool) error {
	if !cipherEnabled {
		return errors.New("encryption key not configured (set WAIM_MASTER_KEY)")
	}
	if s.Jellyfin.URL == "" || s.Jellyfin.APIKey == "" {
		return errors.New("jellyfin is not configured")
	}
	if s.TMDB.APIKey == "" {
		return errors.New("tmdb api key is not configured")
	}
	if len(s.EnabledLibraryIDs()) == 0 {
		return errors.New("no libraries selected for scanning")
	}
	return nil
}
