// Package scanner compares a Jellyfin library against TMDB to discover missing
// seasons, missing episodes and missing entries of movie collections.
package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

// JellyfinAPI is the subset of the Jellyfin client used by the scanner.
type JellyfinAPI interface {
	ResolveUserID(ctx context.Context, configured string) (string, error)
	ItemsInLibrary(ctx context.Context, userID, libraryID string) ([]jellyfin.Item, error)
	Episodes(ctx context.Context, userID, seriesID string) ([]jellyfin.Item, error)
}

// TMDBAPI is the subset of the TMDB client used by the scanner.
type TMDBAPI interface {
	Movie(ctx context.Context, id int64) (tmdb.Movie, error)
	Collection(ctx context.Context, id int64) (tmdb.Collection, error)
	TV(ctx context.Context, id int64) (tmdb.TVShow, error)
	Season(ctx context.Context, tvID int64, seasonNumber int) (tmdb.Season, error)
	SearchMovie(ctx context.Context, title string, year int) ([]tmdb.MovieSearchResult, error)
	SearchTV(ctx context.Context, name string, year int) ([]tmdb.TVSearchResult, error)
}

// Result summarises a scan.
type Result struct {
	Findings         []store.Finding
	LibrariesScanned int
	ItemsScanned     int
	Libraries        []store.LibrarySummary
	Media            []store.MediaStat
}

// Reporter receives live progress updates during a scan.
type Reporter interface {
	// SetCurrent reports the title currently being processed.
	SetCurrent(name string)
	// LibraryStart announces a library and its total item count.
	LibraryStart(id, name string, total int)
	// ItemDone marks an item as processed, adding any missing count to its library.
	ItemDone(libID string, missing int)
}

type nopReporter struct{}

func (nopReporter) SetCurrent(string)                {}
func (nopReporter) LibraryStart(string, string, int) {}
func (nopReporter) ItemDone(string, int)             {}

// Scanner runs a single comparison pass.
type Scanner struct {
	jf       JellyfinAPI
	td       TMDBAPI
	settings config.Settings
	log      *slog.Logger
	now      func() time.Time
	reporter Reporter
}

// New creates a Scanner. The logger may be nil.
func New(jf JellyfinAPI, td TMDBAPI, settings config.Settings, log *slog.Logger) *Scanner {
	if log == nil {
		log = slog.Default()
	}
	return &Scanner{jf: jf, td: td, settings: settings, log: log, now: time.Now, reporter: nopReporter{}}
}

// SetReporter installs a progress reporter (nil restores the no-op reporter).
func (s *Scanner) SetReporter(r Reporter) {
	if r == nil {
		r = nopReporter{}
	}
	s.reporter = r
}

// missingEpisodesDetail / missingCollectionDetail are serialised into the
// finding's Details field.
type missingEpisodesDetail struct {
	SeasonNumber    int    `json:"seasonNumber"`
	EpisodeCount    int    `json:"episodeCount"`
	MissingEpisodes []int  `json:"missingEpisodes"`
	PosterPath      string `json:"posterPath,omitempty"`
}

type missingPart struct {
	TMDBID int64   `json:"tmdbId"`
	Title  string  `json:"title"`
	Year   string  `json:"year,omitempty"`
	Rating float64 `json:"rating,omitempty"`
}

type missingCollectionDetail struct {
	CollectionID   int64         `json:"collectionId"`
	CollectionName string        `json:"collectionName"`
	PosterPath     string        `json:"posterPath,omitempty"`
	MissingParts   []missingPart `json:"missingParts"`
}

