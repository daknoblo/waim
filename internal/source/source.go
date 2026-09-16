// Package source adapts configured media instances into normalized snapshots.
package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/media"
)

type Adapter interface {
	Libraries(context.Context) ([]media.Library, error)
	Snapshot(context.Context) (media.Snapshot, error)
}

type Tester interface{ Test(context.Context) error }

type Factory func(config.Source) (Adapter, error)

func New(src config.Source) (Adapter, error) {
	if src.Type != media.Jellyfin {
		return nil, fmt.Errorf("unsupported real source type %q", src.Type)
	}
	u, err := url.Parse(src.Jellyfin.URL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("source address must not contain credentials, queries or fragments")
	}
	return &jellyfinAdapter{source: src, client: jellyfin.New(src.Jellyfin.URL, src.Jellyfin.APIKey)}, nil
}

type jellyfinAdapter struct {
	source config.Source
	client *jellyfin.Client
}

func (a *jellyfinAdapter) Test(ctx context.Context) error {
	if _, err := a.client.SystemInfo(ctx); err != nil {
		return err
	}
	_, err := a.client.ResolveUserID(ctx, a.source.Jellyfin.UserID)
	return err
}

func (a *jellyfinAdapter) Libraries(ctx context.Context) ([]media.Library, error) {
	libs, err := a.client.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	out := []media.Library{}
	for _, l := range libs {
		out = append(out, media.Library{ID: l.ID, Name: l.Name, Type: l.CollectionType})
	}
	return out, nil
}

func (a *jellyfinAdapter) Snapshot(ctx context.Context) (media.Snapshot, error) {
	var out media.Snapshot
	run := activity.FromContext(ctx)
	run.Subject(a.source.Name)
	run.Operation(activity.Account)
	user, err := a.client.ResolveUserID(ctx, a.source.Jellyfin.UserID)
	if err != nil {
		return out, err
	}
	for _, lib := range a.source.Libraries {
		if !lib.Enabled {
			continue
		}
		libID := media.Qualify(a.source.ID, lib.ID)
		run.Subject(a.source.Name + " · " + lib.Name)
		run.Current("")
		run.Operation(activity.Libraries)
		out.Libraries = append(out.Libraries, media.Library{ID: libID, Name: a.source.Name + " · " + lib.Name, Type: lib.Type})
		items, err := a.client.ItemsInLibrary(ctx, user, lib.ID)
		if err != nil {
			return media.Snapshot{}, err
		}
		for _, it := range items {
			if it.Type != media.Movie && it.Type != media.Series {
				continue
			}
			if virtualPlaceholder(it) {
				out.Warnings = append(out.Warnings, "Ignored virtual title: "+it.Name)
				run.ReportLegacy("Ignored virtual title: "+it.Name, a.source.Name)
				continue
			}
			n := normalize(it)
			n.ID = media.Qualify(a.source.ID, it.ID)
			n.References = []media.Reference{{ID: a.source.ID, Type: a.source.Type, Name: a.source.Name, LibraryID: libID, LibraryName: lib.Name, ItemID: it.ID, ServerURL: a.source.Jellyfin.URL, URL: strings.TrimRight(a.source.Jellyfin.URL, "/") + "/web/#/details?id=" + url.QueryEscape(it.ID)}}
			if it.Type == media.Series {
				run.Current(it.Name)
				run.Operation(activity.Episodes)
				eps, err := a.client.Episodes(ctx, user, it.ID)
				if err != nil {
					return media.Snapshot{}, err
				}
				normalized, warnings := normalizeEpisodes(eps, it.Name)
				n.Episodes = normalized
				out.Warnings = append(out.Warnings, warnings...)
				for _, warning := range warnings {
					run.ReportLegacy(warning, a.source.Name)
				}
			}
			out.Items = append(out.Items, n)
		}
	}
	return out, nil
}

func normalize(i jellyfin.Item) media.Item {
	return media.Item{ID: i.ID, Name: i.Name, Type: i.Type, ProductionYear: i.ProductionYear, ProviderIDs: i.ProviderIDs, SeriesID: i.SeriesID, SeriesName: i.SeriesName, IndexNumber: i.IndexNumber, ParentIndexNumber: i.ParentIndexNumber}
}
