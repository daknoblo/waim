package web

import "strings"

// orDash returns the value, or an em dash when it is empty.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "\u2014"
	}
	return s
}

// logLevelClass returns a CSS class for a log level label.
func logLevelClass(level string) string {
	switch level {
	case "ERROR":
		return "text-rose-400"
	case "WARN":
		return "text-amber-400"
	case "DEBUG":
		return "text-slate-500"
	default:
		return "text-emerald-400"
	}
}

// libraryPalette holds basic, distinct colors for up to ~10 libraries. Each
// entry is a list of individual Tailwind classes (no special characters) so
// they survive templ's class sanitisation, and the literals are scanned by
// Tailwind (the .go files are in the content glob).
var libraryPalette = [][]string{
	{"bg-rose-900", "text-rose-200"},
	{"bg-orange-900", "text-orange-200"},
	{"bg-amber-900", "text-amber-100"},
	{"bg-lime-900", "text-lime-200"},
	{"bg-emerald-900", "text-emerald-200"},
	{"bg-teal-900", "text-teal-200"},
	{"bg-sky-900", "text-sky-200"},
	{"bg-blue-900", "text-blue-200"},
	{"bg-violet-900", "text-violet-200"},
	{"bg-pink-900", "text-pink-200"},
}

// LibraryColor returns a stable set of color classes for a library, derived from
// its ID so the same library always gets the same color.
func LibraryColor(id string) []string {
	if id == "" {
		return libraryPalette[0]
	}
	var h uint32 = 2166136261
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return libraryPalette[int(h%uint32(len(libraryPalette)))]
}
