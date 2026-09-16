package activity

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

const MaxDiagnostics = 100

type Severity string

const (
	Warning Severity = "warning"
	Error   Severity = "error"
	Skip    Severity = "skip"
)

type Reason string

const (
	Unresolved             Reason = "unresolved"
	IdentityUnavailable    Reason = "identityUnavailable"
	SourceUnknown          Reason = "sourceUnknown"
	SourceStale            Reason = "sourceStale"
	UnidentifiedEpisode    Reason = "unidentifiedEpisode"
	InvalidRange           Reason = "invalidRange"
	VirtualIgnored         Reason = "virtualIgnored"
	MovieUnavailable       Reason = "movieUnavailable"
	SeriesUnavailable      Reason = "seriesUnavailable"
	CollectionUnavailable  Reason = "collectionUnavailable"
	SeasonUnavailable      Reason = "seasonUnavailable"
	CacheUnavailable       Reason = "cacheUnavailable"
	SuggestionsUnavailable Reason = "suggestionsUnavailable"
	AIUnavailable          Reason = "aiUnavailable"
	SourceUnavailable      Reason = "sourceUnavailable"
	StorageUnavailable     Reason = "storageUnavailable"
	ScanFailed             Reason = "scanFailed"
	UnfinishedScan         Reason = "unfinishedScan"
	LegacyWarning          Reason = "legacyWarning"
	PendingCatalog         Reason = "pendingCatalog"
	LegacyCatalog          Reason = "legacyCatalog"
)

// Diagnostic contains only display names and allowlisted paths, never errors
// or response bodies. Key is an internal event identity; it is hashed on entry.
type Diagnostic struct {
	Key                     string
	Reason                  Reason
	Severity                Severity
	Phase                   Phase
	Subject, Current, Query string
	Previous                bool
}

func cleanDiagnostic(d Diagnostic) Diagnostic {
	switch d.Reason {
	case Unresolved, IdentityUnavailable, SourceUnknown, SourceStale, UnidentifiedEpisode, InvalidRange, VirtualIgnored,
		MovieUnavailable, SeriesUnavailable, CollectionUnavailable, SeasonUnavailable, CacheUnavailable,
		SuggestionsUnavailable, AIUnavailable, SourceUnavailable, StorageUnavailable, ScanFailed, UnfinishedScan, LegacyWarning,
		PendingCatalog, LegacyCatalog:
	default:
		d.Reason = LegacyWarning
	}
	switch d.Severity {
	case Warning, Error, Skip:
	default:
		d.Severity = Warning
	}
	switch d.Phase {
	case Inventory, Identity, Metadata, Persistence, Refresh, Trending, Similar, Upcoming, AI, Cleanup, "":
	default:
		d.Phase = ""
	}
	d.Subject, d.Current = SafeLabel(d.Subject), SafeLabel(d.Current)
	d.Query = safeEndpoint(d.Query)
	if d.Key == "" {
		d.Key = string(d.Reason) + "\x00" + d.Subject + "\x00" + d.Current + "\x00" + d.Query
	}
	d.Key = fmt.Sprintf("%x", sha256.Sum256([]byte(d.Key)))
	return d
}

// Report records a deduplicated reason independently of work-unit counters.
// A repeated event is shown once, even when observed in more than one phase.
func (r *Run) Report(d Diagnostic) {
	r.update(func(s *State) {
		if d.Phase == "" {
			d.Phase = s.Phase
		}
		d = cleanDiagnostic(d)
		for i := range s.Diagnostics {
			if s.Diagnostics[i].Key == d.Key {
				if s.Diagnostics[i].Previous || (s.Diagnostics[i].Severity != Error && d.Severity == Error) {
					s.Diagnostics[i] = d
				}
				return
			}
		}
		if len(s.Diagnostics) >= MaxDiagnostics {
			for i, previous := range s.Diagnostics {
				if previous.Previous {
					s.Diagnostics = append(s.Diagnostics[:i], s.Diagnostics[i+1:]...)
					break
				}
			}
			if len(s.Diagnostics) >= MaxDiagnostics {
				s.DiagnosticsTruncated = true
				if d.Severity == Error {
					for i := len(s.Diagnostics) - 1; i >= 0; i-- {
						if s.Diagnostics[i].Severity != Error {
							s.Diagnostics[i] = d
							return
						}
					}
				}
				return
			}
		}
		s.Diagnostics = append(s.Diagnostics, d)
	})
}

