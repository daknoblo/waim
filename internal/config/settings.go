// Package config defines the application settings, their on-disk JSON
// representation, and a thread-safe manager that transparently encrypts API
// keys at rest.
package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// SchemaVersion is the current on-disk config schema version. Bump it when the
// stored layout changes in a backwards-incompatible way.
const SchemaVersion = 1

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

// ScanSettings controls scan scheduling and TMDB request behaviour.
type ScanSettings struct {
	IntervalMinutes  int     `json:"intervalMinutes"`  // 0 disables periodic scans
	RunOnStart       bool    `json:"runOnStart"`       // scan once at startup
	TMDBRateLimitRPS float64 `json:"tmdbRateLimitRps"` // requests per second
	IncludeSpecials  bool    `json:"includeSpecials"`  // include season 0 / specials
}

// Settings is the full in-memory configuration with decrypted API keys.
type Settings struct {
	Locale    string           `json:"locale"`
	LogLevel  string           `json:"logLevel"`
	Jellyfin  JellyfinSettings `json:"jellyfin"`
	TMDB      TMDBSettings     `json:"tmdb"`
	Scan      ScanSettings     `json:"scan"`
	Libraries []Library        `json:"libraries"`
}

// Defaults returns a Settings value with sensible defaults.
func Defaults() Settings {
	return Settings{
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
		Libraries: []Library{},
	}
}

// Clone returns a deep copy of the settings (the Libraries slice is copied).
func (s Settings) Clone() Settings {
	cp := s
	cp.Libraries = append([]Library(nil), s.Libraries...)
	return cp
}

// Redacted returns a copy of the settings with API keys blanked out, suitable
// for logging or non-sensitive display.
func (s Settings) Redacted() Settings {
	cp := s.Clone()
	cp.Jellyfin.APIKey = ""
	cp.TMDB.APIKey = ""
	return cp
}

// EnabledLibraryIDs returns the IDs of libraries selected for scanning.
func (s Settings) EnabledLibraryIDs() []string {
	var ids []string
	for _, l := range s.Libraries {
		if l.Enabled {
			ids = append(ids, l.ID)
		}
	}
	return ids
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

// DataDir resolves the directory used to store the config file and database.
// It honours WAIM_DATA_DIR and falls back to a platform-appropriate default.
func DataDir() string {
	if d := strings.TrimSpace(os.Getenv("WAIM_DATA_DIR")); d != "" {
		return d
	}
	// In containers the working directory layout uses /app/appdata.
	if _, err := os.Stat("/app"); err == nil {
		return "/app/appdata"
	}
	// Local development fallback.
	if home, err := os.UserConfigDir(); err == nil {
		return filepath.Join(home, "waim")
	}
	return "appdata"
}
