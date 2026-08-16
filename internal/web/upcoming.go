package web

import (
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
	Available bool
	Total     int
	Episodes  int
	Movies    int
	NextTitle string
	NextLabel string
	Truncated int
	Timeline  UpcomingTimeline
	Groups    []UpcomingGroup
	Filters   []UpcomingFilter
}

// UpcomingGroup bundles the releases of one timeframe.
type UpcomingGroup struct {
	Label string
	Count int
	Items []UpcomingEntry
}

// UpcomingEntry is one tile of the poster grid. Consecutive episodes of the
// same season collapse into a single entry.
type UpcomingEntry struct {
	MediaType string
	Title     string
	Sub       string
	DateLabel string
	Countdown string
	PosterURL string
	Icon      string
	Link      string
}

// UpcomingFilter is an option of the media type filter.
type UpcomingFilter struct {
	Value    string
	Label    string
	Selected bool
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

// buildUpcoming turns the announced releases of a scan into the section model.
func buildUpcoming(t *i18n.Translator, items []store.UpcomingItem, now time.Time) StatsUpcoming {
	su := StatsUpcoming{}
	if len(items) == 0 {
		return su
	}
	today := dayOf(now)

	byKey := map[string]*upcomingAgg{}
	var order []string
	for _, it := range items {
		switch it.MediaType {
		case store.MediaSeries:
			su.Episodes++
		case store.MediaMovie:
			su.Movies++
		}
		key := upcomingKey(it)
		agg, ok := byKey[key]
		if !ok {
			d, dated := parseUpcomingDate(it.ReleaseDate)
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
		if d, dated := parseUpcomingDate(it.ReleaseDate); dated && (!agg.dated || d.Before(agg.date)) {
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
	su.Filters = []UpcomingFilter{
		{Value: "all", Label: t.T("stats.upcomingFilterAll"), Selected: true},
		{Value: store.MediaSeries, Label: t.T("stats.upcomingFilterSeries")},
		{Value: store.MediaMovie, Label: t.T("stats.upcomingFilterMovies")},
	}
	for _, a := range aggs {
		if a.dated {
			su.NextTitle = upcomingTitle(a)
			su.NextLabel = upcomingCountdown(t, a.date, today)
			break
		}
	}
	return su
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
		e.Countdown = upcomingCountdown(t, a.date, today)
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
