package source

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

type subsetAdapter struct {
	Adapter
	id   string
	fail bool
}

func (a subsetAdapter) Snapshot(context.Context) (media.Snapshot, error) {
	if a.fail {
		return media.Snapshot{}, errors.New("private upstream failure")
	}
	return media.Snapshot{Items: []media.Item{{ID: a.id, Type: media.Movie, Name: a.id,
		References: []media.Reference{{ID: a.id, Type: media.Jellyfin}}}}}, nil
}

func TestCatalogForSourcesPreservesUnrefreshedAndFailedInventory(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	settings := config.Defaults()
	for _, id := range []string{"a", "b"} {
		settings.Sources = append(settings.Sources, config.Source{ID: id, Type: media.Jellyfin, Name: id, Enabled: true})
	}
	calls := map[string]int{}
	fail := false
	factory := func(src config.Source) (Adapter, error) {
		calls[src.ID]++
		return subsetAdapter{id: src.ID, fail: fail}, nil
	}
	ctx := context.Background()
	if _, err := Catalog(ctx, st, settings, true, factory); err != nil {
		t.Fatal(err)
	}
	b, _ := settings.Source("b")
	before, err := st.SourceSnapshot(ctx, b.ID, b.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	fail = true
	catalog, err := CatalogForSources(ctx, st, settings, []string{"a", "a", "absent", media.VirtualID}, factory)
	if err != nil || len(catalog.Items) != 2 || len(catalog.Warnings) != 1 {
		t.Fatalf("failed partial refresh lost inventory: %+v %v", catalog, err)
	}
	if !reflect.DeepEqual(calls, map[string]int{"a": 2, "b": 1}) {
		t.Fatalf("partial refresh contacted unrelated or duplicate providers: %v", calls)
	}
	after, err := st.SourceSnapshot(ctx, b.ID, b.Fingerprint())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("unrefreshed source snapshot changed: %v", err)
	}
	for _, ids := range [][]string{nil, {}} {
		catalog, err = CatalogForSources(ctx, st, settings, ids, factory)
		if err != nil || len(catalog.Items) != 2 || !reflect.DeepEqual(calls, map[string]int{"a": 2, "b": 1}) {
			t.Fatalf("empty refresh selection contacted upstream or lost inventory: %+v %v", catalog, err)
		}
	}
}
