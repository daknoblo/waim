// Package refresher periodically re-fetches the oldest slice of the TMDB cache
// so stored data stays current without re-loading everything on each scan.
package refresher

import (
	"context"
	"log/slog"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/tmdbcache"
)

// Refresher incrementally refreshes cached TMDB responses in the background.
type Refresher struct {
	cfg        *config.Manager
	store      *store.Store
	log        *slog.Logger
	activities *activity.Tracker
}

// cleanupHour is the local hour of day (24h) at which the nightly orphan
// cleanup runs.
const cleanupHour = 3

// New creates a Refresher.
func New(cfg *config.Manager, st *store.Store, log *slog.Logger, activities ...*activity.Tracker) *Refresher {
	if log == nil {
		log = slog.Default()
	}
	r := &Refresher{cfg: cfg, store: st, log: log}
	if len(activities) > 0 {
		r.activities = activities[0]
	}
	return r
}

// Run blocks until ctx is cancelled, refreshing a batch of the oldest cache
// entries on each configured interval and pruning orphaned entries once a night.
// Intervals are re-read every tick so settings changes take effect without a
// restart.
func (r *Refresher) Run(ctx context.Context) {
	refreshTimer := time.NewTimer(r.nextInterval())
	defer refreshTimer.Stop()
	cleanupTimer := time.NewTimer(untilNextCleanup(time.Now()))
	defer cleanupTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case due := <-refreshTimer.C:
			r.scheduled(ctx, due, r.refreshBatch)
			resetTimer(refreshTimer, r.nextInterval())
		case due := <-cleanupTimer.C:
			r.scheduled(ctx, due, r.cleanup)
			resetTimer(cleanupTimer, untilNextCleanup(time.Now()))
		}
	}
}

func (r *Refresher) nextInterval() time.Duration {
	m := r.cfg.Get().Cache.RefreshIntervalMinutes
	if m <= 0 {
		m = 1
	}
	return time.Duration(m) * time.Minute
}

func (r *Refresher) scheduled(ctx context.Context, due time.Time, work func(context.Context)) {
	release, err := r.cfg.Gate().EnterScheduled(due)
	if err != nil {
		r.log.Info("scheduled cache work skipped after or during maintenance")
		return
	}
	defer release()
	work(ctx)
}

// untilNextCleanup returns the duration from now until the next cleanupHour.
func untilNextCleanup(now time.Time) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day(), cleanupHour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(now)
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// refreshBatch re-fetches the oldest RefreshPercent share of cache entries.
func (r *Refresher) refreshBatch(ctx context.Context) {
	release, err := r.cfg.Gate().Enter()
	if err != nil {
		r.log.Warn("cache refresh skipped during maintenance")
		return
	}
	defer release()
	settings := r.cfg.Get()
	cache := settings.Cache
	if !cache.RefreshEnabled {
		return
	}
	run := r.activities.Start(activity.Cache)
	ctx = activity.WithRun(ctx, run)
	outcome := activity.Failed
	defer func() { run.Finish(ctx, outcome, 0) }()
	run.Phase(activity.Refresh, -1)
	if settings.TMDB.APIKey == "" {
		outcome = activity.Waiting
		return
	}

	count, err := r.store.TMDBCacheCount(ctx)
	if err != nil {
		r.log.Warn("refresher: count cache", "error", err)
		return
	}
	if count == 0 {
		run.Phase(activity.Refresh, 0)
		outcome = activity.Completed
		return
	}

	pct := cache.RefreshPercent
	switch {
	case pct <= 0:
		pct = 1
	case pct > 100:
		pct = 100
	}
	batch := count * pct / 100
	if batch < 1 {
		batch = 1
	}

	keys, err := r.store.TMDBCacheOldestKeys(ctx, batch)
	if err != nil {
		r.log.Warn("refresher: oldest keys", "error", err)
		return
	}
	if len(keys) == 0 {
		run.Phase(activity.Refresh, 0)
		outcome = activity.Completed
		return
	}
	run.Phase(activity.Refresh, len(keys))

	td := tmdb.New(settings.TMDB.APIKey, settings.TMDB.Language, settings.TMDB.Region, settings.Scan.TMDBRateLimitRPS).
		WithCache(tmdbcache.New(r.store))

	var refreshed, failed int
	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		if err := td.RefreshKey(ctx, key); err != nil {
			run.Advance(true, false)
			failed++
			r.log.Debug("refresher: refresh failed", "key", key, "error", err)
			continue
		}
		refreshed++
		run.Advance(false, false)
	}
	outcome = activity.Completed
	r.log.Info("tmdb cache refreshed", "refreshed", refreshed, "failed", failed, "total", count)
}

// cleanup removes cache entries that have not been used by a scan or suggestion
// for the configured number of days.
func (r *Refresher) cleanup(ctx context.Context) {
	release, err := r.cfg.Gate().Enter()
	if err != nil {
		r.log.Warn("cache cleanup skipped during maintenance")
		return
	}
	defer release()
	cache := r.cfg.Get().Cache
	if !cache.CleanupEnabled {
		return
	}
	run := r.activities.Start(activity.Cache)
	outcome := activity.Failed
	defer func() { run.Finish(ctx, outcome, 0) }()
	run.Phase(activity.Cleanup, -1)
	days := cache.CleanupMaxAgeDays
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	n, err := r.store.TMDBCachePruneUnusedBefore(ctx, cutoff)
	if err != nil {
		r.log.Warn("refresher: cleanup", "error", err)
		return
	}
	if n > 0 {
		r.log.Info("tmdb cache cleanup", "removed", n, "olderThanDays", days)
	}
	outcome = activity.Completed
}
