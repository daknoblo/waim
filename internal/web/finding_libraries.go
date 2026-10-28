package web

import (
	"strings"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
)

type FindingLibraryLabel struct {
	Text  string
	Name  string
	Color []string
	Stale bool
}

// Findings refer to every participating library, not just the first instance.
// Collection origins belong here too, without implying ownership of missing parts.
func FindingLibraryLabels(t *i18n.Translator, refs []media.Reference, fallbackID, fallbackName string) []FindingLibraryLabel {
	var labels []FindingLibraryLabel
	seen := map[string]int{}
	for _, ref := range refs {
		key := media.Qualify(ref.ID, ref.LibraryID)
		if index, ok := seen[key]; ok {
			labels[index].Stale = labels[index].Stale || ref.Stale
			continue
		}
		parts := []string{}
		name := ref.Name
		switch ref.Type {
		case media.Virtual:
			name = t.T("sources.collection")
			parts = append(parts, name)
		default:
			switch ref.Type {
			case media.Jellyfin:
				parts = append(parts, "Jellyfin")
			case "emby":
				parts = append(parts, "Emby")
			case "plex":
				parts = append(parts, "Plex")
			default:
				if ref.Type != "" {
					parts = append(parts, ref.Type)
				}
			}
			if ref.Name != "" {
				parts = append(parts, ref.Name)
			}
			if ref.LibraryName != "" {
				parts = append(parts, ref.LibraryName)
			}
		}
		if len(parts) == 0 {
			continue
		}
		seen[key] = len(labels)
		labels = append(labels, FindingLibraryLabel{Text: strings.Join(parts, " \u00b7 "), Name: name, Color: LibraryColor(ref.LibraryID), Stale: ref.Stale})
	}
	if len(labels) == 0 {
		labels = append(labels, FindingLibraryLabel{Text: LibraryDisplayName(t, fallbackID, fallbackName), Color: LibraryColor(fallbackID)})
	}
	return labels
}
