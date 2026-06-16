package web

import (
	"net/url"
	"strings"
)

// BuildSearchURL renders an external search URL from a template by replacing the
// {query} placeholder with the URL-encoded query and the optional {key}
// placeholder with the URL-encoded key. It returns an empty string when the
// template is empty, lacks a {query} placeholder, the query is empty, or the
// result does not resolve to an http(s) URL.
func BuildSearchURL(template, key, query string) string {
	template = strings.TrimSpace(template)
	query = strings.TrimSpace(query)
	if template == "" || query == "" || !strings.Contains(template, "{query}") {
		return ""
	}
	raw := strings.ReplaceAll(template, "{query}", url.QueryEscape(query))
	raw = strings.ReplaceAll(raw, "{key}", url.QueryEscape(key))
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

// searchRedirectPath builds the internal redirect link for a finding's search
// query. The real provider URL (and any API key) is assembled server-side by the
// /search handler, so the key never appears in the rendered page.
func searchRedirectPath(enabled bool, query string) string {
	query = strings.TrimSpace(query)
	if !enabled || query == "" {
		return ""
	}
	return "/search?q=" + url.QueryEscape(query)
}
