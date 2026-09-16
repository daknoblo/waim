package scanner

import "github.com/daknoblo/waim/internal/media"

// A merged title is one global work unit but may belong to several libraries.
// Multiple occurrences within the same library still count only once.
func itemLibraries(item media.Item, fallback string) []string {
	ids := []string{fallback}
	seen := map[string]bool{fallback: true}
	for _, ref := range item.References {
		if !seen[ref.LibraryID] {
			ids = append(ids, ref.LibraryID)
			seen[ref.LibraryID] = true
		}
	}
	return ids
}
