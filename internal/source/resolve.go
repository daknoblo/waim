package source

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type Resolver interface {
	SearchMovie(context.Context, string, int) ([]tmdb.MovieSearchResult, error)
	SearchTV(context.Context, string, int) ([]tmdb.TVSearchResult, error)
}

// ResolveID only accepts a unique exact title/year match. A name by itself is
// not safe enough to merge two servers' unresolved objects.
func ResolveID(ctx context.Context, item media.Item, td Resolver) int64 {
	if id := item.TMDBID(); id > 0 {
		return id
	}
	if item.ProductionYear <= 0 {
		return 0
	}
	var match int64
	matches := 0
	accept := func(id int64, title, date string) {
		if len(date) < 4 || !strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(item.Name)) {
			return
		}
		year, _ := strconv.Atoi(date[:4])
		if year == item.ProductionYear && id > 0 {
			match = id
			matches++
		}
	}
	if item.Type == media.Movie {
		items, err := td.SearchMovie(ctx, item.Name, item.ProductionYear)
		if err != nil {
			return 0
		}
		for _, found := range items {
			accept(found.ID, found.Title, found.ReleaseDate)
		}
	} else {
		items, err := td.SearchTV(ctx, item.Name, item.ProductionYear)
		if err != nil {
			return 0
		}
		for _, found := range items {
			accept(found.ID, found.Name, found.FirstAirDate)
		}
	}
	if matches != 1 {
		return 0
	}
	return match
}

func resolveCatalog(ctx context.Context, c media.Catalog, td Resolver) (media.Catalog, []media.Item) {
	c.Items = append([]media.Item(nil), c.Items...)
	c.Warnings = append([]string(nil), c.Warnings...)
	var resolved []media.Item
	for i := range c.Items {
		if c.Items[i].TMDBID() > 0 {
			continue
		}
		id := ResolveID(ctx, c.Items[i], td)
		if id <= 0 {
			c.Warnings = append(c.Warnings, "Unresolved title: "+c.Items[i].Name)
			continue
		}
		c.Items[i] = c.Items[i].WithResolvedID(id)
		resolved = append(resolved, c.Items[i])
	}
	c.Items = media.Merge(c.Items)
	return c, resolved
}

// ResolveSavedCatalog is the shared scan/recommendation identity boundary.
// Live views subsequently reuse these aliases through Catalog without TMDB I/O.
func ResolveSavedCatalog(ctx context.Context, st *store.Store, settings config.Settings, c media.Catalog, td Resolver) (media.Catalog, error) {
	c, resolved := resolveCatalog(ctx, c, td)
	if err := ctx.Err(); err != nil {
		return c, err
	}
	bySource := map[string][]media.Item{}
	for _, item := range resolved {
		seen := map[string]bool{}
		for _, ref := range item.References {
			if ref.Type != media.Virtual && !seen[ref.ID] {
				seen[ref.ID] = true
				bySource[ref.ID] = append(bySource[ref.ID], item)
			}
		}
		if len(seen) == 0 {
			return c, fmt.Errorf("resolved occurrence %q lacks source provenance", item.ID)
		}
	}
	for id, items := range bySource {
		src, ok := settings.Source(id)
		if !ok || !src.Enabled {
			return c, store.ErrCatalogChanged
		}
		if err := st.RememberSourceResolutions(ctx, id, src.Fingerprint(), items); err != nil {
			return c, err
		}
	}
	return c, nil
}
