package server

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
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
