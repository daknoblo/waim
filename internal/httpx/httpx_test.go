package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestCrossHostRedirectDropsCredential is the regression test for the header
// leak: Go copies custom headers such as X-Emby-Token to the redirect target,
// so a cross-host redirect must not be followed at all.
//
// The attacker is addressed as "localhost" while the origin uses "127.0.0.1".
// Both resolve to this machine, so the request would genuinely succeed if the
// policy let it through — the assertion therefore proves the policy, not a
// connection failure.
func TestCrossHostRedirectDropsCredential(t *testing.T) {
	var leaked string
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-Emby-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()

	attackerURL, err := url.Parse(attacker.URL)
	if err != nil {
		t.Fatalf("parse attacker url: %v", err)
	}
	attackerAsOtherHost := "http://localhost:" + attackerURL.Port() + "/steal"

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attackerAsOtherHost, http.StatusFound)
	}))
	defer origin.Close()

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Emby-Token", "super-secret")

	resp, err := NewClient(5 * time.Second).Do(req) //nolint:bodyclose // error path returns no body
	if err == nil {
		resp.Body.Close()
		t.Fatal("cross-host redirect was followed")
	}
	if leaked != "" {
		t.Fatalf("credential leaked to the redirect target: %q", leaked)
	}
	if !strings.Contains(err.Error(), "different host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSameHostRedirectIsFollowed keeps the common reverse-proxy case working:
// an http to https upgrade on the same hostname must still succeed.
// TestSameHostRedirectIsFollowed keeps the common reverse-proxy case working.
func TestSameHostRedirectIsFollowed(t *testing.T) {
	var reached bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			reached = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer srv.Close()

	resp, err := NewClient(5 * time.Second).Get(srv.URL + "/start")
	if err != nil {
		t.Fatalf("same-host redirect was rejected: %v", err)
	}
	defer resp.Body.Close()
	if !reached || resp.StatusCode != http.StatusOK {
		t.Fatalf("redirect not followed: reached=%v status=%d", reached, resp.StatusCode)
	}
}

func TestRedirectLoopIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer srv.Close()

	resp, err := NewClient(5 * time.Second).Get(srv.URL) //nolint:bodyclose // error path returns no body
	if err == nil {
		resp.Body.Close()
		t.Fatal("endless same-host redirect was not stopped")
	}
	if !strings.Contains(err.Error(), "stopped after") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSanitizeErrorRemovesQuery covers the TMDB v3 key, which travels as a
// query parameter and would otherwise be printed by url.Error in the UI.
func TestSanitizeErrorRemovesQuery(t *testing.T) {
	raw := &url.Error{
		Op:  "Get",
		URL: "https://api.themoviedb.org/3/configuration?api_key=SECRETKEY&language=en-US",
		Err: errors.New("dial tcp: lookup failed"),
	}
	got := SanitizeError(raw).Error()
	if strings.Contains(got, "SECRETKEY") {
		t.Fatalf("api key survived sanitising: %s", got)
	}
	if !strings.Contains(got, "api.themoviedb.org/3/configuration") {
		t.Fatalf("useful context was lost: %s", got)
	}
	if !strings.Contains(got, "dial tcp") {
		t.Fatalf("underlying cause was lost: %s", got)
	}
}

func TestSanitizeErrorRemovesUserinfo(t *testing.T) {
	raw := &url.Error{
		Op:  "Get",
		URL: "https://user:hunter2@example.com/path",
		Err: errors.New("boom"),
	}
	if got := SanitizeError(raw).Error(); strings.Contains(got, "hunter2") {
		t.Fatalf("userinfo survived sanitising: %s", got)
	}
}

func TestSanitizeErrorPassesOtherErrors(t *testing.T) {
	base := errors.New("plain failure")
	if got := SanitizeError(base); !errors.Is(got, base) {
		t.Fatalf("non-url error was altered: %v", got)
	}
}

// TestRedirectPolicy exercises sameHostOnly directly, because the cases that
// matter most — an http to https upgrade and its reverse — cannot be produced
// with a plain httptest server.
func TestRedirectPolicy(t *testing.T) {
	cases := []struct {
		name      string
		from, to  string
		wantAllow bool
	}{
		{"same host and scheme", "http://jf.local:8096/a", "http://jf.local:8096/b", true},
		{"http upgraded to https", "http://jf.local", "https://jf.local", true},
		{"upgrade that also moves the port", "http://jf.local:8096", "https://jf.local:8920", true},
		{"explicit default port", "http://jf.local", "http://jf.local:80", true},
		{"case insensitive host", "http://JF.local", "http://jf.local", true},
		{"different host", "http://jf.local", "http://attacker.tld", false},
		{"attacker host as a suffix", "http://jf.local", "http://jf.local.attacker.tld", false},
		{"subdomain", "http://jf.local", "http://evil.jf.local", false},
		{"https downgraded to http", "https://jf.local", "http://jf.local", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, err := http.NewRequest(http.MethodGet, tc.from, nil)
			if err != nil {
				t.Fatalf("build from: %v", err)
			}
			to, err := http.NewRequest(http.MethodGet, tc.to, nil)
			if err != nil {
				t.Fatalf("build to: %v", err)
			}
			err = sameHostOnly(to, []*http.Request{from})
			if allowed := err == nil; allowed != tc.wantAllow {
				t.Fatalf("%s -> %s: allowed=%v want %v (err=%v)", tc.from, tc.to, allowed, tc.wantAllow, err)
			}
		})
	}
}
