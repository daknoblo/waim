package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
)

func TestWatchButtonsStayOnSuggestionsAndCollection(t *testing.T) {
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
		add := regexp.MustCompile(`<button\b[^>]*>\s*` + regexp.QuoteMeta(tr.T("sources.watch")) + `\s*</button>`)
		remove := regexp.MustCompile(`<button\b[^>]*>\s*` + regexp.QuoteMeta(tr.T("sources.unwatch")) + `\s*</button>`)
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
			name := filepath.Base(page)
			if remove.Match(html) != (name == "collection.html") {
				t.Fatalf("%s %s: remove action must stay on collection", locale, name)
			}
			if add.Match(html) != (name == "suggestions.html") {
				t.Fatalf("%s %s: demo add actions must appear only on suggestions (collection has saved entries only)", locale, name)
			}
		}
	}
}
