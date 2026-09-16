package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

type diagnosticCache struct {
	mu      sync.Mutex
	key     [32]byte
	valid   bool
	data    web.DiagnosticsData
	sources map[string]cachedSourceDiagnostics
	run     *cachedRunDiagnostics
}

func promote(current, next activity.Severity) activity.Severity {
	if current == activity.Error || next == activity.Error {
		return activity.Error
	}
	if current != "" || next != "" {
		return activity.Warning
	}
	return ""
}

func (s *Server) diagnostics(ctx context.Context) web.DiagnosticsData {
	basis, err := s.store.DiagnosticBasis(ctx)
	if err != nil {
		return s.combineDiagnostics(storageDiagnostics(), store.DiagnosticBasis{})
	}
	settings := s.cfg.Get()
	keyBytes, _ := json.Marshal(struct {
		Basis      store.DiagnosticBasis
		Token      string
		Configured bool
	}{basis, settings.SourcesToken(), settings.TMDB.APIKey != ""})
	key := sha256.Sum256(keyBytes)
	s.diagnosticCache.mu.Lock()
	if !s.diagnosticCache.valid || s.diagnosticCache.key != key {
		data := web.DiagnosticsData{}
		if s.diagnosticCache.sources == nil {
			s.diagnosticCache.sources = map[string]cachedSourceDiagnostics{}
		}
		activeSources := map[string]bool{}
		for _, src := range settings.Sources {
			if src.Enabled && src.Type != media.Virtual {
				activeSources[src.ID] = true
			}
		}
		for id := range s.diagnosticCache.sources {
			if !activeSources[id] {
				delete(s.diagnosticCache.sources, id)
			}
		}
		loadFailed := false
		persistedSeen := map[string]bool{}
		add := func(d activity.Diagnostic) {
			data.Severity = promote(data.Severity, d.Severity)
			keyBytes, _ := json.Marshal([]any{d.Reason, d.Subject, d.Current, d.Query})
			event := string(keyBytes)
			if persistedSeen[event] {
				return
			}
			if len(data.Items) >= activity.MaxDiagnostics {
				data.Truncated = true
				if d.Severity == activity.Error {
					for i := len(data.Items) - 1; i >= 0; i-- {
						if data.Items[i].Severity != activity.Error {
							data.Items[i] = web.DiagnosticView{Diagnostic: d, Persisted: true, Job: activity.Scan}
							persistedSeen[event] = true
							return
						}
					}
				}
				return
			}
			persistedSeen[event] = true
			data.Items = append(data.Items, web.DiagnosticView{Diagnostic: d, Persisted: true, Job: activity.Scan})
		}
		if basis.Finished.HasError || basis.Finished.Status == store.StatusError {
			add(activity.Diagnostic{Reason: activity.ScanFailed, Severity: activity.Error})
		}
		if basis.Successful.ID > 0 {
			cached := s.diagnosticCache.run
			var e error
			if cached == nil || cached.version != basis.Successful {
				var meta store.RunMetadata
				meta, e = s.store.DiagnosticRunMetadata(ctx, basis.Successful.ID)
				if e == nil {
					payload := parseDiagnosticPayload(meta.Warnings, "")
					meta.Warnings = nil
					cached = &cachedRunDiagnostics{version: basis.Successful, metadata: meta, payload: payload}
					s.diagnosticCache.run = cached
				}
			}
			if e != nil {
				data = diagnosticReadFailure(e)
				loadFailed = true
			} else {
				meta := cached.metadata
				if meta.Basis == "" {
					add(activity.Diagnostic{Reason: activity.LegacyCatalog, Severity: activity.Warning})
				}
				if meta.SourcesToken != settings.SourcesToken() || meta.Revision != basis.Revision || meta.Pending {
					add(activity.Diagnostic{Reason: activity.PendingCatalog, Severity: activity.Warning})
				}
				data.Truncated = data.Truncated || cached.payload.truncated
				data.Severity = promote(data.Severity, cached.payload.severity)
				for _, d := range cached.payload.details {
					add(d)
				}
			}
		}
		for _, src := range settings.Sources {
			if !src.Enabled || src.Type == media.Virtual {
				continue
			}
			var saved store.DiagnosticSource
			for _, candidate := range basis.Sources {
				if candidate.ID == src.ID {
					saved = candidate
					break
				}
			}
			name := activity.SafeLabel(src.Name)
			if saved.Fingerprint != src.Fingerprint() || saved.Succeeded == "" {
				delete(s.diagnosticCache.sources, src.ID)
				if settings.TMDB.APIKey != "" || saved.Attempted != "" {
					add(activity.Diagnostic{Reason: activity.SourceUnknown, Severity: activity.Warning, Phase: activity.Inventory, Subject: name})
				}
				continue
			}
			if saved.Failed {
				add(activity.Diagnostic{Reason: activity.SourceStale, Severity: activity.Warning, Phase: activity.Inventory, Subject: name})
			}
			cached, ok := s.diagnosticCache.sources[src.ID]
			var e error
			if !ok || cached.version != saved || cached.name != name {
				var warnings []string
				warnings, e = s.store.DiagnosticSourceWarnings(ctx, src.ID, src.Fingerprint())
				if e == nil {
					cached = cachedSourceDiagnostics{version: saved, name: name, payload: parseDiagnosticPayload(warnings, name)}
					s.diagnosticCache.sources[src.ID] = cached
				}
			}
			if e != nil {
				data = diagnosticReadFailure(e)
				data.Items[0].Subject = name
				loadFailed = true
				break
			}
			data.Truncated = data.Truncated || cached.payload.truncated
			data.Severity = promote(data.Severity, cached.payload.severity)
			for _, d := range cached.payload.details {
				add(d)
			}
			if basis.Successful.ID > 0 && basis.Successful.Finished != "" {
				attempt, attemptErr := time.Parse(time.RFC3339Nano, saved.Attempted)
				finished, finishErr := time.Parse(time.RFC3339Nano, basis.Successful.Finished)
				if attemptErr != nil || finishErr != nil {
					add(activity.Diagnostic{Reason: activity.LegacyWarning, Severity: activity.Warning, Subject: name})
				} else if attempt.After(finished) {
					add(activity.Diagnostic{Reason: activity.PendingCatalog, Severity: activity.Warning})
				}
			}
		}
		s.diagnosticCache.key, s.diagnosticCache.data, s.diagnosticCache.valid = key, data, !loadFailed
	}
	persisted := s.diagnosticCache.data
	persisted.Items = append([]web.DiagnosticView(nil), persisted.Items...)
	s.diagnosticCache.mu.Unlock()
	return s.combineDiagnostics(persisted, basis)
}

