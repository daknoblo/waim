// Package web contains the HTTP presentation layer: templ templates, static
// assets and view helpers that turn domain types into localised, display-ready
// values.
package web

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

// Finding sort keys and directions.
const (
	SortLibrary = "library"
	SortTitle   = "title"
	SortType    = "type"
	SortMissing = "missing"
	DirAsc      = "asc"
	DirDesc     = "desc"
)

// DetailItem is a single missing part/episode line, with an optional rating.
type DetailItem struct {
	Text     string
	Rating   string
	CopyText string
	IMDbURL  string
	sortKey  int
}

// FindingRow is a display-ready representation of a store.Finding.
type FindingRow struct {
	KindLabel    string
	MediaIcon    string
	Title        string
	Library      string
	LibraryID    string
	LibraryColor []string
	Detail       string
	DetailItems  []DetailItem
	MissingCount int
	PosterURL    string
	TMDBLink     string
	JellyfinLink string
	Kinds        string // space-separated gap kinds present in this row
}

type detailPayload struct {
	SeasonNumber    int    `json:"seasonNumber"`
	EpisodeCount    int    `json:"episodeCount"`
	MissingEpisodes []int  `json:"missingEpisodes"`
	PosterPath      string `json:"posterPath"`
	IMDbID          string `json:"imdbId"`
	MissingParts    []struct {
		TMDBID int64   `json:"tmdbId"`
		Title  string  `json:"title"`
		Year   string  `json:"year"`
		Rating float64 `json:"rating"`
		IMDbID string  `json:"imdbId"`
	} `json:"missingParts"`
}

// BuildFindingRows converts findings into localised rows for the UI. Series
// findings are grouped so each series appears once with its season gaps listed.
// jellyfinURL is used to build deep links to the originating Jellyfin item.
func BuildFindingRows(t *i18n.Translator, findings []store.Finding, jellyfinURL string) []FindingRow {
	var rows []FindingRow
	groups := map[string]*FindingRow{}
	groupKinds := map[string]map[string]bool{}
	var groupOrder []string

	for _, f := range findings {
		var d detailPayload
		if f.Details != "" {
			_ = json.Unmarshal([]byte(f.Details), &d)
		}

		switch f.Kind {
		case store.KindMissingCollection:
			row := FindingRow{
				KindLabel:    t.T("finding.kind." + f.Kind),
				MediaIcon:    mediaIcon(f.MediaType),
				Title:        f.Title,
				Library:      f.LibraryName,
				LibraryID:    f.LibraryID,
				LibraryColor: LibraryColor(f.LibraryID),
				JellyfinLink: jellyfinItemURL(jellyfinURL, f.JellyfinID),
				PosterURL:    posterURL(d.PosterPath),
				Detail:       pluralT(t, len(d.MissingParts), "finding.missingCollection.one", "finding.missingCollection.other", len(d.MissingParts)),
				MissingCount: len(d.MissingParts),
				TMDBLink:     tmdbLink("collection", f.TMDBID),
				Kinds:        store.KindMissingCollection,
			}
			sort.SliceStable(d.MissingParts, func(i, j int) bool {
				return yearValue(d.MissingParts[i].Year) < yearValue(d.MissingParts[j].Year)
			})
			for _, p := range d.MissingParts {
				text := p.Title
				if p.Year != "" {
					text += " (" + p.Year + ")"
				}
				item := DetailItem{Text: text}
				if p.Rating > 0 {
					item.Rating = fmt.Sprintf("%.1f", p.Rating)
				}
				item.CopyText = p.Title
				if p.Year != "" {
					item.CopyText += " " + p.Year
				}
				item.IMDbURL = imdbURL(p.IMDbID)
				row.DetailItems = append(row.DetailItems, item)
			}
			rows = append(rows, row)

		case store.KindMissingSeason, store.KindMissingEpisodes:
			key := seriesKey(f)
			g := groups[key]
			if g == nil {
				g = &FindingRow{
					KindLabel:    t.T("finding.kind.series"),
					MediaIcon:    mediaIcon(store.MediaSeries),
					Title:        f.Title,
					Library:      f.LibraryName,
					LibraryID:    f.LibraryID,
					LibraryColor: LibraryColor(f.LibraryID),
					JellyfinLink: jellyfinItemURL(jellyfinURL, f.JellyfinID),
					TMDBLink:     tmdbLink("tv", f.TMDBID),
				}
				groups[key] = g
				groupOrder = append(groupOrder, key)
			}
			if groupKinds[key] == nil {
				groupKinds[key] = map[string]bool{}
			}
			groupKinds[key][f.Kind] = true
			if g.PosterURL == "" {
				g.PosterURL = posterURL(d.PosterPath)
			}
			var text string
			if f.Kind == store.KindMissingSeason {
				text = pluralT(t, len(d.MissingEpisodes), "finding.missingSeason.one", "finding.missingSeason.other", d.SeasonNumber, len(d.MissingEpisodes))
			} else {
				text = pluralT(t, len(d.MissingEpisodes), "finding.missingEpisodes.one", "finding.missingEpisodes.other", d.SeasonNumber, len(d.MissingEpisodes))
			}
			g.DetailItems = append(g.DetailItems, DetailItem{
				Text:     text,
				sortKey:  d.SeasonNumber,
				CopyText: fmt.Sprintf("%s S%02d", g.Title, d.SeasonNumber),
				IMDbURL:  imdbSeasonURL(d.IMDbID, d.SeasonNumber),
			})
			g.MissingCount += len(d.MissingEpisodes)
		}
	}

	for _, key := range groupOrder {
		g := groups[key]
		sort.SliceStable(g.DetailItems, func(i, j int) bool {
			return g.DetailItems[i].sortKey < g.DetailItems[j].sortKey
		})
		g.Detail = pluralT(t, len(g.DetailItems), "finding.seriesGaps.one", "finding.seriesGaps.other", len(g.DetailItems))
		g.Kinds = joinKinds(groupKinds[key])
		rows = append(rows, *g)
	}
	return rows
}

