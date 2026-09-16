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

func TestVirtualLabelsReplaceRedundantWatchOnlyHint(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, kind := range []string{media.Movie, media.Series} {
			item := (store.VirtualEntry{Type: kind, TMDBID: 42, Title: "Virtual title"}).Item()
			ctx := WithProvenance(context.Background(), media.Catalog{Items: []media.Item{item}}, nil)
			link := WatchURL(kind, 42)
			row := FindingRow{Title: item.Name, TMDBLink: link, LibraryID: media.VirtualID, LibraryReferences: item.References}
			for _, component := range []templ.Component{
				FindingsTable(tr, []FindingRow{row}, SortTitle, DirAsc, DataReady),
				MediaActions(tr, link),
			} {
				var b bytes.Buffer
				if err := component.Render(ctx, &b); err != nil {
					t.Fatal(err)
				}
				html := b.String()
				if strings.Contains(html, tr.T("sources.watchOnly")) {
					t.Fatalf("%s %s repeats the watch-only hint next to the virtual label", locale, kind)
				}
				for _, keep := range []string{tr.T("sources.collection"), tr.T("sources.unwatch"), `action="/collection/remove"`, `name="id" value="42"`} {
					if !strings.Contains(html, keep) {
						t.Fatalf("%s %s lost label or remove action: %s", locale, kind, keep)
					}
				}
			}
		}
	}
}
