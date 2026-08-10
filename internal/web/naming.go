package web

import "strings"

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
