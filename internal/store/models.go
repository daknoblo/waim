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

// MediaStat captures TMDB metadata of an owned title for statistics.
type MediaStat struct {
	Type           string       `json:"type"` // movie | series
	Title          string       `json:"title"`
	Year           int          `json:"year"`
	Rating         float64      `json:"rating"`
	Runtime        int          `json:"runtime"` // minutes (per movie, per episode for series)
	Genres         []string     `json:"genres"`
	LibraryID      string       `json:"libraryId"`
	LibraryName    string       `json:"libraryName"`
	TMDBID         int64        `json:"tmdbId,omitempty"`
	JellyfinID     string       `json:"jellyfinId,omitempty"`
	Language       string       `json:"language,omitempty"`      // ISO 639-1 original language
	Country        string       `json:"country,omitempty"`       // ISO 3166-1 production country
	Episodes       int          `json:"episodes,omitempty"`      // owned episodes (series)
	TotalEpisodes  int          `json:"totalEpisodes,omitempty"` // episodes known to TMDB (series)
	Minutes        int          `json:"minutes,omitempty"`       // runtime of the owned episodes (series)
	Seasons        []SeasonStat `json:"seasons,omitempty"`       // seasons known to TMDB (series)
	CollectionID   int64        `json:"collectionId,omitempty"`  // TMDB collection (movies)
	CollectionName string       `json:"collectionName,omitempty"`
}

// SeasonStat captures how many episodes of a season are owned and, when episode
// ratings are enabled, how each episode of that season is rated.
type SeasonStat struct {
	Number   int             `json:"number"`
	Episodes int             `json:"episodes"`
	Total    int             `json:"total"`
	Rating   float64         `json:"rating,omitempty"` // average episode rating
	Ratings  []EpisodeRating `json:"ratings,omitempty"`
}

// EpisodeRating is the TMDB rating of a single episode.
type EpisodeRating struct {
	Number  int     `json:"number"`
	Title   string  `json:"title,omitempty"`
	Rating  float64 `json:"rating"`
	Minutes int     `json:"minutes,omitempty"`
	Owned   bool    `json:"owned,omitempty"`
}

// UpcomingItem is an announced release derived from a title already in the
// library: a future episode of an owned series or an unreleased part of an
// owned movie collection.
type UpcomingItem struct {
	Kind          string  `json:"kind"`      // episode | collection_part
	MediaType     string  `json:"mediaType"` // series | movie
	Title         string  `json:"title"`
	SourceTitle   string  `json:"sourceTitle"` // owned series / collection it derives from
	SourceTMDBID  int64   `json:"sourceTmdbId,omitempty"`
	TMDBID        int64   `json:"tmdbId,omitempty"`
	SeasonNumber  int     `json:"seasonNumber,omitempty"`
	EpisodeNumber int     `json:"episodeNumber,omitempty"`
	ReleaseDate   string  `json:"releaseDate,omitempty"` // ISO 8601 date, empty when unannounced
	PosterPath    string  `json:"posterPath,omitempty"`
	Overview      string  `json:"overview,omitempty"`
	Rating        float64 `json:"rating,omitempty"`
	LibraryID     string  `json:"libraryId,omitempty"`
	LibraryName   string  `json:"libraryName,omitempty"`
	JellyfinID    string  `json:"jellyfinId,omitempty"`
}

// Upcoming kinds.
const (
	UpcomingEpisode        = "episode"
	UpcomingCollectionPart = "collection_part"
)

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
	Media            []MediaStat      `json:"media,omitempty"`
	Upcoming         []UpcomingItem   `json:"upcoming,omitempty"`
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