// Run performs the scan over all enabled libraries.
func (s *Scanner) Run(ctx context.Context) (Result, error) {
	var res Result

	userID, err := s.jf.ResolveUserID(ctx, s.settings.Jellyfin.UserID)
	if err != nil {
		return res, err
	}

	libNames := map[string]string{}
	for _, l := range s.settings.Libraries {
		libNames[l.ID] = l.Name
	}

	// Gather all items across enabled libraries.
	type libItem struct {
		libID string
		item  jellyfin.Item
	}
	var movies, series []libItem
	summaries := map[string]*store.LibrarySummary{}
	var order []string

	for _, libID := range s.settings.EnabledLibraryIDs() {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		items, err := s.jf.ItemsInLibrary(ctx, userID, libID)
		if err != nil {
			return res, err
		}
		res.LibrariesScanned++
		sum := &store.LibrarySummary{ID: libID, Name: libNames[libID]}
		for _, it := range items {
			switch it.Type {
			case "Movie":
				movies = append(movies, libItem{libID, it})
				sum.Total++
			case "Series":
				series = append(series, libItem{libID, it})
				sum.Total++
			}
		}
		summaries[libID] = sum
		order = append(order, libID)
		s.reporter.LibraryStart(libID, sum.Name, sum.Total)
	}

	// --- Movies: build owned-TMDB set, then evaluate collections. ---
	ownedMovie := map[int64]bool{}
	movieTMDB := make(map[string]int64, len(movies))
	for _, m := range movies {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		s.reporter.SetCurrent(m.item.Name)
		id := s.resolveMovieID(ctx, m.item)
		if id != 0 {
			ownedMovie[id] = true
			movieTMDB[m.item.ID] = id
		}
	}

	processedCollections := map[int64]bool{}
	for _, m := range movies {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.ItemsScanned++
		if sum := summaries[m.libID]; sum != nil {
			sum.Scanned++
		}
		s.reporter.SetCurrent(m.item.Name)

		missingCount := 0
		if id := movieTMDB[m.item.ID]; id != 0 {
			movie, err := s.td.Movie(ctx, id)
			if err != nil {
				s.log.Warn("tmdb movie lookup failed", "title", m.item.Name, "tmdbId", id, "err", err)
			} else {
				res.Media = append(res.Media, movieStat(movie))
				missingCount = s.evalCollection(ctx, m.libID, libNames[m.libID], m.item, movie, ownedMovie, processedCollections, &res)
			}
		}
		if sum := summaries[m.libID]; sum != nil {
			sum.Missing += missingCount
		}
		s.reporter.ItemDone(m.libID, missingCount)
	}

	// --- Series: evaluate seasons and episodes. ---
	for _, sv := range series {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.ItemsScanned++
		if sum := summaries[sv.libID]; sum != nil {
			sum.Scanned++
		}
		s.reporter.SetCurrent(sv.item.Name)
		missing := s.scanSeries(ctx, userID, sv.libID, libNames[sv.libID], sv.item, &res)
		if sum := summaries[sv.libID]; sum != nil {
			sum.Missing += missing
		}
		s.reporter.ItemDone(sv.libID, missing)
	}

	for _, libID := range order {
		res.Libraries = append(res.Libraries, *summaries[libID])
	}

	return res, nil
}

// scanMovieCollection evaluates a movie's TMDB collection and appends a finding
// for any missing, released parts. It returns the number of missing parts.
// evalCollection evaluates an already-fetched movie's TMDB collection and
// appends a finding for any missing, released parts. It returns the number of
// missing parts.
func (s *Scanner) evalCollection(ctx context.Context, libID, libName string, item jellyfin.Item, movie tmdb.Movie, ownedMovie, processed map[int64]bool, res *Result) int {
	if movie.BelongsToCollection == nil {
		return 0
	}
	cid := movie.BelongsToCollection.ID
	if processed[cid] {
		return 0
	}
	processed[cid] = true

	col, err := s.td.Collection(ctx, cid)
	if err != nil {
		s.log.Warn("tmdb collection lookup failed", "collection", movie.BelongsToCollection.Name, "err", err)
		return 0
	}
	var missing []missingPart
	for _, p := range col.Parts {
		if ownedMovie[p.ID] {
			continue
		}
		if !s.released(p.ReleaseDate) {
			continue
		}
		missing = append(missing, missingPart{
			TMDBID: p.ID,
			Title:  p.Title,
			Year:   yearOf(p.ReleaseDate),
			Rating: p.VoteAverage,
		})
	}
	if len(missing) == 0 {
		return 0
	}
	detail, _ := json.Marshal(missingCollectionDetail{
		CollectionID:   col.ID,
		CollectionName: col.Name,
		PosterPath:     col.PosterPath,
		MissingParts:   missing,
	})
	res.Findings = append(res.Findings, store.Finding{
		Kind:        store.KindMissingCollection,
		MediaType:   store.MediaMovie,
		LibraryID:   libID,
		LibraryName: libName,
		Title:       col.Name,
		TMDBID:      col.ID,
		JellyfinID:  item.ID,
		Summary:     summaryCollection(col.Name, len(missing)),
		Details:     string(detail),
	})
	return len(missing)
}

