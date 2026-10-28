package source

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/store"
)

func TestUnresolvedTitlesKeepConcreteDeduplicatedDiagnosticsThroughScan(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	settings := config.Defaults()
	settings.Sources = append(settings.Sources, config.Source{ID: "source", Name: "Source", Type: media.Jellyfin, Enabled: true})
	src := settings.Sources[1]
	items := []media.Item{
		{ID: "source/a", Name: "Lost movie", Type: media.Movie},
		{ID: "source/b", Name: "Lost series", Type: media.Series},
	}
	for i := range items {
		items[i].References = []media.Reference{{ID: src.ID, Name: src.Name, Type: media.Jellyfin, LibraryID: "source/lib"}}
	}
	ctx := context.Background()
	if err := st.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), &media.Snapshot{Items: items, Libraries: []media.Library{{ID: "source/lib", Name: "Library"}}}, ""); err != nil {
		t.Fatal(err)
	}
	tracker := activity.New()
	run := tracker.Start(activity.Scan)
	ctx = activity.WithRun(ctx, run)
	catalog, err := Catalog(ctx, st, settings, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = ResolveSavedCatalog(ctx, st, settings, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.New(catalog, nil, settings, nil).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run.Finish(ctx, activity.Completed, len(result.Warnings))
	state := tracker.Snapshot()[0]
	if state.Skipped != 2 || len(state.Diagnostics) != 2 || state.Status != activity.Partial {
		t.Fatalf("skip counters/reasons not deduplicated: %+v", state)
	}
	found := map[string]bool{}
	for _, d := range state.Diagnostics {
		if d.Reason != activity.Unresolved || d.Subject != "Source" || d.Phase != activity.Identity {
			t.Fatalf("skip lacks actionable context: %+v", d)
		}
		found[d.Current] = true
	}
	if !found["Lost movie"] || !found["Lost series"] {
		t.Fatal("unresolved title names absent")
	}
}
