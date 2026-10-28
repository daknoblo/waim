package suggest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
)

func TestSuggestionDiagnosticTimesSurviveCacheRestore(t *testing.T) {
	s, _ := cacheService(t)
	exact := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	attempt := exact.Add(time.Hour)
	for _, legacy := range []bool{false, true} {
		d := activity.Diagnostic{Reason: activity.SuggestionsUnavailable, Severity: activity.Error, Query: "/movie/42"}
		if !legacy {
			d.OccurredAt = exact
		}
		raw, err := json.Marshal(persistedCache{Version: 1, SettingsKey: s.settingsKey(), AttemptedAt: attempt, Diagnostics: []activity.Diagnostic{d}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.store.SetKV(context.Background(), resultCacheKey, string(raw)); err != nil {
			t.Fatal(err)
		}
		restarted := New(s.cfg, s.store, s.log)
		state := restarted.SavedDiagnostics()
		restarted.Close()
		want := exact
		if legacy {
			want = attempt
		}
		if len(state.Diagnostics) != 1 || !state.Diagnostics[0].OccurredAt.Equal(want) || state.Diagnostics[0].TimeApproximate != legacy {
			t.Fatalf("wrong restored diagnostic timestamp: %+v", state.Diagnostics)
		}
	}
}
