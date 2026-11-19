package tmdb

import "regexp"

var titleEndpoint = regexp.MustCompile(`^(/(?:movie|tv|collection)/[1-9][0-9]*)(/recommendations|/season/[0-9]+)?$`)

// TitlePaths maps a title-specific API endpoint to the parent title and
// public page. Never forward a raw cache key, search term or credential.
func TitlePaths(path string) (parent, page string) {
	m := titleEndpoint.FindStringSubmatch(path)
	if m == nil {
		return "", ""
	}
	parent, page = m[1], m[1]
	if m[2] != "/recommendations" {
		page += m[2]
	}
	return parent, page
}
