package web

import (
	"net/url"
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
		switch ref.Type {
		case media.Virtual:
			parts = append(parts, t.T("sources.typeVirtual"), t.T("sources.collection"))
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
			if address := referenceServerURL(ref); address != "" {
				parts = append(parts, address)
			} else if ref.Name != "" {
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
		labels = append(labels, FindingLibraryLabel{Text: strings.Join(parts, " / "), Name: ref.Name, Color: LibraryColor(ref.LibraryID), Stale: ref.Stale})
	}
	if len(labels) == 0 {
		labels = append(labels, FindingLibraryLabel{Text: LibraryDisplayName(t, fallbackID, fallbackName), Color: LibraryColor(fallbackID)})
	}
	return labels
}

func referenceServerURL(ref media.Reference) string {
	raw := ref.ServerURL
	if raw == "" {
		raw = ref.URL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment = nil, "", "", ""
	if ref.ServerURL == "" {
		// Older Jellyfin snapshots only stored their item deep link.
		if ref.Type == media.Jellyfin {
			path := strings.TrimSuffix(u.Path, "/")
			u.Path = strings.TrimSuffix(path, "/web")
		} else {
			u.Path = ""
		}
		u.RawPath = ""
	}
	return strings.TrimRight(u.String(), "/")
}
