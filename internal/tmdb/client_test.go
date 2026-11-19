package tmdb

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type cachedBodies map[string]string

func (c cachedBodies) Get(_ context.Context, key string) ([]byte, bool, error) {
	body, ok := c[key]
	return []byte(body), ok, nil
}

func (c cachedBodies) Put(_ context.Context, key string, body []byte) error {
	c[key] = string(body)
	return nil
}

type responseBody string

func (b responseBody) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(b))), Request: req}, nil
}

func TestInvalidResponsesDoNotReplaceCache(t *testing.T) {
	const key = "/movie/1?language=en-US"
	const previous = `{"id":1,"title":"Previous"}`
	for name, body := range map[string]string{
		"html": "<html>", "truncated": `{"id":`, "null": "null",
		"oversized valid prefix": `{}` + strings.Repeat(" ", maxResponseBytes),
	} {
		t.Run(name, func(t *testing.T) {
			cache := cachedBodies{}
			c := New("fixture", "", "", 50).WithCache(cache)
			c.http.Transport = responseBody(body)
			if _, err := c.Movie(t.Context(), 1); err == nil || len(cache) != 0 {
				t.Fatal("invalid live response accepted or cached")
			}
			cache[key] = previous
			if err := c.RefreshKey(t.Context(), key); err == nil || cache[key] != previous {
				t.Fatal("bad refresh accepted or previous response overwritten")
			}
		})
	}
}

func TestClientsShareOneRateBudget(t *testing.T) {
	a := New("fake-a", "en-US", "US", 1)
	b := New("fake-b", "de-DE", "DE", 1)
	if a.limiter != b.limiter {
		t.Fatal("clients multiplied the configured request budget")
	}
	if a.limiter.Limit() != 1 {
		t.Fatal("shared budget ignored configured rate")
	}
}
