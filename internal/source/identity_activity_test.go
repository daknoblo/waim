package source

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/tmdb"
)

type observingResolver struct {
	Resolver
	tracker *activity.Tracker
	t       *testing.T
}

func (r observingResolver) SearchMovie(_ context.Context, title string, _ int) ([]tmdb.MovieSearchResult, error) {
	state := r.tracker.Snapshot()[0]
	if state.Status != activity.Running || state.Phase != activity.Identity || !state.Known || state.Total != 2 || state.Done != 1 || state.Current != title {
		r.t.Errorf("identity query has no live scope: %+v", state)
	}
	return []tmdb.MovieSearchResult{{ID: 7, Title: title, ReleaseDate: "2020-01-01"}}, nil
}

func TestIdentityResolutionReportsActualCatalogScope(t *testing.T) {
	tracker := activity.New()
	ctx := activity.WithRun(context.Background(), tracker.Start(activity.Scan))
	catalog := media.Catalog{Items: []media.Item{
		{ID: "a/known", Name: "Movie", Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": "7"}},
		{ID: "b/unknown", Name: "Movie", Type: media.Movie, ProductionYear: 2020},
	}}
	resolved, updates := resolveCatalog(ctx, catalog, observingResolver{tracker: tracker, t: t})
	if len(resolved.Items) != 1 || len(updates) != 1 || tracker.Snapshot()[0].Done != 2 {
		t.Fatal("resolution did not finish its pre-merge scope")
	}
}
