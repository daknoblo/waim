package web

import (
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"net/url"
	"strconv"
)

func emptyGaps(t *i18n.Translator, unconfirmed bool) string {
	if unconfirmed {
		return t.T("sources.incomplete")
	}
	return t.T("stats.noGaps")
}

type SourcesData struct {
	AddDraft           config.Source
	EditDraftID        string
	Layout             Layout
	Sources            []config.Source
	Message            string
	Failed             bool
	OpenID             string
	AddOpen            bool
	DefaultScanMinutes int
	IntervalDraft      string
	HasIntervalDraft   bool
	NextRuns           map[string]string
}

func (d SourcesData) OrderedSources() []config.Source {
	out := make([]config.Source, 0, len(d.Sources))
	for _, src := range d.Sources {
		if src.Type == media.Virtual {
			out = append(out, src)
		}
	}
	for _, src := range d.Sources {
		if src.Type != media.Virtual {
			out = append(out, src)
		}
	}
	return out
}

func SourceSettingsURL(id string) string {
	return SettingsURL("media") + "&source=" + url.QueryEscape(id)
}

func SelectedLibraryCount(src config.Source) int {
	count := 0
	for _, library := range src.Libraries {
		if library.Enabled {
			count++
		}

	}
	return count
}

func (d SourcesData) IntervalValue(src config.Source) string {
	if d.HasIntervalDraft && (d.EditDraftID == src.ID || (d.AddOpen && src.ID == d.AddDraft.ID)) {
		return d.IntervalDraft
	}
	return strconv.Itoa(src.ScanInterval(d.DefaultScanMinutes))
}

type CollectionData struct {
	Layout     Layout
	Entries    []store.VirtualEntry
	Results    []store.VirtualEntry
	Query      string
	Kind       string
	Message    string
	Searched   bool
	Configured bool
	Updating   bool
	Ownership  map[string]string
}

const (
	CollectionComplete   = "complete"
	CollectionPartial    = "partial"
	CollectionUnverified = "unverified"
)
