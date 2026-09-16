package source

import (
	"strings"

	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/media"
)

const maxEpisodeRange = 1000

func virtualPlaceholder(item jellyfin.Item) bool {
	return item.IsMissing || item.IsVirtualItem || strings.EqualFold(item.LocationType, "Virtual")
}

// Expand physical combined files into bounded logical episode units before
// the catalog's season/episode union. Invalid ends retain only a valid start.
func normalizeEpisodes(episodes []jellyfin.Item, title string) ([]media.Item, []string) {
	var out []media.Item
	var unidentified, invalidRange, placeholder bool
	for _, ep := range episodes {
		if virtualPlaceholder(ep) {
			placeholder = true
			continue
		}
		if ep.IndexNumber == nil || ep.ParentIndexNumber == nil || *ep.IndexNumber < 1 || *ep.ParentIndexNumber < 0 {
			unidentified = true
			continue
		}
		start, end := *ep.IndexNumber, *ep.IndexNumber
		if ep.IndexNumberEnd != nil {
			end = *ep.IndexNumberEnd
			if end < start || end-start >= maxEpisodeRange {
				invalidRange = true
				end = start
			}
		}
		for offset := 0; offset <= end-start; offset++ {
			number := start + offset
			item := normalize(ep)
			item.IndexNumber = &number
			out = append(out, item)
		}
	}
	var warnings []string
	if unidentified {
		warnings = append(warnings, "Unidentified episode: "+title)
	}
	if invalidRange {
		warnings = append(warnings, "Invalid episode range (only the start episode counted): "+title)
	}
	if placeholder {
		warnings = append(warnings, "Ignored missing or virtual episodes: "+title)
	}
	return out, warnings
}
