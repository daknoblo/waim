package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestStatsTitlesAndUpcomingTilesDoNotRepeatSourceBadges(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	virtual := (store.VirtualEntry{Type: media.Series, TMDBID: 42, Title: "Example series"}).Item()
	real := virtual
	real.WatchOnly = false
	realURL := "https://media.example/web/#/details?id=series"
	real.References = []media.Reference{{ID: "server", Type: media.Jellyfin, Name: "Example server", URL: realURL}}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		ctx := WithProvenance(WithActionTranslator(context.Background(), tr), media.Catalog{Items: media.Merge([]media.Item{real, virtual})}, nil)
		for _, component := range []templ.Component{
			titleLink(virtual.Name, WatchURL(media.Series, 42)),
			upcomingTile(UpcomingEntry{Title: virtual.Name, Link: WatchURL(media.Series, 42)}),
		} {
			var b bytes.Buffer
			if err := component.Render(ctx, &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			if strings.Contains(html, "Example server") || strings.Contains(html, tr.T("sources.collection")) {
				t.Fatal("statistics still repeat source/virtual badges beside a title")
			}
			if strings.Count(html, `href="`+realURL+`"`) != 1 || !strings.Contains(html, virtual.Name) {
				t.Fatal("the actual title link was removed or duplicated")
			}
		}
		var b bytes.Buffer
		if err := MediaSources(tr, WatchURL(media.Series, 42)).Render(ctx, &b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "Example server") {
			t.Fatal("statistics cleanup changed source references on other pages")
		}
	}
}
