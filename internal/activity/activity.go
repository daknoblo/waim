// Package activity tracks bounded, process-local job progress without retaining
// logs, upstream responses, or credentials. A percentage always describes one
// phase, never an estimate of an entire job.
package activity

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Job string

const (
	Scan        Job = "scan"
	Cache       Job = "cache"
	Suggestions Job = "suggestions"
	Sources     Job = "sources"
)

type Status string

const (
	Idle      Status = "idle"
	Running   Status = "running"
	Waiting   Status = "waiting"
	Completed Status = "completed"
	Partial   Status = "partial"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type Phase string

const (
	Inventory   Phase = "inventory"
	Identity    Phase = "identity"
	Metadata    Phase = "metadata"
	Persistence Phase = "persistence"
	Refresh     Phase = "refresh"
	Trending    Phase = "trending"
	Similar     Phase = "similar"
	Upcoming    Phase = "upcoming"
	AI          Phase = "ai"
	Cleanup     Phase = "cleanup"
)

type Operation string

const (
	Account   Operation = "account"
	Libraries Operation = "libraries"
	Episodes  Operation = "episodes"
	Test      Operation = "test"
)

type Mode string

const (
	SourceRefresh Mode = "refresh"
	Recompute     Mode = "recompute"
)

// State is a value-only immutable snapshot. Done includes failed/skipped work
// units; Failures, Skipped and Warnings are run-wide diagnostic counts.
type State struct {
	Job                           Job
	Mode                          Mode
	Status                        Status
	Phase                         Phase
	Operation                     Operation
	Current, Subject, Query       string
	Done, Total                   int
	Known                         bool
	Failures, Skipped, Warnings   int
	Page                          int
	StartedAt, UpdatedAt, EndedAt time.Time
}

func (s State) Percent() int {
	if !s.Known || s.Total == 0 {
		return 0
	}
	return int(float64(s.Done) / float64(s.Total) * 100)
}

func (s State) Remaining() int {
	if !s.Known {
		return 0
	}
	return s.Total - s.Done
}

func (s State) RemainingPercent() int {
	if !s.Known || s.Total == 0 {
		return 0
	}
	return 100 - s.Percent()
}

type slot struct {
	token uint64
	state State
}

// Tracker retains exactly one running/latest entry for each logical job.
type Tracker struct {
	mu    sync.RWMutex
	slots map[Job]slot
}

func New() *Tracker { return &Tracker{slots: make(map[Job]slot)} }

func valid(job Job) bool {
	return job == Scan || job == Cache || job == Suggestions || job == Sources
}

type Run struct {
	tracker *Tracker
	job     Job
	token   uint64
}

func (t *Tracker) Start(job Job) *Run {
	if t == nil || !valid(job) {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.slots == nil {
		t.slots = make(map[Job]slot)
	}
	v := t.slots[job]
	v.token++
	now := time.Now()
	v.state = State{Job: job, Status: Running, StartedAt: now, UpdatedAt: now}
	t.slots[job] = v
	return &Run{t, job, v.token}
}

func (t *Tracker) Snapshot() []State {
	out := make([]State, 0, 4)
	for _, job := range []Job{Scan, Cache, Suggestions, Sources} {
		out = append(out, State{Job: job, Status: Idle})
	}
	if t == nil {
		return out
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := range out {
		if v, ok := t.slots[out[i].Job]; ok {
			out[i] = v.state
		}
	}
	return out
}

func (r *Run) update(fn func(*State)) {
	if r == nil {
		return
	}
	r.tracker.mu.Lock()
	defer r.tracker.mu.Unlock()
	v := r.tracker.slots[r.job]
	if v.token != r.token || v.state.Status != Running {
		return
	}
	fn(&v.state)
	v.state.UpdatedAt = time.Now()
	r.tracker.slots[r.job] = v
}

// Phase starts a new scope. A negative total is explicitly indeterminate.
func (r *Run) Phase(phase Phase, total int) {
	r.update(func(s *State) {
		s.Phase, s.Total, s.Known, s.Done = phase, max(0, total), total >= 0, 0
		s.Current, s.Subject, s.Query, s.Operation, s.Page = "", "", "", "", 0
	})
}

// Mode identifies the whole run and is retained when the current phase changes.
func (r *Run) Mode(mode Mode) {
	r.update(func(s *State) { s.Mode = mode })
}

func (r *Run) Current(title string) {
	r.update(func(s *State) { s.Current, s.Query = safeLabel(title), "" })
}

var endpoint = regexp.MustCompile(`^/(?:movie/[0-9]+(?:/recommendations)?|tv/[0-9]+(?:/recommendations|/season/[0-9]+)?|collection/[0-9]+|search/(?:movie|tv)|trending/(?:movie|tv)/week|discover/(?:movie|tv)|genre/(?:movie|tv)/list|movie/upcoming|tv/on_the_air)$`)

// Endpoint accepts only known TMDB path shapes and never exposes query values.
func (r *Run) Endpoint(path string) {
	path, _, _ = strings.Cut(path, "?")
	if !endpoint.MatchString(path) {
		path = ""
	}
	r.update(func(s *State) { s.Query = path })
}

func (r *Run) Subject(name string) {
	r.update(func(s *State) { s.Subject = safeLabel(name) })
}

func (r *Run) Operation(op Operation) {
	r.update(func(s *State) { s.Operation, s.Page = op, 0 })
}

func (r *Run) Page(page int) {
	r.update(func(s *State) { s.Page = max(0, page) })
}

func (r *Run) Advance(failed, skipped bool) {
	r.update(func(s *State) {
		s.Done++
		if s.Known {
			s.Done = min(s.Done, s.Total)
		}
		if failed {
			s.Failures++
		}
		if skipped {
			s.Skipped++
		}
	})
}

// Warnings publishes the number of diagnostics collected so far. Later phases
// may report a subset, so this count never moves backwards within a run.
func (r *Run) Warnings(count int) {
	r.update(func(s *State) { s.Warnings = max(s.Warnings, count) })
}

// Finish does not turn incomplete/failed work green and cannot update a newer
// run. Call it on every exit; cancellation wins over the requested outcome.
func (r *Run) Finish(ctx context.Context, status Status, warnings int) {
	r.update(func(s *State) {
		switch {
		case ctx.Err() != nil:
			status = Cancelled
		case status == Completed && (warnings > 0 || s.Warnings > 0 || s.Failures > 0 || s.Skipped > 0 || (s.Known && s.Done < s.Total)):
			status = Partial
		}
		switch status {
		case Completed, Partial, Failed, Cancelled, Waiting:
		default:
			status = Failed
		}
		s.Status, s.Warnings, s.EndedAt = status, max(s.Warnings, warnings), time.Now()
	})
}

type contextKey struct{}

func WithRun(ctx context.Context, run *Run) context.Context {
	return context.WithValue(ctx, contextKey{}, run)
}

func FromContext(ctx context.Context) *Run {
	run, _ := ctx.Value(contextKey{}).(*Run)
	return run
}

var address = regexp.MustCompile(`(?i)(?:https?://|www\.)\S+`)

// Titles and display names are the only free-text inputs. Addresses are never
// useful activity labels; strip them even if embedded in a source/title name.
func safeLabel(text string) string {
	text = address.ReplaceAllString(text, "[address]")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 180 {
		return string(runes[:177]) + "…"
	}
	return string(runes)
}
