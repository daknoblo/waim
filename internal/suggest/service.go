// Package suggest builds media recommendations from the shared real-owned catalog
// using TMDB (trending + per-title recommendations) and, optionally, a remote
// AI endpoint.
package suggest

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/ai"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/tmdbcache"
)

const (
	posterBase    = "https://image.tmdb.org/t/p/w154"
	sampleSize    = 12 // owned titles per media type used for recommendations
	trendingTake  = 12
	similarTake   = 18
	upcomingTake  = 12
	topGenres     = 3 // library genres fed into the discover endpoints
	aiOwnedNames  = 40
	generateLimit = 5 * time.Minute
)

// Item is a display-ready TMDB suggestion.
type Item struct {
	TMDBID      int64
	References  []media.Reference
	MediaType   string
	Title       string
	Year        string
	Rating      string
	Overview    string
	PosterURL   string
	TMDBLink    string
	ReleaseDate string
}

// AIItem is a display-ready AI suggestion.
type AIItem struct {
	Title      string
	Type       string
	Year       string
	Reason     string
	SearchLink string
}

// Result is a cached set of suggestions.
type Result struct {
	SourcesToken   string
	Revision       int64
	Trending       []Item
	Similar        []Item
	UpcomingTaste  []Item
	UpcomingRegion []Item
	AI             []AIItem
	AIEnabled      bool
	GeneratedAt    time.Time
	BasedOnRunID   int64
	Errors         []string
}

// Service builds and caches suggestions.
type Service struct {
	cfg   *config.Manager
	store *store.Store
	log   *slog.Logger
	now   func() time.Time

	mu               sync.RWMutex
	result           *Result
	cacheKey         string
	lastAttempt      time.Time
	savedDiagnostics activity.State
	running          atomic.Bool
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	epoch            uint64
	activities       *activity.Tracker
}

// New creates a suggestion service.
func New(cfg *config.Manager, st *store.Store, log *slog.Logger, activities ...*activity.Tracker) *Service {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{cfg: cfg, store: st, log: log, now: time.Now, ctx: ctx, cancel: cancel, activities: activity.New()}
	if len(activities) > 0 && activities[0] != nil {
		s.activities = activities[0]
	}
	s.loadCache()
	return s
}

// Close cooperatively cancels and joins background work before the store closes.
func (s *Service) Close() { s.mu.Lock(); s.cancel(); s.mu.Unlock(); s.wg.Wait() }

// Catalog edits invalidate in-flight work, not the displayed recommendations.
// Incompatible metadata/AI settings discard the old presentation instead.
func (s *Service) Invalidate() {
	key := s.settingsKey()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	if s.cacheKey != key {
		s.result = nil
		s.savedDiagnostics = activity.State{}
		s.cacheKey = key
	}
	if s.result == nil {
		s.lastAttempt = time.Time{}
	}
}

// Clear is called under exclusive reset admission, after SQLite KV is cleared.
func (s *Service) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	s.result = nil
	s.lastAttempt = time.Time{}
	s.savedDiagnostics = activity.State{}
	s.cacheKey = ""
}

// Running reports whether a generation is in progress.
func (s *Service) Running() bool { return s.running.Load() }

// Result returns the cached result (may be nil) and whether it is still fresh.
func (s *Service) Result() (*Result, bool) {
	key := s.settingsKey()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil || s.cacheKey != key {
		return nil, false
	}
	return s.result, s.now().Sub(s.result.GeneratedAt) < RefreshInterval
}

// NeedsRefresh uses the last attempt, so failures do not retry on every visit.
// New scans and virtual-collection edits do not expire this independent cache.
func (s *Service) NeedsRefresh(_ context.Context) bool {
	key := s.settingsKey()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cacheKey != key || s.lastAttempt.IsZero() || !s.now().Before(s.lastAttempt.Add(RefreshInterval))
}

