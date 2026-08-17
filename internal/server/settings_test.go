package server

import (
	"context"
	"io"
	"log/slog"
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
// key field blank must not carry the stored key over, because saving triggers
// a connection test against that host.
func TestStoredKeyIsNotSentToANewHost(t *testing.T) {
	s := newTestServer(t)

	initial := baseForm()
	initial.Set("jellyfin_url", "http://jellyfin.local:8096")
	initial.Set("jellyfin_api_key", "jf-secret")
	initial.Set("ai_endpoint", "https://ai.example.com/v1/chat")
	initial.Set("ai_api_key", "ai-secret")
	ns, rebound := parse(t, s, initial)
	if len(rebound) != 0 {
		t.Fatalf("first configuration should not drop anything, got %v", rebound)
	}
	if err := s.cfg.Save(ns); err != nil {
		t.Fatalf("Save: %v", err)
	}

	attack := baseForm()
	attack.Set("jellyfin_url", "http://attacker.tld")
	attack.Set("jellyfin_api_key", "") // blank on purpose: inherit the stored key
	attack.Set("ai_endpoint", "https://ai.example.com/v1/chat")
	got, rebound := parse(t, s, attack)

	if got.Jellyfin.APIKey != "" {
		t.Fatalf("stored jellyfin key would be sent to a new host: %q", got.Jellyfin.APIKey)
	}
	if len(rebound) != 1 || rebound[0] != "Jellyfin" {
		t.Fatalf("expected a Jellyfin warning, got %v", rebound)
	}
	if got.AI.APIKey != "ai-secret" {
		t.Fatal("unchanged AI endpoint should keep its stored key")
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
	got, rebound := parse(t, s, edit)

	if got.Jellyfin.APIKey != "jf-secret" {
		t.Fatalf("key was dropped although the host did not change: %q", got.Jellyfin.APIKey)
	}
	if len(rebound) != 0 {
		t.Fatalf("unexpected warning: %v", rebound)
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
	got, rebound := parse(t, s, moved)

	if got.Jellyfin.APIKey != "jf-new" {
		t.Fatalf("explicit key not applied: %q", got.Jellyfin.APIKey)
	}
	if len(rebound) != 0 {
		t.Fatalf("unexpected warning: %v", rebound)
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
