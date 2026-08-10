package web

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

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
	SeriesEpisodes int
	Completeness   int
	Libraries      []StatsLibrary
	ByKind         StatsByKind
	TopSeries      []StatsTop
	TopCollections []StatsTop
	LibraryRatings []StatsLibraryRatings
	FindingRatings []StatsLibraryRatings
	SeriesFindings []StatsLibraryRatings
	LongestMovies  []StatsRuntime
	ShortestMovies []StatsRuntime
	LongestSeries  []StatsRuntime
	ShortestSeries []StatsRuntime
	Sagas          []StatsRuntime
	Facts          []StatsFact
	Niches         []StatsNiche
	Genres         []StatsBar
	Years          []StatsBar
	RatingSpread   []StatsBar
	Languages      []StatsBar
	Countries      []StatsBar
	GenrePie       []PieSlice
	YearPie        []PieSlice
	LanguagePie    []PieSlice
	GenreRatings   []StatsRatedGroup
	Completion     []StatsCompletion
	EpisodePct     int
	Growth         []StatsGrowth
	SeriesOptions  []SeriesOption
	Flow           SeriesFlow
}

// StatsCompletion is one series row of the season completion heatmap.
type StatsCompletion struct {
	Title   string
	Library string
	Color   []string
	Owned   int
	Total   int
	Pct     int
	Cells   []CompletionCell
}

// CompletionCell is a single season square of the heatmap.
type CompletionCell struct {
	Label string
	Hint  string
	Fill  string
}

// StatsRatedGroup is the average rating of a group of titles, e.g. a genre.
type StatsRatedGroup struct {
	Label  string
	Rating string
	Count  int
	Pct    int
}

// StatsGrowth is one point of the library growth trend.
type StatsGrowth struct {
	Date   string
	Titles int
	Delta  string
	Up     bool
	Pct    int
}

// StatsFact is a single headline number with an explanatory label.
type StatsFact struct {
	Icon  string
	Label string
	Value string
	Hint  string
}

