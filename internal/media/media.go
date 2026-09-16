// Package media contains credential-free, provider-independent catalog data.
package media

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// Version 1 excludes Jellyfin placeholders and expands combined episodes.
	// Changing normalization invalidates old physical inventories, not sources.
	JellyfinInventoryVersion = 1
	Jellyfin                 = "jellyfin"
	Virtual                  = "virtual"
	VirtualID                = "virtual"
	VirtualName              = "Virtual collection"
	Movie                    = "Movie"
	Series                   = "Series"
)

type Reference struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	LibraryID   string `json:"libraryId,omitempty"`
	LibraryName string `json:"libraryName,omitempty"`
	ItemID      string `json:"itemId,omitempty"`
	URL         string `json:"url,omitempty"`
	ServerURL   string `json:"serverUrl,omitempty"`
	Stale       bool   `json:"stale,omitempty"`
}

// Item is a normalized occurrence. Index fields apply only to episodes.
type Item struct {
	ResolvedTMDBID    int64             `json:"resolvedTmdbId,omitempty"`
	ResolutionInput   string            `json:"resolutionInput,omitempty"`
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"`
	ProductionYear    int               `json:"ProductionYear"`
	ProviderIDs       map[string]string `json:"ProviderIds,omitempty"`
	SeriesID          string            `json:"SeriesId,omitempty"`
	SeriesName        string            `json:"SeriesName,omitempty"`
	IndexNumber       *int              `json:"IndexNumber,omitempty"`
	ParentIndexNumber *int              `json:"ParentIndexNumber,omitempty"`
	References        []Reference       `json:"references,omitempty"`
	WatchOnly         bool              `json:"watchOnly,omitempty"`
	Episodes          []Item            `json:"episodes,omitempty"`
}

func (i Item) ProviderID(name string) (string, bool) {
	for k, v := range i.ProviderIDs {
		if strings.EqualFold(k, name) && v != "" {
			return v, true
		}
	}
	return "", false
}

func (i Item) TMDBID() int64 {
	raw, _ := i.ProviderID("tmdb")
	id, _ := strconv.ParseInt(raw, 10, 64)
	if id > 0 {
		return id
	}
	if i.ResolvedTMDBID > 0 && i.ResolutionInput == i.IdentityInput() {
		return i.ResolvedTMDBID
	}
	return 0
}

// IdentityInput binds a verified resolution to the source-local occurrence and
// its original identity metadata. Episode changes and display-name edits do not
// invalidate it; reusing a local ID for a different title does.
func (i Item) IdentityInput() string {
	data, _ := json.Marshal([]any{i.ID, i.Type, i.Name, i.ProductionYear, i.ProviderIDs})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (i Item) WithResolvedID(id int64) Item {
	i.ResolutionInput = i.IdentityInput()
	i.ResolvedTMDBID = id
	return i
}

func Qualify(source, local string) string {
	return url.PathEscape(source) + "/" + url.PathEscape(local)
}

func Key(kind string, id int64) string { return fmt.Sprintf("%s:%d", strings.ToLower(kind), id) }

func (i Item) Key() string {
	if id := i.TMDBID(); id > 0 {
		return Key(i.Type, id)
	}
	return "local:" + i.ID
}

type Library struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Snapshot struct {
	Warnings  []string  `json:"warnings,omitempty"`
	Items     []Item    `json:"items"`
	Libraries []Library `json:"libraries"`
}

type Catalog struct {
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	Items     []Item    `json:"items"`
	Libraries []Library `json:"libraries"`
	Warnings  []string  `json:"warnings,omitempty"`
	Revision  int64     `json:"revision"`
}

// Merge unions real episodes by season/episode, not by server-local file ID.
// Unresolved identities stay source-local. Real ownership wins over tracking.
func Merge(items []Item) []Item {
	out := []Item{}
	index := map[string]int{}
	for _, item := range items {
		key := item.Key()
		if n, ok := index[key]; ok {
			dst := &out[n]
			refs := append(append([]Reference(nil), dst.References...), item.References...)
			eps := append(append([]Item(nil), dst.Episodes...), item.Episodes...)
			if dst.WatchOnly && !item.WatchOnly {
				*dst = item
			}
			dst.References = uniqueRefs(refs)
			dst.Episodes = uniqueEpisodes(eps)
			dst.WatchOnly = dst.WatchOnly && item.WatchOnly
		} else {
			index[key] = len(out)
			item.References = uniqueRefs(item.References)
			item.Episodes = uniqueEpisodes(item.Episodes)
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

func uniqueRefs(refs []Reference) []Reference {
	out := []Reference{}
	seen := map[string]bool{}
	for _, r := range refs {
		key := Qualify(r.ID, r.LibraryID) + "/" + r.ItemID
		if !seen[key] {
			out = append(out, r)
			seen[key] = true
		}
	}
	return out
}

func uniqueEpisodes(eps []Item) []Item {
	out := []Item{}
	seen := map[[2]int]bool{}
	for _, ep := range eps {
		if ep.ParentIndexNumber == nil || ep.IndexNumber == nil {
			continue
		}
		key := [2]int{*ep.ParentIndexNumber, *ep.IndexNumber}
		if !seen[key] {
			out = append(out, ep)
			seen[key] = true
		}
	}
	return out
}

func Link(refs []Reference) string {
	for _, r := range refs {
		if r.Type != Virtual && r.URL != "" {
			return r.URL
		}
	}
	for _, r := range refs {
		if r.URL != "" {
			return r.URL
		}
	}
	return ""
}
