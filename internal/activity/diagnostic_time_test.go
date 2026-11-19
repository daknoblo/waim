package activity

import (
	"context"
	"testing"
	"time"
)

func TestDiagnosticOccurrenceTimeSurvivesDeduplicationAndRetry(t *testing.T) {
	tracker := New()
	run := tracker.Start(Cache)
	start := time.Now()
	d := Diagnostic{Key: "request", Reason: CacheUnavailable, Severity: Error}
	run.Report(d)
	first := tracker.Snapshot()[1].Diagnostics[0]
	if first.OccurredAt.Before(start) || first.OccurredAt.After(time.Now()) || first.TimeApproximate {
		t.Fatal("new diagnostics must record the real report time")
	}
	run.Report(d)
	run.Finish(context.Background(), Completed, 0)
	if !tracker.Snapshot()[1].Diagnostics[0].OccurredAt.Equal(first.OccurredAt) {
		t.Fatal("deduplication/finish changed the original occurrence time")
	}
	retry := tracker.Start(Cache)
	old := tracker.Snapshot()[1].Diagnostics[0]
	if !old.Previous || !old.OccurredAt.Equal(first.OccurredAt) {
		t.Fatal("retry stamped an older diagnostic as new")
	}
	retry.Report(d)
	current := tracker.Snapshot()[1].Diagnostics[0]
	if current.Previous || current.OccurredAt.Before(old.OccurredAt) {
		t.Fatal("retry should record a new occurrence only when the error recurs")
	}
}

func TestFallbackDiagnosticTimesAreExplicitAndDoNotReplaceExactTime(t *testing.T) {
	at := time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC)
	d := Diagnostic{Reason: LegacyWarning}.WithFallbackTime(at)
	if !d.TimeApproximate || !d.OccurredAt.Equal(at) {
		t.Fatal("legacy fallback not labeled as approximate")
	}
	exact := Diagnostic{OccurredAt: at}.WithFallbackTime(at.Add(time.Hour))
	if exact.TimeApproximate || !exact.OccurredAt.Equal(at) {
		t.Fatal("fallback replaced known occurrence time")
	}
}
