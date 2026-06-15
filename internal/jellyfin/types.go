// Package jellyfin is a minimal, read-only client for the Jellyfin REST API. It
// retrieves libraries, movies, series and episodes including their external
// provider IDs (TMDB/IMDb/TVDB).
package jellyfin

// SystemInfo is a subset of the /System/Info response used to verify
// connectivity and display the server version.
type SystemInfo struct {
	ServerName string `json:"ServerName"`
	Version    string `json:"Version"`
	ID         string `json:"Id"`
}

// User is a subset of the /Users response.
type User struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

// Library represents a top-level media folder (collection folder).
type Library struct {
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	CollectionType string `json:"CollectionType"` // movies, tvshows, ...
}

// Item is a movie, series, box set or episode.
type Item struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"` // Movie, Series, BoxSet, Episode
	ProductionYear    int               `json:"ProductionYear"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	SeriesID          string            `json:"SeriesId"`
	SeriesName        string            `json:"SeriesName"`
	IndexNumber       *int              `json:"IndexNumber"`       // episode number
	ParentIndexNumber *int              `json:"ParentIndexNumber"` // season number
}

// itemsResult is the common envelope returned by item queries.
type itemsResult struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

// ProviderID returns the value of a provider ID (case-insensitive key match)
// and whether it was present.
func (i Item) ProviderID(name string) (string, bool) {
	for k, v := range i.ProviderIDs {
		if equalFold(k, name) && v != "" {
			return v, true
		}
	}
	return "", false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
