package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestInterfaceLanguageRedirectsAgainstEffectiveCookieLocale(t *testing.T) {
	s := featureServer(t)
	form := url.Values{"tab": {"interface"}, "locale": {"en"}, "tmdb_language": {"en-US"}, "tmdb_region": {"US"}}
	req := httptest.NewRequest("POST", "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: localeCookie, Value: "de"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Header().Get("X-Waim-Redirect") != "/settings?tab=interface" {
		t.Fatal("effective UI language change did not rebuild the tab")
	}

	form.Set("locale", "de")
	w = settingsPost(s, form, false)
	if w.Code != 303 || w.Header().Get("Location") != "/settings?tab=interface&saved=1" {
		t.Fatal("native language save did not redirect to the active tab")
	}
	get := httptest.NewRequest("GET", w.Header().Get("Location"), nil)
	for _, cookie := range w.Result().Cookies() {
		get.AddCookie(cookie)
	}
	rendered := httptest.NewRecorder()
	s.Handler().ServeHTTP(rendered, get)
	if !strings.Contains(rendered.Body.String(), `lang="de"`) || !strings.Contains(rendered.Body.String(), `name="tab" value="interface"`) {
		t.Fatal("native language save used stale request cookies")
	}
}
