package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/suggest"
)

func TestSuggestionNoticesDoNotReturnInPageOrPartial(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		ctx := WithActionTranslator(context.Background(), tr)
		for _, tc := range []struct {
			message  string
			severity activity.Severity
		}{
			{"Unresolved title: Last Boy Scout - Das Ziel ist Ueberleben (1991)", activity.Warning},
			{"tmdb trending tv: request failed", activity.Error},
			{"ai: recommendation request failed", activity.Error},
		} {
			d := SuggestionsData{
				Layout:     Layout{T: tr, Active: NavSuggestions, HealthSeverity: tc.severity},
				Configured: true,
				Result: &suggest.Result{
					Errors:  []string{tc.message},
					Similar: []suggest.Item{{Title: "Available recommendation", TMDBLink: WatchURL(media.Series, 42)}},
				},
			}
			for _, fullPage := range []bool{true, false} {
				component := SuggestionsContent(d)
				if fullPage {
					component = Suggestions(d)
				}
				var b bytes.Buffer
				if err := component.Render(ctx, &b); err != nil {
					t.Fatal(err)
				}
				html := b.String()
				if strings.Contains(html, tc.message) || strings.Contains(html, "border-rose-500/40") {
					t.Fatalf("%s fullPage=%v: duplicate diagnostic banner rendered", locale, fullPage)
				}
				if !strings.Contains(html, "Available recommendation") || !strings.Contains(html, `action="/collection/add"`) {
					t.Fatal("removing the diagnostic banner hid recommendations or watch controls")
				}
				if fullPage && (!strings.Contains(html, `data-health="`+string(tc.severity)+`"`) || !strings.Contains(html, `href="/logs#diagnostics"`)) {
					t.Fatal("central diagnostics indicator must remain accessible")
				}
			}
		}
	}
}
