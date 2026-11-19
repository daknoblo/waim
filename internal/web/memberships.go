package web

import (
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func libraryMemberships(p store.Provenance, primaryID, primaryName string) []media.Library {
	out := []media.Library{}
	seen := map[string]bool{}
	add := func(refs []media.Reference) {
		for _, ref := range refs {
			if ref.LibraryID == "" || seen[ref.LibraryID] {
				continue
			}
			seen[ref.LibraryID] = true
			name := ref.LibraryName
			if ref.LibraryID == primaryID {
				name = primaryName
			}
			out = append(out, media.Library{ID: ref.LibraryID, Name: name})
		}
	}
	add(p.References)
	add(p.ContextReferences)
	if len(out) == 0 {
		out = append(out, media.Library{ID: primaryID, Name: primaryName})
	}
	return out
}

func titleIdentity(kind string, id int64, catalogID, legacyID, title string) string {
	if id > 0 {
		return media.Key(kind, id)
	}
	if catalogID != "" {
		return kind + ":local:" + catalogID
	}
	if legacyID != "" {
		return kind + ":legacy:" + legacyID
	}
	return kind + ":title:" + title
}

func findingTitleIdentity(f store.Finding) string {
	kind := f.MediaType
	if f.Kind == store.KindMissingCollection {
		kind = "collection"
	}
	return titleIdentity(kind, f.TMDBID, f.CatalogID, f.JellyfinID, f.Title)
}

// A normalized scan has one stat per canonical title. Defensive de-duplication
// also handles repeated membership rows, without merging unidentified names.
func distinctMediaStats(stats []store.MediaStat) []store.MediaStat {
	out := []store.MediaStat{}
	index := map[string]int{}
	for _, m := range stats {
		key := titleIdentity(m.Type, m.TMDBID, m.CatalogID, m.JellyfinID, m.Title)
		if m.TMDBID <= 0 && m.CatalogID == "" && m.JellyfinID == "" {
			out = append(out, m)
			continue
		}
		if i, ok := index[key]; ok {
			refs := append(append([]media.Reference(nil), out[i].References...), m.References...)
			unconfirmed := out[i].Unconfirmed || m.Unconfirmed
			if out[i].WatchOnly && !m.WatchOnly {
				out[i] = m
			}
			out[i].References = refs
			out[i].Unconfirmed = unconfirmed
			continue
		}
		index[key] = len(out)
		out = append(out, m)
	}
	return out
}

// Expand only the per-library rating input, never the global finding counters.
func membershipFindings(findings []store.Finding, libraries []store.LibrarySummary) []store.Finding {
	names := map[string]string{}
	for _, lib := range libraries {
		names[lib.ID] = lib.Name
	}
	out := []store.Finding{}
	for _, f := range findings {
		for _, lib := range libraryMemberships(f.Provenance, f.LibraryID, f.LibraryName) {
			if lib.ID == media.VirtualID && !f.WatchOnly {
				continue
			}
			copy := f
			copy.LibraryID, copy.LibraryName = lib.ID, lib.Name
			if name, ok := names[lib.ID]; ok {
				copy.LibraryName = name
			}
			out = append(out, copy)
		}
	}
	return out
}
