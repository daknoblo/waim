package web

import (
	"strings"

	"github.com/daknoblo/waim/internal/i18n"
)

// orDash returns the value, or an em dash when it is empty.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "\u2014"
	}
	return s
}

// boolAttr renders a boolean as the string an ARIA attribute expects.
func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// upcomingDirectionValue is the direction the section currently shows, used to
// carry the selection along when a dropdown fires.
func upcomingDirectionValue(u StatsUpcoming) string {
	if u.Past {
		return UpcomingPast
	}
	return UpcomingForward
}

// nextDir returns the sort direction to use when a column header is clicked:
// clicking the active column toggles the direction, otherwise ascending.
func nextDir(col, curSort, curDir string) string {
	if col == curSort && curDir == DirAsc {
		return DirDesc
	}
	return DirAsc
}

// sortArrow returns an arrow glyph for the active sort column, or empty.
func sortArrow(col, curSort, curDir string) string {
	if col != curSort {
		return ""
	}
	if curDir == DirDesc {
		return "\u25BC" // down triangle
	}
	return "\u25B2" // up triangle
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

// Data states describe why a view has nothing to show. Only DataReady allows a
// statement about the library itself; the others mean the data simply is not
// there yet, which must never be presented as "nothing found".
const (
	DataUnconfigured = "unconfigured"  // Jellyfin, TMDB or the library selection is missing
	DataScanning     = "scanning"      // a scan is running and no successful one finished yet
	DataNeverScanned = "never-scanned" // configured, but no successful scan yet
	DataReady        = "ready"         // a successful scan is available
)

// dataStateKey maps a data state to the message explaining why a view is empty.
func dataStateKey(state string) string {
	switch state {
	case DataUnconfigured:
		return "common.stateUnconfigured"
	case DataScanning:
		return "common.stateScanning"
	default:
		return "common.stateNeverScanned"
	}
}

// connStateClass styles a connection chip. Anything that is not a confirmed
// success or a hard failure stays neutral, so a section that simply has not
// been filled in does not look broken.
func connStateClass(c ConnCheck) string {
	switch c.State {
	case ConnOK:
		return "alert-ok"
	case ConnError:
		return "alert-error"
	case ConnNeedsKey:
		return "bg-amber-500/10 text-amber-200"
	default:
		return "bg-slate-900/60 text-slate-400"
	}
}

// saveStateClass styles the save indicator.
func saveStateClass(state string) string {
	switch state {
	case SaveFailed:
		return "text-rose-300"
	case SaveNeedsKey:
		return "text-amber-200"
	case SaveOK:
		return "text-emerald-300"
	default:
		return "text-slate-400"
	}
}

// upcomingSectionTitle names the section for the direction currently shown.
func upcomingSectionTitle(t *i18n.Translator, u StatsUpcoming) string {
	if u.Past {
		return t.T("stats.upcomingPastTitle")
	}
	return t.T("stats.upcomingTitle")
}
