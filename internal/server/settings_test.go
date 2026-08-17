package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

// newTestServer returns a Server wired to a throwaway config directory.
// parseSettingsForm only touches s.cfg, so nothing else has to be built.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return &Server{cfg: mgr}
}

func parse(t *testing.T, s *Server, values url.Values) (config.Settings, []string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/settings", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	return s.parseSettingsForm(req)
}

// baseForm carries the numeric fields so validation-relevant values stay sane.
func baseForm() url.Values {
	return url.Values{
		"locale": {"en"}, "log_level": {"info"},
		"scan_interval": {"60"}, "scan_rate": {"1"},
		"cache_refresh_interval": {"15"}, "cache_refresh_percent": {"1"},
		"cache_cleanup_max_age": {"30"},
	}
}

// TestStoredKeyIsNotSentToANewHost is the regression test for the credential
// exfiltration path: pointing the endpoint at another host while leaving the
// key field blank must never produce the pair (new host, stored key), because
// saving probes the endpoint straight away.
func TestStoredKeyIsNotSentToANewHost(t *testing.T) {
	s := newTestServer(t)

	initial := baseForm()
	initial.Set("jellyfin_url", "http://jellyfin.local:8096")
	initial.Set("jellyfin_api_key", "jf-secret")
	initial.Set("ai_endpoint", "https://ai.example.com/v1/chat")
	initial.Set("ai_api_key", "ai-secret")
	ns, pending := parse(t, s, initial)
	if len(pending) != 0 {
		t.Fatalf("first configuration should not hold anything back, got %v", pending)
	}
	if err := s.cfg.Save(ns); err != nil {
		t.Fatalf("Save: %v", err)
	}

	attack := baseForm()
	attack.Set("jellyfin_url", "http://attacker.tld")
	attack.Set("jellyfin_api_key", "") // blank on purpose: inherit the stored key
	attack.Set("ai_endpoint", "https://ai.example.com/v1/chat")
	got, pending := parse(t, s, attack)

	if got.Jellyfin.URL == "http://attacker.tld" && got.Jellyfin.APIKey == "jf-secret" {
		t.Fatal("stored key would be sent to the attacker-supplied host")
	}
	if got.Jellyfin.URL != "http://jellyfin.local:8096" {
		t.Fatalf("endpoint should stay put until a key arrives, got %q", got.Jellyfin.URL)
	}
	if len(pending) != 1 || pending[0] != sectionJellyfin {
		t.Fatalf("expected the jellyfin section to be held back, got %v", pending)
	}
	if got.AI.APIKey != "ai-secret" || got.AI.Endpoint != "https://ai.example.com/v1/chat" {
		t.Fatal("unchanged AI endpoint should keep its stored key")
	}
}

// Unrelated fields of the same form must still be saved while a section is
// held back, otherwise a pending key would freeze the whole page.
func TestHeldBackSectionDoesNotBlockOtherFields(t *testing.T) {
	s := newTestServer(t)

	initial := baseForm()
	initial.Set("jellyfin_url", "http://jellyfin.local:8096")
	initial.Set("jellyfin_api_key", "jf-secret")
	ns, _ := parse(t, s, initial)
	if err := s.cfg.Save(ns); err != nil {
		t.Fatalf("Save: %v", err)
	}

	edit := baseForm()
	edit.Set("jellyfin_url", "http://attacker.tld") // held back
	edit.Set("scan_interval", "123")                // must still apply
	edit.Set("log_level", "debug")
	got, pending := parse(t, s, edit)

	if len(pending) != 1 {
		t.Fatalf("expected one held-back section, got %v", pending)
	}
	if got.Scan.IntervalMinutes != 123 {
		t.Errorf("unrelated field was not applied: interval=%d", got.Scan.IntervalMinutes)
	}
	if got.LogLevel != "debug" {
		t.Errorf("unrelated field was not applied: logLevel=%q", got.LogLevel)
	}
}

func TestStoredKeyIsKeptWhenHostIsUnchanged(t *testing.T) {
	s := newTestServer(t)

	initial := baseForm()
	initial.Set("jellyfin_url", "http://jellyfin.local:8096")
	initial.Set("jellyfin_api_key", "jf-secret")
	ns, _ := parse(t, s, initial)
	if err := s.cfg.Save(ns); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Same host, only the path changes and the key field stays blank.
	edit := baseForm()
	edit.Set("jellyfin_url", "http://jellyfin.local:8096/jellyfin")
	got, pending := parse(t, s, edit)

	if got.Jellyfin.APIKey != "jf-secret" {
		t.Fatalf("key was dropped although the host did not change: %q", got.Jellyfin.APIKey)
	}
	if got.Jellyfin.URL != "http://jellyfin.local:8096/jellyfin" {
		t.Fatalf("same-host edit was not applied: %q", got.Jellyfin.URL)
	}
	if len(pending) != 0 {
		t.Fatalf("unexpected hold-back: %v", pending)
	}
}

