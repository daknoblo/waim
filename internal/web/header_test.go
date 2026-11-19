package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
)

func TestHeaderPlacesOneIndicatorAfterAboutWithoutLanguageControl(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		for _, severity := range []activity.Severity{"", activity.Warning, activity.Error} {
			var b bytes.Buffer
			layout := Layout{T: catalog.For(locale), HealthSeverity: severity}
			if err := Page(layout, templ.Raw("")).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			about := strings.Index(html, `href="/about"`)
			indicator := strings.Index(html, `id="health-indicator"`)
			if about < 0 || indicator <= about {
				t.Fatal("indicator must follow About")
			}
			if strings.Contains(html, `action="/locale"`) || strings.Contains(html, "<select") {
				t.Fatal("header/mobile menu must not offer a language override")
			}
			if strings.Count(html, `id="health-indicator"`) != 1 || strings.Count(html, `hx-get="/partials/health-indicator"`) != 1 {
				t.Fatal("responsive header must not duplicate the indicator or its polling")
			}
			if strings.Contains(html, "items-baseline") || !strings.Contains(html, `class="flex items-center gap-3"`) {
				t.Fatal("logo and tagline must align by their vertical centers")
			}
			if !strings.Contains(html, `class="hidden md:contents"`) || !strings.Contains(html, `class="flex shrink-0 items-center empty:hidden"`) {
				t.Fatal("indicator must remain outside hidden mobile links and collapse when empty")
			}
		}
	}
}
