package source

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type failingIdentity struct{ Resolver }

func (failingIdentity) SearchMovie(context.Context, string, int) ([]tmdb.MovieSearchResult, error) {
	return nil, errors.New("private upstream response token=secret")
}

func TestFailedIdentityLookupIsOneConcreteSkippedEvent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	tracker := activity.New()
	run := tracker.Start(activity.Scan)
	ctx := activity.WithRun(context.Background(), run)
	catalog := media.Catalog{Items: []media.Item{{ID: "source/movie", Name: "Readable title", Type: media.Movie, ProductionYear: 2020, References: []media.Reference{{ID: "source", Type: media.Jellyfin, Name: "Home", LibraryID: "source/lib"}}}}}
	catalog, err = ResolveSavedCatalog(ctx, st, config.Defaults(), catalog, failingIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.New(catalog, nil, config.Defaults(), nil).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run.Finish(ctx, activity.Completed, len(result.Warnings))
	state := tracker.Snapshot()[0]
	if state.Skipped != 1 || len(state.Diagnostics) != 1 || state.Severity() != activity.Error {
		t.Fatalf("lookup failure duplicated or hidden: %+v", state)
	}
	d := state.Diagnostics[0]
	if d.Reason != activity.IdentityUnavailable || d.Current != "Readable title" || d.Subject != "Home" || d.Query != "/search/movie" || strings.Contains(d.Current, "secret") {
		t.Fatalf("identity failure lacks safe context: %+v", d)
	}
}