// A freshly supplied key is always honoured, even when the host changes.
func TestNewKeyIsAcceptedForANewHost(t *testing.T) {
	s := newTestServer(t)

	initial := baseForm()
	initial.Set("jellyfin_url", "http://jellyfin.local:8096")
	initial.Set("jellyfin_api_key", "jf-secret")
	ns, _ := parse(t, s, initial)
	if err := s.cfg.Save(ns); err != nil {
		t.Fatalf("Save: %v", err)
	}

	moved := baseForm()
	moved.Set("jellyfin_url", "http://newhost.local:8096")
	moved.Set("jellyfin_api_key", "jf-new")
	got, pending := parse(t, s, moved)

	if got.Jellyfin.APIKey != "jf-new" {
		t.Fatalf("explicit key not applied: %q", got.Jellyfin.APIKey)
	}
	if got.Jellyfin.URL != "http://newhost.local:8096" {
		t.Fatalf("new endpoint not applied: %q", got.Jellyfin.URL)
	}
	if len(pending) != 0 {
		t.Fatalf("unexpected hold-back: %v", pending)
	}
}

// The stored API key must never travel to a host the user just typed in.
// sameEndpointHost is what decides that, so pin its behaviour down.
func TestSameEndpointHost(t *testing.T) {
	cases := []struct {
		name     string
		old, new string
		want     bool
	}{
		{"unchanged", "http://jellyfin.local:8096", "http://jellyfin.local:8096", true},
		{"first time configured", "", "http://attacker.tld", true},
		{"scheme upgrade keeps host", "http://jellyfin.local", "https://jellyfin.local", true},
		{"path changes only", "http://jellyfin.local:8096", "http://jellyfin.local:8096/jf", true},
		{"case insensitive", "http://Jellyfin.Local:8096", "http://jellyfin.local:8096", true},
		{"different host", "http://jellyfin.local:8096", "http://attacker.tld", false},
		{"different port", "http://jellyfin.local:8096", "http://jellyfin.local:9999", false},
		{"cleared endpoint", "http://jellyfin.local:8096", "", false},
		{"subdomain is a different host", "http://jellyfin.local", "http://evil.jellyfin.local", false},
		{"host as a prefix is not enough", "http://jellyfin.local", "http://jellyfin.local.evil.tld", false},
		// A downgrade would put the reused key on the wire in cleartext.
		{"https downgraded to http", "https://jellyfin.local", "http://jellyfin.local", false},
		{"https stays https", "https://jellyfin.local", "https://jellyfin.local/jf", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameEndpointHost(tc.old, tc.new); got != tc.want {
				t.Fatalf("sameEndpointHost(%q, %q) = %v, want %v", tc.old, tc.new, got, tc.want)
			}
		})
	}
}

// An empty list must never be presented as a statement about the library
// unless a successful scan actually produced it.
func TestScanConfigured(t *testing.T) {
	full := func() config.Settings {
		s := config.Defaults()
		s.Jellyfin.URL = "http://jellyfin.local:8096"
		s.Jellyfin.APIKey = "jf"
		s.TMDB.APIKey = "td"
		s.Libraries = []config.Library{{ID: "l1", Name: "Movies", Enabled: true}}
		return s
	}
	if !scanConfigured(full()) {
		t.Fatal("a complete configuration should count as configured")
	}

	cases := map[string]func(*config.Settings){
		"no jellyfin url":  func(s *config.Settings) { s.Jellyfin.URL = "" },
		"no jellyfin key":  func(s *config.Settings) { s.Jellyfin.APIKey = "" },
		"no tmdb key":      func(s *config.Settings) { s.TMDB.APIKey = "" },
		"no libraries":     func(s *config.Settings) { s.Libraries = nil },
		"library disabled": func(s *config.Settings) { s.Libraries[0].Enabled = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := full()
			mutate(&s)
			if scanConfigured(s) {
				t.Fatalf("%s should not count as configured", name)
			}
		})
	}
}