// Generate rebuilds suggestions in the background, ignoring overlapping calls.
func (s *Service) Generate() { s.generate(time.Time{}, false) }

func (s *Service) generate(due time.Time, scheduled bool) {
	release, err := s.cfg.Gate().EnterScheduled(due)
	if err != nil {
		s.log.Warn("suggestions skipped during maintenance")
		return
	}
	s.mu.Lock()
	key := s.settingsKey()
	if s.ctx.Err() != nil || (scheduled && s.cacheKey == key && !s.lastAttempt.IsZero() && s.now().Before(s.lastAttempt.Add(RefreshInterval))) {
		s.mu.Unlock()
		release()
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		s.mu.Unlock()
		release()
		return
	}
	s.wg.Add(1)
	epoch := s.epoch
	if s.cacheKey != key {
		s.result = nil
		s.savedDiagnostics = activity.State{}
	}
	s.lastAttempt = s.now()
	s.cacheKey = key
	run := s.activities.Start(activity.Suggestions)
	s.mu.Unlock()
	go func() {
		defer release()
		defer s.wg.Done()
		defer s.running.Store(false)
		ctx, cancel := context.WithTimeout(s.ctx, generateLimit)
		defer cancel()
		ctx = activity.WithRun(ctx, run)
		res := s.build(ctx)
		run.Phase(activity.Persistence, -1)
		outcome := s.publish(ctx, res, epoch, key, run)
		if s.cfg.Get().TMDB.APIKey == "" {
			outcome = activity.Waiting
		}
		warnings := len(res.Errors)
		if outcome == activity.Waiting {
			warnings = 0
		}
		run.Finish(ctx, outcome, warnings)
		s.log.Info("suggestions generated", "trending", len(res.Trending), "similar", len(res.Similar),
			"upcoming", len(res.UpcomingTaste)+len(res.UpcomingRegion), "ai", len(res.AI))
	}()
}

func (s *Service) build(ctx context.Context) *Result {
	run := activity.FromContext(ctx)
	run.Phase(activity.Inventory, -1)
	res := &Result{GeneratedAt: s.now()}
	if run, err := s.store.LatestSuccessfulRun(ctx); err == nil && run != nil {
		res.BasedOnRunID = run.ID
	}
	settings := s.cfg.Get()
	res.SourcesToken = settings.SourcesToken()

	if settings.TMDB.APIKey == "" {
		res.Errors = append(res.Errors, "TMDB must be configured")
		return res
	}
	res.AIEnabled = settings.AI.Enabled && settings.AI.Endpoint != "" && settings.AI.APIKey != ""

	td := tmdb.New(settings.TMDB.APIKey, settings.TMDB.Language, settings.TMDB.Region, settings.Scan.TMDBRateLimitRPS).
		WithCache(tmdbcache.New(s.store))

	ownedTV := map[int64]bool{}
	ownedMovie := map[int64]bool{}
	var sampleTV, sampleMovie []int64
	var seriesNames, movieNames []string
	titles := map[string]string{}

	catalog, err := source.Catalog(ctx, s.store, settings, false, nil)
	if err != nil {
		res.Errors = append(res.Errors, "Catalog unavailable")
		run.Problem(activity.StorageUnavailable, activity.Error)
		run.Finish(ctx, activity.Failed, 1)
		return res
	}
	res.Revision = catalog.Revision
	catalog, err = source.ResolveSavedCatalog(ctx, s.store, settings, catalog, td)
	if err != nil {
		s.log.Error("catalog resolution failed", "err", err)
		res.Errors = append(res.Errors, "Catalog resolution unavailable")
		run.Problem(activity.StorageUnavailable, activity.Error)
		run.Finish(ctx, activity.Failed, 1)
		return res
	}
	res.Errors = append(res.Errors, catalog.Warnings...)
	for _, warning := range catalog.Warnings {
		if !strings.HasPrefix(warning, "Unresolved title: ") {
			run.ReportLegacy(warning, "")
		}
	}
	run.Warnings(len(res.Errors))
	for _, it := range catalog.Items {
		if it.WatchOnly {
			continue
		}
		id := it.TMDBID()
		titles[fmt.Sprintf("%s:%d", it.Type, id)] = it.Name
		switch it.Type {
		case "Series":
			if id != 0 {
				ownedTV[id] = true
				if len(sampleTV) < sampleSize {
					sampleTV = append(sampleTV, id)
				}
			}
			if len(seriesNames) < aiOwnedNames {
				seriesNames = append(seriesNames, it.Name)
			}
		case "Movie":
			if id != 0 {
				ownedMovie[id] = true
				if len(sampleMovie) < sampleSize {
					sampleMovie = append(sampleMovie, id)
				}
			}
			if len(movieNames) < aiOwnedNames {
				movieNames = append(movieNames, it.Name)
			}
		}
	}

	// The result cache controls refresh frequency. Recommendation endpoints
	// must be fetched afresh when a refresh actually runs.
	td.WithCache(nil)
	run.Phase(activity.Trending, 2)
	res.Trending = s.buildTrending(ctx, td, ownedTV, ownedMovie, res)
	run.Phase(activity.Similar, len(sampleTV)+len(sampleMovie))
	res.Similar = s.buildSimilar(ctx, td, sampleTV, sampleMovie, ownedTV, ownedMovie, res, titles)
	run.Phase(activity.Upcoming, -1)
	res.UpcomingTaste, res.UpcomingRegion = s.buildUpcoming(ctx, td, ownedTV, ownedMovie, res)
	run.Warnings(len(res.Errors))
	if res.AIEnabled {
		run.Phase(activity.AI, -1)
		res.AI = s.buildAI(ctx, settings.AI, seriesNames, movieNames, res)
	}
	return res
}

