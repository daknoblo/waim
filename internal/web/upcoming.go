package web

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	HasAny      bool // the run recorded releases at all
	Available   bool // the current selection has releases
	Past        bool // the retrospective direction is selected
	NeedsRescan bool // gaps exist but predate the release dates, so a rescan is needed
	Total       int
	Episodes    int
	Movies      int
	NextTitle   string
	NextLabel   string
	Truncated   int
	Timeline    UpcomingTimeline
	Groups      []UpcomingGroup
	Directions  []UpcomingFilter
	Ranges      []UpcomingFilter
	Types       []UpcomingFilter
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
	Direction string // upcoming | past
	Range     string // 30 | 90 | 180 | 365 | all
	Type      string // all | series | movie
}

const (
	upcomingRangeAll     = "all"
	upcomingTypeAll      = "all"
	upcomingDefaultRange = "90"

	// UpcomingForward lists announced releases, UpcomingPast what was released
	// while it is still missing from the library.
	UpcomingForward = "upcoming"
	UpcomingPast    = "past"
)

var upcomingRanges = []string{"30", upcomingDefaultRange, "180", "365", upcomingRangeAll}

// NormalizeUpcomingQuery clamps user input to the supported options.
func NormalizeUpcomingQuery(directionV, rangeV, typeV string) UpcomingQuery {
	q := UpcomingQuery{Direction: UpcomingForward, Range: upcomingDefaultRange, Type: upcomingTypeAll}
	if directionV == UpcomingPast {
		q.Direction = UpcomingPast
	}
	if slices.Contains(upcomingRanges, rangeV) {
		q.Range = rangeV
	}
	if typeV == store.MediaSeries || typeV == store.MediaMovie {
		q.Type = typeV
	}
	return q
}

// IsPast reports whether the retrospective direction is selected.
func (q UpcomingQuery) IsPast() bool { return q.Direction == UpcomingPast }

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
// what the HTMX partial re-renders when a control changes.
//
// The retrospective direction is derived from the findings instead of the
// recorded releases: what has already aired and is still missing is exactly
// what the scan reports as a gap, so nothing has to be carried across runs.
func BuildUpcomingSection(t *i18n.Translator, run *store.ScanRun, findings []store.Finding, q UpcomingQuery) StatsUpcoming {
	if run == nil {
		return StatsUpcoming{}
	}
	if q.IsPast() {
		items, undated := pastItemsFromFindings(findings)
		su := buildUpcoming(t, items, time.Now(), q)
		su.HasAny = len(items) > 0
		// Gaps exist but none carry a date: the scan predates this feature, so
		// saying "nothing missed" would be wrong.
		su.NeedsRescan = len(items) == 0 && undated > 0
		return su
	}
	return buildUpcoming(t, run.Upcoming, time.Now(), q)
}

// pastItemsFromFindings converts the gaps of a scan into releases that already
// happened. The second return value counts gaps that had to be skipped because
// they carry no date, which happens for findings stored before this feature
// existed.
func pastItemsFromFindings(findings []store.Finding) ([]store.UpcomingItem, int) {
	var out []store.UpcomingItem
	undated := 0
	for _, f := range findings {
		var d detailPayload
		if f.Details != "" {
			_ = json.Unmarshal([]byte(f.Details), &d)
		}
		switch f.Kind {
		case store.KindMissingSeason, store.KindMissingEpisodes:
			for _, ep := range d.MissingEpisodes {
				date := d.AirDates[strconv.Itoa(ep)]
				if strings.TrimSpace(date) == "" {
					undated++
					continue
				}
				out = append(out, store.UpcomingItem{
					Kind:          store.UpcomingEpisode,
					MediaType:     store.MediaSeries,
					SourceTitle:   f.Title,
					TMDBID:        f.TMDBID,
					SourceTMDBID:  f.TMDBID,
					SeasonNumber:  d.SeasonNumber,
					EpisodeNumber: ep,
					ReleaseDate:   date,
					PosterPath:    d.PosterPath,
					LibraryID:     f.LibraryID,
					LibraryName:   f.LibraryName,
					JellyfinID:    f.JellyfinID,
				})
			}
		case store.KindMissingCollection:
			for _, p := range d.MissingParts {
				if strings.TrimSpace(p.ReleaseDate) == "" {
					undated++
					continue
				}
				out = append(out, store.UpcomingItem{
					Kind:         store.UpcomingCollectionPart,
					MediaType:    store.MediaMovie,
					Title:        p.Title,
					SourceTitle:  f.Title,
					TMDBID:       p.TMDBID,
					SourceTMDBID: f.TMDBID,
					ReleaseDate:  p.ReleaseDate,
					PosterPath:   d.PosterPath,
					Rating:       p.Rating,
					LibraryID:    f.LibraryID,
					LibraryName:  f.LibraryName,
				})
			}
		}
	}
	return out, undated
}

