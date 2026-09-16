package reset

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/maintenance"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func fixture(t *testing.T) (*config.Manager, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.Locale = "de"
	settings.TMDB.APIKey = "tmdb-secret"
	settings.AI.APIKey = "ai-secret"
	settings.AI.Endpoint = "https://fixture.invalid"
	settings.Sources = append(settings.Sources, config.Source{ID: "home", Type: media.Jellyfin, Name: "Home", Enabled: true, Jellyfin: config.JellyfinSettings{APIKey: "source-secret"}})
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "waim.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	src, _ := cfg.Get().Source("home")
	season, episode := 1, 2
	item := media.Item{ID: "home/title", Type: media.Series, Name: "Raw source title", ProductionYear: 2020, ProviderIDs: map[string]string{"Imdb": "tt123"}, References: []media.Reference{{ID: "home", Type: media.Jellyfin}}, Episodes: []media.Item{{ID: "home/episode", ParentIndexNumber: &season, IndexNumber: &episode}}}
	item = item.WithResolvedID(7)
	native := media.Item{ID: "home/native", Type: media.Movie, Name: "Native ID title", ProviderIDs: map[string]string{"Tmdb": "8"}}
	if err := st.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), &media.Snapshot{Items: []media.Item{item, native}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 9, Title: "Membership title", Poster: "/poster.jpg"}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.TMDBCachePut(ctx, "/movie/7?language=en-US", []byte(`{"title":"Downloaded"}`)); err != nil {
		t.Fatal(err)
	}
	id, err := st.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := st.VirtualEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PublishScan(ctx, id, []store.Finding{{Kind: store.KindMissingMovie, MediaType: store.MediaMovie, Title: "Derived title", Summary: "Missing"}}, nil, []store.MediaStat{{Title: "Derived metadata"}}, nil, store.RunMetadata{Revision: revision, Basis: "owned-v1"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetKV(ctx, "derived", "result"); err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func TestResetScopeMatrixAndPersistentKey(t *testing.T) {
	for _, scope := range []store.ResetScope{store.ResetMetadata, store.ResetMedia, store.ResetFactory} {
		t.Run(string(scope), func(t *testing.T) {
			cfg, st := fixture(t)
			before := cfg.Get()
			keyBefore, err := os.ReadFile(cfg.KeyPath())
			if err != nil {
				t.Fatal(err)
			}
			configBefore, err := os.ReadFile(cfg.Path())
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			entriesBefore, revisionBefore, err := st.VirtualEntries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = New(cfg, st).Apply(ctx, scope, cfg.Gate().Token(), func() {
				called = true
				if _, err := cfg.Gate().Enter(); !errors.Is(err, maintenance.ErrBusy) {
					t.Error("memory invalidation not exclusive")
				}
			})
			if err != nil || !called {
				t.Fatalf("reset failed: %v", err)
			}
			if run, err := st.LatestRun(ctx); err != nil || run != nil {
				t.Fatal("scan results retained")
			}
			if findings, err := st.FindingsForRun(ctx, 1); err != nil || len(findings) != 0 {
				t.Fatal("findings retained")
			}
			count, err := st.TMDBCacheCount(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if (scope == store.ResetMedia && count != 1) || (scope != store.ResetMedia && count != 0) {
				t.Fatalf("wrong cache scope: %d", count)
			}
			src, _ := before.Source("home")
			snapshot, err := st.SourceSnapshot(ctx, src.ID, src.Fingerprint())
			if err != nil {
				t.Fatal(err)
			}
			if scope == store.ResetMetadata {
				if snapshot.Snapshot == nil || len(snapshot.Snapshot.Items) != 2 {
					t.Fatal("raw inventory lost")
				}
				raw := snapshot.Snapshot.Items[0]
				if raw.ResolvedTMDBID != 0 || raw.ResolutionInput != "" || raw.ProviderIDs["Imdb"] != "tt123" || len(raw.Episodes) != 1 || *raw.Episodes[0].IndexNumber != 2 || snapshot.Snapshot.Items[1].TMDBID() != 8 {
					t.Fatalf("alias reset damaged raw ownership: %+v", snapshot)
				}
			} else if snapshot.Snapshot != nil {
				t.Fatal("imported inventory retained")
			}
			entries, revision, err := st.VirtualEntries(ctx)
			if err != nil || revision != revisionBefore+1 {
				t.Fatalf("catalog revision did not advance: %d %v", revision, err)
			}
			if scope == store.ResetFactory {
				if len(entries) != 0 || !reflect.DeepEqual(cfg.Get(), config.Defaults().Clone()) {
					t.Fatalf("factory defaults not exact: %+v", cfg.Get())
				}
			} else {
				if !reflect.DeepEqual(entries, entriesBefore) || !reflect.DeepEqual(cfg.Get(), before) {
					t.Fatal("retained settings or membership changed")
				}
				configAfter, err := os.ReadFile(cfg.Path())
				if err != nil || !bytes.Equal(configBefore, configAfter) {
					t.Fatal("non-factory reset rewrote credentials/config")
				}
			}
			keyAfter, err := os.ReadFile(cfg.KeyPath())
			if err != nil || !bytes.Equal(keyBefore, keyAfter) {
				t.Fatal("master key changed")
			}
			state, err := st.ResetState(ctx)
			if err != nil || state.FactoryPending || state.Epoch != 1 || cfg.Gate().Epoch() != 1 {
				t.Fatalf("reset epoch/journal incorrect: %+v %v", state, err)
			}
			reloaded, err := config.Load(filepath.Dir(cfg.Path()))
			if err != nil {
				t.Fatal(err)
			}
			if err := New(reloaded, st).Recover(ctx); err != nil {
				t.Fatal(err)
			}
			if reloaded.Gate().Epoch() != 1 || !reflect.DeepEqual(reloaded.Get(), cfg.Get()) {
				t.Fatal("restart lost reset state")
			}
		})
	}
}

func TestFactoryFailureIsRecoverableWithoutRepeatingDatabaseDeletion(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry", true: "restart"}[restart], func(t *testing.T) {
			cfg, st := fixture(t)
			before, err := os.ReadFile(cfg.Path())
			if err != nil {
				t.Fatal(err)
			}
			blocker := cfg.Path() + ".tmp"
			if err := os.Mkdir(blocker, 0o700); err != nil {
				t.Fatal(err)
			}
			child := filepath.Join(blocker, "block")
			if err := os.WriteFile(child, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			token := cfg.Gate().Token()
			if err := New(cfg, st).Apply(ctx, store.ResetFactory, token, nil); err == nil {
				t.Fatal("partial factory reset reported success")
			}
			after, err := os.ReadFile(cfg.Path())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed persistence changed config")
			}
			state, err := st.ResetState(ctx)
			if err != nil || !state.FactoryPending || state.Epoch != 1 {
				t.Fatalf("missing recovery journal: %+v %v", state, err)
			}
			if _, err := cfg.Gate().Enter(); !errors.Is(err, maintenance.ErrRecovery) {
				t.Fatal("partial reset admitted data work")
			}
			if err := os.Remove(child); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(blocker); err != nil {
				t.Fatal(err)
			}
			if restart {
				dbPath := st.Path()
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				st, err = store.Open(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = st.Close() })
				cfg, err = config.Load(filepath.Dir(cfg.Path()))
				if err != nil {
					t.Fatal(err)
				}
				err = New(cfg, st).Recover(ctx)
			} else {
				err = New(cfg, st).Apply(ctx, store.ResetFactory, token, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err = st.ResetState(ctx)
			if err != nil || state.FactoryPending || state.Epoch != 1 || !reflect.DeepEqual(cfg.Get(), config.Defaults().Clone()) {
				t.Fatalf("recovery incomplete or repeated deletion: %+v %v", state, err)
			}
		})
	}
}

func TestDatabaseFailureRollsBackWithoutTouchingConfiguration(t *testing.T) {
	cfg, st := fixture(t)
	before, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", st.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER reject_reset BEFORE DELETE ON scan_runs BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := New(cfg, st).Apply(context.Background(), store.ResetFactory, cfg.Gate().Token(), nil); err == nil {
		t.Fatal("DB failure hidden")
	}
	after, err := os.ReadFile(cfg.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("DB failure changed config")
	}
	if rows, err := st.FindingsForRun(context.Background(), 1); err != nil || len(rows) != 1 {
		t.Fatal("deletion preceding failure escaped transaction")
	}
	state, err := st.ResetState(context.Background())
	if err != nil || state.Epoch != 0 || state.FactoryPending {
		t.Fatalf("failed transaction changed journal: %+v", state)
	}
}

func TestBusyAndConcurrentResetsNeverMutateTwice(t *testing.T) {
	cfg, st := fixture(t)
	service := New(cfg, st)
	token := cfg.Gate().Token()
	release, err := cfg.Gate().Enter()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(context.Background(), store.ResetMedia, token, nil); !errors.Is(err, maintenance.ErrBusy) {
		t.Fatal("busy reset not rejected")
	}
	release()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- service.Apply(context.Background(), store.ResetMetadata, token, nil)
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, maintenance.ErrBusy) && !errors.Is(err, maintenance.ErrStale) {
			t.Fatal(err)
		}
	}
	state, err := st.ResetState(context.Background())
	if err != nil || successes != 1 || state.Epoch != 1 {
		t.Fatalf("concurrent reset mutated twice: %+v successes=%d", state, successes)
	}
}
