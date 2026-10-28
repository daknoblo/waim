package scanner

import (
	"encoding/json"
	"sort"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

func provenance(item media.Item) store.Provenance {
	return store.Provenance{CatalogID: item.ID, References: item.References, WatchOnly: item.WatchOnly}
}

func (s *Scanner) basicStat(item media.Item, libID, libName string) store.MediaStat {
	kind := store.MediaMovie
	if item.Type == media.Series {
		kind = store.MediaSeries
	}
	stat := store.MediaStat{Provenance: provenance(item), MetadataUnavailable: true, Type: kind, Title: item.Name, Year: item.ProductionYear, TMDBID: item.TMDBID(), LibraryID: libID, LibraryName: libName}
	counts := map[int]int{}
	for _, ep := range item.Episodes {
		if ep.IndexNumber == nil || ep.ParentIndexNumber == nil || (*ep.ParentIndexNumber == 0 && !s.settings.Scan.IncludeSpecials) {
			continue
		}
		stat.Episodes++
		counts[*ep.ParentIndexNumber]++
	}
	for sn, count := range counts {
		stat.Seasons = append(stat.Seasons, store.SeasonStat{Number: sn, Episodes: count})
	}
	sort.Slice(stat.Seasons, func(i, j int) bool { return stat.Seasons[i].Number < stat.Seasons[j].Number })
	return stat
}

func (s *Scanner) watchMovie(movie tmdb.Movie, item media.Item, libID, libName string, res *Result) {
	if !s.released(movie.ReleaseDate) {
		res.Upcoming = append(res.Upcoming, store.UpcomingItem{Provenance: provenance(item), Kind: store.UpcomingMovie, MediaType: store.MediaMovie, Title: movie.Title, SourceTitle: movie.Title, TMDBID: movie.ID, ReleaseDate: movie.ReleaseDate, PosterPath: movie.PosterPath, Rating: movie.VoteAverage, LibraryID: libID, LibraryName: libName})
		return
	}
	d, _ := json.Marshal(map[string]any{"posterPath": movie.PosterPath, "releaseDate": movie.ReleaseDate, "imdbId": movie.IMDbID, "rating": movie.VoteAverage})
	res.Findings = append(res.Findings, store.Finding{Provenance: provenance(item), Kind: store.KindMissingMovie, MediaType: store.MediaMovie, Title: movie.Title, TMDBID: movie.ID, LibraryID: libID, LibraryName: libName, Summary: "Watched movie is not owned", Details: string(d)})
}

// Collection context and explicit tracking share movie units. Prefer collection
// grouping for findings; the individual release keeps its actual provenance.
func dedupeMovies(res *Result) {
	contexts := map[int64]store.Provenance{}
	for _, m := range res.Media {
		if m.CollectionID <= 0 {
			continue
		}
		p, ok := contexts[m.CollectionID]
		if !ok {
			p.WatchOnly = true
		}
		p.ContextReferences = append(p.ContextReferences, m.References...)
		p.WatchOnly = p.WatchOnly && m.WatchOnly
		contexts[m.CollectionID] = p
	}
	for i := range res.Findings {
		f := &res.Findings[i]
		if f.Kind != store.KindMissingCollection {
			continue
		}
		if p, ok := contexts[f.TMDBID]; ok {
			f.Provenance = p
		}
		for _, m := range res.Media {
			if m.CollectionID == f.TMDBID && !m.WatchOnly {
				f.LibraryID, f.LibraryName = m.LibraryID, m.LibraryName
				break
			}
		}
	}
	for i := range res.Upcoming {
		u := &res.Upcoming[i]
		if u.Kind == store.UpcomingCollectionPart {
			if p, ok := contexts[u.SourceTMDBID]; ok {
				u.Provenance = p
			}
		}
	}
	parts := map[int64]bool{}
	for _, f := range res.Findings {
		if f.Kind == store.KindMissingCollection {
			var d missingCollectionDetail
			_ = json.Unmarshal([]byte(f.Details), &d)
			for _, p := range d.MissingParts {
				parts[p.TMDBID] = true
			}
		}
	}
	fs := res.Findings[:0]
	for _, f := range res.Findings {
		if f.Kind != store.KindMissingMovie || !parts[f.TMDBID] {
			fs = append(fs, f)
		}
	}
	res.Findings = fs
	refs := map[int64]store.Provenance{}
	for _, m := range res.Media {
		if m.Type == store.MediaMovie {
			refs[m.TMDBID] = m.Provenance
		}
	}
	seen := map[int64]bool{}
	up := res.Upcoming[:0]
	for _, u := range res.Upcoming {
		if u.MediaType == store.MediaMovie {
			if seen[u.TMDBID] {
				continue
			}
			seen[u.TMDBID] = true
			if p, ok := refs[u.TMDBID]; ok {
				u.Provenance = p
			}
		}
		up = append(up, u)
	}
	res.Upcoming = up
	for i := range res.Libraries {
		lib := &res.Libraries[i]
		lib.Total, lib.Scanned, lib.Missing = 0, 0, 0
		for _, m := range res.Media {
			matches := m.LibraryID == lib.ID
			for _, ref := range m.References {
				matches = matches || ref.LibraryID == lib.ID
			}
			if matches {
				lib.Total++
				lib.Scanned++
			}
		}
		for _, f := range res.Findings {
			matches := f.LibraryID == lib.ID
			for _, ref := range append(f.References, f.ContextReferences...) {
				matches = matches || ref.LibraryID == lib.ID
			}
			if !matches {
				continue
			}
			var episodes missingEpisodesDetail
			var collection missingCollectionDetail
			switch f.Kind {
			case store.KindMissingMovie:
				lib.Missing++
			case store.KindMissingCollection:
				_ = json.Unmarshal([]byte(f.Details), &collection)
				lib.Missing += len(collection.MissingParts)
			default:
				_ = json.Unmarshal([]byte(f.Details), &episodes)
				lib.Missing += len(episodes.MissingEpisodes)
			}
		}
	}
}
