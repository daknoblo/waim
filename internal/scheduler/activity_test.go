package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type blockingMetadata struct {
	resolutionTMDB
	entered chan struct{}
	release chan struct{}
	fail    bool
}

func (b blockingMetadata) Movie(ctx context.Context, id int64) (tmdb.Movie, error) {
	close(b.entered)
	select {
	case <-ctx.Done():
		return tmdb.Movie{}, ctx.Err()
	case <-b.release:
	}
	if b.fail {
		return tmdb.Movie{}, errors.New("upstream failed")
	}
	return b.resolutionTMDB.Movie(ctx, id)
}

func TestScanActivityDuringMetadataAndTerminalOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "metadata-failed", "stale", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			settings := cfg.Get()
			settings.TMDB.APIKey = "fixture"
			settings.Sources = append(settings.Sources, config.Source{ID: "a", Name: "Home", Type: media.Jellyfin, Enabled: true})
			if err := cfg.Save(settings); err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(filepath.Join(dir, "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			src, _ := cfg.Get().Source("a")
			item := media.Item{ID: "a/movie", Type: media.Movie, Name: "Movie", ProviderIDs: map[string]string{"Tmdb": "7"}, References: []media.Reference{{ID: "a", Type: media.Jellyfin, LibraryID: "a/one"}, {ID: "a", Type: media.Jellyfin, LibraryID: "a/two"}}}
			if err := st.SaveSourceAttempt(ctx, "a", src.Fingerprint(), &media.Snapshot{Items: []media.Item{item, item}}, ""); err != nil {
				t.Fatal(err)
			}
			tracker := activity.New()
			td := blockingMetadata{entered: make(chan struct{}), release: make(chan struct{}), fail: mode == "metadata-failed"}
			s := New(cfg, st, nil, tracker)
			s.tmdbFactory = func(config.Settings) scanner.TMDBAPI { return td }
			done := make(chan struct{})
			// The stale case attempts a real adapter refresh without connection
			// configuration, then evaluates the preserved snapshot.
			go func() { defer close(done); s.runScan(ctx, mode == "stale") }()
			select {
			case <-td.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("metadata never started")
			}
			state := tracker.Snapshot()[0]
			if lease, err := cfg.Gate().TryReset(); err == nil {
				lease.Finish(cfg.Gate().Epoch(), cfg.Gate().FactoryEpoch(), false)
				t.Error("reset admitted while metadata lookup is in flight")
			}
			if state.Status != activity.Running || state.Phase != activity.Metadata || state.Current != "Movie" || !state.Known || state.Done != 0 || state.Total != 1 {
				t.Errorf("metadata scope must count unique titles, not memberships: %+v", state)
			}
			expectedMode := activity.Recompute
			if mode == "stale" {
				expectedMode = activity.SourceRefresh
			}
			if state.Mode != expectedMode {
				t.Errorf("scan mode lost or incorrect during metadata: got %q, want %q", state.Mode, expectedMode)
			}
			if mode == "cancel" {
				cancel()
			}
			close(td.release)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("scan did not stop")
			}
			state = tracker.Snapshot()[0]
			if state.Mode != expectedMode {
				t.Errorf("scan mode lost after persistence: got %q, want %q", state.Mode, expectedMode)
			}
			expected := activity.Completed
			switch mode {
			case "metadata-failed", "stale":
				expected = activity.Partial
			case "cancel":
				expected = activity.Cancelled
			}
			if state.Status != expected || state.EndedAt.IsZero() {
				t.Fatalf("unexpected terminal state: %+v", state)
			}
			if mode == "stale" && state.Warnings == 0 {
				t.Fatal("stale inventory reported as verified")
			}
			if mode == "stale" {
				found := false
				for _, d := range state.Diagnostics {
					found = found || (d.Reason == activity.SourceStale && d.Subject == "Home")
				}
				if !found {
					t.Fatalf("stale source reason not retained: %+v", state.Diagnostics)
				}
			}
		})
	}
}

func TestScanActivitySetupAndStoreFailure(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	tracker := activity.New()
	s := New(cfg, st, nil, tracker)
	s.runScan(context.Background(), true)
	if tracker.Snapshot()[0].Status != activity.Waiting {
		t.Fatal("missing TMDB not waiting for setup")
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "fixture"
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	s.runScan(context.Background(), false)
	if tracker.Snapshot()[0].Status != activity.Failed {
		t.Fatal("store failure reported as success")
	}
}
