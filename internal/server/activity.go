package server

import (
	"context"
	"net/http"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) activityViews(ctx context.Context) []web.ActivityView {
	_, states := s.diagnosticSnapshot(ctx)
	return web.BuildActivities(states, s.cfg.Get().TMDB.APIKey != "", time.Now())
}

func (s *Server) handlePartialActivity(w http.ResponseWriter, r *http.Request) {
	s.renderPartialPlain(w, r, web.ActivityPanel(s.translator(r), s.activityViews(r.Context())))
}

func (s *Server) activityStates(resolvedAt time.Time) []activity.State {
	states := s.activities.Snapshot()
	for i, state := range states {
		if state.Job != activity.Suggestions {
			continue
		}
		if s.suggest != nil {
			saved := s.suggest.SavedDiagnostics()
			if state.Status == activity.Idle && saved.Job == activity.Suggestions {
				state = saved
			}
			if state.Status == activity.Running {
				state = reconcilePreviousIdentityWarnings(state, saved, resolvedAt)
			}
		}
		states[i] = reconcileIdentityWarnings(state, resolvedAt)
	}

	return states
}

func reconcilePreviousIdentityWarnings(state, saved activity.State, resolvedAt time.Time) activity.State {
	reconciled := reconcileIdentityWarnings(saved, resolvedAt)
	if reconciled.Resolved == 0 {
		return state
	}
	remaining := make([]activity.Diagnostic, 0, len(state.Diagnostics))
	for _, d := range state.Diagnostics {
		if d.Previous && d.Reason == activity.Unresolved && d.Severity == activity.Skip {
			continue
		}
		remaining = append(remaining, d)
	}
	state.Diagnostics = remaining
	state.PreviousSeverity = reconciled.Severity()
	state.Resolved = reconciled.Resolved
	return state
}

// Project historical identity warnings against newer clean scan evidence.
// Keep the cached recommendations, their generation time and raw job history intact.
func reconcileIdentityWarnings(state activity.State, resolvedAt time.Time) activity.State {
	if state.Job != activity.Suggestions || state.Status == activity.Running || state.EndedAt.IsZero() ||
		!resolvedAt.After(state.EndedAt) || state.DiagnosticsTruncated {
		return state
	}
	remaining := make([]activity.Diagnostic, 0, len(state.Diagnostics))
	resolved := 0
	for _, d := range state.Diagnostics {
		if d.Reason == activity.Unresolved && d.Severity == activity.Skip {
			resolved++
		} else {
			remaining = append(remaining, d)
		}
	}
	if resolved == 0 {
		return state
	}
	state.Diagnostics = remaining
	state.Skipped = max(0, state.Skipped-resolved)
	state.Warnings = max(0, state.Warnings-resolved)
	state.Resolved = resolved
	if state.Status == activity.Partial && len(remaining) == 0 && state.Failures == 0 &&
		state.Skipped == 0 && state.Warnings == 0 && state.PreviousSeverity == "" &&
		(!state.Known || state.Done >= state.Total) {
		state.Status = activity.Completed
	}
	return state
}