func TestDataStateKeyMapping(t *testing.T) {
	cases := map[string]string{
		web.DataUnconfigured: "common.stateUnconfigured",
		web.DataScanning:     "common.stateScanning",
		web.DataNeverScanned: "common.stateNeverScanned",
	}
	cat, err := i18n.Load()
	if err != nil {
		t.Fatalf("i18n.Load: %v", err)
	}
	tr := cat.For("en")
	for state, key := range cases {
		if got := tr.T(key); got == "" || got == key {
			t.Fatalf("state %q maps to %q which has no translation", state, key)
		}
	}
}

// dataState is what keeps an empty list from being read as a result, so pin
// down every branch of it.
func TestDataState(t *testing.T) {
	newServer := func(t *testing.T) (*Server, *config.Manager) {
		t.Helper()
		dir := t.TempDir()
		mgr, err := config.Load(dir)
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		st, err := store.Open(filepath.Join(dir, "waim.db"))
		if err != nil {
			t.Fatalf("store.Open: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		return &Server{cfg: mgr, store: st, sched: scheduler.New(mgr, st, log)}, mgr
	}
	configure := func(t *testing.T, mgr *config.Manager) {
		t.Helper()
		cfg := mgr.Get()
		cfg.Jellyfin.URL = "http://jellyfin.local:8096"
		cfg.Jellyfin.APIKey = "jf"
		cfg.TMDB.APIKey = "td"
		cfg.Libraries = []config.Library{{ID: "l1", Name: "Movies", Enabled: true}}
		if err := mgr.Save(cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	t.Run("unconfigured", func(t *testing.T) {
		s, _ := newServer(t)
		if got := s.dataState(context.Background()); got != web.DataUnconfigured {
			t.Fatalf("got %q, want %q", got, web.DataUnconfigured)
		}
	})

	t.Run("never scanned once configured", func(t *testing.T) {
		s, mgr := newServer(t)
		configure(t, mgr)
		if got := s.dataState(context.Background()); got != web.DataNeverScanned {
			t.Fatalf("got %q, want %q", got, web.DataNeverScanned)
		}
	})

	t.Run("ready after a successful run", func(t *testing.T) {
		s, mgr := newServer(t)
		configure(t, mgr)
		ctx := context.Background()
		id, err := s.store.StartScanRun(ctx)
		if err != nil {
			t.Fatalf("StartScanRun: %v", err)
		}
		if err := s.store.FinishScanRun(ctx, id, store.StatusSuccess, "", 1, 10, 0, nil, nil, nil); err != nil {
			t.Fatalf("FinishScanRun: %v", err)
		}
		if got := s.dataState(ctx); got != web.DataReady {
			t.Fatalf("got %q, want %q", got, web.DataReady)
		}
	})
}

// Probing costs network calls (and real money for the AI endpoint), so only
// the section that actually changed may be checked.
func TestChangedSections(t *testing.T) {
	htmx := func(field string) *http.Request {
		r := httptest.NewRequest("POST", "/settings", nil)
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Trigger-Name", field)
		return r
	}

	cases := map[string][]string{
		"jellyfin_url":     {sectionJellyfin},
		"jellyfin_api_key": {sectionJellyfin},
		"tmdb_api_key":     {sectionTMDB},
		"tmdb_language":    {sectionTMDB},
		"ai_endpoint":      {sectionAI},
		"ai_enabled":       {sectionAI},
		"log_level":        nil,
		"scan_interval":    nil,
		"cache_percent":    nil,
		"":                 nil,
	}
	for field, want := range cases {
		t.Run("field "+field, func(t *testing.T) {
			got := changedSections(htmx(field), nil)
			if len(got) != len(want) {
				t.Fatalf("field %q selected %v, want %v", field, got, want)
			}
			for _, w := range want {
				if !got[w] {
					t.Fatalf("field %q did not select %q", field, w)
				}
			}
		})
	}

	t.Run("held back sections are always reported", func(t *testing.T) {
		got := changedSections(htmx("log_level"), []string{sectionAI})
		if !got[sectionAI] {
			t.Fatal("a held-back section must be reported even when untouched")
		}
	})

	t.Run("a plain form post checks everything", func(t *testing.T) {
		got := changedSections(httptest.NewRequest("POST", "/settings", nil), nil)
		for _, s := range []string{sectionJellyfin, sectionTMDB, sectionAI} {
			if !got[s] {
				t.Fatalf("non-HTMX post should check %q", s)
			}
		}
	})
}