// SeriesOption is a selectable series for the season flow diagram.
type SeriesOption struct {
	Title    string
	Label    string
	Selected bool
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

// StatsRuntime is a title ranked by runtime.
type StatsRuntime struct {
	Title   string
	Year    int
	Detail  string
	Runtime string
}

// StatsNiche is a niche or classic title with the reason it qualifies.
type StatsNiche struct {
	Title  string
	Year   int
	Reason string
}

// StatsBar is a labelled count with the bar width (share of the largest value)
// and the share of the total, both in percent.
type StatsBar struct {
	Label string
	Count int
	Pct   int
	Share int
}

// BuildStats computes the statistics view from the latest run and its findings.
// libTypes maps library IDs to their Jellyfin collection type (movies/tvshows).
// history holds the totals of earlier runs for the growth trend.
func BuildStats(t *i18n.Translator, run *store.ScanRun, findings []store.Finding, libTypes map[string]string, history []store.RunTotals) StatsData {
	sd := StatsData{}
	if run == nil {
		return sd
	}
	sd.HasData = true
	sd.LastScan = FormatRelative(t, run.FinishedAt)
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

	computeMediaStats(&sd, run, t)
	sd.FindingRatings = buildFindingRatings(findings)
	sd.SeriesFindings = buildSeriesFindingRatings(findings, run.Media)
	sd.Growth = buildGrowth(history)

	return sd
}

// buildGrowth turns the totals of the recent scans into a trend of how many
// titles the library gained (or lost) per scan.
func buildGrowth(history []store.RunTotals) []StatsGrowth {
	if len(history) < 2 {
		return nil
	}
	max := 0
	for _, h := range history {
		if h.ItemsScanned > max {
			max = h.ItemsScanned
		}
	}
	out := make([]StatsGrowth, 0, len(history))
	for i, h := range history {
		g := StatsGrowth{
			Date:   h.FinishedAt.Local().Format("2006-01-02"),
			Titles: h.ItemsScanned,
			Delta:  "\u2014",
		}
		if max > 0 {
			g.Pct = h.ItemsScanned * 100 / max
		}
		if i > 0 {
			d := h.ItemsScanned - history[i-1].ItemsScanned
			switch {
			case d > 0:
				g.Delta = "+" + strconv.Itoa(d)
				g.Up = true
			case d < 0:
				g.Delta = strconv.Itoa(d)
			default:
				g.Delta = "\u00b10"
			}
		}
		out = append(out, g)
	}
	return out
}

func computeMediaStats(sd *StatsData, run *store.ScanRun, t *i18n.Translator) {
	media := run.Media
	var movies, series []store.MediaStat
	genreCounts := map[string]int{}
	decadeCounts := map[int]int{}
	langCounts := map[string]int{}
	countryCounts := map[string]int{}
	byLib := map[string][]store.MediaStat{}

	for _, m := range media {
		switch m.Type {
		case store.MediaMovie:
			movies = append(movies, m)
		case store.MediaSeries:
			series = append(series, m)
			sd.SeriesEpisodes += m.Episodes
		}
		byLib[m.LibraryID] = append(byLib[m.LibraryID], m)
		for _, g := range m.Genres {
			genreCounts[g]++
		}
		if m.Year > 0 {
			decadeCounts[(m.Year/10)*10]++
		}
		if name := languageName(m.Language); name != "" {
			langCounts[name]++
		}
		if name := countryName(m.Country); name != "" {
			countryCounts[name]++
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
			Top:    toRated(top, 50),
			Lowest: toRated(low, 50),
		})
	}

	// Longest / shortest movies by runtime.
	withRuntime := make([]store.MediaStat, 0, len(movies))
	for _, m := range movies {
		if m.Runtime > 0 {
			withRuntime = append(withRuntime, m)
		}
	}
	long := append([]store.MediaStat(nil), withRuntime...)
	sort.SliceStable(long, func(i, j int) bool { return long[i].Runtime > long[j].Runtime })
	sd.LongestMovies = toRuntime(long, 10)
	short := append([]store.MediaStat(nil), withRuntime...)
	sort.SliceStable(short, func(i, j int) bool { return short[i].Runtime < short[j].Runtime })
	sd.ShortestMovies = toRuntime(short, 10)

	sd.LongestSeries, sd.ShortestSeries = seriesByBingeTime(t, series)
	sd.Sagas = sagasByRuntime(t, movies)
	sd.SeriesOptions, sd.Flow = buildSeriesFlowData(t, series, "")

	// Niche & classic movies: golden-age classics (pre-1960, the black-and-white
	// era) and niche genres.
	nicheGenres := map[string]bool{
		"Documentary": true, "Western": true, "War": true,
		"History": true, "Music": true, "Film-Noir": true,
	}
	classic := t.T("stats.nicheClassic")
	seenNiche := map[string]bool{}
	for _, m := range movies {
		reason := ""
		if m.Year > 0 && m.Year < 1960 {
			reason = classic
		} else {
			for _, g := range m.Genres {
				if nicheGenres[g] {
					reason = g
					break
				}
			}
		}
		if reason == "" || seenNiche[m.Title] {
			continue
		}
		seenNiche[m.Title] = true
		sd.Niches = append(sd.Niches, StatsNiche{Title: m.Title, Year: m.Year, Reason: reason})
	}
	sort.SliceStable(sd.Niches, func(i, j int) bool { return sd.Niches[i].Year < sd.Niches[j].Year })
	if len(sd.Niches) > 12 {
		sd.Niches = sd.Niches[:12]
	}

	// Genre distribution (top 12).
	sd.Genres = topBars(genreCounts, 12, sortByCountDesc)

	// Release decade distribution (sorted ascending).
	decLabels := map[string]int{}
	for dec, c := range decadeCounts {
		decLabels[fmt.Sprintf("%ds", dec)] = c
	}
	sd.Years = topBars(decLabels, 0, sortByLabelAsc)

	sd.GenrePie = pieSlices(sd.Genres)
	sd.YearPie = pieSlices(sd.Years)
	sd.Languages = topBars(langCounts, 10, sortByCountDesc)
	sd.LanguagePie = pieSlices(sd.Languages)
	sd.Countries = topBars(countryCounts, 12, sortByCountDesc)
	sd.RatingSpread = ratingSpread(media)
	sd.GenreRatings = genreRatings(media)
	sd.Completion, sd.EpisodePct = seriesCompletion(t, series)
	sd.Facts = buildFacts(t, movies, series, sd.Genres, sd.EpisodePct)
}

