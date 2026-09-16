package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestDiagnosticsDeduplicateCloneAndBound(t *testing.T) {
	tracker := New()
	run := tracker.Start(Scan)
	run.Phase(Identity, 2)
	run.UnresolvedTitle("a", "Server", "First title")
	run.Phase(Metadata, 2)
	run.UnresolvedTitle("a", "Server", "First title")
	run.UnresolvedTitle("b", "Server", "Second title")
	snapshot := tracker.Snapshot()
	if len(snapshot[0].Diagnostics) != 2 {
		t.Fatal("same event duplicated across phases")
	}
	snapshot[0].Diagnostics[0].Current = "mutated"
	if tracker.Snapshot()[0].Diagnostics[0].Current != "First title" {
		t.Fatal("snapshot diagnostics alias tracker state")
	}
	var wg sync.WaitGroup
	for n := range 150 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run.Report(Diagnostic{Key: fmt.Sprint(n), Reason: InvalidRange, Severity: Warning, Current: fmt.Sprint(n)})
		}()
	}
	wg.Wait()
	state := tracker.Snapshot()[0]
	if len(state.Diagnostics) != MaxDiagnostics || !state.DiagnosticsTruncated {
		t.Fatalf("unbounded diagnostics: %d", len(state.Diagnostics))
	}
	run.Report(Diagnostic{Reason: StorageUnavailable, Severity: Error})
	if tracker.Snapshot()[0].Severity() != Error {
		t.Fatal("detail limit hid a later error")
	}
	tracker.Clear()
	run.Report(Diagnostic{Reason: ScanFailed, Severity: Error})
	if len(tracker.Snapshot()[0].Diagnostics) != 0 {
		t.Fatal("clear revived a stale run")
	}
}

func TestDiagnosticRetryKeepsIssueUntilCompletion(t *testing.T) {
	tracker := New()
	old := tracker.Start(Cache)
	old.Phase(Refresh, 1)
	old.Report(Diagnostic{Reason: CacheUnavailable, Severity: Error, Query: "/movie/7"})
	old.Advance(true, false)
	old.Finish(context.Background(), Completed, 0)
	retry := tracker.Start(Cache)
	state := tracker.Snapshot()[1]
	if state.Severity() != Error || state.PreviousSeverity != Error || len(state.Diagnostics) != 1 || !state.Diagnostics[0].Previous {
		t.Fatal("ongoing retry prematurely cleared failure")
	}
	old.Report(Diagnostic{Reason: StorageUnavailable, Severity: Error})
	retry.Phase(Refresh, 1)
	retry.Advance(false, false)
	retry.Finish(context.Background(), Completed, 0)
	state = tracker.Snapshot()[1]
	if state.Severity() != "" || len(state.Diagnostics) != 0 || state.Status != Completed {
		t.Fatalf("successful retry retained old issues: %+v", state)
	}
}

func TestDiagnosticPrivacyAndLegacyFallback(t *testing.T) {
	tracker := New()
	run := tracker.Start(Cache)
	run.Report(Diagnostic{Reason: CacheUnavailable, Severity: Error, Current: "token=private-secret", Subject: "Host https://user:private-secret@example.invalid/?key=private-secret", Query: "/search/movie?query=private-search&api_key=private-secret"})
	data, err := json.Marshal(tracker.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-secret") || strings.Contains(string(data), "private-search") {
		t.Fatal("diagnostics exposed credentials/query")
	}
	if tracker.Snapshot()[1].Diagnostics[0].Query != "/search/movie" {
		t.Fatal("safe operation path lost")
	}
	legacy := LegacyDiagnostic("unrecognized upstream response body private-secret", "Source")
	if legacy.Reason != LegacyWarning || legacy.Current != "" {
		t.Fatal("unstructured upstream content reflected")
	}
	known := LegacyDiagnostic("Source: Unidentified episode: Real title", "")
	if known.Reason != UnidentifiedEpisode || known.Subject != "Source" || known.Current != "Real title" {
		t.Fatalf("known legacy warning lost: %+v", known)
	}
	run.Report(Diagnostic{Reason: Reason("private-secret"), Severity: Severity("private-secret"), Phase: Phase("private-secret"), Query: "/movie/" + strings.Repeat("9", 1000)})
	last := tracker.Snapshot()[1].Diagnostics[1]
	if last.Reason != LegacyWarning || last.Severity != Warning || last.Phase != "" || last.Query != "" {
		t.Fatal("unbounded/unrecognized structured fields accepted")
	}
}
