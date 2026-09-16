package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoRendersAllSettingsTabsWithDisabledResets(t *testing.T) {
	for _, locale := range []string{"en", "de"} {
		dir := t.TempDir()
		if err := run(dir, locale); err != nil {
			t.Fatal(err)
		}
		for _, asset := range []string{"tmdb-mark.svg", "imdb-mark.svg"} {
			if _, err := os.Stat(filepath.Join(dir, "static", asset)); err != nil {
				t.Fatalf("provider logo missing from demo: %s", asset)
			}
		}

		for _, tab := range []string{"media", "metadata", "interface", "other"} {
			raw, err := os.ReadFile(filepath.Join(dir, "settings-"+tab+".html"))
			if err != nil {
				t.Fatal(err)
			}
			html := string(raw)
			for _, target := range []string{"media", "metadata", "interface", "other"} {
				if !strings.Contains(html, `href="settings-`+target+`.html"`) {
					t.Fatalf("missing static tab %s", target)
				}
			}
			if strings.Contains(html, `action="/settings/reset"`) || strings.Contains(html, `hx-post=`) {
				t.Fatal("live reset/action leaked into static demo")
			}
			if tab == "other" && strings.Count(html, `<fieldset disabled`) != 3 {
				t.Fatal("demo resets not disabled")
			}
			if tab == "metadata" && (!strings.Contains(html, `data-settings-dialog="metadata-tmdb-dialog"`) || !strings.Contains(html, `href="settings-metadata.html"`) || strings.Count(html, `data-metadata-provider=`) != 2) {
				t.Fatal("metadata demo did not preserve provider tiles and local dialog link")
			}
		}
	}
}
