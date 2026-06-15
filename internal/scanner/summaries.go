package scanner

import (
	"fmt"
)

// The summary strings below are English fallbacks persisted with each finding.
// The web UI renders localised text from the finding's structured fields, so
// these are primarily used for exports and logs.

func summaryCollection(name string, missing int) string {
	return fmt.Sprintf("Collection %q is missing %d entr%s", name, missing, plural(missing, "y", "ies"))
}

func summarySeason(title string, season, episodes int) string {
	return fmt.Sprintf("%s: season %d is completely missing (%d episodes)", title, season, episodes)
}

func summaryEpisodes(title string, season, missing int) string {
	return fmt.Sprintf("%s: season %d is missing %d episode%s", title, season, missing, plural(missing, "", "s"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
