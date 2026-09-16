// Package scanner compares a normalized media catalog against TMDB to discover missing
// seasons, missing episodes and missing entries of movie collections.
package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

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
	Warnings         []string
	Findings         []store.Finding
	LibrariesScanned int
	ItemsScanned     int
	Libraries        []store.LibrarySummary
	Media            []store.MediaStat
	Upcoming         []store.UpcomingItem
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
	catalog  media.Catalog
	td       TMDBAPI
	settings config.Settings
	log      *slog.Logger
	now      func() time.Time
	reporter Reporter
}

// New creates a Scanner. The logger may be nil.
func New(catalog media.Catalog, td TMDBAPI, settings config.Settings, log *slog.Logger) *Scanner {
	if log == nil {
		log = slog.Default()
	}
	return &Scanner{catalog: catalog, td: td, settings: settings, log: log, now: time.Now, reporter: nopReporter{}}
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
	// AirDates maps an episode number to its ISO 8601 air date. Added later
	// than the fields above, so findings stored by an older version simply do
	// not carry it; the retrospective view skips those entries.
	AirDates map[string]string `json:"airDates,omitempty"`
}

type missingPart struct {
	TMDBID      int64   `json:"tmdbId"`
	Title       string  `json:"title"`
	Year        string  `json:"year,omitempty"`
	Rating      float64 `json:"rating,omitempty"`
	IMDbID      string  `json:"imdbId,omitempty"`
	ReleaseDate string  `json:"releaseDate,omitempty"` // ISO 8601, added later
}

type missingCollectionDetail struct {
	CollectionID   int64         `json:"collectionId"`
	CollectionName string        `json:"collectionName"`
	PosterPath     string        `json:"posterPath,omitempty"`
	MissingParts   []missingPart `json:"missingParts"`
}

