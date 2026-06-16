package web

import (
	"encoding/json"
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

	return sd
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
