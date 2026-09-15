package refresher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/store"
)

type cacheTransport func(*http.Request) (*http.Response, error)

func (f cacheTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCacheActivityReportsActualBatchAndSafePaths(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "secret-key"
	settings.Cache.RefreshEnabled = true
	settings.Cache.RefreshPercent = 100
	settings.Scan.TMDBRateLimitRPS = 50
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	for _, key := range []string{"/movie/7?language=en-US", "/search/movie?query=private-search&api_key=secret-key"} {
		if err := st.TMDBCachePut(context.Background(), key, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	tracker := activity.New()
	entered := make(chan string)
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := http.DefaultTransport
	http.DefaultTransport = cacheTransport(func(r *http.Request) (*http.Response, error) {
		select {
		case entered <- r.URL.Path:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		status := http.StatusOK
		if r.URL.Path == "/3/search/movie" {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = original }()
	r := New(cfg, st, nil, tracker)
	done := make(chan struct{})
	go func() { defer close(done); r.refreshBatch(ctx) }()
	for i := range 2 {
		select {
		case path := <-entered:
			if lease, err := cfg.Gate().TryReset(); err == nil {
				lease.Finish(cfg.Gate().Epoch(), cfg.Gate().FactoryEpoch(), false)
				t.Error("reset admitted while cache refresh is in flight")
			}
			s := tracker.Snapshot()[1]
			if s.Status != activity.Running || s.Phase != activity.Refresh || !s.Known || s.Done != i || s.Total != 2 || "/3"+s.Query != path {
				t.Errorf("bad live cache scope: %+v, request=%s", s, path)
			}
			if strings.Contains(fmt.Sprint(s), "secret") || strings.Contains(fmt.Sprint(s), "private-search") {
				t.Error("private query exposed")
			}
			release <- struct{}{}
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("cache request never started")
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("cache batch never completed")
	}
	s := tracker.Snapshot()[1]
	if s.Status != activity.Partial || s.Done != 2 || s.Failures != 1 || s.Remaining() != 0 {
		t.Fatalf("failed batch appeared fully successful: %+v", s)
	}
}

func TestCacheWaitingEmptyAndCancelled(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	tracker := activity.New()
	r := New(cfg, st, nil, tracker)
	settings := cfg.Get()
	settings.Cache.RefreshEnabled = true
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	r.refreshBatch(context.Background())
	if tracker.Snapshot()[1].Status != activity.Waiting {
		t.Fatal("missing key was not waiting")
	}
	settings.TMDB.APIKey = "fixture"
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	r.refreshBatch(context.Background())
	if state := tracker.Snapshot()[1]; state.Status != activity.Completed || state.Total != 0 {
		t.Fatalf("empty cache not complete: %+v", state)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.refreshBatch(ctx)
	if tracker.Snapshot()[1].Status != activity.Cancelled {
		t.Fatal("cancelled batch not reported")
	}
}
