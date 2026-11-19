package main

import (
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
)

func TestDemoSetupBannerKeepsMetadataDestination(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		html := staticHTML(`<a href="/settings?tab=metadata">Setup</a>`, catalog.For(locale))
		if !strings.Contains(html, `href="settings-metadata.html"`) || strings.Contains(html, `href="settings.html"`) {
			t.Fatal("demo setup link lost its metadata tab")
		}
	}
}
