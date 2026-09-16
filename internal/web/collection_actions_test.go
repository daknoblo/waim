package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestCollectionEntryWithoutIdentityDoesNotOfferMutation(t *testing.T) {
	var b bytes.Buffer
	if err := collectionEntry(testTranslator(t), store.VirtualEntry{Type: media.Movie, Title: "Unidentified"}, false, "").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "/collection/add") || strings.Contains(b.String(), "/collection/remove") {
		t.Fatal("entry without TMDB identity offered a collection mutation")
	}
}

func TestCollectionEntriesKeepExplicitAddAndRemoveActions(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, kind := range []string{media.Movie, media.Series} {
			entry := store.VirtualEntry{Type: kind, TMDBID: 42, Title: "Collection title"}
			for _, tc := range []struct {
				name          string
				saved, member bool
				want, not     string
				label         string
			}{
				{"new search result", false, false, "/collection/add", "/collection/remove", "sources.watch"},
				{"saved entry without catalog context", true, false, "/collection/remove", "/collection/add", "sources.unwatch"},
				{"search result already tracked", false, true, "/collection/remove", "/collection/add", "sources.unwatch"},
			} {
				ctx := context.Background()
				if tc.member {
					ctx = WithProvenance(ctx, media.Catalog{Items: []media.Item{entry.Item()}}, nil)
				}
				var b bytes.Buffer
				if err := collectionEntry(tr, entry, tc.saved, "").Render(ctx, &b); err != nil {
					t.Fatal(err)
				}
				html := b.String()
				for _, keep := range []string{`action="` + tc.want + `"`, tr.T(tc.label), `name="id" value="42"`, `name="kind" value="` + kind + `"`} {
					if !strings.Contains(html, keep) {
						t.Fatalf("%s %s %s lost %s", locale, kind, tc.name, keep)
					}
				}
				if strings.Contains(html, `action="`+tc.not+`"`) {
					t.Fatalf("%s offered the wrong collection operation", tc.name)
				}
			}
		}
	}
}