// Run evaluates the normalized catalog. The source layer resolves and persists
// occurrence identities before this boundary so live views reuse the same union.
func (s *Scanner) Run(ctx context.Context) (Result, error) {
	var res Result
	run := activity.FromContext(ctx)

	res.Warnings = append(res.Warnings, s.catalog.Warnings...)
	for _, warning := range s.catalog.Warnings {
		if !strings.HasPrefix(warning, "Unresolved title: ") {
			run.ReportLegacy(warning, "")
		}
	}
	libNames := map[string]string{}
	for _, l := range s.catalog.Libraries {
		libNames[l.ID] = l.Name
	}

	// Gather all items across enabled libraries.
	type libItem struct {
		libID string
		item  media.Item
	}
	var movies, series []libItem
	summaries := map[string]*store.LibrarySummary{}
	var order []string

	for _, lib := range s.catalog.Libraries {
		libID := lib.ID
		res.LibrariesScanned++
		sum := &store.LibrarySummary{ID: libID, Name: libNames[libID]}
		summaries[libID] = sum
		order = append(order, libID)
	}
	for _, it := range media.Merge(s.catalog.Items) {
		libID := media.VirtualID
		if len(it.References) > 0 {
			libID = it.References[0].LibraryID
		}
		if it.TMDBID() == 0 {
			warning := "Unresolved title: " + it.Name
			name := ""
			if len(it.References) > 0 {
				name = it.References[0].Name
			}
			run.UnresolvedTitle(it.ID, name, it.Name)
			if !slices.Contains(s.catalog.Warnings, warning) {
				res.Warnings = append(res.Warnings, warning)
				s.log.Warn("title skipped: identity unresolved", "title", it.Name)
			}
			res.Media = append(res.Media, s.basicStat(it, libID, libNames[libID]))
		}
		if it.Type == media.Movie {
			movies = append(movies, libItem{libID, it})
		}
		if it.Type == media.Series {
			series = append(series, libItem{libID, it})
		}
		for _, id := range itemLibraries(it, libID) {
			if sum := summaries[id]; sum != nil {
				sum.Total++
			}
		}
	}
	for _, libID := range order {
		sum := summaries[libID]
		s.reporter.LibraryStart(libID, sum.Name, sum.Total)
	}
	run.Phase(activity.Metadata, len(movies)+len(series))
	itemDone := func(item media.Item, fallback string, missing int) {
		for _, id := range itemLibraries(item, fallback) {
			if sum := summaries[id]; sum != nil {
				sum.Scanned++
				sum.Missing += missing
				s.reporter.ItemDone(id, missing)
			}
		}
	}

	// --- Movies: build owned-TMDB set, then evaluate collections. ---
	ownedMovie := map[int64]bool{}
	movieTMDB := make(map[string]int64, len(movies))
	for _, m := range movies {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		s.reporter.SetCurrent(m.item.Name)
		id := m.item.TMDBID()
		if id != 0 {
			if !m.item.WatchOnly {
				ownedMovie[id] = true
			}
			movieTMDB[m.item.ID] = id
		}
	}

	processedCollections := map[int64]bool{}
	for _, m := range movies {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.ItemsScanned++
		s.reporter.SetCurrent(m.item.Name)

		run.Current(m.item.Name)
		beforeWarnings := len(res.Warnings)
		missingCount := 0
		if id := movieTMDB[m.item.ID]; id != 0 {
			movie, err := s.td.Movie(ctx, id)
			if err != nil {
				res.Warnings = append(res.Warnings, "Movie metadata unavailable: "+m.item.Name)
				run.ReportLegacy("Movie metadata unavailable: "+m.item.Name, "")
				res.Media = append(res.Media, s.basicStat(m.item, m.libID, libNames[m.libID]))
				s.log.Warn("tmdb movie lookup failed", "title", m.item.Name, "tmdbId", id, "err", err)
			} else {
				stat := movieStat(movie, m.item, m.libID, libNames[m.libID])
				stat.Provenance = provenance(m.item)
				res.Media = append(res.Media, stat)
				missingCount = s.evalCollection(ctx, m.libID, libNames[m.libID], m.item, movie, ownedMovie, processedCollections, &res)
				if m.item.WatchOnly {
					s.watchMovie(movie, m.item, m.libID, libNames[m.libID], &res)
				}
			}
		}
		itemDone(m.item, m.libID, missingCount)
		for _, warning := range res.Warnings[beforeWarnings:] {
			run.ReportLegacy(warning, "")
		}
		run.Advance(len(res.Warnings) > beforeWarnings, m.item.TMDBID() == 0 && !slices.Contains(s.catalog.Warnings, "Unresolved title: "+m.item.Name))
	}

	// --- Series: evaluate seasons and episodes. ---
	for _, sv := range series {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.ItemsScanned++
		s.reporter.SetCurrent(sv.item.Name)
		run.Current(sv.item.Name)
		beforeWarnings := len(res.Warnings)
		missing := s.scanSeries(ctx, sv.libID, libNames[sv.libID], sv.item, &res)
		itemDone(sv.item, sv.libID, missing)
		for _, warning := range res.Warnings[beforeWarnings:] {
			run.ReportLegacy(warning, "")
		}
		run.Advance(len(res.Warnings) > beforeWarnings, sv.item.TMDBID() == 0 && !slices.Contains(s.catalog.Warnings, "Unresolved title: "+sv.item.Name))
	}

	for _, libID := range order {
		res.Libraries = append(res.Libraries, *summaries[libID])
	}
	sortUpcoming(res.Upcoming)
	dedupeMovies(&res)
	for i := range res.Findings {
		res.Findings[i].Unconfirmed = len(res.Warnings) > 0
	}
	for i := range res.Media {
		res.Media[i].Unconfirmed = len(res.Warnings) > 0
	}
	for i := range res.Upcoming {
		res.Upcoming[i].Unconfirmed = len(res.Warnings) > 0
	}

	return res, nil
}

