package web

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/store"
)

func upcomingFixture() []store.UpcomingItem {
	ep := func(season, number int, date string) store.UpcomingItem {
		return store.UpcomingItem{
			Kind: store.UpcomingEpisode, MediaType: store.MediaSeries,
			Title: "Episode", SourceTitle: "Show A", SourceTMDBID: 100, TMDBID: 100,
			SeasonNumber: season, EpisodeNumber: number, ReleaseDate: date,
		}
	}
	return []store.UpcomingItem{
		ep(3, 1, "2026-01-03"),
		ep(3, 2, "2026-01-10"),
		ep(3, 3, "2026-01-17"),
		{
			Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie,
			Title: "Movie X 3", SourceTitle: "X Collection", SourceTMDBID: 500, TMDBID: 202,
			ReleaseDate: "2026-04-01",
		},
		{
			Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie,
			Title: "Movie X 4", SourceTitle: "X Collection", SourceTMDBID: 500, TMDBID: 203,
		},
	}
}

func TestBuildUpcomingGroupsAndCounts(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	u := buildUpcoming(testTranslator(t), upcomingFixture(), now, NormalizeUpcomingQuery("", "all", ""))

	if !u.Available {
		t.Fatal("Available = false, want true")
	}
	if u.Total != 5 || u.Episodes != 3 || u.Movies != 2 {
		t.Errorf("counts = %d/%d/%d, want 5/3/2", u.Total, u.Episodes, u.Movies)
	}
	// The three episodes of one season collapse into a single tile.
	var tiles int
	for _, g := range u.Groups {
		tiles += len(g.Items)
	}
	if tiles != 3 {
		t.Errorf("tiles = %d, want 3 (one per season / collection part)", tiles)
	}
	if len(u.Groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(u.Groups))
	}
	if u.Groups[0].Label != "Next 7 days" {
		t.Errorf("first group = %q, want the week bucket", u.Groups[0].Label)
	}
	if last := u.Groups[len(u.Groups)-1]; last.Label != "Date to be announced" {
		t.Errorf("last group = %q, want the undated bucket", last.Label)
	}
	if got := u.Groups[0].Items[0].Sub; got != "S3 · E1–3" {
		t.Errorf("collapsed episode range = %q", got)
	}
	if u.NextTitle != "Show A" || u.NextLabel != "in 2 days" {
		t.Errorf("next release = %q / %q", u.NextTitle, u.NextLabel)
	}
}

func TestBuildUpcomingTimelineGeometry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tl := buildUpcoming(testTranslator(t), upcomingFixture(), now, NormalizeUpcomingQuery("", "all", "")).Timeline

	if !tl.Available {
		t.Fatal("Available = false, want true")
	}
	// Undated entries are excluded from the axis.
	if len(tl.Markers) != 2 {
		t.Fatalf("markers = %d, want 2", len(tl.Markers))
	}
	if tl.Height <= 0 {
		t.Errorf("Height = %d, want > 0", tl.Height)
	}
	prev := -1.0
	for _, m := range tl.Markers {
		x, err := strconv.ParseFloat(m.CX, 64)
		if err != nil {
			t.Fatalf("marker cx %q: %v", m.CX, err)
		}
		if x < timelineLeft || x > timelineRight {
			t.Errorf("marker cx %v outside the axis", x)
		}
		if x < prev {
			t.Errorf("markers not in chronological order: %v after %v", x, prev)
		}
		prev = x
		if strings.Contains(m.CY, "NaN") || m.Title == "" {
			t.Errorf("bad marker %+v", m)
		}
	}
	if len(tl.Ticks) == 0 {
		t.Fatal("no month ticks")
	}
	prev = -1
	for _, tick := range tl.Ticks {
		x, err := strconv.ParseFloat(tick.X, 64)
		if err != nil {
			t.Fatalf("tick x %q: %v", tick.X, err)
		}
		if x < prev {
			t.Errorf("ticks not monotonic: %v after %v", x, prev)
		}
		prev = x
	}
}

func TestBuildUpcomingEmpty(t *testing.T) {
	u := buildUpcoming(testTranslator(t), nil, time.Now(), NormalizeUpcomingQuery("", "", ""))
	if u.HasAny || u.Available || u.Timeline.Available {
		t.Errorf("empty input should not be available: %+v", u)
	}
}

