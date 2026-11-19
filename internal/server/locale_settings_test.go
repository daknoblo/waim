package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/logbuf"
)

func TestSavedInterfaceLanguageIsAuthoritativeAcrossPagesAndClients(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(10)
	// No external suggestion requests are needed to exercise localization.
	settings := s.cfg.Get()
	settings.TMDB.APIKey = ""
	if err := s.cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"de", "en"} {
		setTestLocale(t, s, locale)
		opposite := "de"
		if locale == "de" {
			opposite = "en"
		}
		for _, cookieValue := range []string{"", opposite} {
			for _, path := range []string{"/", "/stats", "/suggestions", "/collection", "/logs", "/about", "/settings?tab=interface"} {
				req := httptest.NewRequest("GET", path, nil)
				req.Header.Set("Accept-Language", opposite)
				if cookieValue != "" {
					req.AddCookie(&http.Cookie{Name: localeCookie, Value: cookieValue})
					req.AddCookie(&http.Cookie{Name: localeGenerationCookie, Value: "0"})
				}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, req)
				html := w.Body.String()
				if w.Code != http.StatusOK || !strings.Contains(html, `lang="`+locale+`"`) {
					t.Fatalf("%s cookie=%q path=%s ignored saved locale", locale, cookieValue, path)
				}
				headerEnd := strings.Index(html, "</header>")
				if headerEnd < 0 || strings.Contains(html[:headerEnd], "<select") || strings.Contains(html, `action="/locale"`) {
					t.Fatal("header still contains a language selector")
				}
				if path == "/settings?tab=interface" && !strings.Contains(html, `name="locale"`) {
					t.Fatal("interface settings lost the only language control")
				}
			}
			req := httptest.NewRequest("GET", "/partials/activity", nil)
			req.AddCookie(&http.Cookie{Name: localeCookie, Value: opposite})
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if !strings.Contains(w.Body.String(), s.catalog.For(locale).T("activity.heading")) {
				t.Fatal("partial response used old language cookie")
			}
		}
	}
}

func TestInterfaceLanguageAutosavePersistsAndReloadsAllLabels(t *testing.T) {
	s := featureServer(t)
	old := s.cfg.Get()
	form := url.Values{"tab": {"interface"}, "locale": {"de"}, "tmdb_language": {old.TMDB.Language}, "tmdb_region": {old.TMDB.Region}}
	w := settingsPost(s, form, true)
	if w.Code != http.StatusNoContent || w.Header().Get("X-Waim-Save") != "ok" || w.Header().Get("X-Waim-Redirect") != "/settings?tab=interface" {
		t.Fatal("language autosave must request a full settings reload")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("language autosave still creates a browser override")
	}
	reloaded, err := config.Load(filepath.Dir(s.cfg.Path()))
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Get().Locale != "de" || reloaded.Get().TMDB.Language != old.TMDB.Language || reloaded.Get().TMDB.Region != old.TMDB.Region {
		t.Fatal("UI language was not durable or changed metadata localization")
	}
	s.cfg = reloaded
	req := httptest.NewRequest("GET", "/settings?tab=interface", nil)
	req.AddCookie(&http.Cookie{Name: localeCookie, Value: "en"})
	if s.locale(req) != "de" {
		t.Fatal("restart resurrected the browser language override")
	}
}