// Problem records the currently displayed operation without copying error text.
func (r *Run) Problem(reason Reason, severity Severity) {
	var d Diagnostic
	r.update(func(s *State) {
		d = Diagnostic{Reason: reason, Severity: severity, Phase: s.Phase, Subject: s.Subject, Current: s.Current, Query: s.Query}
	})
	r.Report(d)
}

func (s State) HasIssues() bool {
	return s.Status == Failed || s.Status == Partial || s.Status == Cancelled ||
		s.Failures > 0 || s.Skipped > 0 || s.Warnings > 0 || len(s.Diagnostics) > 0
}

func (s State) Severity() Severity {
	if s.Status == Waiting || s.Status == Idle {
		return ""
	}
	if s.Status == Failed || s.Failures > 0 || (s.Status == Running && s.PreviousSeverity == Error) {
		return Error
	}
	for _, d := range s.Diagnostics {
		if d.Severity == Error {
			return Error
		}
	}
	if s.HasIssues() || (s.Status == Running && s.PreviousSeverity != "") {
		return Warning
	}
	return ""
}

// LegacyDiagnostic recognizes only waim's own persisted warning formats.
// Unknown messages remain explicit but their arbitrary contents are not exposed.
func LegacyDiagnostic(message, sourceName string) Diagnostic {
	d := Diagnostic{Severity: Warning, Reason: LegacyWarning, Subject: sourceName}
	patterns := []struct {
		prefix string
		reason Reason
		phase  Phase
	}{
		{"Unresolved title: ", Unresolved, Identity},
		{"Movie metadata unavailable: ", MovieUnavailable, Metadata},
		{"Series metadata unavailable: ", SeriesUnavailable, Metadata},
		{"Collection metadata unavailable: ", CollectionUnavailable, Metadata},
		{"Season metadata unavailable: ", SeasonUnavailable, Metadata},
		{"Unidentified episode: ", UnidentifiedEpisode, Inventory},
		{"Invalid episode range (only the start episode counted): ", InvalidRange, Inventory},
		{"Ignored missing or virtual episodes: ", VirtualIgnored, Inventory},
		{"Ignored virtual title: ", VirtualIgnored, Inventory},
	}
	for _, p := range patterns {
		if i := strings.Index(message, p.prefix); i >= 0 {
			d.Reason, d.Phase, d.Current = p.reason, p.phase, message[i+len(p.prefix):]
			if p.reason == Unresolved {
				d.Severity = Skip
			}
			switch p.reason {
			case MovieUnavailable, SeriesUnavailable, CollectionUnavailable, SeasonUnavailable:
				d.Severity = Error
			}
			if i > 0 && sourceName == "" {
				d.Subject = strings.TrimSuffix(message[:i], ": ")
			}
			return cleanDiagnostic(d)
		}
	}
	for _, p := range []struct {
		suffix string
		reason Reason
	}{
		{": unknown inventory; refresh required", SourceUnknown},
		{": stale inventory (last successful snapshot)", SourceStale},
	} {
		if name, ok := strings.CutSuffix(message, p.suffix); ok {
			d.Subject, d.Reason, d.Phase = name, p.reason, Inventory
			return cleanDiagnostic(d)
		}
	}
	return cleanDiagnostic(d)
}

func (r *Run) ReportLegacy(message, sourceName string) {
	d := LegacyDiagnostic(message, sourceName)
	// Report hashes identities itself; preserve deduplication with direct
	// records by using the same display-based default identity.
	d.Key = ""
	r.Report(d)
}
