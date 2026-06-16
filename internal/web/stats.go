package web

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

// StatsData is the model for the statistics page.
type StatsData struct {
	Layout         Layout
	HasData        bool
	LastScan       string
	Duration       string
	ItemsScanned   int
	LibrariesCount int
	TotalGaps      int
	MissingUnits   int
	MoviesScanned  int
	SeriesScanned  int
	Completeness   int
	Libraries      []StatsLibrary
	ByKind         StatsByKind
	TopSeries      []StatsTop
	TopCollections []StatsTop
	LibraryRatings []StatsLibraryRatings
	LongestMovies  []StatsRuntime
	Genres         []StatsBar
	Years          []StatsBar
}

// StatsLibrary is a per-library statistics row.
type StatsLibrary struct {
	Name          string
	Color         []string
	Type          string
	Scanned       int
	Total         int
	ItemsWithGaps int
	MissingUnits  int
	Completeness  int
}

// StatsByKind holds finding counts grouped by kind.
type StatsByKind struct {
	MissingSeasons     int
	MissingEpisodes    int
	MissingCollections int
}

// StatsTop is a ranked "most incomplete" entry.
type StatsTop struct {
	Title   string
	Library string
	Color   []string
	Missing int
}

// StatsRated is a movie ranked by rating.
type StatsRated struct {
	Title  string
	Year   int
	Rating string
}

// StatsLibraryRatings holds the top and lowest rated titles of a single library.
type StatsLibraryRatings struct {
	Name   string
	Color  []string
	Top    []StatsRated
	Lowest []StatsRated
}

// StatsRuntime is a movie ranked by runtime.
type StatsRuntime struct {
	Title   string
	Year    int
	Runtime string
}

// StatsBar is a labelled count with a relative bar percentage.
type StatsBar struct {
	Label string
	Count int
	Pct   int
}

// BuildStats computes the statistics view from the latest run and its findings.
// libTypes maps library IDs to their Jellyfin collection type (movies/tvshows).
func BuildStats(t *i18n.Translator, run *store.ScanRun, findings []store.Finding, libTypes map[string]string) StatsData {
	sd := StatsData{}
	if run == nil {
		return sd
	}
	sd.HasData = true
	sd.LastScan = FormatTime(t, run.FinishedAt)
	sd.Duration = orDash(FormatDuration(run.Duration()))
	sd.ItemsScanned = run.ItemsScanned
	sd.LibrariesCount = len(run.Libraries)
	sd.TotalGaps = len(findings)

	// Distinct titles with gaps per library.
	libGapTitles := map[string]map[string]bool{}
	for _, f := range findings {
		if libGapTitles[f.LibraryID] == nil {
			libGapTitles[f.LibraryID] = map[string]bool{}
		}
		libGapTitles[f.LibraryID][f.Title] = true
	}

	// Per-library missing units (from the persisted summary) and totals.
	totalItems := 0
	itemsWithGapsAll := 0
	for _, l := range run.Libraries {
		typ := libTypes[l.ID]
		switch typ {
		case "movies":
			sd.MoviesScanned += l.Scanned
		case "tvshows":
			sd.SeriesScanned += l.Scanned
		}
		withGaps := len(libGapTitles[l.ID])
		comp := 100
		if l.Total > 0 {
			comp = int(float64(l.Total-withGaps) / float64(l.Total) * 100)
		}
		sd.MissingUnits += l.Missing
		totalItems += l.Total
		itemsWithGapsAll += withGaps
		sd.Libraries = append(sd.Libraries, StatsLibrary{
			Name:          l.Name,
			Color:         LibraryColor(l.ID),
			Type:          typ,
			Scanned:       l.Scanned,
			Total:         l.Total,
			ItemsWithGaps: withGaps,
			MissingUnits:  l.Missing,
			Completeness:  comp,
		})
	}
	if totalItems > 0 {
		sd.Completeness = int(float64(totalItems-itemsWithGapsAll) / float64(totalItems) * 100)
	} else {
		sd.Completeness = 100
	}

	// Findings by kind, plus top incomplete series/collections.
	seriesMissing := map[string]*StatsTop{}
	var collections []StatsTop
	for _, f := range findings {
		kind, count, title := findingMissing(f)
		switch kind {
		case store.KindMissingSeason:
			sd.ByKind.MissingSeasons++
		case store.KindMissingEpisodes:
			sd.ByKind.MissingEpisodes++
		case store.KindMissingCollection:
			sd.ByKind.MissingCollections++
		}
		switch kind {
		case store.KindMissingSeason, store.KindMissingEpisodes:
			key := f.LibraryID + "\x00" + title
			if seriesMissing[key] == nil {
				seriesMissing[key] = &StatsTop{Title: title, Library: f.LibraryName, Color: LibraryColor(f.LibraryID)}
			}
			seriesMissing[key].Missing += count
		case store.KindMissingCollection:
			collections = append(collections, StatsTop{Title: title, Library: f.LibraryName, Color: LibraryColor(f.LibraryID), Missing: count})
		}
	}

	for _, v := range seriesMissing {
		sd.TopSeries = append(sd.TopSeries, *v)
	}
	sd.TopSeries = topN(sd.TopSeries, 5)
	sd.TopCollections = topN(collections, 5)

	computeMediaStats(&sd, run)

	return sd
}