// scanMovieCollection evaluates a movie's TMDB collection and appends a finding
// for any missing, released parts. It returns the number of missing parts.
// evalCollection evaluates an already-fetched movie's TMDB collection and
// appends a finding for any missing, released parts. It returns the number of
// missing parts.
func (s *Scanner) evalCollection(ctx context.Context, libID, libName string, item media.Item, movie tmdb.Movie, ownedMovie, processed map[int64]bool, res *Result) int {
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
		res.Warnings = append(res.Warnings, "Collection metadata unavailable: "+movie.BelongsToCollection.Name)
		activity.FromContext(ctx).ReportLegacy("Collection metadata unavailable: "+movie.BelongsToCollection.Name, "")
		s.log.Warn("tmdb collection lookup failed", "collection", movie.BelongsToCollection.Name, "err", err)
		return 0
	}
	var missing []missingPart
	for _, p := range col.Parts {
		if ownedMovie[p.ID] {
			continue
		}
		if !s.released(p.ReleaseDate) {
			poster := p.PosterPath
			if poster == "" {
				poster = col.PosterPath
			}
			res.Upcoming = append(res.Upcoming, store.UpcomingItem{
				Provenance:   store.Provenance{ContextReferences: item.References, WatchOnly: item.WatchOnly},
				Kind:         store.UpcomingCollectionPart,
				MediaType:    store.MediaMovie,
				Title:        p.Title,
				SourceTitle:  col.Name,
				SourceTMDBID: col.ID,
				TMDBID:       p.ID,
				ReleaseDate:  strings.TrimSpace(p.ReleaseDate),
				PosterPath:   poster,
				Overview:     p.Overview,
				Rating:       p.VoteAverage,
				LibraryID:    libID,
				LibraryName:  libName,
			})
			continue
		}
		missing = append(missing, missingPart{
			TMDBID:      p.ID,
			Title:       p.Title,
			Year:        yearOf(p.ReleaseDate),
			Rating:      p.VoteAverage,
			ReleaseDate: strings.TrimSpace(p.ReleaseDate),
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
		Provenance:  store.Provenance{WatchOnly: item.WatchOnly, ContextReferences: item.References},
		Kind:        store.KindMissingCollection,
		MediaType:   store.MediaMovie,
		LibraryID:   libID,
		LibraryName: libName,
		Title:       col.Name,
		TMDBID:      col.ID,
		Summary:     summaryCollection(col.Name, len(missing)),
		Details:     string(detail),
	})
	return len(missing)
}

