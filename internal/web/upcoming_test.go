package web

import (
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
	u := buildUpcoming(testTranslator(t), upcomingFixture(), now)

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
	tl := buildUpcoming(testTranslator(t), upcomingFixture(), now).Timeline

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
	u := buildUpcoming(testTranslator(t), nil, time.Now())
	if u.Available || u.Timeline.Available {
		t.Errorf("empty input should not be available: %+v", u)
	}
}
