// Package scanner compares a Jellyfin library against TMDB to discover missing
// seasons, missing episodes and missing entries of movie collections.
package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
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
	IMDbID          string `json:"imdbId,omitempty"`
}

type missingPart struct {
	TMDBID int64   `json:"tmdbId"`
	Title  string  `json:"title"`
	Year   string  `json:"year,omitempty"`
	Rating float64 `json:"rating,omitempty"`
	IMDbID string  `json:"imdbId,omitempty"`
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
				res.Media = append(res.Media, movieStat(movie, m.item, m.libID, libNames[m.libID]))
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
	for i := range missing {
		if pm, merr := s.td.Movie(ctx, missing[i].TMDBID); merr == nil {
			missing[i].IMDbID = pm.IMDbID
		}
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
	stat := store.MediaStat{
		Type:        store.MediaSeries,
		Title:       item.Name,
		Year:        yearInt(tv.FirstAirDate),
		Rating:      tv.VoteAverage,
		Runtime:     avgInt(tv.EpisodeRunTime),
		Genres:      genreNames(tv.Genres),
		LibraryID:   libID,
		LibraryName: libName,
		TMDBID:      id,
		JellyfinID:  item.ID,
		Language:    tv.OriginalLanguage,
		Country:     firstString(tv.OriginCountry),
	}
	imdbID, _ := item.ProviderID("Imdb")
	eps, err := s.jf.Episodes(ctx, userID, item.ID)
	if err != nil {
		s.log.Warn("jellyfin episodes failed", "title", item.Name, "err", err)
		res.Media = append(res.Media, stat)
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
	seasons := s.newSeasonCache(id)
	stat.Seasons = ownedSeasons(ctx, tv, present, seasons, s.settings.Scan.EpisodeRatings, s.settings.Scan.IncludeSpecials)
	for _, sn := range stat.Seasons {
		stat.Episodes += sn.Episodes
		stat.TotalEpisodes += sn.Total
	}
	stat.Runtime = episodeRuntime(tv, stat.Seasons)
	stat.Minutes = seriesMinutes(stat.Seasons, stat.Runtime)
	res.Media = append(res.Media, stat)

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
			aired := episodeNumbers(seasons.aired(ctx, season.SeasonNumber))
			if len(aired) == 0 {
				continue
			}
			detail, _ := json.Marshal(missingEpisodesDetail{
				SeasonNumber:    season.SeasonNumber,
				EpisodeCount:    season.EpisodeCount,
				MissingEpisodes: aired,
				PosterPath:      tv.PosterPath,
				IMDbID:          imdbID,
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

		aired := episodeNumbers(seasons.aired(ctx, season.SeasonNumber))
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
			IMDbID:          imdbID,
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

// seasonCache fetches a series' season details at most once per season, so the
// gap detection and the episode ratings share the same TMDB responses.
type seasonCache struct {
	s    *Scanner
	tvID int64
	byNo map[int][]tmdb.Episode
}

func (s *Scanner) newSeasonCache(tvID int64) *seasonCache {
	return &seasonCache{s: s, tvID: tvID, byNo: map[int][]tmdb.Episode{}}
}

// aired returns the already-aired episodes of a season.
func (c *seasonCache) aired(ctx context.Context, seasonNumber int) []tmdb.Episode {
	if eps, ok := c.byNo[seasonNumber]; ok {
		return eps
	}
	sd, err := c.s.td.Season(ctx, c.tvID, seasonNumber)
	if err != nil {
		c.s.log.Warn("tmdb season lookup failed", "tvId", c.tvID, "season", seasonNumber, "err", err)
		c.byNo[seasonNumber] = nil
		return nil
	}
	var out []tmdb.Episode
	for _, ep := range sd.Episodes {
		if ep.EpisodeNumber == 0 && !c.s.settings.Scan.IncludeSpecials {
			continue
		}
		if !c.s.released(ep.AirDate) {
			continue
		}
		out = append(out, ep)
	}
	c.byNo[seasonNumber] = out
	return out
}

func episodeNumbers(eps []tmdb.Episode) []int {
	out := make([]int, 0, len(eps))
	for _, ep := range eps {
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

func movieStat(m tmdb.Movie, item jellyfin.Item, libID, libName string) store.MediaStat {
	st := store.MediaStat{
		Type:        store.MediaMovie,
		Title:       m.Title,
		Year:        yearInt(m.ReleaseDate),
		Rating:      m.VoteAverage,
		Runtime:     m.Runtime,
		Genres:      genreNames(m.Genres),
		LibraryID:   libID,
		LibraryName: libName,
		TMDBID:      m.ID,
		JellyfinID:  item.ID,
		Language:    m.OriginalLanguage,
	}
	if len(m.ProductionCountries) > 0 {
		st.Country = m.ProductionCountries[0].Code
	}
	if m.BelongsToCollection != nil {
		st.CollectionID = m.BelongsToCollection.ID
		st.CollectionName = m.BelongsToCollection.Name
	}
	return st
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

func avgInt(xs []int) int {
	sum, n := 0, 0
	for _, x := range xs {
		if x > 0 {
			sum += x
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

func firstString(xs []string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return ""
}

// ownedSeasons pairs the episodes present in Jellyfin with every season TMDB
// knows about, so seasons that are entirely missing still show up in the
// statistics. With ratings enabled every season is fetched from TMDB to record
// the per-episode votes and runtimes. Seasons unknown to TMDB are appended.
func ownedSeasons(ctx context.Context, tv tmdb.TVShow, present map[int]map[int]bool, seasons *seasonCache, withRatings, includeSpecials bool) []store.SeasonStat {
	var out []store.SeasonStat
	known := map[int]bool{}
	for _, season := range tv.Seasons {
		if season.EpisodeCount == 0 {
			continue
		}
		if season.SeasonNumber == 0 && !includeSpecials {
			continue
		}
		known[season.SeasonNumber] = true
		st := store.SeasonStat{
			Number:   season.SeasonNumber,
			Episodes: len(present[season.SeasonNumber]),
			Total:    season.EpisodeCount,
		}
		if withRatings {
			st.Ratings, st.Rating = seasonRatings(seasons.aired(ctx, season.SeasonNumber), present[season.SeasonNumber])
		}
		out = append(out, st)
	}
	extra := make([]int, 0, len(present))
	for sn := range present {
		if known[sn] || (sn == 0 && !includeSpecials) {
			continue
		}
		extra = append(extra, sn)
	}
	sort.Ints(extra)
	for _, sn := range extra {
		out = append(out, store.SeasonStat{Number: sn, Episodes: len(present[sn]), Total: len(present[sn])})
	}
	return out
}

// episodeRuntime returns the runtime of a single episode: TMDB's per-show value
// when it has one, otherwise the average of the episodes actually seen.
func episodeRuntime(tv tmdb.TVShow, seasons []store.SeasonStat) int {
	if avg := avgInt(tv.EpisodeRunTime); avg > 0 {
		return avg
	}
	sum, n := 0, 0
	for _, sn := range seasons {
		for _, ep := range sn.Ratings {
			if ep.Minutes > 0 {
				sum += ep.Minutes
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// seriesMinutes totals the runtime of the owned episodes, falling back to the
// average episode runtime wherever TMDB has no per-episode value.
func seriesMinutes(seasons []store.SeasonStat, fallback int) int {
	total := 0
	for _, sn := range seasons {
		if len(sn.Ratings) == 0 {
			total += sn.Episodes * fallback
			continue
		}
		for _, ep := range sn.Ratings {
			if !ep.Owned {
				continue
			}
			if ep.Minutes > 0 {
				total += ep.Minutes
			} else {
				total += fallback
			}
		}
	}
	return total
}

// seasonRatings converts TMDB episodes into rating cells plus the season
// average over the episodes that carry a vote.
func seasonRatings(eps []tmdb.Episode, present map[int]bool) ([]store.EpisodeRating, float64) {
	if len(eps) == 0 {
		return nil, 0
	}
	out := make([]store.EpisodeRating, 0, len(eps))
	sum, rated := 0.0, 0
	for _, ep := range eps {
		out = append(out, store.EpisodeRating{
			Number:  ep.EpisodeNumber,
			Title:   ep.Name,
			Rating:  ep.VoteAverage,
			Minutes: ep.Runtime,
			Owned:   present[ep.EpisodeNumber],
		})
		if ep.VoteAverage > 0 {
			sum += ep.VoteAverage
			rated++
		}
	}
	if rated == 0 {
		return out, 0
	}
	return out, sum / float64(rated)
}
