package store

import "time"

// LibrarySummary captures per-library scan counts.
type LibrarySummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Scanned int    `json:"scanned"`
	Total   int    `json:"total"`
	Missing int    `json:"missing"`
}

// Finding kinds.
const (
	KindMissingSeason     = "missing_season"
	KindMissingEpisodes   = "missing_episodes"
	KindMissingCollection = "missing_collection"
)

// Media types.
const (
	MediaSeries = "series"
	MediaMovie  = "movie"
)

// Scan run statuses.
const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusError   = "error"
)

// ScanRun records the lifecycle and summary of a single scan.
type ScanRun struct {
	ID               int64            `json:"id"`
	StartedAt        time.Time        `json:"startedAt"`
	FinishedAt       *time.Time       `json:"finishedAt,omitempty"`
	Status           string           `json:"status"`
	Error            string           `json:"error,omitempty"`
	LibrariesScanned int              `json:"librariesScanned"`
	ItemsScanned     int              `json:"itemsScanned"`
	MissingCount     int              `json:"missingCount"`
	Libraries        []LibrarySummary `json:"libraries,omitempty"`
}

// Duration returns the run duration, or 0 if it has not finished.
func (r ScanRun) Duration() time.Duration {
	if r.FinishedAt == nil {
		return 0
	}
	return r.FinishedAt.Sub(r.StartedAt)
}

// Finding describes a single gap discovered during a scan.
type Finding struct {
	ID           int64     `json:"id"`
	ScanRunID    int64     `json:"scanRunId"`
	Kind         string    `json:"kind"`
	MediaType    string    `json:"mediaType"`
	LibraryID    string    `json:"libraryId"`
	LibraryName  string    `json:"libraryName"`
	Title        string    `json:"title"`
	TMDBID       int64     `json:"tmdbId,omitempty"`
	JellyfinID   string    `json:"jellyfinId,omitempty"`
	SeasonNumber *int      `json:"seasonNumber,omitempty"`
	Summary      string    `json:"summary"`
	Details      string    `json:"details,omitempty"` // JSON-encoded payload
	CreatedAt    time.Time `json:"createdAt"`
}

// SyncState is the exportable snapshot of the most recent completed scan.
type SyncState struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Run         *ScanRun  `json:"run"`
	Findings    []Finding `json:"findings"`
}
