package web

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
)

func TestSettingsManualSaveButtonsOnlyExistWithoutJavaScript(t *testing.T) {
	tr := testTranslator(t)
	withoutNoScript := regexp.MustCompile(`(?s)<noscript>.*?</noscript>`)
	for _, tab := range SettingsTabs {
		var b bytes.Buffer
		d := SettingsData{Tab: tab, Layout: Layout{T: tr}, Settings: config.Defaults(), Sources: SourcesData{Layout: Layout{T: tr}}}
		if err := Settings(d).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		if !strings.Contains(html, `<noscript><button class="btn-primary" type="submit">`+tr.T("settings.save")) {
			t.Fatal("manual fallback must remain available without JavaScript")
		}
		if strings.Contains(withoutNoScript.ReplaceAllString(html, ""), `>`+tr.T("settings.save")+`</button>`) {
			t.Fatalf("%s still shows a regular manual Save button", tab)
		}
		if !strings.Contains(html, `id="settings-retry"`) {
			t.Fatal("failed autosaves need an explicit retry action")
		}
	}
}

func TestSourceAutosaveMarkupAndProviderColors(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	noscript := regexp.MustCompile(`(?s)<noscript>.*?</noscript>`)
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		d := SourcesData{Layout: Layout{T: tr}, Sources: []config.Source{config.VirtualSource(), {ID: "jf", Name: "Home", Type: media.Jellyfin}, {ID: "em", Name: "Archive", Type: "emby"}, {ID: "px", Name: "Cinema", Type: "plex"}}}
		var b bytes.Buffer
		if err := Sources(d).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		for _, provider := range []string{"jellyfin", "emby", "plex"} {
			if !strings.Contains(html, "provider-"+provider) {
				t.Fatal("provider color missing")
			}
		}
		for _, expected := range []string{`data-source-autosave="true"`, `data-source-save-status`, `data-source-retry`, `data-source-library-options`, `data-source-name`, `data-source-schedule`} {
			if !strings.Contains(html, expected) {
				t.Fatalf("missing %s", expected)
			}
		}
		if strings.Contains(noscript.ReplaceAllString(html, ""), ">"+tr.T("sources.save")+"</button>") {
			t.Fatal("normal source dialog still offers a Save button")
		}
		if strings.Index(html, `data-source-dialog="source-add-dialog"`) > strings.Index(html, `data-source-tiles="true"`) {
			t.Fatal("Add source not alongside heading")
		}
	}
}