func (s *Server) combineDiagnostics(persisted web.DiagnosticsData, basis store.DiagnosticBasis) web.DiagnosticsData {
	out := web.DiagnosticsData{Truncated: persisted.Truncated, Retrying: basis.Latest.Status == store.StatusRunning && persisted.Severity != ""}
	seen := map[string]bool{}
	add := func(d web.DiagnosticView) {
		if d.Persisted {
			for _, current := range out.Items {
				sameReason := current.Reason == d.Reason || (current.Reason == activity.IdentityUnavailable && d.Reason == activity.Unresolved)
				if !current.Persisted && sameReason && current.Current == d.Current && (d.Subject == "" || d.Subject == current.Subject) {
					return
				}
			}
		}
		keyBytes, _ := json.Marshal([]any{d.Reason, d.Subject, d.Current, d.Query})
		key := string(keyBytes)
		out.Severity = promote(out.Severity, d.Severity)
		if seen[key] {
			return
		}
		seen[key] = true
		out.Items = append(out.Items, d)
	}
	scanRunning := s.sched != nil && s.sched.Running()
	for _, state := range s.activities.Snapshot() {
		scanRunning = scanRunning || (state.Job == activity.Scan && state.Status == activity.Running)
		if state.Status == activity.Waiting {
			continue
		}
		out.Severity = promote(out.Severity, state.Severity())
		out.Retrying = out.Retrying || (state.Status == activity.Running && state.PreviousSeverity != "")
		out.Truncated = out.Truncated || state.DiagnosticsTruncated
		if (state.HasIssues() || state.PreviousSeverity != "") && len(state.Diagnostics) == 0 {
			out.Unavailable = true
		}
		for _, d := range state.Diagnostics {
			add(web.DiagnosticView{Diagnostic: d, Job: state.Job})
		}
	}
	for _, d := range persisted.Items {
		add(d)
	}
	unfinishedRetry := basis.Latest.Status == store.StatusRunning && basis.Unfinished.ID > basis.Finished.ID
	if basis.Latest.Status == store.StatusRunning && (!scanRunning || unfinishedRetry) {
		add(web.DiagnosticView{Diagnostic: activity.Diagnostic{Reason: activity.UnfinishedScan, Severity: activity.Warning}, Job: activity.Scan, Persisted: true})
		out.Retrying = out.Retrying || (scanRunning && unfinishedRetry)
	}
	out.Severity = promote(out.Severity, persisted.Severity)
	sort.SliceStable(out.Items, func(i, j int) bool {
		return out.Items[i].Severity == activity.Error && out.Items[j].Severity != activity.Error
	})
	if len(out.Items) > activity.MaxDiagnostics {
		out.Items = out.Items[:activity.MaxDiagnostics]
		out.Truncated = true
	}
	return out
}

func storageDiagnostics() web.DiagnosticsData {
	return web.DiagnosticsData{Severity: activity.Error, Items: []web.DiagnosticView{{Diagnostic: activity.Diagnostic{Reason: activity.StorageUnavailable, Severity: activity.Error}, Persisted: true}}}
}

func diagnosticReadFailure(err error) web.DiagnosticsData {
	if errors.Is(err, sql.ErrNoRows) {
		return web.DiagnosticsData{Severity: activity.Warning, Items: []web.DiagnosticView{{Diagnostic: activity.Diagnostic{Reason: activity.PendingCatalog, Severity: activity.Warning}, Persisted: true}}}
	}
	return storageDiagnostics()
}

func (s *Server) handleHealthIndicator(w http.ResponseWriter, r *http.Request) {
	data := s.diagnostics(r.Context())
	s.renderPartialPlain(w, r, web.HealthIndicator(s.translator(r), data.Severity))
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	s.renderPartialPlain(w, r, web.DiagnosticsPanel(s.translator(r), s.diagnostics(r.Context())))
}
