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

func TestFindingLinksAvoidDuplicateDestinations(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	virtual := (store.VirtualEntry{Type: media.Series, TMDBID: 42, Title: "Virtual series"}).Item()
	real := virtual
	real.WatchOnly = false
	realURL := "https://jellyfin.example/web/#/details?id=one"
	real.References = []media.Reference{{ID: "real", Type: media.Jellyfin, Name: "Jellyfin", LibraryID: "series", LibraryName: "Series", URL: realURL}}
	tmdbURL := WatchURL(media.Series, 42)
	previous := &store.ScanRun{Media: []store.MediaStat{{
		Type: store.MediaSeries, TMDBID: 42, Provenance: store.Provenance{References: real.References},
	}}}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, tc := range []struct {
			name       string
			items      []media.Item
			sourceURL  string
			wantSource bool
		}{
			{"virtual only", []media.Item{virtual}, tmdbURL, false},
			{"real server", []media.Item{real}, realURL, true},
			{"real and virtual", media.Merge([]media.Item{real, virtual}), tmdbURL, true},
			{"removed server fallback", []media.Item{virtual}, realURL, false},
			{"no source link", []media.Item{virtual}, "", false},
		} {
			ctx := WithProvenance(context.Background(), media.Catalog{Items: tc.items}, previous)
			row := FindingRow{Title: virtual.Name, TMDBLink: tmdbURL, JellyfinLink: tc.sourceURL, LibraryReferences: tc.items[0].References}
			var b bytes.Buffer
			if err := FindingsTable(tr, []FindingRow{row}, SortTitle, DirAsc, DataReady).Render(ctx, &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			if strings.Count(html, `href="`+tmdbURL+`"`) != 1 {
				t.Fatalf("%s %s: TMDB link missing or duplicated", locale, tc.name)
			}
			if strings.Contains(html, `class="link-jellyfin"`) != tc.wantSource {
				t.Fatalf("%s %s: unexpected source button visibility", locale, tc.name)
			}
			if tc.wantSource && !strings.Contains(html, `href="`+realURL+`"`) {
				t.Fatalf("%s: distinct real-server link lost", tc.name)
			}
			for _, forbidden := range []string{"/collection/add", "/collection/remove"} {
				if strings.Contains(html, forbidden) {
					t.Fatalf("%s: collection action restored on dashboard", tc.name)
				}
			}
		}
	}
}