func computeMediaStats(sd *StatsData, run *store.ScanRun) {
	media := run.Media
	var movies []store.MediaStat
	genreCounts := map[string]int{}
	decadeCounts := map[int]int{}
	byLib := map[string][]store.MediaStat{}

	for _, m := range media {
		if m.Type == store.MediaMovie {
			movies = append(movies, m)
		}
		byLib[m.LibraryID] = append(byLib[m.LibraryID], m)
		for _, g := range m.Genres {
			genreCounts[g]++
		}
		if m.Year > 0 {
			decadeCounts[(m.Year/10)*10]++
		}
	}

	// Top / lowest rated per library (movies and series alike).
	for _, l := range run.Libraries {
		rated := make([]store.MediaStat, 0, len(byLib[l.ID]))
		for _, m := range byLib[l.ID] {
			if m.Rating > 0 {
				rated = append(rated, m)
			}
		}
		top := append([]store.MediaStat(nil), rated...)
		sort.SliceStable(top, func(i, j int) bool { return top[i].Rating > top[j].Rating })
		low := append([]store.MediaStat(nil), rated...)
		sort.SliceStable(low, func(i, j int) bool { return low[i].Rating < low[j].Rating })
		sd.LibraryRatings = append(sd.LibraryRatings, StatsLibraryRatings{
			Name:   l.Name,
			Color:  LibraryColor(l.ID),
			Top:    toRated(top, 10),
			Lowest: toRated(low, 10),
		})
	}

	// Longest movies by runtime.
	long := make([]store.MediaStat, 0, len(movies))
	for _, m := range movies {
		if m.Runtime > 0 {
			long = append(long, m)
		}
	}
	sort.SliceStable(long, func(i, j int) bool { return long[i].Runtime > long[j].Runtime })
	if len(long) > 10 {
		long = long[:10]
	}
	for _, m := range long {
		sd.LongestMovies = append(sd.LongestMovies, StatsRuntime{Title: m.Title, Year: m.Year, Runtime: formatRuntime(m.Runtime)})
	}

	// Genre distribution (top 12).
	sd.Genres = topBars(genreCounts, 12, sortByCountDesc)

	// Release decade distribution (sorted ascending).
	decLabels := map[string]int{}
	for dec, c := range decadeCounts {
		decLabels[fmt.Sprintf("%ds", dec)] = c
	}
	sd.Years = topBars(decLabels, 0, sortByLabelAsc)
}

func toRated(ms []store.MediaStat, n int) []StatsRated {
	if len(ms) > n {
		ms = ms[:n]
	}
	out := make([]StatsRated, 0, len(ms))
	for _, m := range ms {
		out = append(out, StatsRated{Title: m.Title, Year: m.Year, Rating: fmt.Sprintf("%.1f", m.Rating)})
	}
	return out
}

const (
	sortByCountDesc = iota
	sortByLabelAsc
)

func topBars(counts map[string]int, limit, mode int) []StatsBar {
	bars := make([]StatsBar, 0, len(counts))
	max := 0
	for k, c := range counts {
		bars = append(bars, StatsBar{Label: k, Count: c})
		if c > max {
			max = c
		}
	}
	switch mode {
	case sortByLabelAsc:
		sort.SliceStable(bars, func(i, j int) bool { return bars[i].Label < bars[j].Label })
	default:
		sort.SliceStable(bars, func(i, j int) bool { return bars[i].Count > bars[j].Count })
	}
	if limit > 0 && len(bars) > limit {
		bars = bars[:limit]
	}
	for i := range bars {
		if max > 0 {
			bars[i].Pct = bars[i].Count * 100 / max
		}
	}
	return bars
}

func formatRuntime(min int) string {
	if min <= 0 {
		return ""
	}
	h := min / 60
	m := min % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func findingMissing(f store.Finding) (kind string, count int, title string) {
	var d detailPayload
	if f.Details != "" {
		_ = json.Unmarshal([]byte(f.Details), &d)
	}
	switch f.Kind {
	case store.KindMissingCollection:
		return f.Kind, len(d.MissingParts), f.Title
	default:
		return f.Kind, len(d.MissingEpisodes), f.Title
	}
}

func topN(items []StatsTop, n int) []StatsTop {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Missing > items[j].Missing })
	if len(items) > n {
		items = items[:n]
	}
	return items
}
