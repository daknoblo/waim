package server

import (
	"time"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func collectionOwnership(entries []store.VirtualEntry, run, latest *store.ScanRun, includeSpecials, updating bool, now time.Time) map[string]string {
	out := map[string]string{}
	if run == nil {
		return out
	}
	tracked := map[string]bool{}
	for _, entry := range entries {
		tracked[media.Key(entry.Type, entry.TMDBID)] = true
	}
	unverified := updating || run.Metadata.Basis == "" || run.Metadata.Pending || run.Status != store.StatusSuccess ||
		run.Error != "" || run.FinishedAt == nil || len(run.Metadata.Warnings) > 0 ||
		latest == nil || latest.ID != run.ID || latest.Status != store.StatusSuccess || latest.Error != ""
	due := map[string]bool{}
	for _, item := range run.Upcoming {
		if item.Kind != store.UpcomingEpisode {
			continue
		}
		date, err := time.Parse("2006-01-02", item.ReleaseDate)
		if err == nil && !date.After(now) {
			due[media.Key(media.Series, item.TMDBID)] = true
		}
	}
	for _, stat := range run.Media {
		kind := media.Movie
		if stat.Type == store.MediaSeries {
			kind = media.Series
		}
		key := media.Key(kind, stat.TMDBID)
		if !tracked[key] || stat.WatchOnly {
			continue
		}
		real, stale := false, false
		for _, ref := range stat.References {
			if ref.Type != media.Virtual {
				real = true
				stale = stale || ref.Stale
			}
		}
		if !real {
			continue
		}
		availability := stat.LibraryAvailability
		switch {
		case unverified || stale || stat.Unconfirmed || stat.MetadataUnavailable || availability == nil || due[key]:
			out[key] = web.CollectionUnverified
		case kind == media.Series && (availability.IncludeSpecials != includeSpecials || availability.ReleasedEpisodes == 0):
			out[key] = web.CollectionUnverified
		case availability.Complete:
			out[key] = web.CollectionComplete
		default:
			out[key] = web.CollectionPartial
		}
	}
	return out
}
