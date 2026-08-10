package web

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

// chartColor pairs the SVG fill utility of a series colour with the matching
// background utility used for legend swatches.
type chartColor struct {
	Fill   string
	Swatch string
}

// chartPalette is cycled through by the donut and flow charts. The class names
// are spelled out so Tailwind keeps them during purging.
var chartPalette = []chartColor{
	{"fill-indigo-500", "bg-indigo-500"},
	{"fill-sky-500", "bg-sky-500"},
	{"fill-emerald-500", "bg-emerald-500"},
	{"fill-amber-500", "bg-amber-500"},
	{"fill-rose-500", "bg-rose-500"},
	{"fill-violet-500", "bg-violet-500"},
	{"fill-cyan-500", "bg-cyan-500"},
	{"fill-lime-500", "bg-lime-500"},
	{"fill-orange-500", "bg-orange-500"},
	{"fill-fuchsia-500", "bg-fuchsia-500"},
	{"fill-teal-500", "bg-teal-500"},
	{"fill-pink-500", "bg-pink-500"},
}

// PieSlice is one segment of a donut chart, pre-computed as an SVG path so the
// markup needs no inline styles (blocked by the CSP) and no client-side maths.
type PieSlice struct {
	Path   string
	Fill   string
	Swatch string
	Label  string
	Count  int
	Share  int
}

// Donut geometry inside the 0 0 100 100 viewBox of the pie chart.
const (
	pieCenter = 50.0
	pieOuter  = 46.0
	pieInner  = 27.0
)

// pieSlices turns a bar distribution into donut segments.
func pieSlices(bars []StatsBar) []PieSlice {
	total := 0
	for _, b := range bars {
		total += b.Count
	}
	if total == 0 {
		return nil
	}
	out := make([]PieSlice, 0, len(bars))
	angle := -90.0
	for i, b := range bars {
		sweep := float64(b.Count) / float64(total) * 360
		// A full circle would collapse into a zero-length path.
		if sweep > 359.99 {
			sweep = 359.99
		}
		c := chartPalette[i%len(chartPalette)]
		out = append(out, PieSlice{
			Path:   donutPath(angle, angle+sweep),
			Fill:   c.Fill,
			Swatch: c.Swatch,
			Label:  b.Label,
			Count:  b.Count,
			Share:  b.Share,
		})
		angle += sweep
	}
	return out
}

func donutPath(from, to float64) string {
	x0o, y0o := polar(pieOuter, from)
	x1o, y1o := polar(pieOuter, to)
	x0i, y0i := polar(pieInner, from)
	x1i, y1i := polar(pieInner, to)
	large := "0"
	if to-from > 180 {
		large = "1"
	}
	return fmt.Sprintf("M %s %s A %s %s 0 %s 1 %s %s L %s %s A %s %s 0 %s 0 %s %s Z",
		num(x0o), num(y0o), num(pieOuter), num(pieOuter), large, num(x1o), num(y1o),
		num(x1i), num(y1i), num(pieInner), num(pieInner), large, num(x0i), num(y0i))
}

func polar(r, deg float64) (float64, float64) {
	rad := deg * math.Pi / 180
	return pieCenter + r*math.Cos(rad), pieCenter + r*math.Sin(rad)
}

