package web

import (
	"fmt"
	"strconv"
	"time"

	"github.com/daknoblo/waim/internal/activity"
)

type ActivityView struct {
	activity.State
	Elapsed string
}

func BuildActivities(states []activity.State, configured bool, now time.Time) []ActivityView {
	out := make([]ActivityView, 0, len(states))
	for _, s := range states {
		if s.Job == activity.Sources && s.Status == activity.Idle {
			continue
		}
		if s.Job != activity.Sources {
			if !configured && (s.Status == activity.Idle || s.Status == activity.Waiting) {
				s.Status = activity.Waiting
			} else if configured && s.Status == activity.Waiting {
				s = activity.State{Job: s.Job, Status: activity.Idle}
			}
		}
		v := ActivityView{State: s}
		if !s.StartedAt.IsZero() && s.Status != activity.Waiting {
			end := s.EndedAt
			if end.IsZero() {
				end = now
			}
			seconds := max(0, int(end.Sub(s.StartedAt).Seconds()))
			v.Elapsed = fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
		}
		out = append(out, v)
	}
	return out
}

func (v ActivityView) Tone() string {
	switch v.Status {
	case activity.Failed:
		return "activity-error"
	case activity.Partial, activity.Cancelled, activity.Waiting:
		return "activity-warning"
	case activity.Running:
		if v.Failures > 0 || v.Warnings > 0 || v.Skipped > 0 {
			return "activity-warning"
		}
		return "activity-running"
	default:
		return "activity-resting"
	}
}

func (v ActivityView) RingValue() string { return strconv.Itoa(v.Percent()) + " 100" }
func (v ActivityView) IsRunning() bool   { return v.Status == activity.Running }
func (v ActivityView) HasProgress() bool { return v.IsRunning() && v.Known && v.Total > 0 }
func (v ActivityView) IsWaiting() bool   { return v.Status == activity.Waiting }
func (v ActivityView) JobKey() string {
	if v.Job == activity.Scan && v.Mode == activity.Recompute {
		return "activity.job.recompute"
	}
	return "activity.job." + string(v.Job)
}
func (v ActivityView) StatusKey() string    { return "activity.status." + string(v.Status) }
func (v ActivityView) PhaseKey() string     { return "activity.phase." + string(v.Phase) }
func (v ActivityView) OperationKey() string { return "activity.operation." + string(v.Operation) }

func (v ActivityView) ScopeKey() string {
	if v.Known && v.Total == 0 {
		return "activity.empty"
	}
	return "activity.indeterminate"
}
