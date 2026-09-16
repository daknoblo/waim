// Package config defines the application settings, their on-disk JSON
// representation, and a thread-safe manager that transparently encrypts API
// keys at rest.
package config

import (
	"log/slog"
	"os"
	"strings"
)

// SchemaVersion is the current on-disk config schema version. Bump it when the
// stored layout changes in a backwards-incompatible way.
//
// Version 2 dropped the Argon2id salt: the encryption key is no longer derived
// from a passphrase but generated once and stored in the data directory.
const SchemaVersion = 3

// Supported UI locales.
const (
	LocaleEN = "en"
	LocaleDE = "de"
)

// Supported log levels (UI-selectable verbosity).
const (
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelDebug = "debug"
)

// Library represents a Jellyfin library (collection folder) and whether it is
// included in scans.
type Library struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`    // e.g. "movies", "tvshows"
	Enabled bool   `json:"enabled"` // include in scans
}

// JellyfinSettings holds connection details for the Jellyfin server.
type JellyfinSettings struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"` // plaintext in memory; encrypted on disk
	UserID string `json:"userId"`
}

// TMDBSettings holds connection details for The Movie Database.
type TMDBSettings struct {
	APIKey   string `json:"apiKey"` // plaintext in memory; encrypted on disk
	Language string `json:"language"`
	Region   string `json:"region"`
}

// AISettings holds an optional remote AI endpoint (OpenAI/Azure-compatible)
// used to generate library-based suggestions.
type AISettings struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"apiKey"` // plaintext in memory; encrypted on disk
	Model    string `json:"model"`
}

// ScanSettings controls scan scheduling and TMDB request behaviour.
type ScanSettings struct {
	IntervalMinutes  int     `json:"intervalMinutes"`  // legacy default inherited by new sources
	RunOnStart       bool    `json:"runOnStart"`       // scan once at startup
	TMDBRateLimitRPS float64 `json:"tmdbRateLimitRps"` // requests per second
	IncludeSpecials  bool    `json:"includeSpecials"`  // include season 0 / specials
	EpisodeRatings   bool    `json:"episodeRatings"`   // collect per-episode ratings (one TMDB call per season)
}

// CacheSettings controls the background refresh of cached TMDB responses. The
// refresher periodically re-fetches the oldest slice of the cache so data stays
// current without re-loading everything on each scan, and a nightly cleanup
// prunes entries no longer used by any scan or suggestion.
type CacheSettings struct {
	RefreshEnabled         bool `json:"refreshEnabled"`         // run the background refresher
	RefreshIntervalMinutes int  `json:"refreshIntervalMinutes"` // minutes between refresh batches
	RefreshPercent         int  `json:"refreshPercent"`         // percent of cache refreshed per batch (1-100)
	CleanupEnabled         bool `json:"cleanupEnabled"`         // run the nightly orphan cleanup
	CleanupMaxAgeDays      int  `json:"cleanupMaxAgeDays"`      // remove entries unused for this many days
}

// Settings is the full in-memory configuration with decrypted API keys.
type Settings struct {
	Sources   []Source         `json:"sources"`
	Locale    string           `json:"locale"`
	LogLevel  string           `json:"logLevel"`
	Jellyfin  JellyfinSettings `json:"-"` // legacy fixture compatibility; not used at runtime
	TMDB      TMDBSettings     `json:"tmdb"`
	AI        AISettings       `json:"ai"`
	Scan      ScanSettings     `json:"scan"`
	Cache     CacheSettings    `json:"cache"`
	Libraries []Library        `json:"-"` // legacy fixture compatibility
}

// Defaults returns a Settings value with sensible defaults.
func Defaults() Settings {
	return Settings{
		Sources:  []Source{VirtualSource()},
		Locale:   LocaleEN,
		LogLevel: LogLevelInfo,
		Jellyfin: JellyfinSettings{
			URL: "",
		},
		TMDB: TMDBSettings{
			Language: "en-US",
			Region:   "US",
		},
		Scan: ScanSettings{
			IntervalMinutes:  60,
			RunOnStart:       true,
			TMDBRateLimitRPS: 1,
			IncludeSpecials:  false,
		},
		Cache: CacheSettings{
			RefreshEnabled:         true,
			RefreshIntervalMinutes: 15,
			RefreshPercent:         1,
			CleanupEnabled:         true,
			CleanupMaxAgeDays:      30,
		},
		Libraries: []Library{},
	}
}

// Clone returns a deep copy of the settings (the Libraries slice is copied).
func (s Settings) Clone() Settings {
	cp := s
	cp.Sources = append([]Source(nil), s.Sources...)
	for i := range cp.Sources {
		cp.Sources[i].Libraries = append([]Library(nil), s.Sources[i].Libraries...)
		cp.Sources[i].ScanIntervalMinutes = cloneInt(s.Sources[i].ScanIntervalMinutes)
	}
	cp.Libraries = append([]Library(nil), s.Libraries...)
	return cp
}

// Redacted returns a copy of the settings with API keys blanked out, suitable
// for logging or non-sensitive display.
func (s Settings) Redacted() Settings {
	cp := s.Clone()
	cp.Jellyfin.APIKey = ""
	cp.TMDB.APIKey = ""
	cp.AI.APIKey = ""
	for i := range cp.Sources {
		cp.Sources[i].Jellyfin.APIKey = ""
	}
	return cp
}

// NormalizeLocale returns a supported locale, defaulting to English.
func NormalizeLocale(loc string) string {
	switch strings.ToLower(strings.TrimSpace(loc)) {
	case LocaleDE:
		return LocaleDE
	default:
		return LocaleEN
	}
}

// NormalizeLogLevel returns a supported log level string, defaulting to "info".
func NormalizeLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case LogLevelWarn:
		return LogLevelWarn
	case LogLevelDebug:
		return LogLevelDebug
	default:
		return LogLevelInfo
	}
}

// ParseLogLevel maps a log level string to an slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch NormalizeLogLevel(level) {
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelDebug:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

// DataDir resolves the directory used to store the config file and database:
// WAIM_DATA_DIR is set to /data by the image; local development uses ./appdata.
func DataDir() string {
	if dir := strings.TrimSpace(os.Getenv("WAIM_DATA_DIR")); dir != "" {
		return dir
	}
	return "appdata"
}
