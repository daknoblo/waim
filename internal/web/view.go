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
	"github.com/daknoblo/waim/internal/media"
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

// DetailItem describes one missing collection part or one season with gaps.
type DetailItem struct {
	FindingMetadata
	Text     string
	CopyText string
	sortKey  int
}

type FindingMetadata struct {
	TMDBLink, IMDbURL      string
	TMDBRating, IMDbRating string
}

type FindingSources struct {
	TMDB, IMDb bool
}

func (s FindingSources) metadata(tmdbURL, imdbURL string, tmdbRating, imdbRating float64) FindingMetadata {
	var m FindingMetadata
	if s.TMDB {
		m.TMDBLink = tmdbURL
		if tmdbRating > 0 {
			m.TMDBRating = fmt.Sprintf("%.1f", tmdbRating)
		}
	}
	if s.IMDb {
		m.IMDbURL = imdbURL
		if imdbRating > 0 {
			m.IMDbRating = fmt.Sprintf("%.1f", imdbRating)
		}
	}
	return m
}

// FindingRow is a display-ready representation of a store.Finding.
type FindingRow struct {
	FindingMetadata
	LibraryReferences []media.Reference
	LibraryIDs        []string
	KindLabel         string
	MediaIcon         string
	Title             string
	Library           string
	LibraryID         string
	LibraryColor      []string
	Detail            string
	DetailItems       []DetailItem
	MissingCount      int
	PosterURL         string
	JellyfinLink      string
	CollectionLink    string
}

type detailPayload struct {
	Rating          float64           `json:"rating"`
	IMDbRating      float64           `json:"imdbRating"`
	ReleaseDate     string            `json:"releaseDate"`
	SeasonNumber    int               `json:"seasonNumber"`
	EpisodeCount    int               `json:"episodeCount"`
	MissingEpisodes []int             `json:"missingEpisodes"`
	PosterPath      string            `json:"posterPath"`
	IMDbID          string            `json:"imdbId"`
	AirDates        map[string]string `json:"airDates"`
	MissingParts    []struct {
		TMDBID      int64   `json:"tmdbId"`
		Title       string  `json:"title"`
		Year        string  `json:"year"`
		Rating      float64 `json:"rating"`
		IMDbRating  float64 `json:"imdbRating"`
		IMDbID      string  `json:"imdbId"`
		ReleaseDate string  `json:"releaseDate"`
	} `json:"missingParts"`
}