func (s *Scanner) scanSeries(ctx context.Context, libID, libName string, item media.Item, res *Result) int {
	id := item.TMDBID()
	if id == 0 {
		return 0
	}
	tv, err := s.td.TV(ctx, id)
	if err != nil {
		res.Warnings = append(res.Warnings, "Series metadata unavailable: "+item.Name)
		activity.FromContext(ctx).ReportLegacy("Series metadata unavailable: "+item.Name, "")
		res.Media = append(res.Media, s.basicStat(item, libID, libName))
		s.log.Warn("tmdb tv lookup failed", "title", item.Name, "tmdbId", id, "err", err)
		return 0
	}
	stat := store.MediaStat{
		Provenance:  provenance(item),
		Type:        store.MediaSeries,
		Title:       item.Name,
		Year:        yearInt(tv.FirstAirDate),
		Rating:      tv.VoteAverage,
		Runtime:     avgInt(tv.EpisodeRunTime),
		Genres:      genreNames(tv.Genres),
		LibraryID:   libID,
		LibraryName: libName,
		TMDBID:      id,
		Language:    tv.OriginalLanguage,
		Country:     firstString(tv.OriginCountry),
	}
	imdbID, _ := item.ProviderID("Imdb")
	eps := item.Episodes
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
			airedEps := seasons.aired(ctx, season.SeasonNumber)
			aired := episodeNumbers(airedEps)
			if len(aired) == 0 {
				continue
			}
			detail, _ := json.Marshal(missingEpisodesDetail{
				SeasonNumber:    season.SeasonNumber,
				EpisodeCount:    season.EpisodeCount,
				MissingEpisodes: aired,
				PosterPath:      tv.PosterPath,
				IMDbID:          imdbID,
				AirDates:        airDatesOf(airedEps, aired),
			})
			sn := season.SeasonNumber
			res.Findings = append(res.Findings, store.Finding{
				Provenance:   provenance(item),
				Kind:         store.KindMissingSeason,
				MediaType:    store.MediaSeries,
				LibraryID:    libID,
				LibraryName:  libName,
				Title:        item.Name,
				TMDBID:       id,
				SeasonNumber: &sn,
				Summary:      summarySeason(item.Name, season.SeasonNumber, len(aired)),
				Details:      string(detail),
			})
			missingTotal += len(aired)
			continue
		}

		airedEps := seasons.aired(ctx, season.SeasonNumber)
		aired := episodeNumbers(airedEps)
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
			AirDates:        airDatesOf(airedEps, missing),
		})
		sn := season.SeasonNumber
		res.Findings = append(res.Findings, store.Finding{
			Provenance:   provenance(item),
			Kind:         store.KindMissingEpisodes,
			MediaType:    store.MediaSeries,
			LibraryID:    libID,
			LibraryName:  libName,
			Title:        item.Name,
			TMDBID:       id,
			SeasonNumber: &sn,
			Summary:      summaryEpisodes(item.Name, season.SeasonNumber, len(missing)),
			Details:      string(detail),
		})
		missingTotal += len(missing)
	}
	s.collectUpcomingEpisodes(tv, item, libID, libName, seasons, res)
	if seasons.failed {
		res.Warnings = append(res.Warnings, "Season metadata unavailable: "+item.Name)
	}
	return missingTotal
}

// collectUpcomingEpisodes records the announced episodes of an owned series.
// The season details were already fetched for the gap detection, so this costs
// no additional TMDB requests; next_episode_to_air covers announced seasons
// that TMDB does not list episodes for yet.
func (s *Scanner) collectUpcomingEpisodes(tv tmdb.TVShow, item media.Item, libID, libName string, seasons *seasonCache, res *Result) {
	seen := map[[2]int]bool{}
	add := func(ep tmdb.Episode) {
		key := [2]int{ep.SeasonNumber, ep.EpisodeNumber}
		if seen[key] {
			return
		}
		seen[key] = true
		res.Upcoming = append(res.Upcoming, store.UpcomingItem{
			Provenance:    provenance(item),
			Kind:          store.UpcomingEpisode,
			MediaType:     store.MediaSeries,
			Title:         ep.Name,
			SourceTitle:   item.Name,
			SourceTMDBID:  tv.ID,
			TMDBID:        tv.ID,
			SeasonNumber:  ep.SeasonNumber,
			EpisodeNumber: ep.EpisodeNumber,
			ReleaseDate:   strings.TrimSpace(ep.AirDate),
			PosterPath:    tv.PosterPath,
			Rating:        tv.VoteAverage,
			LibraryID:     libID,
			LibraryName:   libName,
		})
	}
	for _, ep := range seasons.upcoming() {
		add(ep)
	}
	if next := tv.NextEpisodeToAir; next != nil && strings.TrimSpace(next.AirDate) != "" && !s.released(next.AirDate) {
		if next.SeasonNumber != 0 || s.settings.Scan.IncludeSpecials {
			add(*next)
		}
	}
}

// sortUpcoming orders releases chronologically, with undated entries last.
func sortUpcoming(items []store.UpcomingItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if (a.ReleaseDate == "") != (b.ReleaseDate == "") {
			return b.ReleaseDate == ""
		}
		if a.ReleaseDate != b.ReleaseDate {
			return a.ReleaseDate < b.ReleaseDate
		}
		if a.SourceTitle != b.SourceTitle {
			return a.SourceTitle < b.SourceTitle
		}
		if a.SeasonNumber != b.SeasonNumber {
			return a.SeasonNumber < b.SeasonNumber
		}
		return a.EpisodeNumber < b.EpisodeNumber
	})
}