// seriesCompletion builds the season heatmap per series plus the overall share
// of episodes owned across all series.
func seriesCompletion(t *i18n.Translator, series []store.MediaStat) ([]StatsCompletion, int) {
	ownedAll, totalAll := 0, 0
	var rows []StatsCompletion
	for _, s := range series {
		if s.TotalEpisodes == 0 {
			continue
		}
		ownedAll += s.Episodes
		totalAll += s.TotalEpisodes
		row := StatsCompletion{
			Title:   s.Title,
			Library: s.LibraryName,
			Color:   LibraryColor(s.LibraryID),
			Owned:   s.Episodes,
			Total:   s.TotalEpisodes,
			Pct:     percent(s.Episodes, s.TotalEpisodes),
		}
		for _, sn := range s.Seasons {
			pct := percent(sn.Episodes, sn.Total)
			cell := CompletionCell{
				Label: strconv.Itoa(sn.Number),
				Hint:  t.T("stats.seasonOwned", sn.Number, sn.Episodes, sn.Total),
				Fill:  completionFill(pct),
			}
			if sn.Number == 0 {
				cell.Label = "S"
				cell.Hint = t.T("stats.specialsOwned", sn.Episodes, sn.Total)
			}
			row.Cells = append(row.Cells, cell)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Pct < rows[j].Pct })
	if len(rows) > 20 {
		rows = rows[:20]
	}
	return rows, percent(ownedAll, totalAll)
}

func completionFill(pct int) string {
	switch {
	case pct >= 100:
		return "bg-emerald-500"
	case pct >= 75:
		return "bg-lime-500"
	case pct >= 50:
		return "bg-amber-500"
	case pct > 0:
		return "bg-orange-500"
	default:
		return "bg-rose-600"
	}
}

func percent(part, total int) int {
	if total <= 0 {
		return 0
	}
	return int(math.Round(float64(part) / float64(total) * 100))
}

// genreRatings averages the ratings of every genre with enough titles to be
// meaningful, the "which genre do you actually collect well" view.
func genreRatings(media []store.MediaStat) []StatsRatedGroup {
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, m := range media {
		if m.Rating <= 0 {
			continue
		}
		for _, g := range m.Genres {
			sums[g] += m.Rating
			counts[g]++
		}
	}
	type genreAvg struct {
		label string
		avg   float64
		count int
	}
	var avgs []genreAvg
	for g, c := range counts {
		if c < 3 {
			continue
		}
		avgs = append(avgs, genreAvg{label: g, avg: sums[g] / float64(c), count: c})
	}
	sort.SliceStable(avgs, func(i, j int) bool { return avgs[i].avg > avgs[j].avg })
	if len(avgs) > 12 {
		avgs = avgs[:12]
	}
	out := make([]StatsRatedGroup, 0, len(avgs))
	for _, a := range avgs {
		out = append(out, StatsRatedGroup{
			Label:  a.label,
			Rating: fmt.Sprintf("%.1f", a.avg),
			Count:  a.count,
			Pct:    int(math.Round(a.avg * 10)),
		})
	}
	return out
}

// seriesByBingeTime ranks series by the time needed to watch every owned
// episode (episode runtime times owned episodes).
func seriesByBingeTime(t *i18n.Translator, series []store.MediaStat) (longest, shortest []StatsRuntime) {
	type binge struct {
		stat    store.MediaStat
		minutes int
	}
	var all []binge
	for _, s := range series {
		if s.Episodes == 0 || s.Runtime <= 0 {
			continue
		}
		all = append(all, binge{stat: s, minutes: s.Episodes * s.Runtime})
	}
	if len(all) == 0 {
		return nil, nil
	}
	toList := func(bs []binge) []StatsRuntime {
		if len(bs) > 10 {
			bs = bs[:10]
		}
		out := make([]StatsRuntime, 0, len(bs))
		for _, b := range bs {
			out = append(out, StatsRuntime{
				Title:   b.stat.Title,
				Year:    b.stat.Year,
				Detail:  t.T("stats.seasonsEpisodes", ownedSeasonCount(b.stat), b.stat.Episodes),
				Runtime: formatWatchTime(b.minutes),
			})
		}
		return out
	}
	desc := append([]binge(nil), all...)
	sort.SliceStable(desc, func(i, j int) bool { return desc[i].minutes > desc[j].minutes })
	asc := append([]binge(nil), all...)
	sort.SliceStable(asc, func(i, j int) bool { return asc[i].minutes < asc[j].minutes })
	return toList(desc), toList(asc)
}

