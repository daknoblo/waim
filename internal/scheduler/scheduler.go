// Package scheduler orchestrates scans: it runs them on startup, on a periodic
// interval and on demand, while exposing the current status for the UI.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/scanner"
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

	mu           sync.RWMutex
	status       Status
	triggerCh    chan struct{}
	forcePending atomic.Bool
	running      atomic.Bool
	progress     *progressState
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
		progress:  &progressState{},
	}
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
	select {
	case s.triggerCh <- struct{}{}:
	default:
	}
}

// TriggerForce requests an immediate full re-scan that bypasses the TMDB cache
// (all data is re-fetched fresh and rewritten). The force flag is sticky so it
// is not lost when triggers are coalesced.
func (s *Scheduler) TriggerForce() {
	s.forcePending.Store(true)
	s.Trigger()
}

// Run starts the scheduler loop and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.restoreStatus(ctx)
	cfg := s.cfg.Get().Scan
	// Manual mode (no interval): do a one-off startup scan only when enabled and
	// nothing has been scanned yet, so restarts don't re-scan.
	if cfg.IntervalMinutes <= 0 && cfg.RunOnStart && s.lastFinished() == nil {
		s.Trigger()
	}

	delay := s.startupDelay()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	s.armNextRun(delay)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.triggerCh:
			s.runScan(ctx, s.forcePending.Swap(false))
			s.armNextRun(s.resetTimer(timer))
		case <-timer.C:
			if s.cfg.Get().Scan.IntervalMinutes > 0 {
				s.runScan(ctx, false)
			}
			s.armNextRun(s.resetTimer(timer))
		}
	}
}

// startupDelay returns how long to wait before the first scan after a (re)start.
// It is based on the last persisted scan so restarts keep the existing cadence
// instead of resetting it, and it honours RunOnStart.
func (s *Scheduler) startupDelay() time.Duration {
	cfg := s.cfg.Get().Scan
	if cfg.IntervalMinutes <= 0 {
		return time.Hour // park; the manual-mode startup scan is handled separately
	}
	due := s.untilNextScan() // 0 when a scan is overdue or none has run yet
	if due == 0 && !cfg.RunOnStart {
		// A scan is due, but startup scans are disabled: wait a full interval.
		return s.nextInterval()
	}
	return due
}

func (s *Scheduler) nextInterval() time.Duration {
	m := s.cfg.Get().Scan.IntervalMinutes
	if m <= 0 {
		return time.Hour // park; periodic runs are skipped when interval is 0
	}
	return time.Duration(m) * time.Minute
}

// untilNextScan returns the time until the next scan is due, based on the last
// successful scan and the interval. It is 0 when a scan is overdue or none has
// run yet, and parks when periodic scans are disabled.
func (s *Scheduler) untilNextScan() time.Duration {
	if s.cfg.Get().Scan.IntervalMinutes <= 0 {
		return time.Hour
	}
	last := s.lastFinished()
	if last == nil {
		return 0
	}
	if d := time.Until(last.Add(s.nextInterval())); d > 0 {
		return d
	}
	return 0
}

func (s *Scheduler) lastFinished() *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status.LastFinished
}

// resetTimer re-arms the timer for a full interval after a scan and returns the
// delay it used.
func (s *Scheduler) resetTimer(t *time.Timer) time.Duration {
	d := s.nextInterval()
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
	return d
}

// armNextRun sets the displayed next-run time to now+d, or clears it when
// periodic scans are disabled.
func (s *Scheduler) armNextRun(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Get().Scan.IntervalMinutes > 0 {
		next := time.Now().Add(d)
		s.status.NextRun = &next
	} else {
		s.status.NextRun = nil
	}
}

// runScan executes a single scan, ignoring overlapping invocations. When force
// is set, cached TMDB responses are bypassed so everything is re-fetched fresh.
func (s *Scheduler) runScan(ctx context.Context, force bool) {
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
	s.log.Info("scan started", "runId", runID, "fullRefresh", force)

	s.progress.reset(started)
	jf := jellyfin.New(settings.Jellyfin.URL, settings.Jellyfin.APIKey)
	td := tmdb.New(settings.TMDB.APIKey, settings.TMDB.Language, settings.TMDB.Region, settings.Scan.TMDBRateLimitRPS).
		WithCache(tmdbcache.New(s.store)).
		WithForceRefresh(force)
	sc := scanner.New(jf, td, settings, s.log)
	sc.SetReporter(s.progress)

	result, scanErr := sc.Run(ctx)
	finished := time.Now()

	if scanErr != nil {
		_ = s.store.FinishScanRun(ctx, runID, store.StatusError, scanErr.Error(),
			result.LibrariesScanned, result.ItemsScanned, 0, result.Libraries, result.Media)
		s.setStatus(func(st *Status) {
			st.State = StateIdle
			st.LastFinished = &finished
			st.LastError = scanErr.Error()
		})
		s.log.Error("scan failed", "runId", runID, "err", scanErr)
		return
	}

	newGaps := s.countNewFindings(ctx, result.Findings)
	if err := s.store.AddFindings(ctx, runID, result.Findings); err != nil {
		s.log.Error("failed to persist findings", "runId", runID, "err", err)
	}
	if err := s.store.FinishScanRun(ctx, runID, store.StatusSuccess, "",
		result.LibrariesScanned, result.ItemsScanned, len(result.Findings), result.Libraries, result.Media); err != nil {
		s.log.Error("failed to finish scan run", "runId", runID, "err", err)
	}
	if pruned, err := s.store.PruneRuns(ctx, 20); err != nil {
		s.log.Warn("failed to prune old scan runs", "err", err)
	} else if pruned > 0 {
		s.log.Info("maintenance: pruned old scan runs", "removed", pruned)
	}

	s.setStatus(func(st *Status) {
		st.State = StateIdle
		st.LastFinished = &finished
		st.LastMissing = len(result.Findings)
		st.LastError = ""
	})
	if newGaps > 0 {
		s.log.Info("new gaps detected since last scan", "new", newGaps, "total", len(result.Findings))
	}
	s.log.Info("scan finished", "runId", runID, "libraries", result.LibrariesScanned,
		"items", result.ItemsScanned, "missing", len(result.Findings), "new", newGaps)
}

// countNewFindings reports how many of the given findings were not present in the
// most recent previous successful scan, so the activity log can highlight what
// changed. It must be called before the current run is marked successful.
func (s *Scheduler) countNewFindings(ctx context.Context, current []store.Finding) int {
	prev, err := s.store.LatestSuccessfulRun(ctx)
	if err != nil || prev == nil {
		return len(current) // no baseline yet -> everything counts as new
	}
	prevFindings, err := s.store.FindingsForRun(ctx, prev.ID)
	if err != nil {
		return 0
	}
	seen := make(map[string]struct{}, len(prevFindings))
	for _, f := range prevFindings {
		seen[findingKey(f)] = struct{}{}
	}
	n := 0
	for _, f := range current {
		if _, ok := seen[findingKey(f)]; !ok {
			n++
		}
	}
	return n
}

func findingKey(f store.Finding) string {
	season := ""
	if f.SeasonNumber != nil {
		season = strconv.Itoa(*f.SeasonNumber)
	}
	return f.Kind + "\x00" + f.LibraryID + "\x00" + f.Title + "\x00" + season
}

func (s *Scheduler) setStatus(mut func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mut(&s.status)
}

// restoreStatus seeds the in-memory status from the most recent persisted run so
// the dashboard still shows the last scan date after a restart.
func (s *Scheduler) restoreStatus(ctx context.Context) {
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
