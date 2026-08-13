package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
	"github.com/daknoblo/waim/internal/web"
)

const (
	libMovies = "lib-movies"
	libSeries = "lib-series"
)

// demoSeries describes one sample show; ratings drift per season so the
// heatmaps and the flow chart have something to show.
type demoSeries struct {
	title   string
	tmdbID  int64
	year    int
	rating  float64
	runtime int
	genres  []string
	seasons []demoSeason
}

type demoSeason struct {
	number int
	owned  int
	total  int
	rating float64
}

var demoShows = []demoSeries{
	{"Chronicles of the Deep", 401, 2016, 8.6, 52, []string{"Drama", "Mystery"}, []demoSeason{
		{1, 10, 10, 8.1}, {2, 10, 10, 8.6}, {3, 8, 10, 9.1}, {4, 0, 8, 7.4},
	}},
	{"Neon Harbour", 402, 2019, 7.9, 45, []string{"Crime", "Drama"}, []demoSeason{
		{1, 8, 8, 8.3}, {2, 6, 8, 7.6}, {3, 8, 8, 6.9},
	}},
	{"The Quiet Frontier", 403, 2021, 8.2, 48, []string{"Western", "Drama"}, []demoSeason{
		{1, 10, 10, 8.2}, {2, 9, 10, 8.4},
	}},
	{"Paper Lanterns", 404, 2014, 7.4, 24, []string{"Comedy"}, []demoSeason{
		{1, 12, 12, 7.2}, {2, 12, 12, 7.5}, {3, 12, 12, 7.8}, {4, 10, 12, 7.1}, {5, 6, 12, 6.4},
	}},
	{"Signal Lost", 405, 2023, 8.9, 55, []string{"Science Fiction", "Thriller"}, []demoSeason{
		{1, 8, 8, 8.7}, {2, 5, 8, 9.3},
	}},
	{"Harbourlight", 406, 2011, 6.8, 42, []string{"Drama"}, []demoSeason{
		{1, 13, 13, 7.0}, {2, 13, 13, 6.6},
	}},
}

// demoMovies are the owned films, two of them part of one collection.
var demoMovies = []store.MediaStat{
	{Type: store.MediaMovie, Title: "The Cartographer", Year: 2003, Rating: 8.4, Runtime: 168, Genres: []string{"Adventure", "Fantasy"}, TMDBID: 301, CollectionID: 900, CollectionName: "The Cartographer Trilogy", Language: "en", Country: "NZ"},
	{Type: store.MediaMovie, Title: "The Cartographer: Tidewater", Year: 2005, Rating: 8.2, Runtime: 173, Genres: []string{"Adventure"}, TMDBID: 302, CollectionID: 900, CollectionName: "The Cartographer Trilogy", Language: "en", Country: "NZ"},
	{Type: store.MediaMovie, Title: "Midnight in Vienna", Year: 1998, Rating: 7.6, Runtime: 112, Genres: []string{"Romance", "Drama"}, TMDBID: 303, Language: "de", Country: "AT"},
	{Type: store.MediaMovie, Title: "Salt & Static", Year: 2022, Rating: 6.9, Runtime: 98, Genres: []string{"Thriller"}, TMDBID: 304, Language: "en", Country: "US"},
	{Type: store.MediaMovie, Title: "Der letzte Zug", Year: 1959, Rating: 8.1, Runtime: 94, Genres: []string{"War", "Drama"}, TMDBID: 305, Language: "de", Country: "DE"},
	{Type: store.MediaMovie, Title: "Kite Season", Year: 2017, Rating: 7.2, Runtime: 105, Genres: []string{"Comedy", "Drama"}, TMDBID: 306, Language: "ja", Country: "JP"},
	{Type: store.MediaMovie, Title: "Glasshouse", Year: 2020, Rating: 5.8, Runtime: 88, Genres: []string{"Horror"}, TMDBID: 307, Language: "en", Country: "GB"},
	{Type: store.MediaMovie, Title: "Northbound", Year: 2012, Rating: 7.9, Runtime: 129, Genres: []string{"Documentary"}, TMDBID: 308, Language: "sv", Country: "SE"},
}

