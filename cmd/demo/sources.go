package main

import (
	"strconv"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func demoSources(run *store.ScanRun, findings []store.Finding) (media.Catalog, []config.Source, []store.VirtualEntry) {
	sources := []config.Source{
		{ID: "demo-a", Type: media.Jellyfin, Name: "Living room", Enabled: true, Jellyfin: config.JellyfinSettings{URL: "https://living-room.example"}},
		{ID: "demo-b", Type: media.Jellyfin, Name: "Archive", Enabled: true, Jellyfin: config.JellyfinSettings{URL: "https://archive.example"}},
		config.VirtualSource(),
	}
	catalog := media.Catalog{}
	entries := []store.VirtualEntry{}
	for i := range run.Media {
		m := &run.Media[i]
		kind := media.Movie
		if m.Type == store.MediaSeries {
			kind = media.Series
		}
		src := sources[i%2]
		ref := media.Reference{ID: src.ID, Type: src.Type, Name: src.Name, LibraryID: m.LibraryID, LibraryName: m.LibraryName, ItemID: m.JellyfinID, ServerURL: src.Jellyfin.URL, URL: src.Jellyfin.URL + "/web/#/details?id=" + m.JellyfinID}
		m.References = []media.Reference{ref}
		item := media.Item{ID: media.Qualify(src.ID, m.JellyfinID), Name: m.Title, Type: kind, ProviderIDs: map[string]string{"Tmdb": strconv.FormatInt(m.TMDBID, 10)}, References: m.References}
		catalog.Items = append(catalog.Items, item)
		if i == 0 {
			second := item
			second.References = []media.Reference{{ID: sources[1].ID, Type: media.Jellyfin, Name: sources[1].Name, LibraryID: m.LibraryID, LibraryName: m.LibraryName, ServerURL: sources[1].Jellyfin.URL, URL: sources[1].Jellyfin.URL + "/web/#/details?id=" + m.JellyfinID}}
			catalog.Items = append(catalog.Items, second)
			v := store.VirtualEntry{Type: kind, TMDBID: m.TMDBID, Title: m.Title, Year: m.Year}
			entries = append(entries, v)
			catalog.Items = append(catalog.Items, v.Item())
			m.References = append(m.References, second.References...)
			m.References = append(m.References, v.Item().References...)
		}
	}
	v := store.VirtualEntry{Type: media.Movie, TMDBID: 999001, Title: "A future adventure (demo)", Year: 2030}
	entries = append(entries, v)
	catalog.Items = append(catalog.Items, v.Item())
	run.Media = append(run.Media, store.MediaStat{Type: store.MediaMovie, TMDBID: v.TMDBID, Title: v.Title, Year: v.Year, Rating: 8.2, Runtime: 120, LibraryID: media.VirtualID, LibraryName: media.VirtualName, Provenance: store.Provenance{WatchOnly: true, References: v.Item().References}})
	run.Upcoming = append(run.Upcoming, store.UpcomingItem{Kind: store.UpcomingMovie, MediaType: store.MediaMovie, TMDBID: v.TMDBID, Title: v.Title, SourceTitle: v.Title, ReleaseDate: "2030-01-01", Provenance: store.Provenance{WatchOnly: true, References: v.Item().References}})
	run.Metadata = store.RunMetadata{Basis: "owned-v1", Mode: "refresh"}
	for i := range findings {
		f := &findings[i]
		for _, m := range run.Media {
			if f.Kind == store.KindMissingCollection && m.CollectionID == f.TMDBID {
				f.ContextReferences = append(f.ContextReferences, m.References...)
				continue
			}
			if m.TMDBID == f.TMDBID && m.Type == f.MediaType {
				f.Provenance = m.Provenance
			}
		}
	}
	catalog.Items = media.Merge(catalog.Items)
	return catalog, sources, entries
}
