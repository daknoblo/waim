package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
)

func TestSourceTilesAreVirtualFirstWithIndependentDialogs(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	minutes := 45
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		d := SourcesData{Layout: Layout{T: tr}, DefaultScanMinutes: 60, OpenID: "home", Sources: []config.Source{
			{ID: "home", Name: "Home", Type: media.Jellyfin, Enabled: true, ScanIntervalMinutes: &minutes},
			config.VirtualSource(),
			{ID: "archive", Name: "Archive", Type: media.Jellyfin},
		}}
		var b bytes.Buffer
		if err := Sources(d).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		if strings.Index(html, `data-source-tile="virtual"`) > strings.Index(html, `data-source-tile="home"`) || strings.Count(html, `data-source-tile=`) != 3 {
			t.Fatal("virtual source must lead the complete tile list")
		}
		if !strings.Contains(html, `sm:grid-cols-2`) {
			t.Fatal("source tiles are not two columns on wider screens")
		}
		for _, id := range []string{"home", "archive"} {
			for _, expected := range []string{`data-source-dialog="source-dialog-` + id + `"`, `id="source-dialog-` + id + `"`, `action="/sources/` + id + `/libraries"`, `action="/sources/` + id + `/test"`, `action="/sources/` + id + `/scan"`} {
				if !strings.Contains(html, expected) {
					t.Fatalf("source dialog missing %s", expected)
				}
			}
		}
		for _, expected := range []string{`name="scan_interval"`, `value="45"`, `value="emby" disabled`, `value="plex" disabled`, `id="source-add-dialog"`, `data-source-auto-open="true"`, tr.T("sources.scanEvery", 45)} {
			if !strings.Contains(html, expected) {
				t.Fatalf("source management missing %s", expected)
			}
		}
		if strings.Contains(html, `source-dialog-virtual`) || strings.Contains(html, `/sources/virtual/remove`) {
			t.Fatal("virtual collection must not expose source editing/removal")
		}
		if strings.LastIndex(html, `data-source-dialog="source-add-dialog"`) < strings.LastIndex(html, "</dialog>") {
			t.Fatal("add action must be below configured sources and dialogs")
		}
	}
}
