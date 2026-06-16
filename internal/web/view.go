// Package web contains the HTTP presentation layer: templ templates, static
// assets and view helpers that turn domain types into localised, display-ready
// values.
package web

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
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
			row.TMDBLink = tmdbLink("tv", f.TMDBID)
		case store.KindMissingEpisodes:
			row.Detail = t.T("finding.missingEpisodes", d.SeasonNumber, len(d.MissingEpisodes))
			row.TMDBLink = tmdbLink("tv", f.TMDBID)
		case store.KindMissingCollection:
			row.Detail = t.T("finding.missingCollection", len(d.MissingParts))
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
