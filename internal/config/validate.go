package config

import (
	"fmt"
	"net/url"
	"strings"
)

// validate performs basic sanity checks on settings before persisting.
func validate(s Settings) error {
	if s.Jellyfin.URL != "" {
		u, err := url.Parse(strings.TrimSpace(s.Jellyfin.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("config: invalid jellyfin url %q", s.Jellyfin.URL)
		}
	}
	if s.Scan.IntervalMinutes < 0 {
		return fmt.Errorf("config: scan interval must be >= 0")
	}
	if s.Scan.TMDBRateLimitRPS < 0 {
		return fmt.Errorf("config: tmdb rate limit must be >= 0")
	}
	return nil
}
