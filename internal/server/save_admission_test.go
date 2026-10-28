package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
)

func TestSettingsSaveProbeHoldsResetAdmission(t *testing.T) {
	s := featureServer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"suggestions\":[]}"}}]}`))
	}))
	defer upstream.Close()
	form := url.Values{"tab": {"metadata"}, "ai_enabled": {"on"}, "ai_endpoint": {upstream.URL}, "ai_api_key": {"fixture-key"}}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequest("POST", "/settings?tab=metadata", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Trigger-Name", "ai_endpoint")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		done <- w
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("save probe never started")
	}
	w := resetPost(s, resetForm(s, "factory"), false)
	close(release)
	result := <-done
	if w.Code != 409 || result.Header().Get("X-Waim-Save") != "ok" || s.cfg.Get().AI.APIKey != "fixture-key" {
		t.Fatal("in-flight save/reset admission failed")
	}
}

func TestSourceValidationPreservesDraftWithoutRebindingKey(t *testing.T) {
	s := featureServer(t)
	if err := s.cfg.AddSource(config.Source{ID: "fixture", Type: media.Jellyfin, Name: "Original", Enabled: true, Jellyfin: config.JellyfinSettings{URL: "https://original.invalid", APIKey: "retained-secret"}}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"revision": {"1"}, "name": {"Draft name"}, "url": {"https://new.invalid"}, "user": {"draft-user"}, "enabled": {"on"}}
	r := httptest.NewRequest("POST", "/sources/fixture", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	html := w.Body.String()
	saved, _ := s.cfg.Get().Source("fixture")
	if !strings.Contains(html, `value="https://new.invalid"`) || !strings.Contains(html, `value="Draft name"`) || !strings.Contains(html, `name="tab" value="media"`) || !strings.Contains(html, `data-source-dirty="true"`) || strings.Contains(html, "retained-secret") || saved.Jellyfin.URL != "https://original.invalid" || saved.Jellyfin.APIKey != "retained-secret" {
		t.Fatal("source validation lost draft or rebound credentials")
	}
}