func (s *Scanner) scanSeries(ctx context.Context, userID, libID, libName string, item jellyfin.Item, res *Result) int {
	id := s.resolveSeriesID(ctx, item)
	if id == 0 {
		return 0
	}
	tv, err := s.td.TV(ctx, id)
	if err != nil {
		s.log.Warn("tmdb tv lookup failed", "title", item.Name, "tmdbId", id, "err", err)
		return 0
	}
	res.Media = append(res.Media, store.MediaStat{
		Type:    store.MediaSeries,
		Title:   item.Name,
		Year:    yearInt(tv.FirstAirDate),
		Rating:  tv.VoteAverage,
		Runtime: firstInt(tv.EpisodeRunTime),
		Genres:  genreNames(tv.Genres),
	})
	eps, err := s.jf.Episodes(ctx, userID, item.ID)
	if err != nil {
		s.log.Warn("jellyfin episodes failed", "title", item.Name, "err", err)
		return 0
	}
	present := map[int]map[int]bool{}
	for _, ep := range eps {
		if ep.ParentIndexNumber == nil || ep.IndexNumber == nil {
			continue
		}
		sn, en := *ep.ParentIndexNumber, *ep.IndexNumber
		if present[sn] == nil {
			present[sn] = map[int]bool{}
		}
		present[sn][en] = true
	}

	missingTotal := 0
	for _, season := range tv.Seasons {
		if season.SeasonNumber == 0 && !s.settings.Scan.IncludeSpecials {
			continue
		}
		if season.EpisodeCount == 0 {
			continue
		}
		presentEps := present[season.SeasonNumber]

		if len(presentEps) == 0 {
			// Possibly a whole missing season; confirm it has aired episodes.
			aired := s.airedEpisodes(ctx, id, season.SeasonNumber)
			if len(aired) == 0 {
				continue
			}
			detail, _ := json.Marshal(missingEpisodesDetail{
				SeasonNumber:    season.SeasonNumber,
				EpisodeCount:    season.EpisodeCount,
				MissingEpisodes: aired,
				PosterPath:      tv.PosterPath,
			})
			sn := season.SeasonNumber
			res.Findings = append(res.Findings, store.Finding{
				Kind:         store.KindMissingSeason,
				MediaType:    store.MediaSeries,
				LibraryID:    libID,
				LibraryName:  libName,
				Title:        item.Name,
				TMDBID:       id,
				JellyfinID:   item.ID,
				SeasonNumber: &sn,
				Summary:      summarySeason(item.Name, season.SeasonNumber, len(aired)),
				Details:      string(detail),
			})
			missingTotal += len(aired)
			continue
		}

		if len(presentEps) >= season.EpisodeCount {
			continue // assume complete
		}

		aired := s.airedEpisodes(ctx, id, season.SeasonNumber)
		var missing []int
		for _, en := range aired {
			if !presentEps[en] {
				missing = append(missing, en)
			}
		}
		if len(missing) == 0 {
			continue
		}
		detail, _ := json.Marshal(missingEpisodesDetail{
			SeasonNumber:    season.SeasonNumber,
			EpisodeCount:    season.EpisodeCount,
			MissingEpisodes: missing,
			PosterPath:      tv.PosterPath,
		})
		sn := season.SeasonNumber
		res.Findings = append(res.Findings, store.Finding{
			Kind:         store.KindMissingEpisodes,
			MediaType:    store.MediaSeries,
			LibraryID:    libID,
			LibraryName:  libName,
			Title:        item.Name,
			TMDBID:       id,
			JellyfinID:   item.ID,
			SeasonNumber: &sn,
			Summary:      summaryEpisodes(item.Name, season.SeasonNumber, len(missing)),
			Details:      string(detail),
		})
		missingTotal += len(missing)
	}
	return missingTotal
}

// airedEpisodes returns the episode numbers of a season that have already aired.
func (s *Scanner) airedEpisodes(ctx context.Context, tvID int64, seasonNumber int) []int {
	sd, err := s.td.Season(ctx, tvID, seasonNumber)
	if err != nil {
		s.log.Warn("tmdb season lookup failed", "tvId", tvID, "season", seasonNumber, "err", err)
		return nil
	}
	var out []int
	for _, ep := range sd.Episodes {
		if ep.EpisodeNumber == 0 && !s.settings.Scan.IncludeSpecials {
			continue
		}
		if !s.released(ep.AirDate) {
			continue
		}
		out = append(out, ep.EpisodeNumber)
	}
	return out
}

func (s *Scanner) resolveMovieID(ctx context.Context, item jellyfin.Item) int64 {
	if v, ok := item.ProviderID("Tmdb"); ok {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	results, err := s.td.SearchMovie(ctx, item.Name, item.ProductionYear)
	if err != nil {
		s.log.Warn("tmdb movie search failed", "title", item.Name, "err", err)
		return 0
	}
	if len(results) > 0 {
		return results[0].ID
	}
	return 0
}

func (s *Scanner) resolveSeriesID(ctx context.Context, item jellyfin.Item) int64 {
	if v, ok := item.ProviderID("Tmdb"); ok {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	results, err := s.td.SearchTV(ctx, item.Name, item.ProductionYear)
	if err != nil {
		s.log.Warn("tmdb tv search failed", "title", item.Name, "err", err)
		return 0
	}
	if len(results) > 0 {
		return results[0].ID
	}
	return 0
}

// released reports whether a TMDB date (YYYY-MM-DD) is non-empty and not in the
// future relative to the scanner's clock.
func (s *Scanner) released(date string) bool {
	d := strings.TrimSpace(date)
	if d == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return false
	}
	return !t.After(s.now())
}

func yearOf(date string) string {
	d := strings.TrimSpace(date)
	if len(d) >= 4 {
		return d[:4]
	}
	return ""
}

func movieStat(m tmdb.Movie) store.MediaStat {
	return store.MediaStat{
		Type:    store.MediaMovie,
		Title:   m.Title,
		Year:    yearInt(m.ReleaseDate),
		Rating:  m.VoteAverage,
		Runtime: m.Runtime,
		Genres:  genreNames(m.Genres),
	}
}

func genreNames(gs []tmdb.Genre) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

func yearInt(date string) int {
	d := strings.TrimSpace(date)
	if len(d) >= 4 {
		if n, err := strconv.Atoi(d[:4]); err == nil {
			return n
		}
	}
	return 0
}

func firstInt(xs []int) int {
	if len(xs) > 0 {
		return xs[0]
	}
	return 0
}
