package scanner

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
)

func TestMetadataFailuresRecordConcreteSafeReasons(t *testing.T) {
	tracker := activity.New()
	run := tracker.Start(activity.Scan)
	ctx := activity.WithRun(context.Background(), run)
	catalog := media.Catalog{Items: []media.Item{
		{ID: "movie", Name: "Missing movie metadata", Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": "99"}},
		{ID: "series", Name: "Missing series metadata", Type: media.Series, ProviderIDs: map[string]string{"Tmdb": "100"}},
	}}
	result, err := New(catalog, &fakeTMDB{}, config.Defaults(), nil).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run.Finish(ctx, activity.Completed, len(result.Warnings))
	state := tracker.Snapshot()[0]
	if state.Failures != 2 || len(state.Diagnostics) != 2 || state.Severity() != activity.Error {
		t.Fatalf("metadata failures lack reasons: %+v", state)
	}
	found := map[activity.Reason]string{}
	for _, d := range state.Diagnostics {
		found[d.Reason] = d.Current
		if d.Phase != activity.Metadata {
			t.Fatal("metadata phase absent")
		}
	}
	if found[activity.MovieUnavailable] != "Missing movie metadata" || found[activity.SeriesUnavailable] != "Missing series metadata" {
		t.Fatal("concrete failed titles missing")
	}
}