func TestNormalizeUpcomingQuery(t *testing.T) {
	cases := []struct {
		dirV, rangeV, typeV string
		want                UpcomingQuery
	}{
		{"", "", "", UpcomingQuery{UpcomingForward, "90", "all"}},
		{"", "30", "series", UpcomingQuery{UpcomingForward, "30", "series"}},
		{"past", "all", "movie", UpcomingQuery{UpcomingPast, "all", "movie"}},
		{"sideways", "7", "anything", UpcomingQuery{UpcomingForward, "90", "all"}}, // unsupported input falls back
	}
	for _, c := range cases {
		if got := NormalizeUpcomingQuery(c.dirV, c.rangeV, c.typeV); got != c.want {
			t.Errorf("NormalizeUpcomingQuery(%q, %q, %q) = %+v, want %+v", c.dirV, c.rangeV, c.typeV, got, c.want)
		}
	}
}

func TestBuildUpcomingFilters(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := testTranslator(t)

	// A window drops the undated entry and anything past the cutoff.
	u := buildUpcoming(tr, upcomingFixture(), now, NormalizeUpcomingQuery("", "30", ""))
	if !u.HasAny || !u.Available {
		t.Fatalf("30-day range should still have entries: %+v", u)
	}
	if u.Total != 3 || u.Movies != 0 {
		t.Errorf("30-day range: total=%d movies=%d, want 3/0", u.Total, u.Movies)
	}

	// The type filter narrows both the counters and the timeline.
	u = buildUpcoming(tr, upcomingFixture(), now, NormalizeUpcomingQuery("", "all", "movie"))
	if u.Episodes != 0 || u.Movies != 2 {
		t.Errorf("movie filter: episodes=%d movies=%d, want 0/2", u.Episodes, u.Movies)
	}
	if len(u.Timeline.Markers) != 1 {
		t.Errorf("movie filter: %d markers, want 1 (the undated part is off-axis)", len(u.Timeline.Markers))
	}

	// An empty selection keeps the controls so the range can be widened again.
	u = buildUpcoming(tr, upcomingFixture(), now, NormalizeUpcomingQuery("", "30", "movie"))
	if !u.HasAny || u.Available {
		t.Errorf("empty selection: HasAny=%v Available=%v, want true/false", u.HasAny, u.Available)
	}
	if len(u.Ranges) == 0 || len(u.Types) == 0 {
		t.Error("empty selection should still offer the dropdown options")
	}
}

func TestUpcomingOptionsMarkSelection(t *testing.T) {
	u := buildUpcoming(testTranslator(t), upcomingFixture(), time.Now(), NormalizeUpcomingQuery("", "180", "series"))
	var gotRange, gotType string
	for _, o := range u.Ranges {
		if o.Selected {
			gotRange = o.Value
		}
	}
	for _, o := range u.Types {
		if o.Selected {
			gotType = o.Value
		}
	}
	if gotRange != "180" || gotType != "series" {
		t.Errorf("selected options = %q / %q, want 180 / series", gotRange, gotType)
	}
}

// pastFindings builds gaps whose releases sit at fixed offsets before now, so
// the window boundaries can be asserted precisely.
func pastFindings(now time.Time) []store.Finding {
	day := func(offset int) string {
		return now.AddDate(0, 0, offset).Format("2006-01-02")
	}
	epDetail := func(season int, missing []int, dates map[string]string) string {
		d := map[string]any{
			"seasonNumber":    season,
			"episodeCount":    10,
			"missingEpisodes": missing,
		}
		if dates != nil {
			d["airDates"] = dates
		}
		b, _ := json.Marshal(d)
		return string(b)
	}
	collDetail, _ := json.Marshal(map[string]any{
		"collectionId":   500,
		"collectionName": "X Collection",
		"missingParts": []map[string]any{
			{"tmdbId": 202, "title": "Movie X 3", "releaseDate": day(-10)},
			{"tmdbId": 203, "title": "Movie X 4"}, // undated, must be skipped
		},
	})
	return []store.Finding{
		{
			Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries, Title: "Show A", TMDBID: 100,
			Details: epDetail(2, []int{1, 2, 3}, map[string]string{
				"1": day(0),   // today counts as released
				"2": day(-3),  // inside every window
				"3": day(-45), // outside a 30-day window
			}),
		},
		{
			Kind: store.KindMissingCollection, MediaType: store.MediaMovie, Title: "X Collection", TMDBID: 500,
			Details: string(collDetail),
		},
		{
			// Predates the feature: no airDates at all.
			Kind: store.KindMissingSeason, MediaType: store.MediaSeries, Title: "Show B", TMDBID: 101,
			Details: epDetail(1, []int{1, 2}, nil),
		},
	}
}

func TestPastItemsFromFindings(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	items, undated := pastItemsFromFindings(pastFindings(now))

	if len(items) != 4 {
		t.Fatalf("got %d dated items, want 4: %+v", len(items), items)
	}
	// Two episodes of Show B plus the undated collection part.
	if undated != 3 {
		t.Errorf("undated = %d, want 3", undated)
	}
	for _, it := range items {
		if it.ReleaseDate == "" {
			t.Errorf("undated entry leaked through: %+v", it)
		}
	}
}

