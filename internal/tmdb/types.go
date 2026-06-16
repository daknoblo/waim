// Package tmdb is a small client for The Movie Database (TMDB) API v3 with a
// configurable client-side rate limiter. It supports both v3 API keys and v4
// bearer tokens; the credential format is auto-detected.
package tmdb

// CollectionRef is the lightweight collection reference embedded in a movie.
type CollectionRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Genre is a TMDB genre.
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Movie is a subset of the /movie/{id} response.
type Movie struct {
	ID                  int64          `json:"id"`
	Title               string         `json:"title"`
	ReleaseDate         string         `json:"release_date"`
	VoteAverage         float64        `json:"vote_average"`
	Runtime             int            `json:"runtime"`
	Genres              []Genre        `json:"genres"`
	BelongsToCollection *CollectionRef `json:"belongs_to_collection"`
}

// CollectionPart is a single entry within a collection.
type CollectionPart struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"release_date"`
	VoteAverage float64 `json:"vote_average"`
}

// Collection is a subset of the /collection/{id} response.
type Collection struct {
	ID         int64            `json:"id"`
	Name       string           `json:"name"`
	PosterPath string           `json:"poster_path"`
	Parts      []CollectionPart `json:"parts"`
}

// SeasonSummary is the per-season summary embedded in a TV show.
type SeasonSummary struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
}

// TVShow is a subset of the /tv/{id} response.
type TVShow struct {
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	NumberOfSeasons int             `json:"number_of_seasons"`
	VoteAverage     float64         `json:"vote_average"`
	PosterPath      string          `json:"poster_path"`
	FirstAirDate    string          `json:"first_air_date"`
	EpisodeRunTime  []int           `json:"episode_run_time"`
	Genres          []Genre         `json:"genres"`
	Seasons         []SeasonSummary `json:"seasons"`
}

// Episode is a subset of an episode in a season response.
type Episode struct {
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
}

// Season is a subset of the /tv/{id}/season/{n} response.
type Season struct {
	SeasonNumber int       `json:"season_number"`
	Episodes     []Episode `json:"episodes"`
}

// MovieSearchResult is a single result from /search/movie.
type MovieSearchResult struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ReleaseDate string `json:"release_date"`
}

// TVSearchResult is a single result from /search/tv.
type TVSearchResult struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	FirstAirDate string `json:"first_air_date"`
}

type movieSearchResponse struct {
	Results []MovieSearchResult `json:"results"`
}

type tvSearchResponse struct {
	Results []TVSearchResult `json:"results"`
}

// MediaResult is a unified result item from trending/recommendation endpoints,
// covering both movies and TV shows.
type MediaResult struct {
	ID           int64   `json:"id"`
	MediaType    string  `json:"media_type"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	VoteAverage  float64 `json:"vote_average"`
	Popularity   float64 `json:"popularity"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
}

type mediaResponse struct {
	Results []MediaResult `json:"results"`
}

// DisplayTitle returns the movie title or TV name.
func (m MediaResult) DisplayTitle() string {
	if m.Name != "" {
		return m.Name
	}
	return m.Title
}

// Year returns the 4-digit release/first-air year, if available.
func (m MediaResult) Year() string {
	d := m.ReleaseDate
	if d == "" {
		d = m.FirstAirDate
	}
	if len(d) >= 4 {
		return d[:4]
	}
	return ""
}
