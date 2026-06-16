// Package suggest builds media recommendations from the user's Jellyfin library
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

	"github.com/daknoblo/waim/internal/ai"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

const (
	posterBase    = "https://image.tmdb.org/t/p/w154"
	sampleSize    = 12 // owned titles per media type used for recommendations
	trendingTake  = 12
	similarTake   = 18
	aiOwnedNames  = 40
	generateLimit = 5 * time.Minute
)

// Item is a display-ready TMDB suggestion.
type Item struct {
	MediaType string
	Title     string
	Year      string
	Rating    string
	Overview  string
	PosterURL string
	TMDBLink  string
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
	Trending     []Item
	Similar      []Item
	AI           []AIItem
	AIEnabled    bool
	GeneratedAt  time.Time
	BasedOnRunID int64
	Errors       []string
}

// Service builds and caches suggestions.
type Service struct {
	cfg   *config.Manager
	store *store.Store
	log   *slog.Logger
	ttl   time.Duration

	mu      sync.RWMutex
	result  *Result
	running atomic.Bool
}

// New creates a suggestion service.
func New(cfg *config.Manager, st *store.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{cfg: cfg, store: st, log: log, ttl: 6 * time.Hour}
}

// Running reports whether a generation is in progress.
func (s *Service) Running() bool { return s.running.Load() }

// Result returns the cached result (may be nil) and whether it is still fresh.
func (s *Service) Result() (*Result, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return nil, false
	}
	return s.result, time.Since(s.result.GeneratedAt) < s.ttl
}

// NeedsRefresh reports whether suggestions should be (re)generated: when there
// is no cached result yet, or a newer successful scan exists than the one the
// cached suggestions were based on.
func (s *Service) NeedsRefresh(ctx context.Context) bool {
	s.mu.RLock()
	res := s.result
	s.mu.RUnlock()
	if res == nil {
		return true
	}
	var latest int64
	if run, err := s.store.LatestSuccessfulRun(ctx); err == nil && run != nil {
		latest = run.ID
	}
	return res.BasedOnRunID != latest
}

// Generate rebuilds suggestions in the background, ignoring overlapping calls.
func (s *Service) Generate() {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.running.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), generateLimit)
		defer cancel()
		res := s.build(ctx)
		s.mu.Lock()
		s.result = res
		s.mu.Unlock()
		s.log.Info("suggestions generated", "trending", len(res.Trending), "similar", len(res.Similar), "ai", len(res.AI))
	}()
}

func (s *Service) build(ctx context.Context) *Result {
	res := &Result{GeneratedAt: time.Now()}
	if run, err := s.store.LatestSuccessfulRun(ctx); err == nil && run != nil {
		res.BasedOnRunID = run.ID
	}
	settings := s.cfg.Get()

	if settings.Jellyfin.URL == "" || settings.Jellyfin.APIKey == "" || settings.TMDB.APIKey == "" {
		res.Errors = append(res.Errors, "jellyfin and tmdb must be configured")
		return res
	}
	res.AIEnabled = settings.AI.Enabled && settings.AI.Endpoint != "" && settings.AI.APIKey != ""

	jf := jellyfin.New(settings.Jellyfin.URL, settings.Jellyfin.APIKey)
	td := tmdb.New(settings.TMDB.APIKey, settings.TMDB.Language, settings.TMDB.Region, settings.Scan.TMDBRateLimitRPS)

	ownedTV := map[int64]bool{}
	ownedMovie := map[int64]bool{}
	var sampleTV, sampleMovie []int64
	var seriesNames, movieNames []string

	userID, err := jf.ResolveUserID(ctx, settings.Jellyfin.UserID)
	if err != nil {
		res.Errors = append(res.Errors, "jellyfin: "+err.Error())
		return res
	}
	for _, lib := range settings.EnabledLibraryIDs() {
		items, err := jf.ItemsInLibrary(ctx, userID, lib)
		if err != nil {
			res.Errors = append(res.Errors, "jellyfin: "+err.Error())
			continue
		}
		for _, it := range items {
			id := providerTMDB(it)
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
	}

	res.Trending = s.buildTrending(ctx, td, ownedTV, ownedMovie, res)
	res.Similar = s.buildSimilar(ctx, td, sampleTV, sampleMovie, ownedTV, ownedMovie, res)
	if res.AIEnabled {
		res.AI = s.buildAI(ctx, settings.AI, seriesNames, movieNames, res)
	}
	return res
}

func (s *Service) buildTrending(ctx context.Context, td *tmdb.Client, ownedTV, ownedMovie map[int64]bool, res *Result) []Item {
	var out []Item
	if tv, err := td.TrendingTV(ctx); err != nil {
		res.Errors = append(res.Errors, "tmdb trending tv: "+err.Error())
	} else {
		out = append(out, dedupeTake(tv, "series", ownedTV, trendingTake)...)
	}
	if mv, err := td.TrendingMovie(ctx); err != nil {
		res.Errors = append(res.Errors, "tmdb trending movies: "+err.Error())
	} else {
		out = append(out, dedupeTake(mv, "movie", ownedMovie, trendingTake)...)
	}
	return out
}

type scored struct {
	res   tmdb.MediaResult
	count int
}

func (s *Service) buildSimilar(ctx context.Context, td *tmdb.Client, sampleTV, sampleMovie []int64, ownedTV, ownedMovie map[int64]bool, res *Result) []Item {
	tvScores := map[int64]*scored{}
	for _, id := range sampleTV {
		recs, err := td.TVRecommendations(ctx, id)
		if err != nil {
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
		recs, err := td.MovieRecommendations(ctx, id)
		if err != nil {
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

func (s *Service) buildAI(ctx context.Context, cfg config.AISettings, seriesNames, movieNames []string, res *Result) []AIItem {
	client := ai.New(cfg.Endpoint, cfg.APIKey, cfg.Model)
	prompt := buildAIPrompt(seriesNames, movieNames)
	suggestions, err := client.Suggest(ctx, prompt)
	if err != nil {
		res.Errors = append(res.Errors, "ai: "+err.Error())
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
	b.WriteString("My Jellyfin library.\n")
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
		MediaType: mediaType,
		Title:     m.DisplayTitle(),
		Year:      m.Year(),
		Overview:  truncate(m.Overview, 220),
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

func providerTMDB(it jellyfin.Item) int64 {
	if v, ok := it.ProviderID("Tmdb"); ok {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	return 0
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "\u2026"
}
