package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/web"
)

func TestStatisticsAndPartialsOmitRepeatedServerReferences(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		run, findings := demoRun(), demoFindings()
		mediaCatalog, sources, _ := demoSources(run, findings)
		data := demoStats(tr, run, findings)
		ctx := web.WithProvenance(web.WithActionTranslator(context.Background(), tr), mediaCatalog, run)
		for name, component := range map[string]templ.Component{
			"page":     web.Stats(data),
			"upcoming": web.UpcomingContent(tr, data.Upcoming),
			"series":   web.SeriesDetailCharts(tr, data.Detail),
		} {
			var b bytes.Buffer
			if err := component.Render(ctx, &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			for _, source := range sources {
				if strings.Contains(html, `aria-label="`+source.Name+`"`) {
					t.Fatalf("%s %s: repeated source badge remains", locale, name)
				}
			}
			if name == "page" {
				if !strings.Contains(html, `href="`+run.Media[0].References[0].URL+`"`) {
					t.Fatal("statistics title links were lost")
				}
				for _, library := range data.LibraryRatings {
					if !strings.Contains(html, library.Name) {
						t.Fatal("section-level library reference was lost")
					}
				}
			}
		}
	}
}
