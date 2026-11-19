package scheduler

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

func intervalSource(id string, minutes int) config.Source {
	return config.Source{ID: id, Name: id, Type: media.Jellyfin, Enabled: true, ScanIntervalMinutes: &minutes,
		Libraries: []config.Library{{ID: "movies", Name: "Movies", Enabled: true}}}
}

func intervalConfig(t *testing.T) *config.Manager {
	t.Helper()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings.TMDB.APIKey = "fixture"
	settings.Scan.RunOnStart = false
	settings.Sources = append(settings.Sources, intervalSource("a", 10), intervalSource("b", 30), intervalSource("manual", 0))
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func assertDeadline(t *testing.T, s *Scheduler, id string, want time.Time) {
	t.Helper()
	got := s.SourceNextRun(id)
	if got == nil || !got.Equal(want) {
		t.Fatalf("%s deadline = %v, want %v", id, got, want)
	}
}

func TestMaximumSourceIntervalDeadline(t *testing.T) {
	cfg := intervalConfig(t)
	settings := cfg.Get()
	settings.Sources = []config.Source{config.VirtualSource(), intervalSource("yearly", config.MaxSourceScanIntervalMinutes)}
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, nil, nil)
	timer, schedule := &recordedTimer{}, &scanSchedule{}
	start := time.Now()
	s.syncSchedule(timer, schedule, start, nil)
	delay := 365 * 24 * time.Hour
	assertDeadline(t, s, "yearly", start.Add(delay))
	if len(timer.resets) != 1 || timer.resets[0] != delay {
		t.Fatalf("one-year interval timer overflowed: %v", timer.resets)
	}
}

