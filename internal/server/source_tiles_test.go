package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
)

func sourceTilePost(s *Server, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestSourceDialogAddProviderIntervalAndDiscovery(t *testing.T) {
	s := featureServer(t)
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"Items":[{"Id":"films","Name":"Films","CollectionType":"movies"}]}`))
	}))
	defer upstream.Close()
	form := url.Values{"type": {media.Jellyfin}, "name": {"Living room"}, "url": {upstream.URL}, "key": {"fixture-secret"}, "scan_interval": {"37"}}
	for _, provider := range []string{"emby", "plex", "unknown"} {
		form.Set("type", provider)
		w := sourceTilePost(s, "/sources", form)
		if !strings.Contains(w.Body.String(), s.catalog.For("en").T("sources.unsupportedProvider")) || len(s.cfg.Get().Sources) != 1 || calls.Load() != 0 {
			t.Fatal("unsupported provider was saved/contacted or not explained")
		}
		if !strings.Contains(w.Body.String(), `id="source-add-dialog"`) || !strings.Contains(w.Body.String(), `data-source-auto-open="true"`) || strings.Contains(w.Body.String(), "fixture-secret") {
			t.Fatal("failed add must reopen safe draft in its dialog")
		}
	}
	form.Set("type", media.Jellyfin)
	for _, value := range []string{"", "-1", "1.5", "525601", "invalid"} {
		form.Set("scan_interval", value)
		w := sourceTilePost(s, "/sources", form)
		if !strings.Contains(w.Body.String(), s.catalog.For("en").T("sources.invalidInterval")) || len(s.cfg.Get().Sources) != 1 {
			t.Fatal("invalid source interval accepted or silently replaced")
		}
	}
	form.Set("scan_interval", "37")
	w := sourceTilePost(s, "/sources", form)
	src := s.cfg.Get().Sources[1]
	if w.Code != http.StatusSeeOther || src.ScanInterval(60) != 37 || len(src.Libraries) != 1 || calls.Load() != 1 {
		t.Fatal("source interval/discovery not saved")
	}
	if !strings.Contains(w.Header().Get("Location"), "source="+src.ID) {
		t.Fatal("new source should open its discovered libraries immediately")
	}
	get := httptest.NewRecorder()
	s.Handler().ServeHTTP(get, httptest.NewRequest("GET", w.Header().Get("Location"), nil))
	if strings.Count(get.Body.String(), `data-source-auto-open="true"`) != 1 {
		t.Fatal("redirect must open exactly the new source dialog")
	}
}

func TestSourceDialogActionsKeepFeedbackInSelectedSource(t *testing.T) {
	s := featureServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Items":[],"ServerName":"Fixture","Version":"10.10.0"}`))
	}))
	defer upstream.Close()
	if err := s.cfg.AddSource(config.Source{ID: "local", Type: media.Jellyfin, Name: "Local", Enabled: true,
		Jellyfin: config.JellyfinSettings{URL: upstream.URL, APIKey: "secret"}, Libraries: []config.Library{{ID: "lib", Name: "Library", Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/sources/local/test", "/sources/local/scan"} {
		w := sourceTilePost(s, path, url.Values{})
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="source-dialog-local"`) || strings.Count(w.Body.String(), `data-source-auto-open="true"`) != 1 {
			t.Fatalf("%s did not return feedback inside the selected source dialog", path)
		}
	}
	w := sourceTilePost(s, "/sources/local/libraries", url.Values{})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "source=local") {
		t.Fatal("library refresh did not reopen the selected dialog")
	}
	req := httptest.NewRequest("POST", "/sources/local/scan", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	cross := httptest.NewRecorder()
	s.Handler().ServeHTTP(cross, req)
	if cross.Code != http.StatusForbidden {
		t.Fatal("source scan route lost CSRF protection")
	}
}

func TestSourceIntervalUpdateStaysIndependentAndPreservesSnapshot(t *testing.T) {
	s := featureServer(t)
	var zero, ninety = 0, 90
	if err := s.cfg.AddSource(config.Source{ID: "other", Name: "Other", Type: media.Jellyfin, Enabled: true, ScanIntervalMinutes: &ninety}); err != nil {
		t.Fatal(err)
	}
	src := auditSource(t, s, nil)
	before := src.Fingerprint()
	form := url.Values{"revision": {strconv.FormatInt(src.Revision, 10)}, "name": {src.Name}, "url": {src.Jellyfin.URL}, "enabled": {"on"}, "scan_interval": {strconv.Itoa(zero)}, "library": {"lib"}}
	w := sourceTilePost(s, "/sources/"+src.ID, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("source interval update failed: %s", w.Body.String())
	}
	updated, _ := s.cfg.Get().Source(src.ID)
	other, _ := s.cfg.Get().Source("other")
	if updated.ScanInterval(60) != 0 || other.ScanInterval(60) != 90 || updated.Fingerprint() != before {
		t.Fatal("interval edit changed ownership identity or another source")
	}
	snapshot, err := s.store.SourceSnapshot(context.Background(), src.ID, updated.Fingerprint())
	if err != nil || snapshot.Snapshot == nil {
		t.Fatal("changing the schedule discarded the saved inventory")
	}
	form.Set("scan_interval", "bad-input")
	form.Set("revision", strconv.FormatInt(updated.Revision, 10))
	w = sourceTilePost(s, "/sources/"+src.ID, form)
	if !strings.Contains(w.Body.String(), `value="bad-input"`) || !strings.Contains(w.Body.String(), `data-source-auto-open="true"`) {
		t.Fatal("invalid interval draft/dialog was not retained")
	}
	after, _ := s.cfg.Get().Source(src.ID)
	if after.ScanInterval(60) != 0 {
		t.Fatal("invalid input changed saved interval")
	}
}
