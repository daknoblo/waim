package web

import (
	"strings"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/logbuf"
)

// Nav identifiers for highlighting the active page.
const (
	NavDashboard = "dashboard"
	NavLogs      = "logs"
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
	Duration         string
	CurrentItem      string
	ItemsScanned     int
	LibrariesScanned int
	MissingTotal     int
	LastError        string
	Libraries        []LibraryStatusView
}

// LibraryStatusView is a per-library row in the scan status card.
type LibraryStatusView struct {
	Name    string
	Color   []string
	Scanned int
	Total   int
	Missing int
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
	Sort     string
	Dir      string
}

// ConnCheck is the result of testing a connection (Jellyfin or TMDB).
type ConnCheck struct {
	Checked bool
	OK      bool
	Message string
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
	JellyfinCheck  ConnCheck
	TMDBCheck      ConnCheck
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

// LogPageData is the model for the dedicated activity-log page.
type LogPageData struct {
	Layout Layout
	Logs   []LogEntryView
}

// LangChoice is a selectable metadata language for TMDB.
type LangChoice struct {
	Code  string
	Label string
}

// metadataLanguages is the curated list of common TMDB metadata languages.
var metadataLanguages = []LangChoice{
	{"en-US", "English (US)"},
	{"en-GB", "English (UK)"},
	{"de-DE", "Deutsch"},
	{"fr-FR", "Fran\u00e7ais"},
	{"es-ES", "Espa\u00f1ol"},
	{"it-IT", "Italiano"},
	{"nl-NL", "Nederlands"},
	{"pt-PT", "Portugu\u00eas"},
	{"pt-BR", "Portugu\u00eas (Brasil)"},
	{"ru-RU", "\u0420\u0443\u0441\u0441\u043a\u0438\u0439"},
	{"ja-JP", "\u65e5\u672c\u8a9e"},
	{"ko-KR", "\ud55c\uad6d\uc5b4"},
	{"zh-CN", "\u4e2d\u6587 (\u7b80\u4f53)"},
	{"pl-PL", "Polski"},
	{"sv-SE", "Svenska"},
	{"da-DK", "Dansk"},
	{"fi-FI", "Suomi"},
	{"nb-NO", "Norsk"},
	{"cs-CZ", "\u010ce\u0161tina"},
	{"hu-HU", "Magyar"},
	{"tr-TR", "T\u00fcrk\u00e7e"},
}

// MetadataLanguages returns the curated language list, ensuring the current
// value is present (added at the top if it is not already in the list).
func MetadataLanguages(current string) []LangChoice {
	current = strings.TrimSpace(current)
	if current == "" {
		return metadataLanguages
	}
	for _, lc := range metadataLanguages {
		if lc.Code == current {
			return metadataLanguages
		}
	}
	return append([]LangChoice{{Code: current, Label: current}}, metadataLanguages...)
}

// regionChoices is the curated list of common TMDB regions (ISO 3166-1).
var regionChoices = []LangChoice{
	{"US", "United States"},
	{"GB", "United Kingdom"},
	{"DE", "Germany"},
	{"AT", "Austria"},
	{"CH", "Switzerland"},
	{"FR", "France"},
	{"ES", "Spain"},
	{"IT", "Italy"},
	{"NL", "Netherlands"},
	{"BE", "Belgium"},
	{"PT", "Portugal"},
	{"BR", "Brazil"},
	{"RU", "Russia"},
	{"JP", "Japan"},
	{"KR", "South Korea"},
	{"CN", "China"},
	{"PL", "Poland"},
	{"SE", "Sweden"},
	{"DK", "Denmark"},
	{"FI", "Finland"},
	{"NO", "Norway"},
	{"CZ", "Czechia"},
	{"HU", "Hungary"},
	{"TR", "Turkey"},
	{"CA", "Canada"},
	{"AU", "Australia"},
	{"IE", "Ireland"},
	{"NZ", "New Zealand"},
	{"MX", "Mexico"},
}

// Regions returns the curated region list with a leading "none" option,
// ensuring the current value is present.
func Regions(current string) []LangChoice {
	current = strings.TrimSpace(current)
	out := []LangChoice{{Code: "", Label: "\u2014"}}
	found := current == ""
	for _, r := range regionChoices {
		if r.Code == current {
			found = true
		}
	}
	if !found {
		out = append(out, LangChoice{Code: current, Label: current})
	}
	return append(out, regionChoices...)
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