// sagasByRuntime groups owned movies by their TMDB collection and ranks the
// resulting sagas by the total runtime of the parts on the shelf.
func sagasByRuntime(t *i18n.Translator, movies []store.MediaStat) []StatsRuntime {
	type saga struct {
		name    string
		parts   int
		minutes int
		year    int
	}
	byID := map[int64]*saga{}
	var order []int64
	for _, m := range movies {
		if m.CollectionID == 0 || m.Runtime <= 0 {
			continue
		}
		s := byID[m.CollectionID]
		if s == nil {
			s = &saga{name: m.CollectionName, year: m.Year}
			byID[m.CollectionID] = s
			order = append(order, m.CollectionID)
		}
		s.parts++
		s.minutes += m.Runtime
		if m.Year > 0 && (s.year == 0 || m.Year < s.year) {
			s.year = m.Year
		}
	}
	var sagas []saga
	for _, id := range order {
		if byID[id].parts > 1 {
			sagas = append(sagas, *byID[id])
		}
	}
	sort.SliceStable(sagas, func(i, j int) bool { return sagas[i].minutes > sagas[j].minutes })
	if len(sagas) > 10 {
		sagas = sagas[:10]
	}
	out := make([]StatsRuntime, 0, len(sagas))
	for _, s := range sagas {
		out = append(out, StatsRuntime{
			Title:   s.name,
			Year:    s.year,
			Detail:  t.T("stats.partsOwned", s.parts),
			Runtime: formatWatchTime(s.minutes),
		})
	}
	return out
}

// ratingSpread buckets every rated title into whole-point rating bands, the
// distribution IMDb and TMDB show next to a rating.
func ratingSpread(media []store.MediaStat) []StatsBar {
	buckets := map[string]int{}
	for _, m := range media {
		if m.Rating <= 0 {
			continue
		}
		b := int(m.Rating)
		if b > 9 {
			b = 9
		}
		buckets[fmt.Sprintf("%d–%d", b, b+1)] = buckets[fmt.Sprintf("%d–%d", b, b+1)] + 1
	}
	return topBars(buckets, 0, sortByLabelAsc)
}

// ownedSeasonCount counts regular seasons (specials excluded) that have at
// least one episode on the shelf.
func ownedSeasonCount(m store.MediaStat) int {
	n := 0
	for _, sn := range m.Seasons {
		if sn.Number > 0 && sn.Episodes > 0 {
			n++
		}
	}
	return n
}

// specialEpisodes returns the owned episodes of season 0.
func specialEpisodes(m store.MediaStat) int {
	for _, sn := range m.Seasons {
		if sn.Number == 0 {
			return sn.Episodes
		}
	}
	return 0
}

