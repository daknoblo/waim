package source

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/tmdb"
)

type fixedSearch struct{ movies []tmdb.MovieSearchResult }

func (f fixedSearch) SearchMovie(context.Context, string, int) ([]tmdb.MovieSearchResult, error) {
	return f.movies, nil
}
func (f fixedSearch) SearchTV(context.Context, string, int) ([]tmdb.TVSearchResult, error) {
	return nil, nil
}

func TestResolutionRequiresUniqueExactTitleAndYear(t *testing.T) {
	item := media.Item{Type: media.Movie, Name: "Same title", ProductionYear: 2020}
	search := fixedSearch{movies: []tmdb.MovieSearchResult{{ID: 1, Title: "Same title", ReleaseDate: "2020-01-01"}, {ID: 2, Title: "Same title", ReleaseDate: "2021-01-01"}}}
	if id := ResolveID(context.Background(), item, search); id != 1 {
		t.Fatalf("unique match=%d", id)
	}
	search.movies[1].ReleaseDate = "2020-02-01"
	if id := ResolveID(context.Background(), item, search); id != 0 {
		t.Fatal("ambiguous search merged")
	}
	search.movies = search.movies[:1]
	item.ProductionYear = 0
	if id := ResolveID(context.Background(), item, search); id != 0 {
		t.Fatal("name-only identity accepted")
	}
}
