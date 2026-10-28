package scanner

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
)

func TestMetadataFailureDoesNotEraseRealOwnership(t *testing.T) {
	c := media.Catalog{Items: []media.Item{
		{ID: "a/movie", Name: "Movie", Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": "99"}},
		{ID: "a/series", Name: "Series", Type: media.Series, ProviderIDs: map[string]string{"Tmdb": "100"}, Episodes: []media.Item{{ParentIndexNumber: intptr(1), IndexNumber: intptr(1)}}},
	}}
	res, err := New(c, &fakeTMDB{}, config.Defaults(), nil).Run(context.Background())
	if err != nil || len(res.Media) != 2 || len(res.Warnings) != 2 {
		t.Fatalf("metadata failure erased inventory: %+v %v", res, err)
	}
	episodes := 0
	for _, m := range res.Media {
		if m.WatchOnly || !m.MetadataUnavailable || !m.Unconfirmed {
			t.Fatal("unavailable metadata misrepresented ownership")
		}
		episodes += m.Episodes
	}
	if episodes != 1 {
		t.Fatal("known real episodes erased on metadata failure")
	}
}
