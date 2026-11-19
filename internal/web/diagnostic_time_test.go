package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
)

func TestDiagnosticTimeLabelsDistinguishExactFallbackAndUnknown(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	exact := time.Date(2026, 10, 28, 16, 5, 6, 0, time.UTC)
	for _, locale := range []string{"en", "de"} {
		tr := catalog.For(locale)
		for _, tc := range []struct {
			at          time.Time
			approximate bool
			key         string
		}{
			{exact, false, "diagnostics.occurredAt"},
			{exact, true, "diagnostics.recordedAt"},
			{time.Time{}, false, "diagnostics.timeUnknown"},
		} {
			var b strings.Builder
			d := DiagnosticView{Diagnostic: activity.Diagnostic{Reason: activity.CacheUnavailable, Severity: activity.Error, OccurredAt: tc.at, TimeApproximate: tc.approximate}}
			if err := DiagnosticsPanel(tr, DiagnosticsData{Items: []DiagnosticView{d}}).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			if !strings.Contains(html, tr.T(tc.key)) || strings.Contains(html, "<time") == tc.at.IsZero() {
				t.Fatal("diagnostic timestamp must be labeled truthfully")
			}
			if !tc.at.IsZero() && (!strings.Contains(html, FormatTimeValue(exact)) || !strings.Contains(html, exact.Format(time.RFC3339Nano))) {
				t.Fatal("diagnostics need a full date, time and machine-readable timezone")
			}
		}
	}
}