func TestBuildPastWindowAndOrder(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := testTranslator(t)
	items, _ := pastItemsFromFindings(pastFindings(now))

	// A 30-day window keeps today, -3 and -10 but drops the -45 episode.
	u := buildUpcoming(tr, items, now, NormalizeUpcomingQuery(UpcomingPast, "30", ""))
	if !u.Past {
		t.Fatal("query direction not reflected in the section")
	}
	if u.Total != 3 || u.Episodes != 2 || u.Movies != 1 {
		t.Fatalf("30-day window: total=%d episodes=%d movies=%d, want 3/2/1", u.Total, u.Episodes, u.Movies)
	}

	// The unlimited window also includes the -45 episode.
	u = buildUpcoming(tr, items, now, NormalizeUpcomingQuery(UpcomingPast, "all", ""))
	if u.Episodes != 3 {
		t.Fatalf("unlimited window: episodes=%d, want 3", u.Episodes)
	}

	// The type filter applies to the retrospective too.
	u = buildUpcoming(tr, items, now, NormalizeUpcomingQuery(UpcomingPast, "all", store.MediaSeries))
	if u.Movies != 0 {
		t.Errorf("series filter kept %d movies", u.Movies)
	}
}

func TestBuildPastSortsNewestFirst(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	items := []store.UpcomingItem{
		{Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie, Title: "older", SourceTitle: "C", TMDBID: 1, ReleaseDate: now.AddDate(0, 0, -30).Format("2006-01-02")},
		{Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie, Title: "newer", SourceTitle: "C", TMDBID: 2, ReleaseDate: now.AddDate(0, 0, -2).Format("2006-01-02")},
	}
	u := buildUpcoming(testTranslator(t), items, now, NormalizeUpcomingQuery(UpcomingPast, "all", ""))
	if len(u.Groups) == 0 || len(u.Groups[0].Items) == 0 {
		t.Fatal("no groups built")
	}
	if got := u.Groups[0].Items[0].Title; got != "newer" {
		t.Fatalf("first tile = %q, want the most recent release", got)
	}
}

func TestBuildPastExcludesTheFuture(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	items := []store.UpcomingItem{
		{Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie, Title: "future", SourceTitle: "C", TMDBID: 1, ReleaseDate: now.AddDate(0, 0, 5).Format("2006-01-02")},
	}
	u := buildUpcoming(testTranslator(t), items, now, NormalizeUpcomingQuery(UpcomingPast, "all", ""))
	if u.Available || u.Total != 0 {
		t.Fatalf("a future release must not appear in the retrospective: %+v", u)
	}
}

func TestBuildUpcomingSectionNeedsRescan(t *testing.T) {
	now := time.Now()
	run := &store.ScanRun{ID: 1}
	tr := testTranslator(t)

	// Gaps exist but none carry a date: that is a missing-data state, not
	// "nothing missed".
	undatedOnly := []store.Finding{{
		Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries, Title: "Show B",
		Details: `{"seasonNumber":1,"episodeCount":10,"missingEpisodes":[1,2]}`,
	}}
	u := BuildUpcomingSection(tr, run, undatedOnly, NormalizeUpcomingQuery(UpcomingPast, "all", ""))
	if !u.NeedsRescan {
		t.Fatal("undated gaps should ask for a rescan")
	}

	// No gaps at all genuinely means nothing was missed.
	u = BuildUpcomingSection(tr, run, nil, NormalizeUpcomingQuery(UpcomingPast, "all", ""))
	if u.NeedsRescan {
		t.Fatal("without any gaps there is nothing to rescan for")
	}
	if u.HasAny {
		t.Fatal("no gaps means no entries")
	}

	// Dated gaps produce entries and no rescan hint.
	u = BuildUpcomingSection(tr, run, pastFindings(now), NormalizeUpcomingQuery(UpcomingPast, "all", ""))
	if u.NeedsRescan || !u.HasAny {
		t.Fatalf("dated gaps should populate the view: %+v", u)
	}
}

// The forward direction must keep working exactly as before.
func TestBuildUpcomingSectionForwardUnaffected(t *testing.T) {
	run := &store.ScanRun{ID: 1, Upcoming: upcomingFixture()}
	u := BuildUpcomingSection(testTranslator(t), run, pastFindings(time.Now()), NormalizeUpcomingQuery("", "all", ""))
	if u.Past || u.NeedsRescan {
		t.Fatalf("forward direction must ignore findings: %+v", u)
	}
	if !u.HasAny || u.Total == 0 {
		t.Fatal("forward direction lost its entries")
	}
}