func demoRun() *store.ScanRun {
	started := time.Now().Add(-42 * time.Minute)
	finished := started.Add(6*time.Minute + 12*time.Second)

	media := make([]store.MediaStat, 0, len(demoMovies)+len(demoShows))
	for _, m := range demoMovies {
		m.LibraryID = libMovies
		m.LibraryName = "Movies"
		m.JellyfinID = fmt.Sprintf("demo-movie-%d", m.TMDBID)
		media = append(media, m)
	}
	moviesTotal := len(demoMovies)
	seriesTotal := len(demoShows)

	for _, show := range demoShows {
		stat := store.MediaStat{
			Type: store.MediaSeries, Title: show.title, Year: show.year, Rating: show.rating,
			Runtime: show.runtime, Genres: show.genres, LibraryID: libSeries, LibraryName: "Series",
			TMDBID: show.tmdbID, JellyfinID: fmt.Sprintf("demo-series-%d", show.tmdbID),
			Language: "en", Country: "US",
		}
		for _, sn := range show.seasons {
			season := store.SeasonStat{Number: sn.number, Episodes: sn.owned, Total: sn.total, Rating: sn.rating}
			for e := 1; e <= sn.total; e++ {
				// Spread the episode votes around the season average.
				drift := float64((e*7)%9-4) / 10
				season.Ratings = append(season.Ratings, store.EpisodeRating{
					Number:  e,
					Title:   fmt.Sprintf("Episode %d", e),
					Rating:  clampRating(sn.rating + drift),
					Minutes: show.runtime,
					Owned:   e <= sn.owned,
				})
			}
			stat.Episodes += sn.owned
			stat.TotalEpisodes += sn.total
			stat.Minutes += sn.owned * show.runtime
			stat.Seasons = append(stat.Seasons, season)
		}
		media = append(media, stat)
	}

	return &store.ScanRun{
		ID: 42, StartedAt: started, FinishedAt: &finished, Status: store.StatusSuccess,
		LibrariesScanned: 2, ItemsScanned: moviesTotal + seriesTotal, MissingCount: 16,
		Libraries: []store.LibrarySummary{
			{ID: libMovies, Name: "Movies", Scanned: moviesTotal, Total: moviesTotal, Missing: 1},
			{ID: libSeries, Name: "Series", Scanned: seriesTotal, Total: seriesTotal, Missing: 15},
		},
		Media: media,
	}
}

func clampRating(v float64) float64 {
	switch {
	case v < 1:
		return 1
	case v > 10:
		return 10
	default:
		return v
	}
}

