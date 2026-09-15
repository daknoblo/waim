package source

import (
	"context"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

// Virtual adapts saved observations without connection credentials or invented
// files/episodes. Its only permanent library is the reserved virtual collection.
func Virtual(entries []store.VirtualEntry) Adapter {
	return virtualAdapter{entries: entries}
}

type virtualAdapter struct{ entries []store.VirtualEntry }

func (v virtualAdapter) Libraries(ctx context.Context) ([]media.Library, error) {
	return []media.Library{{ID: media.VirtualID, Name: media.VirtualName, Type: media.Virtual}}, ctx.Err()
}
func (v virtualAdapter) Snapshot(ctx context.Context) (media.Snapshot, error) {
	libs, err := v.Libraries(ctx)
	if err != nil {
		return media.Snapshot{}, err
	}
	snapshot := media.Snapshot{Libraries: libs}
	for _, entry := range v.entries {
		snapshot.Items = append(snapshot.Items, entry.Item())
	}
	return snapshot, nil
}
