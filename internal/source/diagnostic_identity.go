package source

import (
	"context"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/media"
)

func reportIdentityFailure(ctx context.Context, item media.Item, path string) {
	name := ""
	if len(item.References) > 0 {
		name = item.References[0].Name
	}
	activity.FromContext(ctx).Report(activity.Diagnostic{
		Key: "unresolved:" + item.ID, Reason: activity.IdentityUnavailable, Severity: activity.Error,
		Phase: activity.Identity, Subject: name, Current: item.Name, Query: path,
	})
}
