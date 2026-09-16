package web

import (
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
)

func emptyGaps(t *i18n.Translator, unconfirmed bool) string {
	if unconfirmed {
		return t.T("sources.incomplete")
	}
	return t.T("stats.noGaps")
}

type SourcesData struct {
	AddDraft    config.Source
	EditDraftID string
	Layout      Layout
	Sources     []config.Source
	Message     string
	Failed      bool
	Warnings    []string
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
}
