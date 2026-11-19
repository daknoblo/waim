package server

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/media"
)

func TestSourceDiagnosticDatesUseSnapshotAndFailedAttemptTimes(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	src := auditSource(t, s, nil)
	if err := s.store.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), &media.Snapshot{Warnings: []string{"Invalid episode range (only the start episode counted): Example series"}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), nil, "offline"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.store.SourceSnapshot(ctx, src.ID, src.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, diagnostic := range s.diagnostics(ctx).Items {
		switch diagnostic.Reason {
		case activity.SourceStale:
			if !diagnostic.OccurredAt.Equal(snapshot.AttemptedAt) || !diagnostic.TimeApproximate {
				t.Fatal("stale-source warning must show its failed attempt time")
			}
			found++
		case activity.InvalidRange:
			if snapshot.SucceededAt == nil || !diagnostic.OccurredAt.Equal(*snapshot.SucceededAt) || !diagnostic.TimeApproximate {
				t.Fatal("snapshot warning must retain the successful snapshot time, not the later failure")
			}
			found++
		}
	}
	if found != 2 {
		t.Fatal("source time assertions did not cover both warning types")
	}
}
