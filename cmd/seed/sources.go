package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func sourceFixtures(ctx context.Context, st *store.Store, path string, stats []store.MediaStat, findings []store.Finding, upcoming []store.UpcomingItem) ([]store.MediaStat, []store.Finding, []store.UpcomingItem, []store.LibrarySummary, store.RunMetadata, error) {
	cfg, err := config.Load(filepath.Dir(path))
	if err != nil {
		return nil, nil, nil, nil, store.RunMetadata{}, err
	}
	settings := cfg.Get()
	settings.Scan.RunOnStart = false
	settings.Scan.IntervalMinutes = 0
	settings.Cache.RefreshEnabled = false
	settings.Cache.CleanupEnabled = false
	settings.Sources = nil
	for _, id := range []string{"seed-a", "seed-b"} {
		settings.Sources = append(settings.Sources, config.Source{ID: id, Type: media.Jellyfin, Name: id, Enabled: true, Jellyfin: config.JellyfinSettings{URL: "https://" + id + ".invalid"}, Libraries: []config.Library{{ID: libMovies, Name: "Movies", Type: "movies", Enabled: true}, {ID: libSeries, Name: "Series", Type: "tvshows", Enabled: true}}, Revision: 1})
	}
	settings.Sources = append(settings.Sources, config.VirtualSource())
	if err := cfg.Save(settings); err != nil {
		return nil, nil, nil, nil, store.RunMetadata{}, err
	}
	settings = cfg.Get()
	snapshots := map[string]*media.Snapshot{}
	libs := []store.LibrarySummary{}
	for _, src := range settings.Sources {
		if src.Type == media.Virtual {
			continue
		}
		snap := &media.Snapshot{}
		for _, lib := range src.Libraries {
			id := media.Qualify(src.ID, lib.ID)
			snap.Libraries = append(snap.Libraries, media.Library{ID: id, Name: src.Name + " · " + lib.Name, Type: lib.Type})
			libs = append(libs, store.LibrarySummary{ID: id, Name: src.Name + " · " + lib.Name})
		}
		snapshots[src.ID] = snap
	}
	for i := range stats {
		m := &stats[i]
		src := settings.Sources[i%2]
		localID, localLib := m.JellyfinID, m.LibraryID
		m.LibraryID = media.Qualify(src.ID, localLib)
		m.LibraryName = src.Name + " · " + m.LibraryName
		m.JellyfinID = media.Qualify(src.ID, localID)
		m.References = []media.Reference{{ID: src.ID, Type: media.Jellyfin, Name: src.Name, LibraryID: m.LibraryID, LibraryName: m.LibraryName, ItemID: localID, URL: src.Jellyfin.URL + "/web/#/details?id=" + localID}}
		kind := media.Movie
		if m.Type == store.MediaSeries {
			kind = media.Series
		}
		item := media.Item{ID: m.JellyfinID, Name: m.Title, Type: kind, ProductionYear: m.Year, ProviderIDs: map[string]string{"Tmdb": strconv.FormatInt(m.TMDBID, 10)}, References: m.References}
		for _, season := range m.Seasons {
			for _, rating := range season.Ratings {
				if rating.Owned {
					sn, en := season.Number, rating.Number
					item.Episodes = append(item.Episodes, media.Item{ParentIndexNumber: &sn, IndexNumber: &en})
				}
			}
		}
		snapshots[src.ID].Items = append(snapshots[src.ID].Items, item)
		if i == 0 {
			second := settings.Sources[1]
			other := item
			other.ID = media.Qualify(second.ID, localID)
			other.References = []media.Reference{{ID: second.ID, Type: media.Jellyfin, Name: second.Name, LibraryID: media.Qualify(second.ID, localLib), LibraryName: second.Name + " · Series", ItemID: localID, URL: second.Jellyfin.URL + "/web/#/details?id=" + localID}}
			snapshots[second.ID].Items = append(snapshots[second.ID].Items, other)
			v := store.VirtualEntry{Type: kind, TMDBID: m.TMDBID, Title: m.Title, Year: m.Year}
			if err := st.MutateVirtual(ctx, v, false); err != nil {
				return nil, nil, nil, nil, store.RunMetadata{}, err
			}
			m.References = append(m.References, other.References...)
			m.References = append(m.References, v.Item().References...)
		}
	}
	for _, src := range settings.Sources {
		if snap := snapshots[src.ID]; snap != nil {
			if err := st.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), snap, ""); err != nil {
				return nil, nil, nil, nil, store.RunMetadata{}, err
			}
		}
	}
	for i := range findings {
		f := &findings[i]
		for _, m := range stats {
			if f.MediaType == m.Type && f.TMDBID == m.TMDBID {
				f.Provenance = m.Provenance
				f.LibraryID, f.LibraryName, f.JellyfinID = m.LibraryID, m.LibraryName, m.JellyfinID
			}
			if f.Kind == store.KindMissingCollection && m.CollectionID == f.TMDBID {
				f.ContextReferences = append(f.ContextReferences, m.References...)
				f.LibraryID, f.LibraryName = m.LibraryID, m.LibraryName
			}
		}
	}
	for i := range upcoming {
		for _, m := range stats {
			if upcoming[i].MediaType == m.Type && upcoming[i].TMDBID == m.TMDBID {
				upcoming[i].Provenance = m.Provenance
				upcoming[i].LibraryID = m.LibraryID
			}
		}
	}
	for i := 0; i < 2; i++ {
		v := store.VirtualEntry{Type: media.Movie, TMDBID: 990001 + int64(i), Title: fmt.Sprintf("Watch-only demo %d", i+1), Year: 2026}
		if err := st.MutateVirtual(ctx, v, false); err != nil {
			return nil, nil, nil, nil, store.RunMetadata{}, err
		}
		p := store.Provenance{WatchOnly: true, References: v.Item().References}
		stats = append(stats, store.MediaStat{Type: store.MediaMovie, Title: v.Title, TMDBID: v.TMDBID, Rating: 8.1, Runtime: 120, LibraryID: media.VirtualID, LibraryName: media.VirtualName, Provenance: p})
		if i == 0 {
			findings = append(findings, store.Finding{Kind: store.KindMissingMovie, MediaType: store.MediaMovie, TMDBID: v.TMDBID, Title: v.Title, Summary: "Tracked movie is not owned", LibraryID: media.VirtualID, LibraryName: media.VirtualName, Provenance: p, Details: `{"releaseDate":"` + time.Now().AddDate(0, 0, -7).Format("2006-01-02") + `"}`})
		} else {
			upcoming = append(upcoming, store.UpcomingItem{Kind: store.UpcomingMovie, MediaType: store.MediaMovie, TMDBID: v.TMDBID, Title: v.Title, SourceTitle: v.Title, ReleaseDate: time.Now().AddDate(0, 0, 14).Format("2006-01-02"), Provenance: p})
		}
	}
	libs = append(libs, store.LibrarySummary{ID: media.VirtualID, Name: media.VirtualName})
	for i := range libs {
		for _, m := range stats {
			for _, ref := range m.References {
				if ref.LibraryID == libs[i].ID {
					libs[i].Total++
					libs[i].Scanned++
					break
				}
			}
		}
	}
	_, revision, err := st.VirtualEntries(ctx)
	return stats, findings, upcoming, libs, store.RunMetadata{Basis: "owned-v1", Mode: "refresh", Revision: revision, SourcesToken: settings.SourcesToken()}, err
}
