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
	Text   string
	Rating string
}

// FindingRow is a display-ready representation of a store.Finding.
type FindingRow struct {
	KindLabel    string
	MediaIcon    string
	Title        string
	Library      string
	LibraryColor []string
	Detail       string
	DetailItems  []DetailItem
	MissingCount int
	TMDBLink     string
	JellyfinLink string
}

type detailPayload struct {
	SeasonNumber    int   `json:"seasonNumber"`
	EpisodeCount    int   `json:"episodeCount"`
	MissingEpisodes []int `json:"missingEpisodes"`
	MissingParts    []struct {
		TMDBID int64   `json:"tmdbId"`
		Title  string  `json:"title"`
		Year   string  `json:"year"`
		Rating float64 `json:"rating"`
	} `json:"missingParts"`
}

// BuildFindingRows converts findings into localised rows for the UI. jellyfinURL
// is used to build deep links to the originating Jellyfin item.
func BuildFindingRows(t *i18n.Translator, findings []store.Finding, jellyfinURL string) []FindingRow {
	rows := make([]FindingRow, 0, len(findings))
	for _, f := range findings {
		var d detailPayload
		if f.Details != "" {
			_ = json.Unmarshal([]byte(f.Details), &d)
		}
		row := FindingRow{
			KindLabel:    t.T("finding.kind." + f.Kind),
			MediaIcon:    mediaIcon(f.MediaType),
			Title:        f.Title,
			Library:      f.LibraryName,
			LibraryColor: LibraryColor(f.LibraryID),
			JellyfinLink: jellyfinItemURL(jellyfinURL, f.JellyfinID),
		}
		switch f.Kind {
		case store.KindMissingSeason:
			row.Detail = t.T("finding.missingSeason", d.SeasonNumber, len(d.MissingEpisodes))
			row.MissingCount = len(d.MissingEpisodes)
			row.TMDBLink = tmdbLink("tv", f.TMDBID)
		case store.KindMissingEpisodes:
			row.Detail = t.T("finding.missingEpisodes", d.SeasonNumber, len(d.MissingEpisodes))
			row.MissingCount = len(d.MissingEpisodes)
			row.TMDBLink = tmdbLink("tv", f.TMDBID)
		case store.KindMissingCollection:
			row.Detail = t.T("finding.missingCollection", len(d.MissingParts))
			row.MissingCount = len(d.MissingParts)
			// Sort the missing parts by release year (unknown years last).
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
				row.DetailItems = append(row.DetailItems, item)
			}
			row.TMDBLink = tmdbLink("collection", f.TMDBID)
		}
		rows = append(rows, row)
	}
	return rows
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

// FormatTime renders a time for display, or a localised "never" placeholder.
func FormatTime(t *i18n.Translator, ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return t.T("common.never")
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

// FormatDuration renders a duration like "2m 35s" or "12s", or empty for zero.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

// FormatTimeValue renders a non-pointer time for display, or empty when zero.
func FormatTimeValue(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}