func num(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// SeriesOption identifiers are the plain series titles, so the flow partial can
// be requested with a readable query.
//
// SeriesFlow is a pre-computed sankey diagram: every season of a series feeds
// its episodes into one node representing the complete show.
type SeriesFlow struct {
	Available bool
	Title     string
	Library   string
	Color     []string
	Height    int
	Seasons   []FlowNode
	Links     []FlowLink
	Total     FlowNode
	Summary   string
}

// FlowNode is a rectangle of the flow chart with its pre-placed label.
type FlowNode struct {
	X, Y, W, H   string
	Fill         string
	Label        string
	Sub          string
	LabelX       string
	LabelY, SubY string
	Anchor       string
}

// FlowLink is a ribbon connecting a season to the series total.
type FlowLink struct {
	Path string
	Fill string
}

// Flow chart geometry inside the 0 0 720 H viewBox.
const (
	flowWidth     = 720.0
	flowNodeW     = 14.0
	flowLeftX     = 172.0
	flowRightX    = 534.0
	flowPadding   = 10.0
	flowGap       = 8.0
	flowMinHeight = 150.0
	flowMaxHeight = 520.0
)

// buildSeriesFlowData returns the dropdown options for every series with season
// data plus the flow chart of the selected series (the first one by default).
func buildSeriesFlowData(t *i18n.Translator, series []store.MediaStat, selected string) ([]SeriesOption, SeriesFlow) {
	withSeasons := make([]store.MediaStat, 0, len(series))
	for _, s := range series {
		if len(s.Seasons) > 0 {
			withSeasons = append(withSeasons, s)
		}
	}
	if len(withSeasons) == 0 {
		return nil, SeriesFlow{}
	}
	sort.SliceStable(withSeasons, func(i, j int) bool {
		return strings.ToLower(withSeasons[i].Title) < strings.ToLower(withSeasons[j].Title)
	})

	chosen := withSeasons[0]
	for _, s := range withSeasons {
		if s.Title == selected {
			chosen = s
			break
		}
	}
	options := make([]SeriesOption, 0, len(withSeasons))
	for _, s := range withSeasons {
		options = append(options, SeriesOption{
			Title:    s.Title,
			Label:    fmt.Sprintf("%s (%s)", s.Title, t.T("stats.seasonsEpisodes", len(s.Seasons), s.Episodes)),
			Selected: s.Title == chosen.Title,
		})
	}
	return options, buildSeriesFlow(t, chosen)
}

// buildSeriesFlow lays out the sankey: season bars on the left, sized by their
// episode count, and one full-height node for the whole series on the right.
func buildSeriesFlow(t *i18n.Translator, s store.MediaStat) SeriesFlow {
	seasons := s.Seasons
	total := 0
	for _, sn := range seasons {
		total += sn.Episodes
	}
	if total == 0 {
		return SeriesFlow{}
	}

	height := float64(len(seasons)) * 34
	height = math.Max(flowMinHeight, math.Min(flowMaxHeight, height))
	inner := height - 2*flowPadding
	// Season bars are separated by gaps; the ribbons on the right side are not.
	barSpace := inner - float64(len(seasons)-1)*flowGap
	if barSpace < float64(len(seasons))*4 {
		barSpace = float64(len(seasons)) * 4
		height = barSpace + float64(len(seasons)-1)*flowGap + 2*flowPadding
		inner = height - 2*flowPadding
	}

	flow := SeriesFlow{
		Available: true,
		Title:     s.Title,
		Library:   s.LibraryName,
		Color:     LibraryColor(s.LibraryID),
		Height:    int(math.Ceil(height)),
		Summary:   t.T("stats.seasonsEpisodes", len(seasons), total),
	}

	y := flowPadding
	ty := flowPadding
	for i, sn := range seasons {
		share := float64(sn.Episodes) / float64(total)
		h := math.Max(4, share*barSpace)
		th := share * inner
		c := chartPalette[i%len(chartPalette)]

		flow.Seasons = append(flow.Seasons, FlowNode{
			X: num(flowLeftX), Y: num(y), W: num(flowNodeW), H: num(h),
			Fill:   c.Fill,
			Label:  t.T("stats.seasonEpisodes", sn.Number, sn.Episodes),
			LabelX: num(flowLeftX - 10),
			LabelY: num(y + h/2),
			Anchor: "end",
		})
		flow.Links = append(flow.Links, FlowLink{
			Path: ribbon(y, y+h, ty, ty+th),
			Fill: c.Fill,
		})
		y += h + flowGap
		ty += th
	}

	flow.Total = FlowNode{
		X: num(flowRightX), Y: num(flowPadding), W: num(flowNodeW), H: num(inner),
		Fill:   "fill-slate-500",
		Label:  s.Title,
		Sub:    flow.Summary,
		LabelX: num(flowRightX + flowNodeW + 10),
		LabelY: num(flowPadding + inner/2 - 4),
		SubY:   num(flowPadding + inner/2 + 12),
		Anchor: "start",
	}
	return flow
}

// ribbon draws a season-to-total band as a closed bezier shape.
func ribbon(ay0, ay1, by0, by1 float64) string {
	x0 := flowLeftX + flowNodeW
	x1 := flowRightX
	mx := (x0 + x1) / 2
	return fmt.Sprintf("M %s %s C %s %s %s %s %s %s L %s %s C %s %s %s %s %s %s Z",
		num(x0), num(ay0), num(mx), num(ay0), num(mx), num(by0), num(x1), num(by0),
		num(x1), num(by1), num(mx), num(by1), num(mx), num(ay1), num(x0), num(ay1))
}

// BuildSeriesFlow builds the flow chart of a single series from a scan run, used
// by the dropdown-driven partial.
func BuildSeriesFlow(t *i18n.Translator, run *store.ScanRun, title string) SeriesFlow {
	if run == nil {
		return SeriesFlow{}
	}
	var series []store.MediaStat
	for _, m := range run.Media {
		if m.Type == store.MediaSeries {
			series = append(series, m)
		}
	}
	_, flow := buildSeriesFlowData(t, series, title)
	return flow
}