// buildFacts assembles the headline numbers of the library.
func buildFacts(t *i18n.Translator, movies, series []store.MediaStat, genres []StatsBar, episodePct int) []StatsFact {
	movieMinutes := 0
	for _, m := range movies {
		movieMinutes += m.Runtime
	}
	seriesMinutes, episodes, seasons, specials := 0, 0, 0, 0
	for _, s := range series {
		seriesMinutes += s.Episodes * s.Runtime
		episodes += s.Episodes
		seasons += ownedSeasonCount(s)
		specials += specialEpisodes(s)
	}

	ratingSum, ratingCount := 0.0, 0
	oldest, newest := store.MediaStat{}, store.MediaStat{}
	for _, m := range append(append([]store.MediaStat(nil), movies...), series...) {
		if m.Rating > 0 {
			ratingSum += m.Rating
			ratingCount++
		}
		if m.Year > 0 && (oldest.Year == 0 || m.Year < oldest.Year) {
			oldest = m
		}
		if m.Year > newest.Year {
			newest = m
		}
	}

	var longestSeries store.MediaStat
	for _, s := range series {
		if ownedSeasonCount(s) > ownedSeasonCount(longestSeries) {
			longestSeries = s
		}
	}

	facts := []StatsFact{{
		Icon:  "\u23F3",
		Label: t.T("stats.factWatchTime"),
		Value: orDash(formatWatchTime(movieMinutes + seriesMinutes)),
		Hint:  t.T("stats.factWatchTimeHint", formatWatchTime(movieMinutes), formatWatchTime(seriesMinutes)),
	}}
	if ratingCount > 0 {
		facts = append(facts, StatsFact{
			Icon:  "\u2605",
			Label: t.T("stats.factAvgRating"),
			Value: fmt.Sprintf("%.1f", ratingSum/float64(ratingCount)),
			Hint:  t.T("stats.factAvgRatingHint", ratingCount),
		})
	}
	if len(series) > 0 && episodes > 0 {
		facts = append(facts, StatsFact{
			Icon:  "\U0001F4FA",
			Label: t.T("stats.factEpisodes"),
			Value: strconv.Itoa(episodes),
			Hint:  t.T("stats.factEpisodesHint", seasons, episodes/len(series)),
		})
	}
	if episodePct > 0 {
		facts = append(facts, StatsFact{
			Icon:  "\U0001F9E9",
			Label: t.T("stats.factEpisodeCompletion"),
			Value: strconv.Itoa(episodePct) + "%",
			Hint:  t.T("stats.factEpisodeCompletionHint"),
		})
	}
	if specials > 0 {
		facts = append(facts, StatsFact{
			Icon:  "\U0001F381",
			Label: t.T("stats.factSpecials"),
			Value: strconv.Itoa(percent(specials, episodes)) + "%",
			Hint:  t.T("stats.factSpecialsHint", specials),
		})
	}
	if longestSeries.Title != "" {
		facts = append(facts, StatsFact{
			Icon:  "\U0001F3C6",
			Label: t.T("stats.factMostSeasons"),
			Value: longestSeries.Title,
			Hint:  t.T("stats.seasonsEpisodes", ownedSeasonCount(longestSeries), longestSeries.Episodes),
		})
	}
	if oldest.Title != "" {
		facts = append(facts, StatsFact{
			Icon:  "\U0001F570",
			Label: t.T("stats.factOldest"),
			Value: oldest.Title,
			Hint:  strconv.Itoa(oldest.Year),
		})
	}
	if newest.Title != "" {
		facts = append(facts, StatsFact{
			Icon:  "\u2728",
			Label: t.T("stats.factNewest"),
			Value: newest.Title,
			Hint:  strconv.Itoa(newest.Year),
		})
	}
	if len(genres) > 0 {
		facts = append(facts, StatsFact{
			Icon:  "\U0001F3AD",
			Label: t.T("stats.factTopGenre"),
			Value: genres[0].Label,
			Hint:  t.T("stats.factTopGenreHint", genres[0].Count, genres[0].Share),
		})
	}
	return facts
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

// buildFindingRatings ranks the missing collection parts (individual missing
// movies, which carry a TMDB rating) by rating per library, so the user can see
// which missing titles are worth getting. Missing seasons/episodes have no
// standalone rating and are not included.
func buildFindingRatings(findings []store.Finding) []StatsLibraryRatings {
	type ratedPart struct {
		title  string
		year   int
		rating float64
	}
	byLib := map[string][]ratedPart{}
	names := map[string]string{}
	var order []string
	for _, f := range findings {
		if f.Kind != store.KindMissingCollection {
			continue
		}
		var d detailPayload
		if f.Details != "" {
			_ = json.Unmarshal([]byte(f.Details), &d)
		}
		for _, p := range d.MissingParts {
			if p.Rating <= 0 {
				continue
			}
			if _, ok := byLib[f.LibraryID]; !ok {
				order = append(order, f.LibraryID)
				names[f.LibraryID] = f.LibraryName
			}
			byLib[f.LibraryID] = append(byLib[f.LibraryID], ratedPart{title: p.Title, year: yearInt(p.Year), rating: p.Rating})
		}
	}

	toRatedParts := func(parts []ratedPart, n int) []StatsRated {
		if len(parts) > n {
			parts = parts[:n]
		}
		out := make([]StatsRated, 0, len(parts))
		for _, p := range parts {
			out = append(out, StatsRated{Title: p.title, Year: p.year, Rating: fmt.Sprintf("%.1f", p.rating)})
		}
		return out
	}

	var out []StatsLibraryRatings
	for _, id := range order {
		parts := byLib[id]
		top := append([]ratedPart(nil), parts...)
		sort.SliceStable(top, func(i, j int) bool { return top[i].rating > top[j].rating })
		low := append([]ratedPart(nil), parts...)
		sort.SliceStable(low, func(i, j int) bool { return low[i].rating < low[j].rating })
		out = append(out, StatsLibraryRatings{
			Name:   names[id],
			Color:  LibraryColor(id),
			Top:    toRatedParts(top, 50),
			Lowest: toRatedParts(low, 50),
		})
	}
	return out
}

// buildSeriesFindingRatings ranks the series that have gaps by their own TMDB
// rating per library. Seasons and episodes carry no standalone rating, so the
// rating of the owned series is taken from the scan's media stats.
func buildSeriesFindingRatings(findings []store.Finding, media []store.MediaStat) []StatsLibraryRatings {
	type ratedSeries struct {
		title   string
		year    int
		rating  float64
		missing int
	}
	stats := map[string]store.MediaStat{}
	for _, m := range media {
		if m.Type == store.MediaSeries {
			stats[m.LibraryID+"\x00"+m.Title] = m
		}
	}

	byLib := map[string]map[string]*ratedSeries{}
	names := map[string]string{}
	var order []string
	for _, f := range findings {
		if f.Kind != store.KindMissingSeason && f.Kind != store.KindMissingEpisodes {
			continue
		}
		m, ok := stats[f.LibraryID+"\x00"+f.Title]
		if !ok || m.Rating <= 0 {
			continue
		}
		if byLib[f.LibraryID] == nil {
			byLib[f.LibraryID] = map[string]*ratedSeries{}
			names[f.LibraryID] = f.LibraryName
			order = append(order, f.LibraryID)
		}
		entry := byLib[f.LibraryID][f.Title]
		if entry == nil {
			entry = &ratedSeries{title: f.Title, year: m.Year, rating: m.Rating}
			byLib[f.LibraryID][f.Title] = entry
		}
		_, count, _ := findingMissing(f)
		entry.missing += count
	}

	toRatedSeries := func(items []ratedSeries, n int) []StatsRated {
		if len(items) > n {
			items = items[:n]
		}
		out := make([]StatsRated, 0, len(items))
		for _, it := range items {
			out = append(out, StatsRated{Title: it.title, Year: it.year, Rating: fmt.Sprintf("%.1f", it.rating)})
		}
		return out
	}

	var out []StatsLibraryRatings
	for _, id := range order {
		items := make([]ratedSeries, 0, len(byLib[id]))
		for _, v := range byLib[id] {
			items = append(items, *v)
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].title < items[j].title })
		top := append([]ratedSeries(nil), items...)
		sort.SliceStable(top, func(i, j int) bool { return top[i].rating > top[j].rating })
		low := append([]ratedSeries(nil), items...)
		sort.SliceStable(low, func(i, j int) bool { return low[i].rating < low[j].rating })
		out = append(out, StatsLibraryRatings{
			Name:   names[id],
			Color:  LibraryColor(id),
			Top:    toRatedSeries(top, 50),
			Lowest: toRatedSeries(low, 50),
		})
	}
	return out
}

