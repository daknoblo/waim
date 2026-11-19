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
	configured := s.cfg.Get().TMDB.APIKey != ""
	views := web.BuildActivities(states, configured, time.Now())
	for i := range views {
		switch views[i].Job {
		case activity.Scan:
			views[i].CanStart = configured && s.sched != nil && !s.sched.Busy()
		case activity.Cache:
			views[i].CanStart = configured && s.refresher != nil && !s.refresher.Busy()
		case activity.Suggestions:
			views[i].CanStart = configured && s.suggest != nil && !s.suggest.Running()
		}
		views[i].CanStart = views[i].CanStart && !views[i].IsRunning()
	}
	return views
}

func (s *Server) handlePartialActivity(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(web.WithMutationEpoch(r.Context(), s.cfg.Gate().Token()))
	s.renderPartialPlain(w, r, web.ActivityPanel(s.translator(r), s.activityViews(r.Context())))
}

func (s *Server) handleStartActivity(w http.ResponseWriter, r *http.Request) {
	job := activity.Job(r.PathValue("job"))
	if job != activity.Scan && job != activity.Cache && job != activity.Suggestions {
		http.Error(w, "unknown activity", http.StatusBadRequest)
		return
	}
	if s.cfg.Get().TMDB.APIKey == "" {
		http.Error(w, s.translator(r).T("sources.tmdbRequired"), http.StatusConflict)
		return
	}
	switch job {
	case activity.Scan:
		if s.sched == nil || s.sched.Busy() {
			http.Error(w, s.translator(r).T("activity.startBusy"), http.StatusConflict)
			return
		}
		s.sched.Trigger()
	case activity.Cache:
		if s.refresher == nil {
			http.Error(w, s.translator(r).T("activity.startUnavailable"), http.StatusServiceUnavailable)
			return
		}
		if err := s.refresher.Trigger(); err != nil {
			s.log.Warn("manual cache refresh rejected", "err", err)
			http.Error(w, s.translator(r).T("activity.startBusy"), http.StatusConflict)
			return
		}
	case activity.Suggestions:
		if s.suggest == nil || s.suggest.Running() {
			http.Error(w, s.translator(r).T("activity.startBusy"), http.StatusConflict)
			return
		}
		s.suggest.Generate()
	}
	if isHTMX(r) {
		s.handlePartialActivity(w, r)
		return
	}
	http.Redirect(w, r, "/logs", http.StatusSeeOther)
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
