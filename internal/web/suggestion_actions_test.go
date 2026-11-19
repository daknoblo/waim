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

func TestSuggestionActionsAddOnlyAndReflectMembership(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, kind := range []string{media.Movie, media.Series} {
			entry := store.VirtualEntry{Type: kind, TMDBID: 42, Title: "Suggestion"}
			for _, tracked := range []bool{false, true} {
				ctx := context.Background()
				if tracked {
					ctx = WithProvenance(ctx, media.Catalog{Items: []media.Item{entry.Item()}}, nil)
				}
				var b bytes.Buffer
				if err := SuggestionActions(tr, WatchURL(kind, 42)).Render(ctx, &b); err != nil {
					t.Fatal(err)
				}
				html := b.String()
				if strings.Contains(html, "/collection/remove") || strings.Contains(html, tr.T("sources.unwatch")) {
					t.Fatal("suggestions must never offer removal")
				}
				if strings.Contains(html, `action="/collection/add"`) == tracked {
					t.Fatalf("%s %s: incorrect add action for tracked=%v", locale, kind, tracked)
				}
				if tracked {
					if !strings.Contains(html, tr.T("sources.alreadyTracked")) || !strings.Contains(html, `href="/collection"`) {
						t.Fatal("tracked suggestion needs a management link")
					}
				} else if !strings.Contains(html, `name="kind" value="`+kind+`"`) || !strings.Contains(html, `name="id" value="42"`) {
					t.Fatal("suggestion form lost canonical identity")
				}
			}
		}
	}
}

func TestCollectionCompleteStatusOffersRemovalOnlyThere(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, status := range []string{CollectionComplete, CollectionPartial, CollectionUnverified} {
			var b bytes.Buffer
			entry := store.VirtualEntry{Type: media.Series, TMDBID: 42, Title: "Owned series"}
			if err := collectionEntry(tr, entry, true, status).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			if !strings.Contains(html, `data-collection-ownership="`+status+`"`) || !strings.Contains(html, `action="/collection/remove"`) {
				t.Fatal("collection status or manual removal missing")
			}
			if strings.Contains(html, tr.T("sources.fullyOwned")) != (status == CollectionComplete) {
				t.Fatal("unverified/partial inventory shown as complete")
			}
		}
	}
}