// yearInt parses a year string, returning 0 when it is empty or invalid.
func yearInt(y string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(y)); err == nil && n > 0 {
		return n
	}
	return 0
}

func toRuntime(ms []store.MediaStat, n int) []StatsRuntime {
	if len(ms) > n {
		ms = ms[:n]
	}
	out := make([]StatsRuntime, 0, len(ms))
	for _, m := range ms {
		out = append(out, StatsRuntime{Title: m.Title, Year: m.Year, Runtime: formatRuntime(m.Runtime)})
	}
	return out
}

const (
	sortByCountDesc = iota
	sortByLabelAsc
)

func topBars(counts map[string]int, limit, mode int) []StatsBar {
	bars := make([]StatsBar, 0, len(counts))
	max, total := 0, 0
	for k, c := range counts {
		bars = append(bars, StatsBar{Label: k, Count: c})
		total += c
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
		if total > 0 {
			bars[i].Share = int(math.Round(float64(bars[i].Count) / float64(total) * 100))
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

// formatWatchTime renders long totals with days, which runtime alone would hide.
func formatWatchTime(min int) string {
	if min <= 0 {
		return ""
	}
	if min < 24*60 {
		return formatRuntime(min)
	}
	d := min / (24 * 60)
	h := (min % (24 * 60)) / 60
	return fmt.Sprintf("%dd %dh", d, h)
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
