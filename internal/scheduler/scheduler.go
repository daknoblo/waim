// Package scheduler orchestrates scans: it runs them on startup, on a periodic
// interval and on demand, while exposing the current status for the UI.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
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

	mu              sync.RWMutex
	status          Status
	triggerCh       chan int64
	recomputeCh     chan int64
	scheduleCh      chan struct{}
	resetScheduleCh chan struct{}
	sourceTriggerCh chan struct{}
	sourceRequests  map[string]int64
	sourceSchedules map[string]sourceSchedule
	running         atomic.Bool
	progress        *progressState
	tmdbFactory     func(config.Settings) scanner.TMDBAPI
	sourceFactory   source.Factory
	activities      *activity.Tracker
}

// New creates a Scheduler.
func New(cfg *config.Manager, st *store.Store, log *slog.Logger, activities ...*activity.Tracker) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	s := &Scheduler{
		cfg:             cfg,
		store:           st,
		log:             log,
		status:          Status{State: StateIdle},
		triggerCh:       make(chan int64, 1),
		recomputeCh:     make(chan int64, 1),
		scheduleCh:      make(chan struct{}, 1),
		resetScheduleCh: make(chan struct{}, 1),
		sourceTriggerCh: make(chan struct{}, 1),
		sourceRequests:  make(map[string]int64),
		sourceSchedules: make(map[string]sourceSchedule),
		progress:        &progressState{},
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

// TriggerSource queues a manual refresh of one enabled, supported real source.
// Validation is local and happens before any provider or comparison work.
func (s *Scheduler) TriggerSource(id string) error {
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		return err
	}
	defer release()
	src, ok := s.cfg.Get().Source(id)
	if !ok {
		return fmt.Errorf("source not found")
	}
	if src.Type != media.Jellyfin {
		return fmt.Errorf("unsupported source type")
	}
	if !src.Enabled {
		return fmt.Errorf("source is disabled")
	}
	s.mu.Lock()
	s.sourceRequests[id] = s.cfg.Gate().Epoch()
	s.mu.Unlock()
	select {
	case s.sourceTriggerCh <- struct{}{}:
	default:
	}
	return nil
}

func (s *Scheduler) takeSourceRequests() ([]string, int64) {
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		return nil, 0
	}
	defer release()
	epoch := s.cfg.Gate().Epoch()
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id, queuedEpoch := range s.sourceRequests {
		if queuedEpoch == epoch {
			ids = append(ids, id)
		}
	}
	clear(s.sourceRequests)
	return ids, epoch
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
	case s.scheduleCh <- struct{}{}:
	default:
	}
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
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	schedule := scanSchedule{}
	s.syncSchedule(timer, &schedule, time.Now(), nil)

	for {
		select {
		case <-ctx.Done():
			return
		case epoch := <-s.triggerCh:
			s.syncSchedule(timer, &schedule, time.Now(), nil)
			refreshed := s.runSources(ctx, true, nil, epoch)
			s.syncSchedule(timer, &schedule, time.Now(), refreshed)
		case <-s.sourceTriggerCh:
			ids, epoch := s.takeSourceRequests()
			s.syncSchedule(timer, &schedule, time.Now(), nil)
			var refreshed []string
			if len(ids) > 0 {
				refreshed = s.runSources(ctx, false, ids, epoch)
			}
			s.syncSchedule(timer, &schedule, time.Now(), refreshed)
		case epoch := <-s.recomputeCh:
			s.syncSchedule(timer, &schedule, time.Now(), nil)
			s.runScan(ctx, false, epoch)
			s.syncSchedule(timer, &schedule, time.Now(), nil)
		case <-s.scheduleCh:
			s.syncSchedule(timer, &schedule, time.Now(), nil)
		case <-s.resetScheduleCh:
			s.syncSchedule(timer, &schedule, time.Now(), nil)
		case due := <-timer.C:
			// A consumed timer must be rearmed even when maintenance rejects it.
			schedule.initialized = false
			release, err := s.cfg.Gate().EnterScheduled(due)
			var refreshed []string
			if err != nil {
				s.log.Info("scheduled scan skipped after or during maintenance")
			} else {
				s.syncSchedule(timer, &schedule, time.Now(), nil)
				if ids := s.dueSources(time.Now()); len(ids) > 0 {
					refreshed = s.runSources(ctx, false, ids)
				}
				release()
			}
			s.syncSchedule(timer, &schedule, time.Now(), refreshed)
		}
	}
}

// runScan executes a single scan, ignoring overlapping invocations.
func (s *Scheduler) runScan(ctx context.Context, refresh bool, epochs ...int64) {
	s.runSources(ctx, refresh, nil, epochs...)
}

func (s *Scheduler) runSources(ctx context.Context, fullRefresh bool, refreshIDs []string, epochs ...int64) (refreshed []string) {
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

	settings := s.cfg.Get()
	requested := make(map[string]bool, len(refreshIDs))
	for _, id := range refreshIDs {
		requested[id] = true
	}
	var selected []string
	for _, src := range settings.Sources {
		if src.Enabled && src.Type == media.Jellyfin && (fullRefresh || requested[src.ID]) {
			selected = append(selected, src.ID)
		}
	}
	if !fullRefresh && len(refreshIDs) > 0 && len(selected) == 0 {
		return nil
	}
	refresh := fullRefresh || len(selected) > 0
	run := s.activities.Start(activity.Scan)
	ctx = activity.WithRun(ctx, run)
	outcome, warnings := activity.Failed, 0
	defer func() {
		if outcome == activity.Failed && ctx.Err() == nil {
			run.Report(activity.Diagnostic{Reason: activity.ScanFailed, Severity: activity.Error})
		}
		run.Finish(ctx, outcome, warnings)
	}()
	run.Phase(activity.Inventory, -1)
	if refresh {
		run.Mode(activity.SourceRefresh)
	} else {
		run.Mode(activity.Recompute)
	}
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
	factory := s.sourceFactory
	if factory == nil {
		factory = source.New
	}
	catalog, scanErr := source.CatalogForSources(ctx, s.store, settings, selected, func(src config.Source) (source.Adapter, error) {
		refreshed = append(refreshed, src.ID)
		return factory(src)
	})
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
	if fullRefresh && len(refreshed) > 0 {
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
	return refreshed
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
