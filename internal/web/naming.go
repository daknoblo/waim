package web

import (
	"strings"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

// LibraryDisplayName translates only the reserved virtual library, never a
// user-provided source/library name that happens to use the same text.
func LibraryDisplayName(t *i18n.Translator, id, name string) string {
	if id == media.VirtualID {
		return t.T("sources.collection")
	}
	return name
}

func localizedLibraryData(t *i18n.Translator, run *store.ScanRun, findings []store.Finding) (*store.ScanRun, []store.Finding) {
	localizedFindings := append([]store.Finding(nil), findings...)
	for i := range localizedFindings {
		f := &localizedFindings[i]
		f.LibraryName = LibraryDisplayName(t, f.LibraryID, f.LibraryName)
	}
	if run == nil {
		return nil, localizedFindings
	}
	localizedRun := *run
	localizedRun.Libraries = append([]store.LibrarySummary(nil), run.Libraries...)
	localizedRun.Media = append([]store.MediaStat(nil), run.Media...)
	for i := range localizedRun.Libraries {
		lib := &localizedRun.Libraries[i]
		lib.Name = LibraryDisplayName(t, lib.ID, lib.Name)
	}
	for i := range localizedRun.Media {
		m := &localizedRun.Media[i]
		m.LibraryName = LibraryDisplayName(t, m.LibraryID, m.LibraryName)
	}
	return &localizedRun, localizedFindings
}

// languageNames maps the ISO 639-1 codes TMDB reports as a title's original
// language to a display name. Unknown codes fall back to the upper-cased code.
var languageNames = map[string]string{
	"ar": "Arabic", "cs": "Czech", "da": "Danish", "de": "German", "el": "Greek",
	"en": "English", "es": "Spanish", "fa": "Persian", "fi": "Finnish", "fr": "French",
	"he": "Hebrew", "hi": "Hindi", "hu": "Hungarian", "id": "Indonesian", "is": "Icelandic",
	"it": "Italian", "ja": "Japanese", "ko": "Korean", "ml": "Malayalam", "ms": "Malay",
	"nb": "Norwegian", "nl": "Dutch", "no": "Norwegian", "pl": "Polish", "pt": "Portuguese",
	"ro": "Romanian", "ru": "Russian", "sv": "Swedish", "ta": "Tamil", "te": "Telugu",
	"th": "Thai", "tr": "Turkish", "uk": "Ukrainian", "vi": "Vietnamese", "zh": "Chinese",
}

// countryNames maps ISO 3166-1 alpha-2 production countries to a display name.
var countryNames = map[string]string{
	"AR": "Argentina", "AT": "Austria", "AU": "Australia", "BE": "Belgium", "BR": "Brazil",
	"CA": "Canada", "CH": "Switzerland", "CN": "China", "CZ": "Czechia", "DE": "Germany",
	"DK": "Denmark", "ES": "Spain", "FI": "Finland", "FR": "France", "GB": "United Kingdom",
	"GR": "Greece", "HK": "Hong Kong", "HU": "Hungary", "IE": "Ireland", "IL": "Israel",
	"IN": "India", "IS": "Iceland", "IT": "Italy", "JP": "Japan", "KR": "South Korea",
	"MX": "Mexico", "NL": "Netherlands", "NO": "Norway", "NZ": "New Zealand", "PL": "Poland",
	"PT": "Portugal", "RO": "Romania", "RU": "Russia", "SE": "Sweden", "TH": "Thailand",
	"TR": "Turkey", "TW": "Taiwan", "UA": "Ukraine", "US": "United States", "ZA": "South Africa",
}

func languageName(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return ""
	}
	if name, ok := languageNames[code]; ok {
		return name
	}
	return strings.ToUpper(code)
}

func countryName(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return ""
	}
	if name, ok := countryNames[code]; ok {
		return name
	}
	return code
}
