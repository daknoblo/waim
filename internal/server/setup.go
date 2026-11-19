package server

import (
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/web"
)

func setupNotices(settings config.Settings, virtualEntries int) []web.SetupNotice {
	var notices []web.SetupNotice
	if settings.TMDB.APIKey == "" {
		notices = append(notices, web.SetupNotice{
			CategoryKey: "settings.tab.metadata", MessageKey: "sources.tmdbRequired",
			ActionKey: "setup.metadataAction", URL: web.SettingsURL("metadata"),
		})
	}
	// A failed virtual-entry read is unknown, not proof of an empty collection.
	if virtualEntries < 0 || virtualEntries > 0 {
		return notices
	}
	message := "setup.mediaMissing"
	for _, src := range settings.Sources {
		if src.Type == media.Virtual || !src.Enabled {
			continue
		}
		if message == "setup.mediaMissing" {
			message = "setup.mediaConnection"
		}
		if src.Jellyfin.URL == "" || src.Jellyfin.APIKey == "" || src.KeyUnreadable {
			continue
		}
		message = "setup.mediaLibraries"
		for _, lib := range src.Libraries {
			if lib.Enabled {
				return notices
			}
		}
	}
	return append(notices, web.SetupNotice{
		CategoryKey: "sources.title", MessageKey: message,
		ActionKey: "setup.mediaAction", URL: web.SettingsURL("media"),
		VirtualAlternative: true,
	})
}