func TestIndependentSourceDeadlines(t *testing.T) {
	cfg := intervalConfig(t)
	s := New(cfg, nil, nil)
	timer, schedule := &recordedTimer{}, &scanSchedule{}
	start := time.Now()
	s.syncSchedule(timer, schedule, start, nil)
	assertDeadline(t, s, "a", start.Add(10*time.Minute))
	assertDeadline(t, s, "b", start.Add(30*time.Minute))
	if s.SourceNextRun("manual") != nil || s.SourceNextRun(media.VirtualID) != nil || s.SourceNextRun("absent") != nil {
		t.Fatal("manual, virtual or absent source scheduled")
	}
	copy := s.SourceNextRun("a")
	*copy = time.Time{}
	assertDeadline(t, s, "a", start.Add(10*time.Minute))
	s.syncSchedule(timer, schedule, start.Add(11*time.Minute), []string{"a"})
	assertDeadline(t, s, "a", start.Add(21*time.Minute))
	assertDeadline(t, s, "b", start.Add(30*time.Minute))
	src, _ := cfg.Get().Source("a")
	if err := cfg.UpdateSource("a", src.Revision, func(src *config.Source) error {
		src.Name = "Renamed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.syncSchedule(timer, schedule, start.Add(12*time.Minute), nil)
	assertDeadline(t, s, "a", start.Add(21*time.Minute))
	if err := cfg.AddSource(intervalSource("c", 5)); err != nil {
		t.Fatal(err)
	}
	s.syncSchedule(timer, schedule, start.Add(13*time.Minute), nil)
	assertDeadline(t, s, "c", start.Add(18*time.Minute))
	assertDeadline(t, s, "a", start.Add(21*time.Minute))
	assertDeadline(t, s, "b", start.Add(30*time.Minute))
	src, _ = cfg.Get().Source("a")
	if err := cfg.UpdateSource("a", src.Revision, func(src *config.Source) error {
		minutes := 60
		src.ScanIntervalMinutes = &minutes
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.syncSchedule(timer, schedule, start.Add(14*time.Minute), nil)
	assertDeadline(t, s, "a", start.Add(74*time.Minute))
	assertDeadline(t, s, "b", start.Add(30*time.Minute))
	src, _ = cfg.Get().Source("b")
	if err := cfg.UpdateSource("b", src.Revision, func(src *config.Source) error { src.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	s.syncSchedule(timer, schedule, start.Add(15*time.Minute), nil)
	if s.SourceNextRun("b") != nil {
		t.Fatal("disabled source kept its deadline")
	}
	src, _ = cfg.Get().Source("b")
	if err := cfg.UpdateSource("b", src.Revision, func(src *config.Source) error { src.Enabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	s.syncSchedule(timer, schedule, start.Add(16*time.Minute), nil)
	assertDeadline(t, s, "b", start.Add(46*time.Minute))
	if !s.Status().NextRun.Equal(start.Add(18 * time.Minute)) {
		t.Fatal("NextRun does not reflect minimum source deadline")
	}
}

type intervalAdapter struct {
	source.Adapter
	src   config.Source
	calls map[string]int
}

func (a intervalAdapter) Snapshot(context.Context) (media.Snapshot, error) {
	a.calls[a.src.ID]++
	libID := media.Qualify(a.src.ID, "movies")
	// Distinct libraries own the same title; all references must survive each
	// partial refresh to keep combined comparison ownership accurate.
	return media.Snapshot{
		Items: []media.Item{{ID: media.Qualify(a.src.ID, "item"), Type: media.Movie, Name: "Owned", ProviderIDs: map[string]string{"Tmdb": "1"},
			References: []media.Reference{{ID: a.src.ID, Type: media.Jellyfin, LibraryID: libID, ItemID: "item"}}}},
		Libraries: []media.Library{{ID: libID, Name: a.src.ID, Type: "movies"}},
	}, nil
}

type intervalTMDB struct{ scanner.TMDBAPI }

func (intervalTMDB) Movie(context.Context, int64) (tmdb.Movie, error) {
	return tmdb.Movie{ID: 1, Title: "Owned", ReleaseDate: "2020-01-01"}, nil
}

func TestDueAndRequestedSourcesRefreshOnlySelectedInventory(t *testing.T) {
	cfg := intervalConfig(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	s := New(cfg, st, nil)
	calls := map[string]int{}
	s.sourceFactory = func(src config.Source) (source.Adapter, error) {
		return intervalAdapter{src: src, calls: calls}, nil
	}
	s.tmdbFactory = func(config.Settings) scanner.TMDBAPI { return intervalTMDB{} }
	ctx := context.Background()
	s.runScan(ctx, true)
	if !reflect.DeepEqual(calls, map[string]int{"a": 1, "b": 1, "manual": 1}) {
		t.Fatalf("full scan did not refresh all real enabled sources: %v", calls)
	}
	full, err := st.LatestSuccessfulRun(ctx)
	if err != nil || full == nil || full.Metadata.Mode != "refresh" {
		t.Fatalf("full scan history missing: %+v %v", full, err)
	}
	start := time.Now()
	timer, schedule := &recordedTimer{}, &scanSchedule{}
	s.syncSchedule(timer, schedule, start, nil)
	ids := s.dueSources(start.Add(10 * time.Minute))
	if !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatalf("wrong sources due: %v", ids)
	}
	refreshed := s.runSources(ctx, false, ids)
	s.syncSchedule(timer, schedule, start.Add(11*time.Minute), refreshed)
	assertDeadline(t, s, "b", start.Add(30*time.Minute))
	if !reflect.DeepEqual(calls, map[string]int{"a": 2, "b": 1, "manual": 1}) {
		t.Fatalf("scheduled scan refreshed unrelated source: %v", calls)
	}
	for range 3 {
		if err := s.TriggerSource("manual"); err != nil {
			t.Fatal(err)
		}
	}
	ids, epoch := s.takeSourceRequests()
	if !reflect.DeepEqual(ids, []string{"manual"}) {
		t.Fatalf("manual requests not coalesced: %v", ids)
	}
	refreshed = s.runSources(ctx, false, ids, epoch)
	s.syncSchedule(timer, schedule, start.Add(12*time.Minute), refreshed)
	assertDeadline(t, s, "a", start.Add(21*time.Minute))
	assertDeadline(t, s, "b", start.Add(30*time.Minute))
	if !reflect.DeepEqual(calls, map[string]int{"a": 2, "b": 1, "manual": 2}) {
		t.Fatalf("manual scan refreshed unrelated source: %v", calls)
	}
	partial, err := st.LatestSuccessfulRun(ctx)
	if err != nil || partial == nil || partial.Metadata.Mode != "recompute" {
		t.Fatalf("subset refresh masqueraded as full history: %+v %v", partial, err)
	}
	s.runScan(ctx, false)
	partial, err = st.LatestSuccessfulRun(ctx)
	if err != nil || partial == nil {
		t.Fatalf("recomputation failed: %v", err)
	}
	catalog, err := source.CatalogForSources(ctx, st, cfg.Get(), nil, s.sourceFactory)
	if err != nil || len(catalog.Items) != 1 || len(catalog.Items[0].References) != 3 {
		t.Fatalf("subset scan lost combined ownership: %+v %v", catalog, err)
	}
	if !reflect.DeepEqual(calls, map[string]int{"a": 2, "b": 1, "manual": 2}) {
		t.Fatalf("saved catalog/recompute contacted upstream: %v", calls)
	}
	// Both overdue sources are batched into a single comparison.
	ids = s.dueSources(start.Add(31 * time.Minute))
	slices.Sort(ids)
	if !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("due batch incomplete: %v", ids)
	}
	s.runSources(ctx, false, ids)
	batch, err := st.LatestSuccessfulRun(ctx)
	if err != nil || batch == nil || batch.ID != partial.ID+1 || batch.Metadata.Mode != "recompute" {
		t.Fatalf("due batch did not produce exactly one comparison: %+v %v", batch, err)
	}
	for range 25 {
		s.runSources(ctx, false, []string{"a"})
	}
	history, err := st.SuccessfulRunTotals(ctx, 20)
	if err != nil || len(history) != 1 || !history[0].FinishedAt.Equal(*full.FinishedAt) {
		t.Fatalf("subset refreshes polluted or pruned complete-refresh history: %+v %v", history, err)
	}
}

func TestSourceRequestValidationAndReset(t *testing.T) {
	cfg := intervalConfig(t)
	s := New(cfg, nil, nil)
	src, _ := cfg.Get().Source("b")
	if err := cfg.UpdateSource("b", src.Revision, func(src *config.Source) error { src.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"absent", "b", media.VirtualID} {
		if err := s.TriggerSource(id); err == nil {
			t.Fatalf("invalid source %q accepted", id)
		}
	}
	if len(s.sourceRequests) != 0 {
		t.Fatal("invalid request queued work")
	}
	if err := s.TriggerSource("manual"); err != nil {
		t.Fatal(err)
	}
	staleIDs, staleEpoch := s.takeSourceRequests()
	src, _ = cfg.Get().Source("manual")
	if err := cfg.RemoveSource("manual", src.Revision); err != nil {
		t.Fatal(err)
	}
	s.runSources(context.Background(), false, staleIDs, staleEpoch) // Validation precedes even local store work.
	if err := s.TriggerSource("a"); err != nil {
		t.Fatal(err)
	}
	ids, epoch := s.takeSourceRequests()
	if err := s.TriggerSource("a"); err != nil {
		t.Fatal(err)
	}
	timer, schedule := &recordedTimer{}, &scanSchedule{}
	s.syncSchedule(timer, schedule, time.Now(), nil)
	oldDue := time.Now().Add(-time.Second)
	lease, err := cfg.Gate().TryReset()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TriggerSource("a"); err == nil {
		t.Fatal("request admitted during reset")
	}
	s.ResetState()
	if s.SourceNextRun("a") != nil || len(s.sourceRequests) != 0 {
		t.Fatal("reset retained schedules or source queue")
	}
	// Consuming the reset signal before lease completion must schedule a retry.
	s.syncSchedule(timer, schedule, time.Now(), nil)
	if schedule.initialized || timer.resets[len(timer.resets)-1] != 100*time.Millisecond {
		t.Fatal("maintenance admission retry not armed")
	}
	lease.Finish(epoch+1, 0, false)
	s.runSources(context.Background(), false, ids, epoch) // nil store proves no work starts.
	if release, err := cfg.Gate().EnterScheduled(oldDue); err == nil {
		release()
		t.Fatal("old timer admitted after reset")
	}
	s.syncSchedule(timer, schedule, time.Now(), nil)
	if s.SourceNextRun("a") == nil {
		t.Fatal("schedule not restored after reset")
	}
}
