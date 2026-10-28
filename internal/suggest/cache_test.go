package suggest

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/reset"
	"github.com/daknoblo/waim/internal/store"
)

type cacheBlock struct {
	entered, release chan struct{}
	once             sync.Once
}

type cacheTransport struct {
	calls   atomic.Int64
	version atomic.Int64
	fail    atomic.Bool
	block   atomic.Pointer[cacheBlock]
}

func (f *cacheTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	if b := f.block.Load(); b != nil {
		b.once.Do(func() { close(b.entered) })
		select {
		case <-b.release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	status := http.StatusOK
	if f.fail.Load() {
		status = http.StatusUnauthorized
	}
	body := fmt.Sprintf(`{"results":[{"id":900,"name":"Pick %d","title":"Pick %d","release_date":"2030-01-01","first_air_date":"2030-01-01"}]}`, f.version.Load(), f.version.Load())
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func cacheService(t *testing.T) (*Service, *cacheTransport) {
	t.Helper()
	fake := &cacheTransport{}
	fake.version.Store(1)
	old := http.DefaultTransport
	http.DefaultTransport = fake
	t.Cleanup(func() { http.DefaultTransport = old })
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "private-cache-test-key"
	settings.Scan.TMDBRateLimitRPS = 50
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { s.Close(); _ = st.Close() })
	return s, fake
}

func cacheTitle(t *testing.T, s *Service) string {
	t.Helper()
	r, _ := s.Result()
	if r == nil || len(r.Trending) == 0 {
		t.Fatal("expected cached recommendations")
	}
	return r.Trending[0].Title
}

func TestSuggestionCacheSurvivesVisitsCatalogEditsAndRestart(t *testing.T) {
	s, fake := cacheService(t)
	s.Generate()
	s.wg.Wait()
	calls := fake.calls.Load()
	if cacheTitle(t, s) != "Pick 1" {
		t.Fatal("generation failed")
	}
	ctx := context.Background()
	if err := s.store.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 7, Title: "Watched"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.AddSource(config.Source{ID: "disabled", Type: media.Jellyfin, Name: "Disabled"}); err != nil {
		t.Fatal(err)
	}
	s.Invalidate()
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PublishScan(ctx, id, nil, nil, nil, nil, store.RunMetadata{Basis: "owned-v1", Revision: 1, SourcesToken: s.cfg.Get().SourcesToken()}); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if s.NeedsRefresh(ctx) || cacheTitle(t, s) != "Pick 1" {
			t.Fatal("ordinary navigation, scan or watch edit invalidated the cache")
		}
	}
	raw, found, err := s.store.GetKV(ctx, resultCacheKey)
	if err != nil || !found || strings.Contains(raw, "private-cache-test-key") {
		t.Fatal("cache missing or credentials persisted")
	}
	s.Close()
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Dir(s.cfg.Path()))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(s.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(cfg, st, s.log)
	defer func() { restarted.Close(); _ = st.Close() }()
	if cacheTitle(t, restarted) != "Pick 1" || restarted.NeedsRefresh(ctx) || fake.calls.Load() != calls {
		t.Fatal("restart did not restore the cache without remote requests")
	}
}

func TestBackgroundRefreshCadenceIsTwelveHours(t *testing.T) {
	s, fake := cacheService(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	s.now = func() time.Time { return time.Unix(0, clock.Load()) }
	s.scheduled(s.now())
	s.wg.Wait()
	calls := fake.calls.Load()
	if calls == 0 {
		t.Fatal("first background generation never started")
	}
	clock.Add(int64(RefreshInterval - time.Nanosecond))
	s.scheduled(s.now())
	if fake.calls.Load() != calls || s.NeedsRefresh(context.Background()) {
		t.Fatal("cache refreshed before twelve hours")
	}
	for version := int64(2); version <= 3; version++ {
		if version == 2 {
			clock.Add(1)
		} else {
			clock.Add(int64(RefreshInterval))
		}
		fake.version.Store(version)
		s.scheduled(s.now())
		s.wg.Wait()
		if cacheTitle(t, s) != fmt.Sprintf("Pick %d", version) || fake.calls.Load() <= calls {
			t.Fatal("due refresh did not fetch fresh remote recommendations")
		}
		calls = fake.calls.Load()
	}
}

func TestRefreshKeepsCacheThroughOverlapInvalidationAndFailure(t *testing.T) {
	s, fake := cacheService(t)
	s.Generate()
	s.wg.Wait()
	block := &cacheBlock{entered: make(chan struct{}), release: make(chan struct{})}
	fake.block.Store(block)
	fake.version.Store(2)
	s.Generate()
	select {
	case <-block.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	if !s.Running() || cacheTitle(t, s) != "Pick 1" {
		t.Fatal("refresh hid cached suggestions")
	}
	calls := fake.calls.Load()
	s.Generate()
	if fake.calls.Load() != calls {
		t.Fatal("overlapping refresh started a second request")
	}
	if lease, err := s.cfg.Gate().TryReset(); err == nil {
		lease.Finish(0, 0, false)
		t.Fatal("reset admitted during a suggestion refresh")
	}
	s.Invalidate()
	close(block.release)
	s.wg.Wait()
	fake.block.Store(nil)
	if cacheTitle(t, s) != "Pick 1" {
		t.Fatal("late invalidated generation replaced the cache")
	}
	s.Generate()
	s.wg.Wait()
	if cacheTitle(t, s) != "Pick 2" {
		t.Fatal("manual refresh did not replace cached results")
	}
	fake.fail.Store(true)
	s.Generate()
	s.wg.Wait()
	if cacheTitle(t, s) != "Pick 2" || s.NeedsRefresh(context.Background()) || s.SavedDiagnostics().Severity() != activity.Error {
		t.Fatal("failed refresh lost the cache, retried immediately, or hid errors")
	}
	restarted := New(s.cfg, s.store, s.log)
	defer restarted.Close()
	if cacheTitle(t, restarted) != "Pick 2" || restarted.SavedDiagnostics().Severity() != activity.Error {
		t.Fatal("saved refresh failure/cache did not survive a new service")
	}
	fake.fail.Store(false)
	fake.version.Store(3)
	s.Generate()
	s.wg.Wait()
	if cacheTitle(t, s) != "Pick 3" || s.SavedDiagnostics().Severity() != "" {
		t.Fatal("successful retry did not replace old results and diagnostics")
	}
}

func TestCacheWriteFailureKeepsLastPersistedResult(t *testing.T) {
	s, fake := cacheService(t)
	s.Generate()
	s.wg.Wait()
	db, err := sql.Open("sqlite", s.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER reject_cache_update BEFORE UPDATE ON kv BEGIN SELECT RAISE(FAIL, 'fixture write failure'); END`); err != nil {
		t.Fatal(err)
	}
	fake.version.Store(2)
	s.Generate()
	s.wg.Wait()
	if cacheTitle(t, s) != "Pick 1" || s.activities.Snapshot()[2].Severity() != activity.Error {
		t.Fatal("storage failure replaced the last good result or was hidden")
	}
	restarted := New(s.cfg, s.store, s.log)
	defer restarted.Close()
	if cacheTitle(t, restarted) != "Pick 1" {
		t.Fatal("failed write changed persisted results")
	}
}

func TestResetClearsDurableCacheAndRejectsOldTicks(t *testing.T) {
	for _, scope := range []store.ResetScope{store.ResetMetadata, store.ResetMedia, store.ResetFactory} {
		t.Run(string(scope), func(t *testing.T) {
			s, fake := cacheService(t)
			s.Generate()
			s.wg.Wait()
			oldTick := time.Now().Add(-time.Minute)
			if err := reset.New(s.cfg, s.store).Apply(context.Background(), scope, s.cfg.Gate().Token(), s.Clear); err != nil {
				t.Fatal(err)
			}
			if result, _ := s.Result(); result != nil {
				t.Fatal("reset retained in-memory suggestions")
			}
			if _, found, err := s.store.GetKV(context.Background(), resultCacheKey); err != nil || found {
				t.Fatal("reset retained durable suggestions")
			}
			calls := fake.calls.Load()
			s.scheduled(oldTick)
			if s.Running() || fake.calls.Load() != calls {
				t.Fatal("pre-reset tick repopulated suggestions")
			}
		})
	}
}

func TestBackgroundLoopDoesNotStartAfterCancellation(t *testing.T) {
	s, fake := cacheService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.run(ctx, nil)
	if fake.calls.Load() != 0 || s.Running() {
		t.Fatal("cancelled worker started remote work")
	}
}

func TestBackgroundLoopRefreshesWithoutBrowserAndJoinsOnShutdown(t *testing.T) {
	s, fake := cacheService(t)
	s.Generate()
	s.wg.Wait()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	s.now = func() time.Time { return time.Unix(0, clock.Load()) }
	block := &cacheBlock{entered: make(chan struct{}), release: make(chan struct{})}
	fake.block.Store(block)
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.run(ctx, ticks); close(done) }()
	clock.Add(int64(RefreshInterval))
	select {
	case ticks <- s.now():
	case <-time.After(5 * time.Second):
		t.Fatal("background loop did not accept a timer tick")
	}
	select {
	case <-block.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("background refresh required a page visit")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("background refresh did not cancel/join during shutdown")
	}
	if s.Running() {
		t.Fatal("generation remained active after background worker stopped")
	}
}

func TestCacheCompatibilityAndCorruptPayload(t *testing.T) {
	s, _ := cacheService(t)
	s.Generate()
	s.wg.Wait()
	settings := s.cfg.Get()
	settings.TMDB.Language = "de-DE"
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}

	s.Invalidate()
	if result, _ := s.Result(); result != nil || !s.NeedsRefresh(context.Background()) {
		t.Fatal("incompatible metadata language reused old cached text")
	}
	restarted := New(s.cfg, s.store, s.log)
	defer restarted.Close()
	if result, _ := restarted.Result(); result != nil {
		t.Fatal("incompatible persisted cache was restored")
	}
	if err := s.store.SetKV(context.Background(), resultCacheKey, "{not-json"); err != nil {
		t.Fatal(err)
	}
	corrupt := New(s.cfg, s.store, s.log)
	defer corrupt.Close()
	if result, _ := corrupt.Result(); result != nil || corrupt.activities.Snapshot()[2].Severity() != activity.Error {
		t.Fatal("corrupt cache was silently accepted")
	}
}
