package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMetadataDialogRoutesAndAssets(t *testing.T) {
	s := featureServer(t)
	for _, locale := range []string{"en", "de"} {
		setTestLocale(t, s, locale)
		for _, provider := range []string{"tmdb", "imdb", "unknown"} {
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/settings?tab=metadata&provider="+provider, nil))
			if w.Code != http.StatusOK {
				t.Fatal("metadata settings did not render")
			}
			wantOpen := provider == "tmdb"
			if strings.Contains(w.Body.String(), `data-settings-auto-open="true"`) != wantOpen {
				t.Fatal("only TMDB should open a metadata dialog")
			}
		}
	}
	for _, asset := range []string{"tmdb-mark.svg", "imdb-mark.svg"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/static/"+asset, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "image/svg+xml") || !strings.Contains(w.Body.String(), "<svg") {
			t.Fatalf("provider mark not served: %s", asset)
		}
	}
}

func TestNativeMetadataDialogSaveRetainsValidationDraft(t *testing.T) {
	s := featureServer(t)
	form := url.Values{"tab": {"metadata"}, "cache_refresh_percent": {"0"}}
	req := httptest.NewRequest("POST", "/settings?tab=metadata&provider=tmdb", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `data-settings-auto-open="true"`) || !strings.Contains(w.Body.String(), `name="cache_refresh_percent" value="0"`) {
		t.Fatal("native failure must reopen the dialog with the invalid draft")
	}
}

func TestMetadataDialogAutosavePreservesAIAndProvidesInlineFeedback(t *testing.T) {
	s := featureServer(t)
	initial := s.cfg.Get()
	initial.AI.Enabled = true
	initial.AI.Endpoint = "https://ai.example"
	initial.AI.APIKey = "keep-ai-secret"
	initial.AI.Model = "example-model"
	if err := s.cfg.Save(initial); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"tab": {"metadata"}, "scan_rate": {"3"}, "scan_episode_ratings": {"on"},
		"cache_refresh_enabled": {"on"}, "cache_refresh_interval": {"20"}, "cache_refresh_percent": {"5"},
		"cache_cleanup_enabled": {"on"}, "cache_cleanup_max_age": {"45"},
		"ai_enabled": {"on"}, "ai_endpoint": {initial.AI.Endpoint}, "ai_model": {initial.AI.Model}}
	w := settingsPost(s, form, true)
	if w.Header().Get("X-Waim-Save") != "ok" {
		t.Fatal("dialog save failed")
	}
	for _, id := range []string{"save-indicator", "metadata-save-indicator", "metadata-tmdb-status"} {
		if !strings.Contains(w.Body.String(), `id="`+id+`"`) {
			t.Fatal("feedback host missing: " + id)
		}
	}
	saved := s.cfg.Get()
	if saved.AI != initial.AI || saved.TMDB != initial.TMDB || saved.Scan.TMDBRateLimitRPS != 3 || !saved.Scan.EpisodeRatings || saved.Cache.RefreshPercent != 5 {
		t.Fatal("dialog save altered hidden AI settings/credentials or lost TMDB fields")
	}
	if strings.Contains(w.Body.String(), "keep-ai-secret") || strings.Contains(w.Body.String(), initial.TMDB.APIKey) {
		t.Fatal("autosave echoed saved credentials")
	}
	form.Set("cache_refresh_percent", "0")
	w = settingsPost(s, form, true)
	if w.Header().Get("X-Waim-Save") != "failed" || !strings.Contains(w.Body.String(), `id="metadata-save-indicator"`) || s.cfg.Get().Cache.RefreshPercent != 5 {
		t.Fatal("failed dialog save not surfaced without changing stored settings")
	}
}
