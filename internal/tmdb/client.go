package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const baseURL = "https://api.themoviedb.org/3"

// Client talks to the TMDB API with a client-side rate limiter.
type Client struct {
	apiKey   string
	useBuilt bool // true when apiKey is a v4 bearer token
	language string
	region   string
	http     *http.Client
	limiter  *rate.Limiter
}

// New constructs a TMDB client. The rps argument bounds the request rate; a
// value <= 0 falls back to a conservative default.
func New(apiKey, language, region string, rps float64) *Client {
	apiKey = strings.TrimSpace(apiKey)
	if rps <= 0 {
		rps = 4
	}
	burst := int(math.Ceil(rps))
	if burst < 1 {
		burst = 1
	}
	if language == "" {
		language = "en-US"
	}
	return &Client{
		apiKey:   apiKey,
		useBuilt: strings.HasPrefix(apiKey, "eyJ"), // JWT => v4 bearer token
		language: language,
		region:   region,
		http:     &http.Client{Timeout: 20 * time.Second},
		limiter:  rate.NewLimiter(rate.Limit(rps), burst),
	}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.apiKey == "" {
		return fmt.Errorf("tmdb: API key not configured")
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	if q == nil {
		q = url.Values{}
	}
	if c.language != "" {
		q.Set("language", c.language)
	}
	if !c.useBuilt {
		q.Set("api_key", c.apiKey)
	}

	u := baseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("tmdb: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.useBuilt {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tmdb: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("tmdb: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("tmdb: decode %s: %w", path, err)
	}
	return nil
}

// ErrNotFound is returned when TMDB responds with HTTP 404.
var ErrNotFound = fmt.Errorf("tmdb: not found")

// Movie fetches movie details, including its collection reference if any.
func (c *Client) Movie(ctx context.Context, id int64) (Movie, error) {
	var m Movie
	err := c.get(ctx, "/movie/"+strconv.FormatInt(id, 10), nil, &m)
	return m, err
}

// Collection fetches a collection and its parts.
func (c *Client) Collection(ctx context.Context, id int64) (Collection, error) {
	var col Collection
	err := c.get(ctx, "/collection/"+strconv.FormatInt(id, 10), nil, &col)
	return col, err
}

// TV fetches TV show details, including the list of seasons.
func (c *Client) TV(ctx context.Context, id int64) (TVShow, error) {
	var tv TVShow
	err := c.get(ctx, "/tv/"+strconv.FormatInt(id, 10), nil, &tv)
	return tv, err
}

// Season fetches a single TV season including its episodes.
func (c *Client) Season(ctx context.Context, tvID int64, seasonNumber int) (Season, error) {
	var s Season
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tvID, seasonNumber), nil, &s)
	return s, err
}

// SearchMovie searches movies by title and optional year (0 to ignore).
func (c *Client) SearchMovie(ctx context.Context, title string, year int) ([]MovieSearchResult, error) {
	q := url.Values{}
	q.Set("query", title)
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	if c.region != "" {
		q.Set("region", c.region)
	}
	var resp movieSearchResponse
	if err := c.get(ctx, "/search/movie", q, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// SearchTV searches TV shows by name and optional first-air year (0 to ignore).
func (c *Client) SearchTV(ctx context.Context, name string, year int) ([]TVSearchResult, error) {
	q := url.Values{}
	q.Set("query", name)
	if year > 0 {
		q.Set("first_air_date_year", strconv.Itoa(year))
	}
	var resp tvSearchResponse
	if err := c.get(ctx, "/search/tv", q, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// Ping performs a lightweight authenticated request to validate credentials.
func (c *Client) Ping(ctx context.Context) error {
	return c.get(ctx, "/configuration", nil, nil)
}

// TrendingTV returns the trending TV shows for the week.
func (c *Client) TrendingTV(ctx context.Context) ([]MediaResult, error) {
	return c.trending(ctx, "tv")
}

// TrendingMovie returns the trending movies for the week.
func (c *Client) TrendingMovie(ctx context.Context) ([]MediaResult, error) {
	return c.trending(ctx, "movie")
}

func (c *Client) trending(ctx context.Context, kind string) ([]MediaResult, error) {
	var resp mediaResponse
	if err := c.get(ctx, "/trending/"+kind+"/week", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// TVRecommendations returns recommended TV shows for the given show.
func (c *Client) TVRecommendations(ctx context.Context, id int64) ([]MediaResult, error) {
	return c.recommendations(ctx, "tv", id)
}

// MovieRecommendations returns recommended movies for the given movie.
func (c *Client) MovieRecommendations(ctx context.Context, id int64) ([]MediaResult, error) {
	return c.recommendations(ctx, "movie", id)
}

func (c *Client) recommendations(ctx context.Context, kind string, id int64) ([]MediaResult, error) {
	var resp mediaResponse
	if err := c.get(ctx, fmt.Sprintf("/%s/%d/recommendations", kind, id), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}