// seasonCache fetches a series' season details at most once per season, so the
// gap detection, the episode ratings and the upcoming releases share the same
// TMDB responses.
type seasonCache struct {
	failed       bool
	s            *Scanner
	tvID         int64
	loaded       map[int]bool
	airedByNo    map[int][]tmdb.Episode
	upcomingByNo map[int][]tmdb.Episode
}

func (s *Scanner) newSeasonCache(tvID int64) *seasonCache {
	return &seasonCache{
		s:            s,
		tvID:         tvID,
		loaded:       map[int]bool{},
		airedByNo:    map[int][]tmdb.Episode{},
		upcomingByNo: map[int][]tmdb.Episode{},
	}
}

// load fetches a season once and splits its episodes into aired and upcoming.
func (c *seasonCache) load(ctx context.Context, seasonNumber int) {
	if c.loaded[seasonNumber] {
		return
	}
	c.loaded[seasonNumber] = true
	sd, err := c.s.td.Season(ctx, c.tvID, seasonNumber)
	if err != nil {
		c.failed = true
		c.s.log.Warn("tmdb season lookup failed", "tvId", c.tvID, "season", seasonNumber, "err", err)
		return
	}
	for _, ep := range sd.Episodes {
		if ep.EpisodeNumber == 0 && !c.s.settings.Scan.IncludeSpecials {
			continue
		}
		if c.s.released(ep.AirDate) {
			c.airedByNo[seasonNumber] = append(c.airedByNo[seasonNumber], ep)
			continue
		}
		if strings.TrimSpace(ep.AirDate) == "" {
			continue
		}
		if ep.SeasonNumber == 0 {
			ep.SeasonNumber = seasonNumber
		}
		c.upcomingByNo[seasonNumber] = append(c.upcomingByNo[seasonNumber], ep)
	}
}

// aired returns the already-aired episodes of a season.
func (c *seasonCache) aired(ctx context.Context, seasonNumber int) []tmdb.Episode {
	c.load(ctx, seasonNumber)
	return c.airedByNo[seasonNumber]
}

// upcoming returns the dated but not yet aired episodes of every season
// fetched so far, ordered by season number.
func (c *seasonCache) upcoming() []tmdb.Episode {
	nums := make([]int, 0, len(c.upcomingByNo))
	for n := range c.upcomingByNo {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out []tmdb.Episode
	for _, n := range nums {
		out = append(out, c.upcomingByNo[n]...)
	}
	return out
}

func episodeNumbers(eps []tmdb.Episode) []int {
	out := make([]int, 0, len(eps))
	for _, ep := range eps {
		out = append(out, ep.EpisodeNumber)
	}
	return out
}

// airDatesOf maps the wanted episode numbers to their air dates, so the
// retrospective view can place a gap on a timeline. Episodes without a date are
// omitted rather than stored empty.
func airDatesOf(eps []tmdb.Episode, wanted []int) map[string]string {
	if len(wanted) == 0 {
		return nil
	}
	want := make(map[int]bool, len(wanted))
	for _, n := range wanted {
		want[n] = true
	}
	out := make(map[string]string, len(wanted))
	for _, ep := range eps {
		if !want[ep.EpisodeNumber] {
			continue
		}
		if d := strings.TrimSpace(ep.AirDate); d != "" {
			out[strconv.Itoa(ep.EpisodeNumber)] = d
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

func movieStat(m tmdb.Movie, item media.Item, libID, libName string) store.MediaStat {
	st := store.MediaStat{
		Provenance:  provenance(item),
		Type:        store.MediaMovie,
		Title:       m.Title,
		Year:        yearInt(m.ReleaseDate),
		Rating:      m.VoteAverage,
		Runtime:     m.Runtime,
		Genres:      genreNames(m.Genres),
		LibraryID:   libID,
		LibraryName: libName,
		TMDBID:      m.ID,
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

// ownedSeasons pairs the real-owned episode union with every season TMDB
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
