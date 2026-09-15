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

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/tmdbcache"
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

	mu          sync.RWMutex
	status      Status
	triggerCh   chan int64
	recomputeCh chan int64
	running     atomic.Bool
	progress    *progressState
	tmdbFactory func(config.Settings) scanner.TMDBAPI
	activities  *activity.Tracker
}

// New creates a Scheduler.
func New(cfg *config.Manager, st *store.Store, log *slog.Logger, activities ...*activity.Tracker) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	s := &Scheduler{
		cfg:         cfg,
		store:       st,
		log:         log,
		status:      Status{State: StateIdle},
		triggerCh:   make(chan int64, 1),
		recomputeCh: make(chan int64, 1),
		progress:    &progressState{},
	}
	if len(activities) > 0 {
		s.activities = activities[0]
	}
	return s
}

// Progress is a live snapshot of an in-flight scan.
type Progress struct {
	Current   string                 `json:"current"`
	StartedAt time.Time              `json:"startedAt"`
	Libraries []store.LibrarySummary `json:"libraries"`
}

// progressState tracks live scan progress and implements scanner.Reporter.
type progressState struct {
	mu        sync.Mutex
	current   string
	startedAt time.Time
	order     []string
	libs      map[string]*store.LibrarySummary
}

func (p *progressState) reset(started time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = ""
	p.startedAt = started
	p.order = nil
	p.libs = map[string]*store.LibrarySummary{}
}

// SetCurrent implements scanner.Reporter.
func (p *progressState) SetCurrent(name string) {
	p.mu.Lock()
	p.current = name
	p.mu.Unlock()
}

// LibraryStart implements scanner.Reporter.
func (p *progressState) LibraryStart(id, name string, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.libs[id]; !ok {
		p.order = append(p.order, id)
	}
	p.libs[id] = &store.LibrarySummary{ID: id, Name: name, Total: total}
}

// ItemDone implements scanner.Reporter.
func (p *progressState) ItemDone(libID string, missing int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l := p.libs[libID]; l != nil {
		l.Scanned++
		l.Missing += missing
	}
}

func (p *progressState) snapshot() Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := Progress{Current: p.current, StartedAt: p.startedAt}
	for _, id := range p.order {
		if l := p.libs[id]; l != nil {
			out.Libraries = append(out.Libraries, *l)
		}
	}
	return out
}

// Progress returns a snapshot of the current scan progress.
func (s *Scheduler) Progress() Progress {
	return s.progress.snapshot()
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
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		s.log.Warn("scan request rejected during maintenance")
		return
	}
	defer release()
	select {
	case s.triggerCh <- s.cfg.Gate().Epoch():
	default:
	}
}

// Recompute coalesces edits without contacting a media server.
func (s *Scheduler) Recompute() {
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		s.log.Warn("recompute request rejected during maintenance")
		return
	}
	defer release()
	select {
	case s.recomputeCh <- s.cfg.Gate().Epoch():
	default:
	}
}

