// Package httpx provides the HTTP behaviour shared by the upstream API clients
// (Jellyfin, TMDB and the AI endpoint): a redirect policy that never hands
// credentials to another host, and error sanitising that keeps query strings
// out of user-visible messages.
package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxRedirects mirrors the Go default. The point of the custom policy is the
// host check, not a lower limit.
const maxRedirects = 10

// NewClient returns an HTTP client that only follows redirects which stay on
// the original host.
//
// This matters because the clients authenticate with custom headers
// (`X-Emby-Token`, `api-key`). On a cross-host redirect Go strips only
// Authorization, Www-Authenticate, Cookie, Cookie2 and the Proxy-* variants —
// every other header, including those two, is copied verbatim to the new host.
// Following such a redirect would therefore hand the plaintext credential to
// whatever host the upstream names.
//
// Redirects that stay on the same hostname are still followed, so a reverse
// proxy upgrading http to https keeps working even when that moves the port
// (Jellyfin's own defaults are 8096 and 8920). The port is deliberately not
// part of the comparison: a redirect is issued by the origin server itself, so
// a malicious origin already holds the credential from the first request, and
// a hop to another port on the same host does not hand it to a third party.
// A downgrade from https to http is refused so a credential that was travelling
// encrypted is never replayed in the clear.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: sameHostOnly,
	}
}

func sameHostOnly(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	origin := via[0].URL
	if !strings.EqualFold(req.URL.Hostname(), origin.Hostname()) {
		return fmt.Errorf("refusing redirect from %q to a different host %q; configure the target address directly",
			origin.Hostname(), req.URL.Hostname())
	}
	if strings.EqualFold(origin.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("refusing redirect from https to %s: credentials would be sent unencrypted", req.URL.Scheme)
	}
	return nil
}

// SanitizeError removes the query string and any userinfo from the URL that Go
// embeds in transport errors.
//
// net/http reports these as *url.Error, whose Error() prints the full URL. Its
// internal stripPassword only masks userinfo passwords, so a credential passed
// as a query parameter (TMDB v3 keys are) would otherwise end up verbatim in
// error text that is rendered in the UI.
func SanitizeError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	cleaned := *ue
	if u, perr := url.Parse(ue.URL); perr == nil {
		u.RawQuery = ""
		u.Fragment = ""
		u.User = nil
		cleaned.URL = u.String()
	} else {
		cleaned.URL = "(redacted)"
	}
	return &cleaned
}