func (s *Service) buildTrending(ctx context.Context, td *tmdb.Client, ownedTV, ownedMovie map[int64]bool, res *Result) []Item {
	var out []Item
	run := activity.FromContext(ctx)
	if tv, err := td.TrendingTV(ctx); err != nil {
		run.Problem(activity.SuggestionsUnavailable, activity.Error)
		run.Advance(true, false)
		res.Errors = append(res.Errors, "tmdb trending tv: request failed")
	} else {
		run.Advance(false, false)
		out = append(out, dedupeTake(tv, "series", ownedTV, trendingTake)...)
	}
	if mv, err := td.TrendingMovie(ctx); err != nil {
		run.Problem(activity.SuggestionsUnavailable, activity.Error)
		run.Advance(true, false)
		res.Errors = append(res.Errors, "tmdb trending movies: request failed")
	} else {
		run.Advance(false, false)
		out = append(out, dedupeTake(mv, "movie", ownedMovie, trendingTake)...)
	}
	return out
}

type scored struct {
	res   tmdb.MediaResult
	count int
}

func (s *Service) buildSimilar(ctx context.Context, td *tmdb.Client, sampleTV, sampleMovie []int64, ownedTV, ownedMovie map[int64]bool, res *Result, titles map[string]string) []Item {
	run := activity.FromContext(ctx)
	tvScores := map[int64]*scored{}
	for _, id := range sampleTV {
		if ctx.Err() != nil {
			return nil
		}
		run.Current(titles[fmt.Sprintf("Series:%d", id)])
		recs, err := td.TVRecommendations(ctx, id)
		run.Advance(err != nil, false)
		if err != nil {
			run.Problem(activity.SuggestionsUnavailable, activity.Error)
			res.Errors = append(res.Errors, "TV recommendations unavailable")
			continue
		}
		for _, r := range recs {
			if ownedTV[r.ID] {
				continue
			}
			if tvScores[r.ID] == nil {
				tvScores[r.ID] = &scored{res: r}
			}
			tvScores[r.ID].count++
		}
	}
	movieScores := map[int64]*scored{}
	for _, id := range sampleMovie {
		if ctx.Err() != nil {
			return nil
		}
		run.Current(titles[fmt.Sprintf("Movie:%d", id)])
		recs, err := td.MovieRecommendations(ctx, id)
		run.Advance(err != nil, false)
		if err != nil {
			run.Problem(activity.SuggestionsUnavailable, activity.Error)
			res.Errors = append(res.Errors, "Movie recommendations unavailable")
			continue
		}
		for _, r := range recs {
			if ownedMovie[r.ID] {
				continue
			}
			if movieScores[r.ID] == nil {
				movieScores[r.ID] = &scored{res: r}
			}
			movieScores[r.ID].count++
		}
	}

	out := append(rankScores(tvScores, "series", similarTake/2), rankScores(movieScores, "movie", similarTake/2)...)
	return out
}

