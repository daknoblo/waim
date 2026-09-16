package refresher

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/store"
)

func TestCleanupKeepsRecentlyUsedCacheAndReportsOutcome(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	for _, key := range []string{"/movie/old", "/movie/recent"} {
		if err := st.TMDBCachePut(ctx, key, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `UPDATE tmdb_cache SET last_used_at=? WHERE cache_key=?`, time.Now().AddDate(0, 0, -40).UTC().Format(time.RFC3339Nano), "/movie/old"); err != nil {
		t.Fatal(err)
	}
	tracker := activity.New()
	r := New(cfg, st, nil, tracker)
	settings := cfg.Get()
	settings.Cache.CleanupEnabled = false
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	r.cleanup(ctx)
	if n, err := st.TMDBCacheCount(ctx); err != nil || n != 2 || tracker.Snapshot()[1].Status != activity.Idle {
		t.Fatal("disabled cleanup changed data or activity")
	}
	settings.Cache.CleanupEnabled = true
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	lease, err := cfg.Gate().TryReset()
	if err != nil {
		t.Fatal(err)
	}
	r.cleanup(ctx)
	lease.Finish(0, 0, false)
	if n, err := st.TMDBCacheCount(ctx); err != nil || n != 2 {
		t.Fatal("cleanup ran during exclusive maintenance")
	}
	r.cleanup(ctx)
	if _, found, err := st.TMDBCacheGet(ctx, "/movie/old"); err != nil || found {
		t.Fatal("old cache entry survived cleanup")
	}
	if _, found, err := st.TMDBCacheGet(ctx, "/movie/recent"); err != nil || !found {
		t.Fatal("cleanup deleted a recently used cache entry")
	}
	if state := tracker.Snapshot()[1]; state.Status != activity.Completed || state.Phase != activity.Cleanup {
		t.Fatalf("cleanup status incorrect: %+v", state)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	r.cleanup(ctx)
	if tracker.Snapshot()[1].Status != activity.Failed {
		t.Fatal("storage failure was reported as successful cleanup")
	}
}

func TestNextCleanupUsesLocalCalendarAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before daily cleanup", time.Date(2026, 2, 12, 2, 0, 0, 0, location), time.Date(2026, 2, 12, 3, 0, 0, 0, location)},
		{"exact deadline goes to next day", time.Date(2026, 2, 12, 3, 0, 0, 0, location), time.Date(2026, 2, 13, 3, 0, 0, 0, location)},
		{"spring forward", time.Date(2026, 3, 28, 4, 0, 0, 0, location), time.Date(2026, 3, 29, 3, 0, 0, 0, location)},
		{"fall back", time.Date(2026, 10, 24, 4, 0, 0, 0, location), time.Date(2026, 10, 25, 3, 0, 0, 0, location)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.now.Add(untilNextCleanup(tc.now)); !got.Equal(tc.want) {
				t.Fatalf("next cleanup = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScheduledCleanupSkipsPreResetTickAndRunStopsOnCancellation(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := New(cfg, nil, nil)
	due := time.Now().Add(-time.Second)
	lease, err := cfg.Gate().TryReset()
	if err != nil {
		t.Fatal(err)
	}
	called := false
	work := func(context.Context) { called = true }
	r.scheduled(context.Background(), time.Time{}, work)
	lease.Finish(1, 0, false)
	r.scheduled(context.Background(), due, work)
	if called {
		t.Fatal("pre-reset or maintenance-time work was admitted")
	}
	r.scheduled(context.Background(), time.Time{}, work)
	if !called {
		t.Fatal("new work was not admitted after maintenance")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { defer close(done); r.Run(ctx) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresher did not stop on cancellation")
	}
}
