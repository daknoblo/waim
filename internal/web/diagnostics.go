package web

import "github.com/daknoblo/waim/internal/activity"

type DiagnosticView struct {
	activity.Diagnostic
	Job       activity.Job
	Persisted bool
}
type DiagnosticsData struct {
	Items                            []DiagnosticView
	Truncated, Unavailable, Retrying bool
	Severity                         activity.Severity
}

func (d DiagnosticView) ReasonKey() string { return "diagnostics.reason." + string(d.Reason) }
func (d DiagnosticView) PhaseKey() string  { return "activity.phase." + string(d.Phase) }
func (d DiagnosticView) JobKey() string    { return "activity.job." + string(d.Job) }
func (d DiagnosticView) Tone() string {
	if d.Severity == activity.Error {
		return "text-rose-300"
	}
	return "text-amber-200"
}
func HealthKey(severity activity.Severity) string {
	if severity == activity.Error {
		return "diagnostics.indicator.error"
	}
	return "diagnostics.indicator.warning"
}
