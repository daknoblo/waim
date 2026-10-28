package media

import "testing"

func TestMergeRealVirtualAndEpisodeUnion(t *testing.T) {
	ep := func(season, number int) Item { return Item{ParentIndexNumber: &season, IndexNumber: &number} }
	a := Item{ID: Qualify("a", "same"), Name: "Title", Type: Series, ProviderIDs: map[string]string{"Tmdb": "42"}, References: []Reference{{ID: "a", URL: "https://a/item"}}, Episodes: []Item{ep(1, 1), ep(1, 2)}}
	b := a
	b.ID = Qualify("b", "same")
	b.References = []Reference{{ID: "b", URL: "https://b/item"}}
	b.Episodes = []Item{ep(1, 2), ep(2, 1)}
	v := a
	v.ID = "virtual"
	v.WatchOnly = true
	v.References = []Reference{{ID: VirtualID, Type: Virtual}}
	v.Episodes = nil
	for _, input := range [][]Item{{a, b, v}, {v, b, a}} {
		out := Merge(input)
		if len(out) != 1 || out[0].WatchOnly || len(out[0].References) != 3 || len(out[0].Episodes) != 3 {
			t.Fatalf("wrong union: %+v", out)
		}
		if Link(out[0].References) == "" {
			t.Fatal("real primary link missing")
		}
	}
	b.Type = Movie
	if len(Merge([]Item{a, b})) != 2 {
		t.Fatal("movie and series numeric IDs collided")
	}
	a.ProviderIDs, b.ProviderIDs = nil, nil
	b.Type = Series
	if len(Merge([]Item{a, b})) != 2 {
		t.Fatal("unresolved local identities collided")
	}
}
