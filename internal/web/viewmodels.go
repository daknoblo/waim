package web

import (
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/logbuf"
)

// Nav identifiers for highlighting the active page.
const (
	NavDashboard = "dashboard"
	NavSettings  = "settings"
	NavAbout     = "about"
)

// LangOption is a selectable interface language.
type LangOption struct {
	Code   string
	Label  string
	Active bool
}

// Layout carries data shared by every page (header, navigation, footer).
type Layout struct {
	T                *i18n.Translator
	Active           string
	Version          string
	Repo             string
	MasterKeyMissing bool
	Languages        []LangOption
}

// StatusView is the display model for the scan status card.
type StatusView struct {
	State            string
	StateLabel       string
	Running          bool
	LastScan         string
	NextScan         string
	ItemsScanned     int
	LibrariesScanned int
	MissingTotal     int
	LastError        string
}

// LogEntryView is a display model for a single log line.
type LogEntryView struct {
	Time    string
	Level   string
	Message string
}

// DashboardData is the full model for the dashboard page.
type DashboardData struct {
	Layout   Layout
	Status   StatusView
	Findings []FindingRow
	Logs     []LogEntryView
}

// SettingsData is the full model for the settings page.
type SettingsData struct {
	Layout         Layout
	Settings       config.Settings
	Libraries      []config.Library
	HasJellyfinKey bool
	HasTMDBKey     bool
	Message        string
	IsError        bool
}

// AboutData is the model for the about page.
type AboutData struct {
	Layout    Layout
	Version   string
	Commit    string
	BuildDate string
	GoVersion string
	Repo      string
}

// BuildLogViews converts buffered log entries (newest first) into view models.
func BuildLogViews(entries []logbuf.Entry) []LogEntryView {
	out := make([]LogEntryView, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		out = append(out, LogEntryView{
			Time:    e.Time.Local().Format("15:04:05"),
			Level:   e.Level,
			Message: e.Message,
		})
	}
	return out
}

// LanguageOptions builds the language switcher options for the given catalog.
func LanguageOptions(active string) []LangOption {
	labels := map[string]string{
		config.LocaleEN: "English",
		config.LocaleDE: "Deutsch",
	}
	codes := []string{config.LocaleEN, config.LocaleDE}
	out := make([]LangOption, 0, len(codes))
	for _, c := range codes {
		out = append(out, LangOption{Code: c, Label: labels[c], Active: c == active})
	}
	return out
}
