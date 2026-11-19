package refresher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/maintenance"
	"github.com/daknoblo/waim/internal/reset"
	"github.com/daknoblo/waim/internal/store"
)

func manualFixture(t *testing.T) (*Refresher, *activity.Tracker) {
	t.Helper()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "fixture"
	settings.Scan.TMDBRateLimitRPS = 50
	settings.Cache.RefreshEnabled = false
	settings.Cache.CleanupEnabled = false
	settings.Cache.RefreshPercent = 25
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for i := range 8 {
		if err := st.TMDBCachePut(context.Background(), fmt.Sprintf("/movie/%d", i+1), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	tracker := activity.New()
	return New(cfg, st, nil, tracker), tracker
}

func TestFailedSeasonRefreshUsesCachedSeriesTitle(t *testing.T) {
	r, tracker := manualFixture(t)
	ctx := t.Context()
	if err := r.store.TMDBCachePut(ctx, "/tv/42?language=de-DE", []byte(`{"name":"Beispielserie"}`)); err != nil {
		t.Fatal(err)
	}
	if err := r.store.TMDBCachePut(ctx, "/tv/42/season/2?language=de-DE", []byte(`{"name":"Staffel 2"}`)); err != nil {
		t.Fatal(err)
	}
	settings := r.cfg.Get()
	settings.Cache.RefreshPercent = 100
	if err := r.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = cacheTransport(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		if strings.HasSuffix(req.URL.Path, "/season/2") {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Request: req, Body: io.NopCloser(strings.NewReader(`{"name":"Beispielserie"}`))}, nil
	})
	r.refresh(ctx, true)
	d := tracker.Snapshot()[1].Diagnostics
	if len(d) != 1 || d[0].Current != "Beispielserie" || d[0].Query != "/tv/42/season/2" {
		t.Fatalf("missing series context in diagnostic: %+v", d)
	}
}

func TestFailedOldestKeyDoesNotStarveNextRefresh(t *testing.T) {
	r, tracker := manualFixture(t)
	settings := r.cfg.Get()
	settings.Cache.RefreshPercent = 1 // one key per batch
	if err := r.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	keys, err := r.store.TMDBCacheOldestKeys(t.Context(), 2)
	if err != nil || len(keys) != 2 {
		t.Fatalf("initial queue: %v, %v", keys, err)
	}
	var paths []string
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = cacheTransport(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		status := http.StatusOK
		if req.URL.Path == "/3"+keys[0] {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"id":2}`)), Request: req}, nil
	})
	r.refresh(t.Context(), true)
	if tracker.Snapshot()[1].Failures != 1 {
		t.Fatal("404 not reported")
	}
	r.refresh(t.Context(), true)
	if len(paths) != 2 || paths[1] != "/3"+keys[1] || tracker.Snapshot()[1].Failures != 0 {
		t.Fatalf("second batch did not advance: %v", paths)
	}
	if body, ok, err := r.store.TMDBCacheGet(t.Context(), keys[0]); err != nil || !ok || string(body) != "{}" {
		t.Fatalf("failed refresh lost cached body: %q, %v", body, err)
	}
}

func TestManualCacheRefreshUsesConfiguredBatchAndCoalesces(t *testing.T) {
	r, tracker := manualFixture(t)
	var calls atomic.Int64
	started, release := make(chan struct{}, 2), make(chan struct{})
	old := http.DefaultTransport
	http.DefaultTransport = cacheTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
	})
	defer func() { http.DefaultTransport = old }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.Run(ctx) }()
	defer func() { cancel(); <-done }()
	if err := r.Trigger(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("manual request never reached refresher loop")
	}
	if !r.Busy() || tracker.Snapshot()[1].Total != 2 {
		t.Fatal("manual refresh must use 25% of 8 cached entries")
	}
	if err := r.Trigger(); !errors.Is(err, maintenance.ErrBusy) {
		t.Fatal("second manual start should not queue a duplicate batch")
	}
	if lease, err := r.cfg.Gate().TryReset(); err == nil {
		lease.Finish(0, 0, false)
		t.Fatal("reset admitted during manual cache work")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for r.Busy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.Busy() || calls.Load() != 2 || tracker.Snapshot()[1].Status != activity.Completed || tracker.Snapshot()[1].Done != 2 {
		t.Fatalf("manual batch did not complete as expected: calls=%d state=%+v", calls.Load(), tracker.Snapshot()[1])
	}
	if r.cfg.Get().Cache.RefreshEnabled {
		t.Fatal("manual action must not enable scheduled refresh")
	}
	if count, err := r.store.TMDBCacheCount(ctx); err != nil || count != 8 {
		t.Fatal("manual refresh must not clear the cache")
	}
}

func TestManualCacheQueueCannotCrossResetOrRunWithoutKey(t *testing.T) {
	r, _ := manualFixture(t)
	if err := r.Trigger(); err != nil {
		t.Fatal(err)
	}
	epoch := <-r.triggerCh
	if err := reset.New(r.cfg, r.store).Apply(context.Background(), store.ResetMetadata, r.cfg.Gate().Token(), nil); err != nil {
		t.Fatal(err)
	}
	r.manual(context.Background(), epoch)
	if r.Busy() {
		t.Fatal("stale queued request did not release its pending state")
	}
	settings := r.cfg.Get()
	settings.TMDB.APIKey = ""
	if err := r.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	if err := r.Trigger(); err == nil || r.Busy() {
		t.Fatal("manual cache work accepted without TMDB setup")
	}
}

func TestManualCacheRefreshCancelsWithWorker(t *testing.T) {
	r, tracker := manualFixture(t)
	entered := make(chan struct{})
	old := http.DefaultTransport
	http.DefaultTransport = cacheTransport(func(req *http.Request) (*http.Response, error) {
		close(entered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	defer func() { http.DefaultTransport = old }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.Run(ctx) }()
	defer func() { cancel(); <-done }()
	if err := r.Trigger(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("manual refresh did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("manual worker did not join during shutdown")
	}
	if r.Busy() || tracker.Snapshot()[1].Status != activity.Cancelled {
		t.Fatal("cancelled manual refresh kept running/success state")
	}
}