// buildUpcoming returns announced titles the library does not have yet: one set
// matching the library's dominant genres, one set of general releases for the
// configured region.
func (s *Service) buildUpcoming(ctx context.Context, td *tmdb.Client, ownedTV, ownedMovie map[int64]bool, res *Result) (taste, region []Item) {
	from := time.Now()
	tvNames, movieNames := s.libraryGenres(ctx)

	if ids := genreIDs(ctx, td.TVGenres, tvNames, res, "tmdb tv genres"); len(ids) > 0 || len(tvNames) == 0 {
		if tv, err := td.DiscoverUpcomingTV(ctx, ids, from); err != nil {
			activity.FromContext(ctx).Problem(activity.SuggestionsUnavailable, activity.Error)
			res.Errors = append(res.Errors, "tmdb upcoming tv: request failed")
		} else {
			taste = append(taste, dedupeTake(tv, "series", ownedTV, upcomingTake/2)...)
		}
	}
	if ids := genreIDs(ctx, td.MovieGenres, movieNames, res, "tmdb movie genres"); len(ids) > 0 || len(movieNames) == 0 {
		if mv, err := td.DiscoverUpcomingMovies(ctx, ids, from); err != nil {
			activity.FromContext(ctx).Problem(activity.SuggestionsUnavailable, activity.Error)
			res.Errors = append(res.Errors, "tmdb upcoming movies: request failed")
		} else {
			taste = append(taste, dedupeTake(mv, "movie", ownedMovie, upcomingTake/2)...)
		}
	}

	if mv, err := td.UpcomingMovies(ctx); err != nil {
		activity.FromContext(ctx).Problem(activity.SuggestionsUnavailable, activity.Error)
		res.Errors = append(res.Errors, "tmdb movie releases: request failed")
	} else {
		region = append(region, dedupeTake(mv, "movie", ownedMovie, upcomingTake/2)...)
	}
	if tv, err := td.OnTheAirTV(ctx); err != nil {
		activity.FromContext(ctx).Problem(activity.SuggestionsUnavailable, activity.Error)
		res.Errors = append(res.Errors, "tmdb on the air: request failed")
	} else {
		region = append(region, dedupeTake(tv, "series", ownedTV, upcomingTake/2)...)
	}
	return taste, region
}

// libraryGenres returns the most common genre names per media type of the last
// successful scan.
func (s *Service) libraryGenres(ctx context.Context) (tv, movie []string) {
	run, err := s.store.LatestSuccessfulRun(ctx)
	if err != nil || run == nil {
		return nil, nil
	}
	tvCounts := map[string]int{}
	movieCounts := map[string]int{}
	for _, m := range run.Media {
		counts := movieCounts
		if m.Type == store.MediaSeries {
			counts = tvCounts
		}
		for _, g := range m.Genres {
			counts[g]++
		}
	}
	return topKeys(tvCounts, topGenres), topKeys(movieCounts, topGenres)
}

func topKeys(counts map[string]int, n int) []string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys
}

