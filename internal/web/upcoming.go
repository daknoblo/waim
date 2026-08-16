package web

import (
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

// Geometry of the release timeline inside its 0 0 1000 H viewBox. Positions are
// pre-computed into attribute strings because the CSP forbids inline styles.
const (
	timelineLeft     = 24.0
	timelineRight    = 976.0
	timelineAxisY    = 26.0
	timelineLaneTop  = 46.0
	timelineLaneStep = 15.0
	timelineMinGap   = 13.0
	timelineMaxLanes = 8
	timelineRadius   = 5.0
	timelineLabelGap = 78.0
)

// upcomingGridLimit caps how many tiles the poster grid renders; the counters
// above it always reflect the full set.
const upcomingGridLimit = 120

// UpcomingTimeline is a pre-computed calendar rail of announced releases.
type UpcomingTimeline struct {
	Available bool
	Height    int
	AxisY     string
	AxisFrom  string
	AxisTo    string
	Ticks     []TimelineTick
	Markers   []TimelineMarker
}

// TimelineTick is a month boundary on the timeline axis.
type TimelineTick struct {
	X     string
	Y2    string
	Label string
}

// TimelineMarker is a single release plotted on the timeline.
type TimelineMarker struct {
	CX        string
	CY        string
	R         string
	Fill      string
	Title     string
	MediaType string
}

// StatsUpcoming is the "coming up" section of the statistics page.
type StatsUpcoming struct {
	HasAny    bool // the run recorded upcoming releases at all
	Available bool // the current selection has releases
	Total     int
	Episodes  int
	Movies    int
	NextTitle string
	NextLabel string
	Truncated int
	Timeline  UpcomingTimeline
	Groups    []UpcomingGroup
	Ranges    []UpcomingFilter
	Types     []UpcomingFilter
}

// UpcomingGroup bundles the releases of one timeframe.
type UpcomingGroup struct {
	Label string
	Count int
	Items []UpcomingEntry
}

// UpcomingEntry is one tile of the poster row. Consecutive episodes of the
// same season collapse into a single entry.
type UpcomingEntry struct {
	MediaType string
	Title     string
	Sub       string
	DateLabel string
	Hint      string
	PosterURL string
	Icon      string
	Link      string
}

// UpcomingFilter is an option of one of the section's dropdowns.
type UpcomingFilter struct {
	Value    string
	Label    string
	Selected bool
}

// UpcomingQuery is the current selection of the upcoming section.
type UpcomingQuery struct {
	Range string // 30 | 90 | 180 | 365 | all
	Type  string // all | series | movie
}

const (
	upcomingRangeAll     = "all"
	upcomingTypeAll      = "all"
	upcomingDefaultRange = "90"
)

var upcomingRanges = []string{"30", upcomingDefaultRange, "180", "365", upcomingRangeAll}

// NormalizeUpcomingQuery clamps user input to the supported options.
func NormalizeUpcomingQuery(rangeV, typeV string) UpcomingQuery {
	q := UpcomingQuery{Range: upcomingDefaultRange, Type: upcomingTypeAll}
	if slices.Contains(upcomingRanges, rangeV) {
		q.Range = rangeV
	}
	if typeV == store.MediaSeries || typeV == store.MediaMovie {
		q.Type = typeV
	}
	return q
}

// days returns the selected window in days, or 0 for the unlimited range.
func (q UpcomingQuery) days() int {
	n, err := strconv.Atoi(q.Range)
	if err != nil {
		return 0
	}
	return n
}

// upcomingAgg accumulates the episodes of one season into a single tile.
type upcomingAgg struct {
	item     store.UpcomingItem
	firstEp  int
	lastEp   int
	episodes int
	date     time.Time
	dated    bool
}

// BuildUpcomingSection builds the "coming up" section for a scan run, which is
// what the HTMX partial re-renders when a dropdown changes.
func BuildUpcomingSection(t *i18n.Translator, run *store.ScanRun, q UpcomingQuery) StatsUpcoming {
	if run == nil {
		return StatsUpcoming{}
	}
	return buildUpcoming(t, run.Upcoming, time.Now(), q)
}

// buildUpcoming turns the announced releases of a scan into the section model.
func buildUpcoming(t *i18n.Translator, items []store.UpcomingItem, now time.Time, q UpcomingQuery) StatsUpcoming {
	su := StatsUpcoming{HasAny: len(items) > 0}
	if !su.HasAny {
		return su
	}
	today := dayOf(now)
	su.Ranges = upcomingRangeOptions(t, q.Range)
	su.Types = upcomingTypeOptions(t, q.Type)

	var cutoff time.Time
	if d := q.days(); d > 0 {
		cutoff = today.AddDate(0, 0, d)
	}

	byKey := map[string]*upcomingAgg{}
	var order []string
	for _, it := range items {
		if q.Type != upcomingTypeAll && it.MediaType != q.Type {
			continue
		}
		d, dated := parseUpcomingDate(it.ReleaseDate)
		if !cutoff.IsZero() {
			// A window selects by date, so undated entries only show up in the
			// unlimited range.
			if !dated || d.After(cutoff) {
				continue
			}
		}
		switch it.MediaType {
		case store.MediaSeries:
			su.Episodes++
		case store.MediaMovie:
			su.Movies++
		}
		key := upcomingKey(it)
		agg, ok := byKey[key]
		if !ok {
			agg = &upcomingAgg{item: it, firstEp: it.EpisodeNumber, lastEp: it.EpisodeNumber, date: d, dated: dated}
			byKey[key] = agg
			order = append(order, key)
		}
		agg.episodes++
		if it.EpisodeNumber < agg.firstEp {
			agg.firstEp = it.EpisodeNumber
		}
		if it.EpisodeNumber > agg.lastEp {
			agg.lastEp = it.EpisodeNumber
		}
		if dated && (!agg.dated || d.Before(agg.date)) {
			agg.date, agg.dated = d, true
			agg.item = it
		}
	}
	su.Total = su.Episodes + su.Movies
	if su.Total == 0 {
		return su
	}
	su.Available = true

	aggs := make([]*upcomingAgg, 0, len(order))
	for _, k := range order {
		aggs = append(aggs, byKey[k])
	}
	sort.SliceStable(aggs, func(i, j int) bool {
		a, b := aggs[i], aggs[j]
		if a.dated != b.dated {
			return a.dated
		}
		if !a.date.Equal(b.date) {
			return a.date.Before(b.date)
		}
		return a.item.SourceTitle < b.item.SourceTitle
	})

	if len(aggs) > upcomingGridLimit {
		su.Truncated = len(aggs) - upcomingGridLimit
		aggs = aggs[:upcomingGridLimit]
	}

	su.Timeline = buildUpcomingTimeline(t, aggs, today)
	su.Groups = upcomingGroups(t, aggs, today)
	for _, a := range aggs {
		if a.dated {
			su.NextTitle = upcomingTitle(a)
			su.NextLabel = upcomingCountdown(t, a.date, today)
			break
		}
	}
	return su
}

func upcomingRangeOptions(t *i18n.Translator, selected string) []UpcomingFilter {
	out := make([]UpcomingFilter, 0, len(upcomingRanges))
	for _, r := range upcomingRanges {
		label := t.T("stats.upcomingRangeAll")
		if r != upcomingRangeAll {
			label = t.T("stats.upcomingRangeDays", mustAtoi(r))
		}
		out = append(out, UpcomingFilter{Value: r, Label: label, Selected: r == selected})
	}
	return out
}

func upcomingTypeOptions(t *i18n.Translator, selected string) []UpcomingFilter {
	return []UpcomingFilter{
		{Value: upcomingTypeAll, Label: t.T("stats.upcomingFilterAll"), Selected: selected == upcomingTypeAll},
		{Value: store.MediaSeries, Label: t.T("stats.upcomingFilterSeries"), Selected: selected == store.MediaSeries},
		{Value: store.MediaMovie, Label: t.T("stats.upcomingFilterMovies"), Selected: selected == store.MediaMovie},
	}
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func upcomingGroups(t *i18n.Translator, aggs []*upcomingAgg, today time.Time) []UpcomingGroup {
	weekEnd := today.AddDate(0, 0, 7)
	var groups []UpcomingGroup
	index := map[string]int{}
	add := func(key, label string, e UpcomingEntry) {
		i, ok := index[key]
		if !ok {
			groups = append(groups, UpcomingGroup{Label: label})
			i = len(groups) - 1
			index[key] = i
		}
		groups[i].Items = append(groups[i].Items, e)
		groups[i].Count++
	}
	// Entries are sorted dated-first, so the buckets are created in display order.
	for _, a := range aggs {
		e := upcomingEntry(t, a, today)
		switch {
		case !a.dated:
			add("tba", t.T("stats.upcomingTba"), e)
		case a.date.Before(weekEnd):
			add("week", t.T("stats.upcomingThisWeek"), e)
		default:
			add(a.date.Format("2006-01"), monthLabel(t, a.date), e)
		}
	}
	return groups
}

func upcomingEntry(t *i18n.Translator, a *upcomingAgg, today time.Time) UpcomingEntry {
	e := UpcomingEntry{
		MediaType: a.item.MediaType,
		Title:     upcomingTitle(a),
		Sub:       upcomingSub(t, a),
		PosterURL: posterURL(a.item.PosterPath),
		Icon:      mediaIcon(a.item.MediaType),
		DateLabel: t.T("stats.upcomingTba"),
	}
	if a.item.MediaType == store.MediaSeries {
		e.Link = tmdbLink("tv", a.item.TMDBID)
	} else {
		e.Link = tmdbLink("movie", a.item.TMDBID)
	}
	if a.dated {
		e.DateLabel = dateLabel(t, a.date)
	}
	// The tiles are narrow, so the full context lives in the tooltip.
	e.Hint = e.Title + " \u00b7 " + e.Sub + " \u00b7 " + e.DateLabel
	if a.dated {
		e.Hint += " (" + upcomingCountdown(t, a.date, today) + ")"
	}
	return e
}

func upcomingTitle(a *upcomingAgg) string {
	if a.item.MediaType == store.MediaSeries {
		return a.item.SourceTitle
	}
	return a.item.Title
}

func upcomingSub(t *i18n.Translator, a *upcomingAgg) string {
	if a.item.MediaType != store.MediaSeries {
		return t.T("stats.upcomingPartOf", a.item.SourceTitle)
	}
	if a.episodes > 1 && a.lastEp > a.firstEp {
		return t.T("stats.upcomingSeasonRange", a.item.SeasonNumber, a.firstEp, a.lastEp)
	}
	return t.T("stats.upcomingSeasonEpisode", a.item.SeasonNumber, a.item.EpisodeNumber)
}

// buildUpcomingTimeline plots the dated releases on a linear time axis, moving
// markers into additional lanes whenever they would overlap.
func buildUpcomingTimeline(t *i18n.Translator, aggs []*upcomingAgg, today time.Time) UpcomingTimeline {
	tl := UpcomingTimeline{}
	var dated []*upcomingAgg
	for _, a := range aggs {
		if a.dated {
			dated = append(dated, a)
		}
	}
	if len(dated) == 0 {
		return tl
	}
	start := today
	if first := dated[0].date; first.Before(start) {
		start = first
	}
	end := dated[len(dated)-1].date
	if !end.After(start.AddDate(0, 0, 14)) {
		end = start.AddDate(0, 0, 14)
	}
	span := end.Sub(start).Seconds()
	pos := func(d time.Time) float64 {
		x := timelineLeft + d.Sub(start).Seconds()/span*(timelineRight-timelineLeft)
		if x < timelineLeft {
			return timelineLeft
		}
		if x > timelineRight {
			return timelineRight
		}
		return x
	}

	laneX := make([]float64, 0, timelineMaxLanes)
	markers := make([]TimelineMarker, 0, len(dated))
	maxLane := 0
	for _, a := range dated {
		x := pos(a.date)
		lane := len(laneX)
		for i, last := range laneX {
			if x-last >= timelineMinGap {
				lane = i
				break
			}
		}
		if lane == len(laneX) {
			if lane >= timelineMaxLanes {
				lane = 0
			} else {
				laneX = append(laneX, 0)
			}
		}
		laneX[lane] = x
		if lane > maxLane {
			maxLane = lane
		}
		markers = append(markers, TimelineMarker{
			CX:        num(x),
			CY:        num(timelineLaneTop + float64(lane)*timelineLaneStep),
			R:         num(timelineRadius),
			Fill:      upcomingFill(a.item.MediaType),
			Title:     dateLabel(t, a.date) + " \u00b7 " + upcomingTitle(a) + " \u00b7 " + upcomingSub(t, a),
			MediaType: a.item.MediaType,
		})
	}

	height := int(timelineLaneTop+float64(maxLane)*timelineLaneStep+timelineRadius) + 8
	tl.Available = true
	tl.Height = height
	tl.AxisY = num(timelineAxisY)
	tl.AxisFrom = num(timelineLeft)
	tl.AxisTo = num(timelineRight)
	tl.Markers = markers

	tickBottom := num(float64(height) - 4)
	lastLabelX := -timelineLabelGap
	for m := monthStart(start); !m.After(end); m = m.AddDate(0, 1, 0) {
		if m.Before(start) {
			continue
		}
		x := pos(m)
		tick := TimelineTick{X: num(x), Y2: tickBottom}
		if x-lastLabelX >= timelineLabelGap {
			tick.Label = monthLabel(t, m)
			lastLabelX = x
		}
		tl.Ticks = append(tl.Ticks, tick)
	}
	return tl
}

func upcomingFill(mediaType string) string {
	if mediaType == store.MediaMovie {
		return "fill-sky-400"
	}
	return "fill-indigo-400"
}

// upcomingKey collapses all episodes of one season of one series into a single
// tile; collection parts stay individual.
func upcomingKey(it store.UpcomingItem) string {
	if it.Kind == store.UpcomingEpisode {
		return "s:" + strconv.FormatInt(it.SourceTMDBID, 10) + ":" + strconv.Itoa(it.SeasonNumber)
	}
	return "m:" + strconv.FormatInt(it.TMDBID, 10)
}

func parseUpcomingDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func monthName(t *i18n.Translator, m time.Month) string {
	return t.T("month." + strconv.Itoa(int(m)))
}

func monthLabel(t *i18n.Translator, d time.Time) string {
	return monthName(t, d.Month()) + " " + strconv.Itoa(d.Year())
}

func dateLabel(t *i18n.Translator, d time.Time) string {
	return t.T("stats.upcomingDate", monthName(t, d.Month()), d.Day())
}

// suggestionDate formats a TMDB release date, dropping the day for releases
// that are further out than the current year.
func suggestionDate(t *i18n.Translator, iso string) string {
	d, ok := parseUpcomingDate(iso)
	if !ok {
		return ""
	}
	if d.Year() == time.Now().Year() {
		return dateLabel(t, d)
	}
	return monthLabel(t, d)
}

func upcomingCountdown(t *i18n.Translator, d, today time.Time) string {
	days := int(dayOf(d).Sub(today).Hours() / 24)
	switch {
	case days <= 0:
		return t.T("stats.upcomingToday")
	case days == 1:
		return t.T("stats.upcomingTomorrow")
	case days <= 30:
		return t.T("stats.upcomingInDays", days)
	default:
		return t.T("stats.upcomingInMonths", days/30)
	}
}
