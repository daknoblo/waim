package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/media"
)

func resetPost(s *Server, values url.Values, foreign bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/settings/reset", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if foreign {
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func resetForm(s *Server, scope string) url.Values {
	return url.Values{"scope": {scope}, "confirm": {"yes"}, "confirmation": {"RESET"}, "_epoch": {s.cfg.Gate().Token()}}
}

func TestResetValidationCSRFAndBusyDoNotMutate(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	if err := s.store.TMDBCachePut(ctx, "/movie/7", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"scope", "confirm", "confirmation", "_epoch"} {
		form := resetForm(s, "metadata")
		form.Set(field, "invalid")
		w := resetPost(s, form, false)
		if w.Code != 400 && w.Code != 409 {
			t.Fatalf("invalid %s accepted: %d", field, w.Code)
		}
	}
	if w := resetPost(s, resetForm(s, "metadata"), true); w.Code != 403 {
		t.Fatal("cross-origin reset allowed")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/settings/reset", nil))
	if w.Code != 405 {
		t.Fatal("GET reset did not reject mutation")
	}
	release, err := s.cfg.Gate().Enter()
	if err != nil {
		t.Fatal(err)
	}
	w = resetPost(s, resetForm(s, "metadata"), false)
	release()
	if w.Code != 409 {
		t.Fatal("reset during active work allowed")
	}
	if count, err := s.store.TMDBCacheCount(ctx); err != nil || count != 1 {
		t.Fatal("rejected reset mutated cache")
	}
	if state, err := s.store.ResetState(ctx); err != nil || state.Epoch != 0 {
		t.Fatal("rejected reset advanced epoch")
	}
}

func TestFactoryResetClearsMemoryCookiesAndRejectsOldForms(t *testing.T) {
	s := featureServer(t)
	s.logs = logbuf.New(10)
	s.activities = activity.New()
	s.logs.Add(logbuf.Entry{Message: "Private title"})
	run := s.activities.Start(activity.Scan)
	run.Current("Private activity")
	run.Finish(context.Background(), activity.Completed, 0)
	before := s.cfg.Gate().Token()
	form := resetForm(s, "factory")
	w := resetPost(s, form, false)
	if w.Code != 303 || w.Header().Get("Location") != "/settings?tab=other&reset=factory" {
		t.Fatalf("reset failed: %d %s", w.Code, w.Body.String())
	}
	if len(s.logs.Entries()) != 0 || s.activities.Snapshot()[0].Current != "" {
		t.Fatal("factory reset retained user-facing memory")
	}
	settings := s.cfg.Get()
	if len(settings.Sources) != 1 || settings.Sources[0].ID != media.VirtualID || !settings.Sources[0].Enabled || settings.TMDB.APIKey != "" {
		t.Fatal("factory settings incorrect")
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == localeCookie && cookie.MaxAge != -1 {
			t.Fatal("locale cookie not expired")
		}
	}
	request := httptest.NewRequest("GET", "/settings?tab=interface", nil)
	request.AddCookie(&http.Cookie{Name: localeCookie, Value: "de"})
	if s.locale(request) != "en" {
		t.Fatal("another browser's stale language preference survived factory reset")
	}
	invalid := settingsPost(s, url.Values{"tab": {"other"}, "log_level": {"debug"}, "_epoch": {before}}, true)
	if invalid.Code != 409 || s.cfg.Get().LogLevel != "info" {
		t.Fatal("pre-reset form resurrected settings")
	}
	if w := settingsPost(s, url.Values{"tab": {"other"}, "log_level": {"warn"}, "_epoch": {s.cfg.Gate().Token()}}, true); w.Header().Get("X-Waim-Save") != "ok" {
		t.Fatal("fresh form blocked")
	}
	if w := resetPost(s, form, false); w.Code != 409 {
		t.Fatal("old reset form replayed")
	}
}

func TestSourceDiscoveryAndSettingsSaveHoldAdmission(t *testing.T) {
	s := featureServer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"Items":[]}`))
	}))
	defer upstream.Close()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequest("POST", "/sources", strings.NewReader(url.Values{"name": {"Busy source"}, "url": {upstream.URL}, "key": {"fixture"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		done <- w
	}()
	<-entered
	w := resetPost(s, resetForm(s, "media"), false)
	close(release)
	result := <-done
	if w.Code != 409 || result.Code != 303 {
		t.Fatalf("source/reset admission broken: reset=%d source=%d", w.Code, result.Code)
	}
	state, err := s.store.ResetState(context.Background())
	if err != nil || state.Epoch != 0 || len(s.cfg.Get().Sources) != 2 {
		t.Fatal("busy reset mutated source config")
	}
}