// joinKinds returns the kinds in set as a sorted, space-separated string for a
// data attribute.
func joinKinds(set map[string]bool) string {
	ks := make([]string, 0, len(set))
	for k := range set {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, " ")
}

func seriesKey(f store.Finding) string {
	if f.JellyfinID != "" {
		return "j:" + f.JellyfinID
	}
	return "t:" + strconv.FormatInt(f.TMDBID, 10) + ":" + f.Title
}

func posterURL(path string) string {
	if path == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w92" + path
}

// SortFindingRows sorts rows in place by the given key and direction.
func SortFindingRows(rows []FindingRow, key, dir string) {
	less := func(a, b FindingRow) bool {
		switch key {
		case SortTitle:
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		case SortType:
			return a.KindLabel < b.KindLabel
		case SortMissing:
			return a.MissingCount < b.MissingCount
		default: // SortLibrary
			return strings.ToLower(a.Library) < strings.ToLower(b.Library)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return less(rows[i], rows[j]) })
	if dir == DirDesc {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
}

// NormalizeSort returns a supported sort key, defaulting to library.
func NormalizeSort(key string) string {
	switch key {
	case SortTitle, SortType, SortMissing, SortLibrary:
		return key
	default:
		return SortLibrary
	}
}

// NormalizeDir returns a supported sort direction, defaulting to ascending.
func NormalizeDir(dir string) string {
	if dir == DirDesc {
		return DirDesc
	}
	return DirAsc
}

func yearValue(y string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(y)); err == nil && n > 0 {
		return n
	}
	return 1<<31 - 1
}

func mediaIcon(mediaType string) string {
	switch mediaType {
	case store.MediaSeries:
		return "\U0001F4FA" // television
	case store.MediaMovie:
		return "\U0001F3AC" // clapper board
	default:
		return ""
	}
}

func jellyfinItemURL(base, itemID string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || itemID == "" {
		return ""
	}
	return base + "/web/#/details?id=" + url.QueryEscape(itemID)
}

func tmdbLink(kind string, id int64) string {
	if id == 0 {
		return ""
	}
	return "https://www.themoviedb.org/" + kind + "/" + strconv.FormatInt(id, 10)
}

func imdbURL(id string) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "tt") {
		return ""
	}
	return "https://www.imdb.com/title/" + id + "/"
}

func imdbSeasonURL(id string, season int) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "tt") {
		return ""
	}
	return fmt.Sprintf("https://www.imdb.com/title/%s/episodes/?season=%d", id, season)
}

// FormatTime renders a time for display, or a localised "never" placeholder.
func FormatTime(t *i18n.Translator, ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return t.T("common.never")
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

// FormatRelative renders a time relative to now, e.g. "5 minutes ago" or
// "in 1 hour". Zero times yield a localised "never" placeholder.
func FormatRelative(t *i18n.Translator, ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return t.T("common.never")
	}
	d := time.Until(*ts)
	future := d >= 0
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		if future {
			return t.T("relative.soon")
		}
		return t.T("relative.now")
	}
	var phrase string
	switch {
	case d < time.Hour:
		phrase = relUnit(t, int(d/time.Minute), "relative.minute", "relative.minutes")
	case d < 24*time.Hour:
		phrase = relUnit(t, int(d/time.Hour), "relative.hour", "relative.hours")
	default:
		phrase = relUnit(t, int(d/(24*time.Hour)), "relative.day", "relative.days")
	}
	if future {
		return t.T("relative.in", phrase)
	}
	return t.T("relative.ago", phrase)
}

func relUnit(t *i18n.Translator, n int, singular, plural string) string {
	if n == 1 {
		return t.T(singular, n)
	}
	return t.T(plural, n)
}

// pluralT selects the singular (n == 1) or plural i18n key and formats it with
// args, so messages read correctly for one vs many.
func pluralT(t *i18n.Translator, n int, one, other string, args ...any) string {
	if n == 1 {
		return t.T(one, args...)
	}
	return t.T(other, args...)
}

// FormatDuration renders a duration like "1h 5m 12s", "2m 35s" or "12s"; empty for zero.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm %ds", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

// FormatTimeValue renders a non-pointer time for display, or empty when zero.
func FormatTimeValue(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

// HumanSize formats a byte count as a human-readable string.
func HumanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
