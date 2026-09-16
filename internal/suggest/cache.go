package suggest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/daknoblo/waim/internal/activity"
)

const RefreshInterval = 12 * time.Hour
const resultCacheKey = "suggestions.result.v1"

type persistedCache struct {
	Version     int
	SettingsKey string
	AttemptedAt time.Time
	Result      *Result
	Diagnostics []activity.Diagnostic
	Truncated   bool
}

func (s *Service) settingsKey() string {
	cfg := s.cfg.Get()
	b, _ := json.Marshal([]any{cfg.TMDB, cfg.AI})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func (s *Service) loadCache() {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	raw, found, err := s.store.GetKV(ctx, resultCacheKey)
	if err != nil {
		s.cacheLoadError(err)
		return
	}
	if !found {
		return
	}
	var cached persistedCache
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		s.cacheLoadError(err)
		return
	}
	if cached.Version != 1 || cached.AttemptedAt.IsZero() || (cached.Result != nil && cached.Result.GeneratedAt.IsZero()) {
		s.cacheLoadError(fmt.Errorf("unsupported or incomplete suggestion cache"))
		return
	}
	if cached.SettingsKey != s.settingsKey() {
		return
	}
	s.result, s.cacheKey, s.lastAttempt = cached.Result, cached.SettingsKey, cached.AttemptedAt
	// Revalidate persisted reason codes and labels through the normal bounded
	// activity API, without pretending a generation ran during startup.
	tracker := activity.New()
	run := tracker.Start(activity.Suggestions)
	for _, diagnostic := range cached.Diagnostics {
		run.Report(diagnostic)
	}
	run.Finish(ctx, activity.Completed, 0)
	s.savedDiagnostics = tracker.Snapshot()[2]
	s.savedDiagnostics.DiagnosticsTruncated = s.savedDiagnostics.DiagnosticsTruncated || cached.Truncated
}

func (s *Service) cacheLoadError(err error) {
	s.log.Error("suggestion cache could not be loaded", "err", err)
	run := s.activities.Start(activity.Suggestions)
	run.Report(activity.Diagnostic{Reason: activity.StorageUnavailable, Severity: activity.Error})
	run.Finish(context.Background(), activity.Failed, 0)
}

func (s *Service) SavedDiagnostics() activity.State {
	key := s.settingsKey()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cacheKey != key {
		return activity.State{}
	}
	state := s.savedDiagnostics
	state.Diagnostics = append([]activity.Diagnostic(nil), state.Diagnostics...)
	return state
}

func hasSuggestions(r *Result) bool {
	return len(r.Trending)+len(r.Similar)+len(r.UpcomingTaste)+len(r.UpcomingRegion)+len(r.AI) > 0
}

func (s *Service) publish(ctx context.Context, res *Result, epoch uint64, key string, run *activity.Run) activity.Status {
	state := s.activities.Snapshot()[2]
	diagnostics := make([]activity.Diagnostic, 0, len(state.Diagnostics))
	failed := state.Failures > 0
	for _, diagnostic := range state.Diagnostics {
		if !diagnostic.Previous {
			diagnostics = append(diagnostics, diagnostic)
			failed = failed || diagnostic.Severity == activity.Error
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || epoch != s.epoch || key != s.settingsKey() {
		return activity.Cancelled
	}
	result := s.result
	if !failed || (result == nil && hasSuggestions(res)) {
		result = res
	}
	cached := persistedCache{
		Version: 1, SettingsKey: key, AttemptedAt: s.now(),
		Result: result, Diagnostics: diagnostics, Truncated: state.DiagnosticsTruncated,
	}
	raw, err := json.Marshal(cached)
	if err == nil {
		err = s.store.SetKV(ctx, resultCacheKey, string(raw))
	}
	if err != nil {
		s.log.Error("suggestion cache could not be saved", "err", err)
		run.Report(activity.Diagnostic{Reason: activity.StorageUnavailable, Severity: activity.Error})
		return activity.Failed
	}
	s.result, s.cacheKey, s.lastAttempt = result, key, cached.AttemptedAt
	state.Diagnostics = diagnostics
	state.PreviousSeverity = ""
	state.Status = activity.Completed
	if len(diagnostics) > 0 || state.Warnings > 0 || state.Failures > 0 || state.Skipped > 0 {
		state.Status = activity.Partial
	}
	s.savedDiagnostics = state
	return activity.Completed
}

// Run refreshes a due cache without requiring an open browser. A timer owns no
// maintenance admission while idle; each generation takes its own lease.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.run(ctx, ticker.C)
}

func (s *Service) run(ctx context.Context, ticks <-chan time.Time) {
	defer s.Close()
	if ctx.Err() != nil || s.ctx.Err() != nil {
		return
	}
	s.scheduled(s.now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case due, ok := <-ticks:
			if !ok {
				return
			}
			s.scheduled(due)
		}
	}
}

func (s *Service) scheduled(due time.Time) {
	if s.cfg.Get().TMDB.APIKey != "" && s.NeedsRefresh(s.ctx) {
		s.generate(due, true)
	}
}
