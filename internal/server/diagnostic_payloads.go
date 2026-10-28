package server

import (
	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/store"
)

type diagnosticPayload struct {
	details   []activity.Diagnostic
	severity  activity.Severity
	truncated bool
}
type cachedSourceDiagnostics struct {
	version store.DiagnosticSource
	name    string
	payload diagnosticPayload
}
type cachedRunDiagnostics struct {
	version  store.DiagnosticRun
	metadata store.RunMetadata
	payload  diagnosticPayload
}

// Cache only bounded, sanitized warnings. Raw warning/error payloads are not
// retained by the server's polling cache.
func parseDiagnosticPayload(warnings []string, sourceName string) diagnosticPayload {
	out := diagnosticPayload{}
	for _, warning := range warnings {
		d := activity.LegacyDiagnostic(warning, sourceName)
		out.severity = promote(out.severity, d.Severity)
		found := false
		for _, old := range out.details {
			if old.Key == d.Key {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if len(out.details) < activity.MaxDiagnostics {
			out.details = append(out.details, d)
			continue
		}
		out.truncated = true
		if d.Severity == activity.Error {
			for i := len(out.details) - 1; i >= 0; i-- {
				if out.details[i].Severity != activity.Error {
					out.details[i] = d
					break
				}
			}
		}
	}
	return out
}