// BuildFindingRows converts findings into localised rows for the UI. Series
// findings are grouped so each series appears once with its season gaps listed.
func BuildFindingRows(t *i18n.Translator, findings []store.Finding, sources FindingSources, stats []store.MediaStat) []FindingRow {
	var rows []FindingRow
	titles := map[string]*store.MediaStat{}
	for i := range stats {
		if stats[i].TMDBID > 0 {
			titles[media.Key(stats[i].Type, stats[i].TMDBID)] = &stats[i]
		}
	}
	groups := map[string]*FindingRow{}
	var groupOrder []string

	for _, f := range findings {
		f.LibraryName = LibraryDisplayName(t, f.LibraryID, f.LibraryName)
		var d detailPayload
		if f.Details != "" {
			_ = json.Unmarshal([]byte(f.Details), &d)
		}

		if !sources.TMDB {
			d.PosterPath = ""
		}
		stat := titles[media.Key(f.MediaType, f.TMDBID)]
		collection := ""
		if sources.TMDB && stat != nil && stat.CollectionID > 0 {
			collection = tmdbLink("collection", stat.CollectionID)
		}
		switch f.Kind {
		case store.KindMissingMovie:
			rows = append(rows, FindingRow{LibraryReferences: findingReferences(f), LibraryIDs: findingLibraries(f), KindLabel: t.T("finding.kind.missing_movie"), MediaIcon: mediaIcon(store.MediaMovie), Title: f.Title, Library: f.LibraryName, LibraryID: f.LibraryID, LibraryColor: LibraryColor(f.LibraryID), Detail: t.T("finding.missingMovie"), MissingCount: 1, PosterURL: posterURL(d.PosterPath), FindingMetadata: sources.metadata(tmdbLink("movie", f.TMDBID), imdbURL(d.IMDbID), d.Rating, d.IMDbRating), CollectionLink: collection, JellyfinLink: findingLink(f, "")})
		case store.KindMissingCollection:
			row := FindingRow{
				LibraryReferences: findingReferences(f),
				LibraryIDs:        findingLibraries(f),
				KindLabel:         t.T("finding.kind." + f.Kind),
				MediaIcon:         mediaIcon(f.MediaType),
				Title:             f.Title,
				Library:           f.LibraryName,
				LibraryID:         f.LibraryID,
				LibraryColor:      LibraryColor(f.LibraryID),
				PosterURL:         posterURL(d.PosterPath),
				Detail:            relUnit(t, len(d.MissingParts), "finding.missingCollection.one", "finding.missingCollection.other"),
				MissingCount:      len(d.MissingParts),
				FindingMetadata:   sources.metadata(tmdbLink("collection", f.TMDBID), "", 0, 0),
			}
			sort.SliceStable(d.MissingParts, func(i, j int) bool {
				return yearValue(d.MissingParts[i].Year) < yearValue(d.MissingParts[j].Year)
			})
			for _, p := range d.MissingParts {
				text := p.Title
				if p.Year != "" {
					text += " (" + p.Year + ")"
				}
				item := DetailItem{Text: text, FindingMetadata: sources.metadata(tmdbLink("movie", p.TMDBID), imdbURL(p.IMDbID), p.Rating, p.IMDbRating)}
				item.CopyText = p.Title
				if p.Year != "" {
					item.CopyText += " " + p.Year
				}
				row.DetailItems = append(row.DetailItems, item)
			}
			rows = append(rows, row)

		case store.KindMissingSeason, store.KindMissingEpisodes:
			key := seriesKey(f)
			g := groups[key]
			if g == nil {
				g = &FindingRow{
					LibraryIDs:      findingLibraries(f),
					KindLabel:       t.T("finding.kind.series"),
					MediaIcon:       mediaIcon(store.MediaSeries),
					Title:           f.Title,
					Library:         f.LibraryName,
					LibraryID:       f.LibraryID,
					LibraryColor:    LibraryColor(f.LibraryID),
					FindingMetadata: sources.metadata(tmdbLink("tv", f.TMDBID), imdbURL(d.IMDbID), 0, 0),
					JellyfinLink:    findingLink(f, ""),
					CollectionLink:  collection,
				}
				groups[key] = g
				groupOrder = append(groupOrder, key)
			}
			if g.PosterURL == "" {
				g.PosterURL = posterURL(d.PosterPath)
			}
			g.LibraryReferences = append(g.LibraryReferences, findingReferences(f)...)
			g.LibraryIDs = append(g.LibraryIDs, findingLibraries(f)...)
			var text string
			if n := len(d.MissingEpisodes); n > 0 && d.EpisodeCount >= n {
				text = t.T("finding.missingOfTotal", d.SeasonNumber, n, d.EpisodeCount)
			} else if f.Kind == store.KindMissingSeason {
				text = t.T("finding.missingSeason", d.SeasonNumber, len(d.MissingEpisodes))
			} else {
				text = t.T("finding.missingEpisodes", d.SeasonNumber, len(d.MissingEpisodes))
			}
			line := DetailItem{
				Text:     text,
				sortKey:  d.SeasonNumber,
				CopyText: fmt.Sprintf("%s S%02d", g.Title, d.SeasonNumber),
			}
			if base := g.TMDBLink; base != "" && d.SeasonNumber >= 0 {
				line.TMDBLink = fmt.Sprintf("%s/season/%d", base, d.SeasonNumber)
			}
			if base := g.IMDbURL; base != "" && d.SeasonNumber >= 0 {
				line.IMDbURL = fmt.Sprintf("%sepisodes/?season=%d", base, d.SeasonNumber)
			}
			if stat != nil {
				g.FindingMetadata = sources.metadata(tmdbLink("tv", f.TMDBID), imdbURL(d.IMDbID), stat.Rating, stat.IMDbRating)
				for _, season := range stat.Seasons {
					if season.Number != d.SeasonNumber {
						continue
					}
					line.FindingMetadata = sources.metadata(line.TMDBLink, line.IMDbURL, season.Rating, season.IMDbRating)
					break
				}
			}
			g.DetailItems = append(g.DetailItems, line)
			g.MissingCount += len(d.MissingEpisodes)
		}
	}

	for _, key := range groupOrder {
		g := groups[key]
		sort.SliceStable(g.DetailItems, func(i, j int) bool {
			return g.DetailItems[i].sortKey < g.DetailItems[j].sortKey
		})
		g.Detail = t.T("finding.seriesGaps", len(g.DetailItems))
		rows = append(rows, *g)
	}
	return rows
}

func findingLibraries(f store.Finding) []string {
	out := []string{f.LibraryID}
	for _, ref := range findingReferences(f) {
		out = append(out, ref.LibraryID, ref.ID)
	}

	return out
}

func findingReferences(f store.Finding) []media.Reference {
	refs := append([]media.Reference(nil), f.References...)
	return append(refs, f.ContextReferences...)
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
