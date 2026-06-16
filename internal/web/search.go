package web

import (
	"net/url"
	"strings"
)

// BuildSearchURL renders an external search URL from a template by replacing the
// {query} placeholder with the URL-encoded query. It returns an empty string
// when the template is empty, lacks a {query} placeholder, the query is empty,
// or the result does not resolve to an http(s) URL.
func BuildSearchURL(template, query string) string {
	template = strings.TrimSpace(template)
	query = strings.TrimSpace(query)
	if template == "" || query == "" || !strings.Contains(template, "{query}") {
		return ""
	}
	raw := strings.ReplaceAll(template, "{query}", url.QueryEscape(query))
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}
