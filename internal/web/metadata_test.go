package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
)

func TestMetadataProvidersKeepTMDBFieldsInOneDialogAndAISeparate(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		d := SettingsData{Layout: Layout{T: tr}, Tab: "metadata", Settings: config.Defaults(), HasTMDBKey: true}
		var b bytes.Buffer
		if err := Settings(d).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		if strings.Count(html, `data-metadata-provider=`) != 2 || !strings.Contains(html, `data-metadata-tiles`) || !strings.Contains(html, `sm:grid-cols-2`) {
			t.Fatal("metadata overview needs TMDB and IMDb tiles in a responsive grid")
		}
		start := strings.Index(html, `<dialog id="metadata-tmdb-dialog"`)
		if start < 0 {
			t.Fatal("TMDB dialog missing")
		}
		end := strings.Index(html[start:], "</dialog>") + start
		if end < start {
			t.Fatal("TMDB dialog missing")
		}
		dialog := html[start:end]
		for _, field := range []string{"tmdb_api_key", "scan_rate", "scan_episode_ratings", "cache_refresh_enabled", "cache_refresh_interval", "cache_refresh_percent", "cache_cleanup_enabled", "cache_cleanup_max_age"} {
			if strings.Count(html, `name="`+field+`"`) != 1 || !strings.Contains(dialog, `name="`+field+`"`) {
				t.Fatalf("field %s must exist only inside the TMDB dialog", field)
			}
		}
		if strings.Contains(dialog, `name="ai_`) || !strings.Contains(html, `name="ai_api_key"`) {
			t.Fatal("AI settings should stay separate below metadata tiles")
		}
		for _, asset := range []string{"tmdb-mark.svg", "imdb-mark.svg"} {
			if !strings.Contains(html, asset) {
				t.Fatal("local provider logo missing")
			}
		}
		if strings.Contains(html, `metadata-imdb-dialog`) || !strings.Contains(html, tr.T("metadata.imdbDescription")) {
			t.Fatal("IMDb must remain an explicit unavailable placeholder")
		}
		if strings.Count(html, `<form`) != 1 || strings.Contains(dialog, `<form`) {
			t.Fatal("provider dialog must reuse the tab form without nested/duplicate forms")
		}
	}
}
