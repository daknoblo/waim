package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Bounds for the numeric settings. They keep a typo (or a crafted form post)
// from turning into a request flood against Jellyfin/TMDB, or from scheduling
// work so far out that the feature silently stops working.
const (
	maxScanIntervalMinutes  = 7 * 24 * 60 // one week
	maxTMDBRateLimitRPS     = 50          // TMDB itself allows roughly 50 req/s
	maxCacheIntervalMinutes = 7 * 24 * 60 // one week
	maxCleanupMaxAgeDays    = 3650        // ten years
)

// validate performs basic sanity checks on settings before persisting.
func validate(s Settings) error {
	if err := validateEndpoint("jellyfin url", s.Jellyfin.URL); err != nil {
		return err
	}
	if err := validateEndpoint("ai endpoint", s.AI.Endpoint); err != nil {
		return err
	}
	if err := inRange("scan interval", s.Scan.IntervalMinutes, 0, maxScanIntervalMinutes); err != nil {
		return err
	}
	if s.Scan.TMDBRateLimitRPS < 0 || s.Scan.TMDBRateLimitRPS > maxTMDBRateLimitRPS {
		return fmt.Errorf("config: tmdb rate limit must be between 0 and %d", maxTMDBRateLimitRPS)
	}
	if err := inRange("cache refresh interval", s.Cache.RefreshIntervalMinutes, 1, maxCacheIntervalMinutes); err != nil {
		return err
	}
	if err := inRange("cache refresh percent", s.Cache.RefreshPercent, 1, 100); err != nil {
		return err
	}
	if err := inRange("cache cleanup max age", s.Cache.CleanupMaxAgeDays, 1, maxCleanupMaxAgeDays); err != nil {
		return err
	}
	return nil
}

// validateEndpoint accepts an empty value (feature not configured) or an
// absolute http(s) URL. Any other scheme is rejected so API keys cannot be sent
// to an unexpected protocol handler.
func validateEndpoint(name, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("config: invalid %s %q (expected an absolute http(s) URL)", name, raw)
	}
	return nil
}

func inRange(name string, v, lo, hi int) error {
	if v < lo || v > hi {
		return fmt.Errorf("config: %s must be between %d and %d", name, lo, hi)
	}
	return nil
}