func demoFindings() []store.Finding {
	created := time.Now().Add(-40 * time.Minute)
	season4 := 4
	season2 := 2
	season5 := 5

	seasonDetail := func(season, episodes int, missing []int) string {
		b, _ := json.Marshal(map[string]any{
			"seasonNumber":    season,
			"episodeCount":    episodes,
			"missingEpisodes": missing,
		})
		return string(b)
	}
	collectionDetail, _ := json.Marshal(map[string]any{
		"collectionId":   900,
		"collectionName": "The Cartographer Trilogy",
		"missingParts": []map[string]any{
			{"tmdbId": 309, "title": "The Cartographer: Northern Light", "year": "2008", "rating": 8.5},
		},
	})

	return []store.Finding{
		{
			ID: 1, ScanRunID: 42, Kind: store.KindMissingSeason, MediaType: store.MediaSeries,
			LibraryID: libSeries, LibraryName: "Series", Title: "Chronicles of the Deep",
			TMDBID: 401, JellyfinID: "demo-series-401", SeasonNumber: &season4,
			Summary: "season 4 is completely missing", Details: seasonDetail(4, 8, []int{1, 2, 3, 4, 5, 6, 7, 8}),
			CreatedAt: created,
		},
		{
			ID: 2, ScanRunID: 42, Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries,
			LibraryID: libSeries, LibraryName: "Series", Title: "Neon Harbour",
			TMDBID: 402, JellyfinID: "demo-series-402", SeasonNumber: &season2,
			Summary: "season 2 is missing 2 episodes", Details: seasonDetail(2, 8, []int{5, 6}),
			CreatedAt: created,
		},
		{
			ID: 3, ScanRunID: 42, Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries,
			LibraryID: libSeries, LibraryName: "Series", Title: "Paper Lanterns",
			TMDBID: 404, JellyfinID: "demo-series-404", SeasonNumber: &season5,
			Summary: "season 5 is missing 6 episodes", Details: seasonDetail(5, 12, []int{7, 8, 9, 10, 11, 12}),
			CreatedAt: created,
		},
		{
			ID: 4, ScanRunID: 42, Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries,
			LibraryID: libSeries, LibraryName: "Series", Title: "Signal Lost",
			TMDBID: 405, JellyfinID: "demo-series-405", SeasonNumber: &season2,
			Summary: "season 2 is missing 3 episodes", Details: seasonDetail(2, 8, []int{6, 7, 8}),
			CreatedAt: created,
		},
		{
			ID: 5, ScanRunID: 42, Kind: store.KindMissingCollection, MediaType: store.MediaMovie,
			LibraryID: libMovies, LibraryName: "Movies", Title: "The Cartographer Trilogy",
			TMDBID: 900, JellyfinID: "demo-movie-301",
			Summary: "collection is missing 1 entry", Details: string(collectionDetail),
			CreatedAt: created,
		},
	}
}

func demoDashboard(t *i18n.Translator, run *store.ScanRun, findings []store.Finding) web.DashboardData {
	return web.DashboardData{
		Layout: demoLayout(t, web.NavDashboard),
		Status: web.StatusView{
			State:            "idle",
			StateLabel:       t.T("dashboard.state.idle"),
			LastScan:         web.FormatRelative(t, run.FinishedAt),
			NextScan:         t.T("relative.in", t.T("relative.minutes", 18)),
			Duration:         web.FormatDuration(run.Duration()),
			ItemsScanned:     run.ItemsScanned,
			LibrariesScanned: run.LibrariesScanned,
			MissingTotal:     run.MissingCount,
			Libraries: []web.LibraryStatusView{
				{Name: "Movies", Color: web.LibraryColor(libMovies), Scanned: 8, Total: 8, Missing: 1},
				{Name: "Series", Color: web.LibraryColor(libSeries), Scanned: 6, Total: 6, Missing: 15},
			},
		},
		Findings: web.BuildFindingRows(t, findings, ""),
		Libraries: []web.LibraryFilter{
			{ID: libMovies, Name: "Movies"},
			{ID: libSeries, Name: "Series"},
		},
		Sort: web.SortTitle,
		Dir:  web.DirAsc,
	}
}

func demoStats(t *i18n.Translator, run *store.ScanRun, findings []store.Finding) web.StatsData {
	now := time.Now()
	d := web.BuildStats(t, web.StatsInput{
		Run:      run,
		Findings: findings,
		LibTypes: map[string]string{libMovies: "movies", libSeries: "tvshows"},
		History: []store.RunTotals{
			{FinishedAt: now.Add(-96 * time.Hour), ItemsScanned: 11, MissingCount: 21},
			{FinishedAt: now.Add(-72 * time.Hour), ItemsScanned: 12, MissingCount: 19},
			{FinishedAt: now.Add(-48 * time.Hour), ItemsScanned: 12, MissingCount: 19},
			{FinishedAt: now.Add(-24 * time.Hour), ItemsScanned: 13, MissingCount: 17},
			{FinishedAt: now, ItemsScanned: 14, MissingCount: 16},
		},
	})
	d.Layout = demoLayout(t, web.NavStats)
	return d
}

