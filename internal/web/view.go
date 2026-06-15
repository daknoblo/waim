// Package web contains the HTTP presentation layer: templ templates, static
// assets and view helpers that turn domain types into localised, display-ready
// values.
package web

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

// FindingRow is a display-ready representation of a store.Finding.
type FindingRow struct {
	KindLabel string
	Title     string
	Library   string
	Detail    string
	TMDBLink  string
}

type detailPayload struct {
	SeasonNumber    int   `json:"seasonNumber"`
	EpisodeCount    int   `json:"episodeCount"`
	MissingEpisodes []int `json:"missingEpisodes"`
	MissingParts    []struct {
		TMDBID int64  `json:"tmdbId"`
		Title  string `json:"title"`
		Year   string `json:"year"`
	} `json:"missingParts"`
}

// BuildFindingRows converts findings into localised rows for the UI.
func BuildFindingRows(t *i18n.Translator, findings []store.Finding) []FindingRow {
	rows := make([]FindingRow, 0, len(findings))
	for _, f := range findings {
		var d detailPayload
		if f.Details != "" {
			_ = json.Unmarshal([]byte(f.Details), &d)
		}
		row := FindingRow{
			KindLabel: t.T("finding.kind." + f.Kind),
			Title:     f.Title,
			Library:   f.LibraryName,
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
			if names := partNames(d); names != "" {
				row.Detail += " — " + names
			}
			row.TMDBLink = tmdbLink("collection", f.TMDBID)
		}
		rows = append(rows, row)
	}
	return rows
}

func partNames(d detailPayload) string {
	parts := make([]string, 0, len(d.MissingParts))
	for _, p := range d.MissingParts {
		if p.Year != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", p.Title, p.Year))
		} else {
			parts = append(parts, p.Title)
		}
	}
	return strings.Join(parts, ", ")
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
