package web

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
)

func TestCachedSuggestionsStayVisibleAndRefreshButtonRecovers(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	button := regexp.MustCompile(`<button[^>]*id="suggestion-refresh"[^>]*>`)
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, running := range []bool{false, true} {
			d := SuggestionsData{
				Layout: Layout{T: tr}, Configured: true, Running: running,
				Result: &suggest.Result{Trending: []suggest.Item{{Title: "Cached pick", TMDBLink: WatchURL(media.Series, 42)}}},
			}
			var b bytes.Buffer
			if err := SuggestionsContent(d).Render(WithActionTranslator(context.Background(), tr), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			if !strings.Contains(html, "Cached pick") || !strings.Contains(html, `action="/collection/add"`) || strings.Count(html, `id="suggestions"`) != 1 {
				t.Fatal("refresh hid the cache or watch action, or duplicated the swap region")
			}
			control := button.FindString(html)
			if control == "" || strings.Contains(control, "disabled") != running {
				t.Fatal("refresh button did not reflect running/completed state")
			}
			if strings.Contains(html, `hx-get="/partials/suggestions"`) != running {
				t.Fatal("only an active refresh should poll")
			}
			if running && (!strings.Contains(html, tr.T("suggestions.refreshingCached")) || !strings.Contains(html, `hx-target="#suggestions"`)) {
				t.Fatal("cached refresh lost its background status/whole-section swap")
			}
		}
	}
}

func TestCachedSuggestionsFilterLiveOwnershipWithoutMutation(t *testing.T) {
	virtual := (store.VirtualEntry{Type: media.Series, TMDBID: 42, Title: "Tracked"}).Item()
	real := virtual
	real.WatchOnly = false
	real.References = []media.Reference{{ID: "server", Type: media.Jellyfin}}
	items := []suggest.Item{
		{TMDBLink: WatchURL(media.Series, 42), Title: "Tracked"},
		{TMDBLink: WatchURL(media.Series, 43), Title: "New"},
	}
	ctx := WithProvenance(context.Background(), media.Catalog{Items: []media.Item{virtual}}, nil)
	if got := unownedSuggestions(ctx, items); len(got) != 2 {
		t.Fatal("watch-only titles should remain in suggestions")
	}
	ctx = WithProvenance(context.Background(), media.Catalog{Items: media.Merge([]media.Item{virtual, real})}, nil)
	filtered := unownedSuggestions(ctx, items)
	if len(filtered) != 1 || filtered[0].Title != "New" || items[0].Title != "Tracked" || len(items) != 2 {
		t.Fatal("live ownership was not filtered or modified the shared cached result")
	}
}
