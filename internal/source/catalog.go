package source

import (
	"context"
	"fmt"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

// Catalog loads only saved snapshots unless refresh was explicitly requested.
// Remote failures are safe bounded warnings, not raw URLs or credentials.
func Catalog(ctx context.Context, st *store.Store, settings config.Settings, refresh bool, factory Factory) (media.Catalog, error) {
	out := media.Catalog{}
	if factory == nil {
		factory = New
	}
	for _, src := range settings.Sources {
		if !src.Enabled || src.Type == media.Virtual {
			continue
		}
		fp := src.Fingerprint()
		if refresh {
			adapter, err := factory(src)
			var snapshot media.Snapshot
			if err == nil {
				snapshot, err = adapter.Snapshot(ctx)
			}
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			var saveErr error
			if err != nil {
				saveErr = st.SaveSourceAttempt(ctx, src.ID, fp, nil, "Source refresh failed; check connection and access.")
			} else {
				saveErr = st.SaveSourceAttempt(ctx, src.ID, fp, &snapshot, "")
			}
			if saveErr != nil {
				return out, saveErr
			}
		}
		saved, err := st.SourceSnapshot(ctx, src.ID, fp)
		if err != nil {
			return out, err
		}
		if saved.AttemptedAt.After(out.UpdatedAt) {
			out.UpdatedAt = saved.AttemptedAt
		}
		if saved.Snapshot == nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: unknown inventory; refresh required", src.Name))
			continue
		}
		stale := saved.Error != ""
		for _, warning := range saved.Snapshot.Warnings {
			out.Warnings = append(out.Warnings, src.Name+": "+warning)
		}
		if stale {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: stale inventory (last successful snapshot)", src.Name))
		}
		for _, item := range saved.Snapshot.Items {
			for i := range item.References {
				item.References[i].Name = src.Name
				item.References[i].Stale = stale
			}
			out.Items = append(out.Items, item)
		}
		for _, lib := range saved.Snapshot.Libraries {
			for _, selected := range src.Libraries {
				if media.Qualify(src.ID, selected.ID) == lib.ID {
					lib.Name = src.Name + " · " + selected.Name
					break
				}
			}
			out.Libraries = append(out.Libraries, lib)
		}
	}
	entries, revision, err := st.VirtualEntries(ctx)
	if err != nil {
		return out, err
	}
	out.Revision = revision
	virtual, err := Virtual(entries).Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.Libraries = append(out.Libraries, virtual.Libraries...)
	out.Items = append(out.Items, virtual.Items...)
	out.Items = media.Merge(out.Items)
	return out, nil
}
