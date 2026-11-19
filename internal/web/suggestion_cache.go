package web

import (
	"context"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/suggest"
)

// The result cache can outlive a scan. Filter live real ownership at render
// time without regenerating recommendations or mutating the cached slices.
func unownedSuggestions(ctx context.Context, items []suggest.Item) []suggest.Item {
	out := make([]suggest.Item, 0, len(items))
	for _, item := range items {
		owned := false
		for _, ref := range Target(ctx, item.TMDBLink).References {
			owned = owned || ref.Type != media.Virtual
		}
		if !owned {
			out = append(out, item)
		}
	}
	return out
}