func demoSuggestions(t *i18n.Translator) web.SuggestionsData {
	item := func(kind, title, year, rating, overview string, id int64) suggest.Item {
		return suggest.Item{
			MediaType: kind, Title: title, Year: year, Rating: rating, Overview: overview,
			TMDBLink: fmt.Sprintf("https://www.themoviedb.org/%s/%d", kind, id),
		}
	}
	return web.SuggestionsData{
		Layout:     demoLayout(t, web.NavSuggestions),
		Configured: true,
		Result: &suggest.Result{
			GeneratedAt:  time.Now().Add(-12 * time.Minute),
			BasedOnRunID: 42,
			AIEnabled:    true,
			Trending: []suggest.Item{
				item("movie", "Lantern Bay", "2026", "7.8", "A lighthouse keeper uncovers a decades-old shipwreck manifest.", 501),
				item("tv", "Grid Nine", "2025", "8.1", "Engineers race to keep a failing power grid alive.", 502),
				item("movie", "The Understudy", "2026", "7.1", "A stand-in actor is mistaken for the lead — on and off stage.", 503),
			},
			Similar: []suggest.Item{
				item("tv", "Cold Harbour", "2020", "8.4", "Because you own Neon Harbour.", 504),
				item("movie", "Atlas of Small Things", "2015", "7.7", "Because you own The Cartographer.", 505),
			},
			AI: []suggest.AIItem{
				{Title: "The Salt Road", Type: "movie", Year: "2019", Reason: "Slow-burn desert thriller in the vein of your documentary picks.", SearchLink: "https://www.themoviedb.org/search?query=The+Salt+Road"},
				{Title: "Tideline", Type: "tv", Year: "2022", Reason: "Coastal mystery with the ensemble cast you seem to like.", SearchLink: "https://www.themoviedb.org/search?query=Tideline"},
			},
		},
	}
}

func demoLogs(t *i18n.Translator) web.LogPageData {
	now := time.Now()
	entries := []logbuf.Entry{
		{Time: now.Add(-42 * time.Minute), Level: "INFO", Message: "scan started libraries=2"},
		{Time: now.Add(-41 * time.Minute), Level: "INFO", Message: "jellyfin items fetched library=Movies count=8"},
		{Time: now.Add(-40 * time.Minute), Level: "DEBUG", Message: "tmdb cache hit key=/movie/301"},
		{Time: now.Add(-39 * time.Minute), Level: "WARN", Message: "tmdb tv lookup slow title=\"Harbourlight\" ms=1840"},
		{Time: now.Add(-38 * time.Minute), Level: "INFO", Message: "finding recorded kind=missing_season title=\"Chronicles of the Deep\" season=4"},
		{Time: now.Add(-36 * time.Minute), Level: "INFO", Message: "scan finished items=14 missing=16 duration=6m12s"},
	}
	return web.LogPageData{
		Layout: demoLayout(t, web.NavLogs),
		Logs:   web.BuildLogViews(entries),
	}
}

func demoSettings(t *i18n.Translator) web.SettingsData {
	s := config.Defaults()
	s.Locale = t.Locale()
	s.Jellyfin.URL = "https://jellyfin.example.com"
	s.TMDB.Language = "en-US"
	s.TMDB.Region = "US"
	s.Scan.IntervalMinutes = 360
	s.Scan.TMDBRateLimitRPS = 2
	s.Scan.EpisodeRatings = true
	s.Libraries = []config.Library{
		{ID: libMovies, Name: "Movies", Type: "movies", Enabled: true},
		{ID: libSeries, Name: "Series", Type: "tvshows", Enabled: true},
		{ID: "lib-music", Name: "Music videos", Type: "musicvideos"},
	}
	return web.SettingsData{
		Layout:         demoLayout(t, web.NavSettings),
		Settings:       s,
		Libraries:      s.Libraries,
		HasJellyfinKey: true,
		HasTMDBKey:     true,
		CacheEntries:   4820,
	}
}

func demoAbout(t *i18n.Translator) web.AboutData {
	return web.AboutData{
		Layout:     demoLayout(t, web.NavAbout),
		Version:    "demo",
		Commit:     "0000000000",
		CommitURL:  repoURL,
		DBSize:     web.HumanSize(18 << 20),
		ConfigSize: web.HumanSize(2048),
		GoVersion:  "go1.25",
		Repo:       repoURL,
	}
}
