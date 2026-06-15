package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const pageSize = 500

// Client is a read-only Jellyfin API client.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New creates a client for the given base URL and API key.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.baseURL == "" {
		return fmt.Errorf("jellyfin: base URL not configured")
	}
	if c.apiKey == "" {
		return fmt.Errorf("jellyfin: API key not configured")
	}
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("jellyfin: build request: %w", err)
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("jellyfin: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jellyfin: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("jellyfin: decode %s: %w", path, err)
	}
	return nil
}

// SystemInfo verifies connectivity and returns basic server information.
func (c *Client) SystemInfo(ctx context.Context) (SystemInfo, error) {
	var si SystemInfo
	err := c.get(ctx, "/System/Info", nil, &si)
	return si, err
}

// Users lists configured Jellyfin users.
func (c *Client) Users(ctx context.Context) ([]User, error) {
	var users []User
	if err := c.get(ctx, "/Users", nil, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// ResolveUserID returns the configured user ID, or the first available user's
// ID when none is configured.
func (c *Client) ResolveUserID(ctx context.Context, configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		return configured, nil
	}
	users, err := c.Users(ctx)
	if err != nil {
		return "", err
	}
	if len(users) == 0 {
		return "", fmt.Errorf("jellyfin: no users available")
	}
	return users[0].ID, nil
}

// Libraries returns the top-level media folders.
func (c *Client) Libraries(ctx context.Context) ([]Library, error) {
	var res itemsResultLibraries
	if err := c.get(ctx, "/Library/MediaFolders", nil, &res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

// itemsResultLibraries is the envelope for /Library/MediaFolders.
type itemsResultLibraries struct {
	Items []Library `json:"Items"`
}

// ItemsInLibrary returns all movies, series and box sets in a library,
// transparently paging through the result set.
func (c *Client) ItemsInLibrary(ctx context.Context, userID, libraryID string) ([]Item, error) {
	var all []Item
	start := 0
	for {
		q := url.Values{}
		q.Set("ParentId", libraryID)
		q.Set("Recursive", "true")
		q.Set("IncludeItemTypes", "Movie,Series,BoxSet")
		q.Set("Fields", "ProviderIds,ProductionYear")
		q.Set("EnableImages", "false")
		q.Set("StartIndex", strconv.Itoa(start))
		q.Set("Limit", strconv.Itoa(pageSize))
		if userID != "" {
			q.Set("userId", userID)
		}
		var res itemsResult
		if err := c.get(ctx, "/Items", q, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Items...)
		start += len(res.Items)
		if len(res.Items) == 0 || start >= res.TotalRecordCount {
			break
		}
	}
	return all, nil
}

// Episodes returns all episodes of a series with their season/episode numbers.
func (c *Client) Episodes(ctx context.Context, userID, seriesID string) ([]Item, error) {
	q := url.Values{}
	q.Set("Fields", "ProviderIds")
	q.Set("EnableImages", "false")
	if userID != "" {
		q.Set("userId", userID)
	}
	var res itemsResult
	if err := c.get(ctx, "/Shows/"+url.PathEscape(seriesID)+"/Episodes", q, &res); err != nil {
		return nil, err
	}
	return res.Items, nil
}
