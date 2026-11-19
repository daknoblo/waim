package web

import (
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/store"
)

func TestGrowthOnlyShowsChangedDailyClosingTotals(t *testing.T) {
	first := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
	var history []store.RunTotals
	for day, counts := range [][]int{{98, 100}, {101, 100}, {103, 105}, {}, {101}} {
		for i, count := range counts {
			history = append(history, store.RunTotals{FinishedAt: first.AddDate(0, 0, day).Add(time.Duration(i) * time.Hour), ItemsScanned: count})
		}
	}
	got := buildGrowth(history)
	if len(got) != 2 || got[0].Date != "2026-11-05" || got[0].Delta != "-4" || got[1].Date != "2026-11-03" || got[1].Delta != "+5" {
		t.Fatalf("unexpected change days: %+v", got)
	}
}

func TestGrowthCardInitiallyShowsFiveDays(t *testing.T) {
	var b strings.Builder
	if err := growthCard(testTranslator(t), make([]StatsGrowth, 12), "").Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Count(b.String(), " hidden") != 7 || !strings.Contains(b.String(), `class="rated-limit lang-select"`) {
		t.Fatal("expected five visible days and a list-size selector")
	}
}