// genreIDs maps genre names recorded during a scan onto TMDB genre IDs. Scans
// store names only, so the genre list has to be resolved at request time.
func genreIDs(ctx context.Context, list func(context.Context) ([]tmdb.Genre, error), names []string, res *Result, label string) []int {
	if len(names) == 0 {
		return nil
	}
	all, err := list(ctx)
	if err != nil {
		activity.FromContext(ctx).Problem(activity.SuggestionsUnavailable, activity.Error)
		res.Errors = append(res.Errors, label+": request failed")
		return nil
	}
	byName := make(map[string]int, len(all))
	for _, g := range all {
		byName[strings.ToLower(g.Name)] = g.ID
	}
	var out []int
	for _, n := range names {
		if id, ok := byName[strings.ToLower(n)]; ok {
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) buildAI(ctx context.Context, cfg config.AISettings, seriesNames, movieNames []string, res *Result) []AIItem {
	client := ai.New(cfg.Endpoint, cfg.APIKey, cfg.Model)
	prompt := buildAIPrompt(seriesNames, movieNames)
	suggestions, err := client.Suggest(ctx, prompt)
	if err != nil {
		activity.FromContext(ctx).Problem(activity.AIUnavailable, activity.Error)
		res.Errors = append(res.Errors, "ai: recommendation request failed")
		return nil
	}
	out := make([]AIItem, 0, len(suggestions))
	for _, sug := range suggestions {
		if strings.TrimSpace(sug.Title) == "" {
			continue
		}
		out = append(out, AIItem{
			Title:      sug.Title,
			Type:       sug.Type,
			Year:       sug.Year,
			Reason:     sug.Reason,
			SearchLink: "https://www.themoviedb.org/search?query=" + url.QueryEscape(sug.Title),
		})
	}
	return out
}

func buildAIPrompt(seriesNames, movieNames []string) string {
	var b strings.Builder
	b.WriteString("My owned media catalog.\n")
	if len(seriesNames) > 0 {
		b.WriteString("TV series I own: ")
		b.WriteString(strings.Join(seriesNames, ", "))
		b.WriteString("\n")
	}
	if len(movieNames) > 0 {
		b.WriteString("Movies I own: ")
		b.WriteString(strings.Join(movieNames, ", "))
		b.WriteString("\n")
	}
	b.WriteString("Recommend new movies and TV series I likely do not already own, matching my taste.")
	return b.String()
}

func dedupeTake(items []tmdb.MediaResult, mediaType string, owned map[int64]bool, n int) []Item {
	var out []Item
	for _, m := range items {
		if owned[m.ID] {
			continue
		}
		out = append(out, toItem(m, mediaType))
		if len(out) >= n {
			break
		}
	}
	return out
}

func rankScores(m map[int64]*scored, mediaType string, n int) []Item {
	list := make([]*scored, 0, len(m))
	for _, v := range m {
		list = append(list, v)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].res.Popularity > list[j].res.Popularity
	})
	var out []Item
	for _, sc := range list {
		out = append(out, toItem(sc.res, mediaType))
		if len(out) >= n {
			break
		}
	}
	return out
}

func toItem(m tmdb.MediaResult, mediaType string) Item {
	it := Item{
		TMDBID:    m.ID,
		MediaType: mediaType,
		Title:     m.DisplayTitle(),
		Year:      m.Year(),
		Overview:  truncate(m.Overview, 220),
	}
	it.ReleaseDate = m.ReleaseDate
	if it.ReleaseDate == "" {
		it.ReleaseDate = m.FirstAirDate
	}
	if m.VoteAverage > 0 {
		it.Rating = fmt.Sprintf("%.1f", m.VoteAverage)
	}
	if m.PosterPath != "" {
		it.PosterURL = posterBase + m.PosterPath
	}
	kind := "tv"
	if mediaType == "movie" {
		kind = "movie"
	}
	it.TMDBLink = "https://www.themoviedb.org/" + kind + "/" + strconv.FormatInt(m.ID, 10)
	return it
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "\u2026"
}
