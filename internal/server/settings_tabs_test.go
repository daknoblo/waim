package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/daknoblo/waim/internal/config"
)

func settingsPost(s *Server, form url.Values, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Trigger-Name", "scan_rate")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func TestSettingsTabsRenderOnlyOwnedFieldsAndSourceRedirect(t *testing.T) {
	s := featureServer(t)
	fields := map[string][]string{
		"media":     {"scan_interval", "scan_run_on_start", "scan_include_specials"},
		"metadata":  {"tmdb_api_key", "ai_enabled", "scan_rate", "cache_refresh_enabled", "scan_episode_ratings"},
		"interface": {"tmdb_language", "tmdb_region"},
		"other":     {"log_level"},
	}
	for _, locale := range []string{"en", "de"} {
		setTestLocale(t, s, locale)
		for tab, owned := range fields {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/settings?tab="+tab, nil)
			req.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
			s.Handler().ServeHTTP(w, req)
			html := w.Body.String()
			if w.Code != 200 || !strings.Contains(html, `name="tab" value="`+tab+`"`) || strings.Count(html, `aria-current="page"`) != 1 {
				t.Fatalf("bad tab %s: %s", tab, html)
			}
			for _, field := range owned {
				if !strings.Contains(html, `name="`+field+`"`) {
					t.Errorf("%s lacks %s", tab, field)
				}
			}
			for other, list := range fields {
				if other != tab {
					for _, field := range list {
						if strings.Contains(html, `name="`+field+`"`) {
							t.Errorf("%s contains unrelated %s", tab, field)
						}
					}
				}
			}
			if strings.Contains(html, `href="/sources"`) {
				t.Fatal("sources remains in navigation")
			}
			if !strings.Contains(html, s.catalog.For(locale).T("settings.tab."+tab)) {
				t.Fatal("tab not localized")
			}
			if tab == "interface" && !strings.Contains(html, `name="locale" aria-label="`+s.catalog.For(locale).T("settings.uiLanguage")+`" class="input w-full max-w-xs"`) {
				t.Fatal("interface language select lacks a localized accessible name")
			}
			if tab == "media" && !strings.Contains(html, `action="/sources"`) {
				t.Fatal("source forms not integrated")
			}
			if tab == "media" && (!strings.Contains(html, `data-source-edit="true"`) || !strings.Contains(html, s.catalog.For(locale).T("sources.unsavedPrompt"))) {
				t.Fatal("source edit forms lack localized unsaved-change protection")
			}
			if tab == "other" && (!strings.Contains(html, `action="/settings/reset"`) || !strings.Contains(html, s.cfg.Path()[:strings.LastIndex(s.cfg.Path(), "/")])) {
				t.Fatal("storage/reset details missing")
			}
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/sources", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/settings?tab=media" {
		t.Fatal("source bookmark not redirected")
	}
}

func TestSectionSavesPreserveOtherTabsAndKeys(t *testing.T) {
	s := featureServer(t)
	initial := s.cfg.Get()
	initial.Locale = "de"
	initial.TMDB.APIKey = "retained-tmdb"
	initial.TMDB.Language = "de-DE"
	initial.TMDB.Region = "DE"
	initial.AI.Enabled = true
	initial.AI.APIKey = "retained-ai"
	initial.AI.Endpoint = "https://fixture.invalid"
	initial.Scan.EpisodeRatings = true
	initial.Cache.RefreshEnabled = true
	if err := s.cfg.Save(initial); err != nil {
		t.Fatal(err)
	}
	w := settingsPost(s, url.Values{"tab": {"media"}, "scan_interval": {"123"}, "scan_include_specials": {"on"}}, true)
	if w.Header().Get("X-Waim-Save") != "ok" {
		t.Fatalf("save failed: %s", w.Body.String())
	}
	got := s.cfg.Get()
	if got.Scan.RunOnStart || !got.Scan.IncludeSpecials || got.Scan.IntervalMinutes != 123 || got.TMDB != initial.TMDB || got.AI != initial.AI || got.Cache != initial.Cache || !got.Scan.EpisodeRatings || got.Locale != "de" {
		t.Fatalf("media save crossed sections: %+v", got.Redacted())
	}
	w = settingsPost(s, url.Values{"tab": {"metadata"}, "scan_rate": {"2"}, "ai_endpoint": {initial.AI.Endpoint}, "cache_refresh_percent": {"5"}}, true)
	got = s.cfg.Get()
	if w.Header().Get("X-Waim-Save") != "ok" || got.AI.Enabled || got.Cache.RefreshEnabled || got.Scan.EpisodeRatings || got.AI.APIKey != "retained-ai" || got.TMDB.APIKey != "retained-tmdb" || got.TMDB.Language != "de-DE" || got.Scan.IntervalMinutes != 123 || !got.Scan.IncludeSpecials {
		t.Fatal("metadata checkbox/key isolation failed")
	}
	w = settingsPost(s, url.Values{"tab": {"other"}, "log_level": {"debug"}}, true)
	if w.Header().Get("X-Waim-Save") != "ok" || s.cfg.Get().LogLevel != "debug" || s.cfg.Get().Scan != got.Scan || s.cfg.Get().Locale != "de" {
		t.Fatal("other tab overwrote sections")
	}
}

func TestConcurrentTabSavesUseLatestConfigAndPreserveValidationDraft(t *testing.T) {
	s := featureServer(t)
	var wg sync.WaitGroup
	for _, form := range []url.Values{
		{"tab": {"media"}, "scan_interval": {"222"}, "scan_run_on_start": {"on"}},
		{"tab": {"other"}, "log_level": {"warn"}},
		{"tab": {"interface"}, "locale": {"de"}, "tmdb_language": {"de-DE"}, "tmdb_region": {"DE"}},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := settingsPost(s, form, true)
			if w.Header().Get("X-Waim-Save") != "ok" {
				t.Errorf("save failed: %s", w.Body.String())
			}
		}()
	}
	wg.Wait()
	got := s.cfg.Get()
	if got.Scan.IntervalMinutes != 222 || got.LogLevel != config.LogLevelWarn || got.Locale != "de" || got.TMDB.Region != "DE" {
		t.Fatalf("tab race lost settings: %+v", got.Redacted())
	}
	w := settingsPost(s, url.Values{"tab": {"media"}, "scan_interval": {"-3"}}, false)
	if !strings.Contains(w.Body.String(), `value="-3"`) || !strings.Contains(w.Body.String(), `name="tab" value="media"`) || s.cfg.Get().Scan.IntervalMinutes != 222 {
		t.Fatal("validation draft lost or invalid settings persisted")
	}
	w = settingsPost(s, url.Values{"tab": {"unknown"}, "log_level": {"debug"}}, true)
	if w.Code != 400 || s.cfg.Get().LogLevel != config.LogLevelWarn {
		t.Fatal("unknown section accepted")
	}
}

func TestLegacyLocaleActionCannotOverrideSettings(t *testing.T) {
	s := featureServer(t)
	req := httptest.NewRequest("POST", "/locale", strings.NewReader("locale=de"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://example.com/settings?tab=metadata")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || s.cfg.Get().Locale != "en" || len(w.Result().Cookies()) != 0 {
		t.Fatal("removed language endpoint still modifies browser or settings language")
	}
}