// Run starts the scheduler loop and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.restoreStatus(ctx)
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
		case epoch := <-s.triggerCh:
			s.runScan(ctx, true, epoch)
			s.resetTimer(timer)
		case epoch := <-s.recomputeCh:
			s.runScan(ctx, false, epoch)
		case due := <-timer.C:
			release, err := s.cfg.Gate().EnterScheduled(due)
			if err != nil {
				s.log.Info("scheduled scan skipped after or during maintenance")
			} else {
				if settings := s.cfg.Get(); settings.Scan.IntervalMinutes > 0 && settings.TMDB.APIKey != "" {
					s.runScan(ctx, true)
				}
				release()
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
func (s *Scheduler) runScan(ctx context.Context, refresh bool, epochs ...int64) {
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		s.log.Warn("scan skipped during maintenance")
		return
	}
	defer release()
	if len(epochs) > 0 && epochs[0] != s.cfg.Gate().Epoch() {
		s.log.Info("queued scan discarded after reset")
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	run := s.activities.Start(activity.Scan)
	ctx = activity.WithRun(ctx, run)
	outcome, warnings := activity.Failed, 0
	defer func() { run.Finish(ctx, outcome, warnings) }()
	run.Phase(activity.Inventory, -1)
	if refresh {
		run.Mode(activity.SourceRefresh)
	} else {
		run.Mode(activity.Recompute)
	}
	settings := s.cfg.Get()
	if settings.TMDB.APIKey == "" {
		outcome = activity.Waiting
		s.setStatus(func(st *Status) {
			st.State = StateIdle
			st.NextRun = nil
		})
		s.log.Info("scan waiting for setup", "reason", "tmdb api key is not configured")
		return
	}

	started := time.Now()
	runID, err := s.store.StartScanRun(ctx)
	if err != nil {
		s.setStatus(func(st *Status) { st.State = StateIdle; st.LastError = err.Error() })
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

	s.progress.reset(started)
	catalog, scanErr := source.Catalog(ctx, s.store, settings, refresh, nil)
	var td scanner.TMDBAPI = tmdb.New(settings.TMDB.APIKey, settings.TMDB.Language, settings.TMDB.Region, settings.Scan.TMDBRateLimitRPS).
		WithCache(tmdbcache.New(s.store))
	if s.tmdbFactory != nil {
		td = s.tmdbFactory(settings)
	}
	if scanErr == nil {
		catalog, scanErr = source.ResolveSavedCatalog(ctx, s.store, settings, catalog, td)
	}
	sc := scanner.New(catalog, td, settings, s.log)
	sc.SetReporter(s.progress)

	var result scanner.Result
	if scanErr == nil {
		result, scanErr = sc.Run(ctx)
	}
	mode := "recompute"
	if refresh {
		mode = "refresh"
	}
	if scanErr == nil {
		run.Phase(activity.Persistence, -1)
		scanErr = s.cfg.WithSourcesToken(settings.SourcesToken(), func() error {
			return s.store.PublishScan(ctx, runID, result.Findings, result.Libraries, result.Media, result.Upcoming, store.RunMetadata{SourcesToken: settings.SourcesToken(), Basis: "owned-v1", Mode: mode, Revision: catalog.Revision, Warnings: result.Warnings})
		})
	}
	if errors.Is(scanErr, store.ErrCatalogChanged) || errors.Is(scanErr, config.ErrSourcesChanged) {
		outcome = activity.Cancelled
		s.Recompute()
	}
	finished := time.Now()

	if scanErr != nil {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		finishErr := s.store.FinishScanRun(finishCtx, runID, store.StatusError, scanErr.Error(),
			result.LibrariesScanned, result.ItemsScanned, 0, result.Libraries, result.Media, result.Upcoming)
		cancel()
		if finishErr != nil {
			s.log.Error("failed to persist scan failure", "runId", runID, "err", finishErr)
		}
		s.setStatus(func(st *Status) {
			st.State = StateIdle
			st.LastFinished = &finished
			st.LastError = scanErr.Error()
		})
		s.log.Error("scan failed", "runId", runID, "err", scanErr)
		return
	}

	if err := s.store.PruneRuns(ctx, 20); err != nil {
		warnings++
		s.log.Error("failed to prune scan history", "err", err)
	}
	warnings += len(result.Warnings)
	outcome = activity.Completed

	s.setStatus(func(st *Status) {
		st.State = StateIdle
		st.LastFinished = &finished
		st.LastMissing = len(result.Findings)
		st.LastError = ""
	})
	s.log.Info("scan finished", "runId", runID, "libraries", result.LibrariesScanned,
		"items", result.ItemsScanned, "missing", len(result.Findings), "upcoming", len(result.Upcoming))
}

func (s *Scheduler) setStatus(mut func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mut(&s.status)
}

// restoreStatus seeds the in-memory status from the most recent persisted run so
// the dashboard still shows the last scan date after a restart.
func (s *Scheduler) restoreStatus(ctx context.Context) {
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		s.log.Info("status restoration skipped during maintenance")
		return
	}
	defer release()
	run, err := s.store.LatestSuccessfulRun(ctx)
	if err != nil || run == nil {
		return
	}
	s.setStatus(func(st *Status) {
		st.LastRunID = run.ID
		started := run.StartedAt
		st.LastStarted = &started
		if run.FinishedAt != nil {
			finished := *run.FinishedAt
			st.LastFinished = &finished
		}
		st.LastMissing = run.MissingCount
	})
}
