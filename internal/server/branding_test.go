package server

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/version"
)

func TestBrandingAndDevelopmentTabTitles(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(10)
	s.assetVer = "branding-test"
	for _, build := range []struct {
		version string
		dev     bool
	}{
		{"dev-20260915-1616", true},
		{"dev", true},
		{"stable-20260915-1616", false},
		{"1.5.0", false},
		{"1.5.1", false},
		{"demo", false},
	} {
		for _, locale := range []string{"en", "de"} {
			setTestLocale(t, s, locale)
			for _, path := range []string{"/", "/logs", "/settings?tab=media", "/collection", "/about"} {
				t.Run(build.version+"/"+locale+path, func(t *testing.T) {
					s.info = version.Info{Version: build.version}
					req := httptest.NewRequest("GET", path, nil)
					req.AddCookie(&http.Cookie{Name: localeCookie, Value: locale})
					w := httptest.NewRecorder()
					s.Handler().ServeHTTP(w, req)
					tr := s.catalog.For(locale)
					title := tr.T("app.title") + " — " + tr.T("app.tagline")
					if build.dev {
						title = "[DEV] " + title
					}
					html := w.Body.String()
					if w.Code != 200 || !strings.Contains(html, "<title>"+title+"</title>") {
						t.Fatalf("incorrect tab title for %s", build.version)
					}
					if !strings.Contains(html, `rel="icon" type="image/svg+xml" sizes="any" href="/static/waim-icon.svg?v=branding-test"`) {
						t.Fatal("versioned SVG favicon missing")
					}
					if !strings.Contains(html, `src="/static/waim-icon.svg?v=branding-test" width="32" height="32"`) || !strings.Contains(html, `alt="" aria-hidden="true"`) {
						t.Fatal("decorative header icon missing")
					}
				})
			}
		}
	}
}

func TestIconIsServedAsEmbeddedSVG(t *testing.T) {
	s := featureServer(t)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/static/waim-icon.svg?v=test", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("favicon not served correctly: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	decoder := xml.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	root := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid SVG: %v", err)
		}
		if start, ok := token.(xml.StartElement); ok {
			if !root {
				if start.Name.Local != "svg" || start.Name.Space != "http://www.w3.org/2000/svg" {
					t.Fatal("icon root must be an SVG")
				}
				root = true
			}
			if start.Name.Local == "script" || start.Name.Local == "foreignObject" {
				t.Fatal("icon must not require active content")
			}
		}
	}
	if !root {
		t.Fatal("empty icon")
	}
}
