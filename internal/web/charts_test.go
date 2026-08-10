package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

func testTranslator(t *testing.T) *i18n.Translator {
	t.Helper()
	c, err := i18n.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return c.For("en")
}

func TestPieSlicesShares(t *testing.T) {
	bars := topBars(map[string]int{"Drama": 30, "Comedy": 10}, 0, sortByCountDesc)
	slices := pieSlices(bars)
	if len(slices) != 2 {
		t.Fatalf("want 2 slices, got %d", len(slices))
	}
	if slices[0].Share != 75 || slices[1].Share != 25 {
		t.Errorf("unexpected shares: %d / %d", slices[0].Share, slices[1].Share)
	}
	for _, s := range slices {
		if !strings.HasPrefix(s.Path, "M ") || !strings.HasSuffix(s.Path, "Z") {
			t.Errorf("malformed donut path: %q", s.Path)
		}
	}
}

func TestPieSlicesSingleValueStaysVisible(t *testing.T) {
	slices := pieSlices(topBars(map[string]int{"Drama": 5}, 0, sortByCountDesc))
	if len(slices) != 1 {
		t.Fatalf("want 1 slice, got %d", len(slices))
	}
	if strings.Contains(slices[0].Path, "NaN") {
		t.Errorf("degenerate path: %q", slices[0].Path)
	}
}

func TestBuildSeriesFlow(t *testing.T) {
	tr := testTranslator(t)
	series := []store.MediaStat{{
		Type:     store.MediaSeries,
		Title:    "Example Show",
		Episodes: 30,
		Seasons: []store.SeasonStat{
			{Number: 1, Episodes: 10, Total: 10},
			{Number: 2, Episodes: 20, Total: 20},
		},
	}}
	opts, flow := buildSeriesFlowData(tr, series, "")
	if len(opts) != 1 || !opts[0].Selected {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if !flow.Available || len(flow.Seasons) != 2 || len(flow.Links) != 2 {
		t.Fatalf("unexpected flow: %+v", flow)
	}
	if flow.Total.Label != "Example Show" {
		t.Errorf("unexpected total label: %q", flow.Total.Label)
	}
	first, err := strconv.ParseFloat(flow.Seasons[0].H, 64)
	if err != nil {
		t.Fatalf("parse height: %v", err)
	}
	second, err := strconv.ParseFloat(flow.Seasons[1].H, 64)
	if err != nil {
		t.Fatalf("parse height: %v", err)
	}
	if first*2 > second+0.01 || first*2 < second-0.01 {
		t.Errorf("season bars are not proportional: %v vs %v", first, second)
	}
}

func TestFormatWatchTime(t *testing.T) {
	cases := map[int]string{0: "", 45: "45m", 150: "2h 30m", 3000: "2d 2h"}
	for in, want := range cases {
		if got := formatWatchTime(in); got != want {
			t.Errorf("formatWatchTime(%d) = %q, want %q", in, got, want)
		}
	}
}
