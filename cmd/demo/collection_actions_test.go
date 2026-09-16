package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
)

func TestOnlyCollectionPageOffersWatchButtons(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		dir := t.TempDir()
		if err := run(dir, locale); err != nil {
			t.Fatal(err)
		}
		tr := catalog.For(locale)
		action := regexp.MustCompile(`<button\b[^>]*>\s*(?:` + regexp.QuoteMeta(tr.T("sources.watch")) + `|` + regexp.QuoteMeta(tr.T("sources.unwatch")) + `)\s*</button>`)
		pages, err := filepath.Glob(filepath.Join(dir, "*.html"))
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) == 0 {
			t.Fatal("demo did not render any pages")
		}
		for _, page := range pages {
			html, err := os.ReadFile(page)
			if err != nil {
				t.Fatal(err)
			}
			wantActions := filepath.Base(page) == "collection.html"
			if action.Match(html) != wantActions {
				t.Fatalf("%s %s: expected collection buttons=%v", locale, filepath.Base(page), wantActions)
			}
		}
	}
}