// buildUpcoming turns releases into the section model. Depending on the query
// direction the window either extends forward from today or back into the past.
func buildUpcoming(t *i18n.Translator, items []store.UpcomingItem, now time.Time, q UpcomingQuery) StatsUpcoming {
	su := StatsUpcoming{HasAny: len(items) > 0, Past: q.IsPast()}
	if !su.HasAny {
		return su
	}
	today := dayOf(now)
	su.Directions = upcomingDirectionOptions(t, q.Direction)
	su.Ranges = upcomingRangeOptions(t, q.Range, q.IsPast())
	su.Types = upcomingTypeOptions(t, q.Type)

	var cutoff time.Time
	if d := q.days(); d > 0 {
		if q.IsPast() {
			cutoff = today.AddDate(0, 0, -d)
		} else {
			cutoff = today.AddDate(0, 0, d)
		}
	}

	byKey := map[string]*upcomingAgg{}
	var order []string
	for _, it := range items {
		if q.Type != upcomingTypeAll && it.MediaType != q.Type {
			continue
		}
		d, dated := parseUpcomingDate(it.ReleaseDate)
		if q.IsPast() {
			// A release without a date cannot be placed in the past at all.
			if !dated || d.After(today) {
				continue
			}
			if !cutoff.IsZero() && d.Before(cutoff) {
				continue
			}
		} else if !cutoff.IsZero() {
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
			// The retrospective reads best with the most recent release first.
			if q.IsPast() {
				return a.date.After(b.date)
			}
			return a.date.Before(b.date)
		}
		return a.item.SourceTitle < b.item.SourceTitle
	})

	if len(aggs) > upcomingGridLimit {
		su.Truncated = len(aggs) - upcomingGridLimit
		aggs = aggs[:upcomingGridLimit]
	}

	su.Timeline = buildUpcomingTimeline(t, aggs, today)
	su.Groups = upcomingGroups(t, aggs, today, q.IsPast())
	for _, a := range aggs {
		if a.dated {
			su.NextTitle = upcomingTitle(a)
			if q.IsPast() {
				su.NextLabel = upcomingElapsed(t, a.date, today)
			} else {
				su.NextLabel = upcomingCountdown(t, a.date, today)
			}
			break
		}
	}
	return su
}

func upcomingRangeOptions(t *i18n.Translator, selected string, past bool) []UpcomingFilter {
	out := make([]UpcomingFilter, 0, len(upcomingRanges))
	for _, r := range upcomingRanges {
		var label string
		switch {
		case r == upcomingRangeAll && past:
			label = t.T("stats.upcomingRangeAllPast")
		case r == upcomingRangeAll:
			label = t.T("stats.upcomingRangeAll")
		case past:
			label = t.T("stats.upcomingRangeDaysPast", mustAtoi(r))
		default:
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

func upcomingDirectionOptions(t *i18n.Translator, selected string) []UpcomingFilter {
	return []UpcomingFilter{
		{Value: UpcomingForward, Label: t.T("stats.upcomingDirForward"), Selected: selected != UpcomingPast},
		{Value: UpcomingPast, Label: t.T("stats.upcomingDirPast"), Selected: selected == UpcomingPast},
	}
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func upcomingGroups(t *i18n.Translator, aggs []*upcomingAgg, today time.Time, past bool) []UpcomingGroup {
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
	weekEnd := today.AddDate(0, 0, 7)
	weekStart := today.AddDate(0, 0, -7)
	// Entries are sorted dated-first, so the buckets are created in display order.
	for _, a := range aggs {
		e := upcomingEntry(t, a, today, past)
		switch {
		case !a.dated:
			add("tba", t.T("stats.upcomingTba"), e)
		case past && !a.date.Before(weekStart):
			add("week", t.T("stats.upcomingLastWeek"), e)
		case !past && a.date.Before(weekEnd):
			add("week", t.T("stats.upcomingThisWeek"), e)
		default:
			add(a.date.Format("2006-01"), monthLabel(t, a.date), e)
		}
	}
	return groups
}

func upcomingEntry(t *i18n.Translator, a *upcomingAgg, today time.Time, past bool) UpcomingEntry {
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
		if past {
			e.Hint += " (" + upcomingElapsed(t, a.date, today) + ")"
		} else {
			e.Hint += " (" + upcomingCountdown(t, a.date, today) + ")"
		}
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
	// Derive the span from the actual extremes rather than the slice order, so
	// the retrospective (sorted newest first) plots the same way.
	minDate, maxDate := dated[0].date, dated[0].date
	for _, a := range dated[1:] {
		if a.date.Before(minDate) {
			minDate = a.date
		}
		if a.date.After(maxDate) {
			maxDate = a.date
		}
	}
	start := today
	if minDate.Before(start) {
		start = minDate
	}
	end := maxDate
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

// upcomingElapsed is the retrospective counterpart of upcomingCountdown.
func upcomingElapsed(t *i18n.Translator, d, today time.Time) string {
	days := int(today.Sub(dayOf(d)).Hours() / 24)
	switch {
	case days <= 0:
		return t.T("stats.upcomingToday")
	case days == 1:
		return t.T("stats.upcomingYesterday")
	case days <= 30:
		return t.T("stats.upcomingDaysAgo", days)
	default:
		return t.T("stats.upcomingMonthsAgo", days/30)
	}
}
