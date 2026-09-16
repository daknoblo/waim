package web

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

type actionContextKey struct{}
type actionTranslatorKey struct{}

func WithActionTranslator(ctx context.Context, t *i18n.Translator) context.Context {
	return context.WithValue(ctx, actionTranslatorKey{}, t)
}
func ActionTranslator(ctx context.Context) *i18n.Translator {
	t, _ := ctx.Value(actionTranslatorKey{}).(*i18n.Translator)
	return t
}

type ActionTarget struct {
	Kind       string
	ID         int64
	References []media.Reference
	Watching   bool
	WatchOnly  bool
}
type actionIndex struct {
	targets map[string]ActionTarget
	aliases map[string]string
}

func WatchURL(kind string, id int64) string {
	path := "movie"
	if kind == media.Series || kind == store.MediaSeries || kind == "tv" {
		path = "tv"
	}
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("https://www.themoviedb.org/%s/%d", path, id)
}

func WithProvenance(ctx context.Context, catalog media.Catalog, run *store.ScanRun) context.Context {
	idx := actionIndex{targets: map[string]ActionTarget{}, aliases: map[string]string{}}
	for _, item := range catalog.Items {
		target := ActionTarget{Kind: item.Type, ID: item.TMDBID(), References: item.References, WatchOnly: item.WatchOnly}
		link := WatchURL(item.Type, item.TMDBID())
		if link == "" {
			link = item.ID
		}
		for _, ref := range item.References {
			if ref.Type == media.Virtual {
				target.Watching = true
			}
			if ref.URL != "" {
				idx.aliases[ref.URL] = link
			}
		}
		idx.targets[link] = target
	}
	if run != nil {
		for _, m := range run.Media {
			link := WatchURL(m.Type, m.TMDBID)
			if link == "" {
				continue
			}
			for _, ref := range m.References {
				if ref.URL != "" {
					idx.aliases[ref.URL] = link
				}
			}
		}
	}
	return context.WithValue(ctx, actionContextKey{}, idx)
}

func Target(ctx context.Context, link string) ActionTarget {
	idx, _ := ctx.Value(actionContextKey{}).(actionIndex)
	if alias, ok := idx.aliases[link]; ok {
		link = alias
	}
	if target, ok := idx.targets[link]; ok {
		return target
	}
	u, err := url.Parse(link)
	if err != nil || u.Host != "www.themoviedb.org" {
		return ActionTarget{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || (parts[0] != "movie" && parts[0] != "tv") {
		return ActionTarget{}
	}
	id, _ := strconv.ParseInt(parts[1], 10, 64)
	kind := media.Movie
	if parts[0] == "tv" {
		kind = media.Series
	}
	return ActionTarget{Kind: kind, ID: id}
}

// MediaHref also repairs links in an older computed result after an instance
// is removed or disabled; the historical URL is never reused as live ownership.
func MediaHref(ctx context.Context, link string) string {
	target := Target(ctx, link)
	if target.ID <= 0 {
		return link
	}
	if real := media.Link(target.References); real != "" {
		return real
	}
	return WatchURL(target.Kind, target.ID)
}

func InstanceRefs(refs []media.Reference) []media.Reference {
	out := []media.Reference{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if !seen[ref.ID] {
			out = append(out, ref)
			seen[ref.ID] = true
		}
	}
	return out
}
