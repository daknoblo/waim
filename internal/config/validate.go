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
	if t := strings.TrimSpace(s.Search.URLTemplate); t != "" {
		u, err := url.Parse(t)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("config: invalid search url %q", s.Search.URLTemplate)
		}
		if !strings.Contains(t, "{query}") {
			return fmt.Errorf("config: search url must contain the {query} placeholder")
		}
		if strings.Contains(t, "{key}") && strings.TrimSpace(s.Search.APIKey) == "" {
			return fmt.Errorf("config: search url uses {key} but no api key is set")
		}
	}
	return nil
}
