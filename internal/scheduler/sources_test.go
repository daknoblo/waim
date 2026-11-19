package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type blockingTMDB struct {
	scanner.TMDBAPI
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingTMDB) Movie(ctx context.Context, id int64) (tmdb.Movie, error) {
	b.once.Do(func() {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
	return tmdb.Movie{ID: id, Title: "Tracked", ReleaseDate: "2020-01-01"}, ctx.Err()
}

func TestMutationDuringComputationIsRequeuedNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "local-fake"
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	if err := st.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 1, Title: "First"}, false); err != nil {
		t.Fatal(err)
	}
	td := &blockingTMDB{entered: make(chan struct{}), release: make(chan struct{})}
	s := New(cfg, st, nil)
	s.tmdbFactory = func(config.Settings) scanner.TMDBAPI { return td }
	done := make(chan struct{})
	go func() { defer close(done); s.runScan(ctx, false) }()
	<-td.entered
	if err := st.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 2, Title: "Second"}, false); err != nil {
		t.Fatal(err)
	}
	close(td.release)
	<-done
	if run, err := st.LatestSuccessfulRun(ctx); err != nil || run != nil {
		t.Fatal("stale computation published")
	}
	select {
	case <-s.recomputeCh:
	default:
		t.Fatal("newer revision not queued")
	}
	s.runScan(ctx, false)
	run, err := st.LatestSuccessfulRun(ctx)
	if err != nil || run == nil || len(run.Media) != 2 || run.Metadata.Revision != 2 || run.ItemsScanned != 0 {
		t.Fatalf("follow-up revision failed: %+v %v", run, err)
	}
	if s.Status().LastError != "" {
		t.Fatal("successful recomputation left stale error")
	}
}
