// Package ai is a minimal client for an OpenAI-compatible chat-completions
// endpoint (e.g. Azure AI Foundry). It is used to generate library-based media
// suggestions. Authentication uses the Azure-style "api-key" header.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Suggestion is a single AI-generated recommendation.
type Suggestion struct {
	Title  string `json:"title"`
	Type   string `json:"type"` // "movie" or "series"
	Year   string `json:"year"`
	Reason string `json:"reason"`
}

// Client talks to an OpenAI-compatible chat-completions endpoint.
type Client struct {
	endpoint string
	apiKey   string
	model    string
	http     *http.Client
}

// New creates an AI client. endpoint must be the full chat-completions URL.
func New(endpoint, apiKey, model string) *Client {
	return &Client{
		endpoint: strings.TrimSpace(endpoint),
		apiKey:   strings.TrimSpace(apiKey),
		model:    strings.TrimSpace(model),
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

const systemPrompt = `You are a media recommendation assistant for a personal Jellyfin library.
Given a summary of the user's existing movies and TV series, recommend new titles
they likely do NOT already own, matching their taste and genres, plus a few popular picks.
Respond with ONLY a JSON object of the form:
{"suggestions":[{"title":"...","type":"movie|series","year":"YYYY","reason":"one short sentence"}]}
Return at most 12 suggestions. Do not include any prose outside the JSON.`

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model,omitempty"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Suggest sends the user prompt and returns parsed suggestions.
func (c *Client) Suggest(ctx context.Context, userPrompt string) ([]Suggestion, error) {
	if c.endpoint == "" || c.apiKey == "" {
		return nil, fmt.Errorf("ai: endpoint or api key not configured")
	}
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.7,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("ai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", c.apiKey)                 // Azure style
	req.Header.Set("Authorization", "Bearer "+c.apiKey) // OpenAI style (harmless if ignored)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai: endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("ai: decode response: %w", err)
	}
	if cr.Error != nil {
		return nil, fmt.Errorf("ai: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("ai: empty response")
	}
	return parseSuggestions(cr.Choices[0].Message.Content)
}

// parseSuggestions leniently extracts the JSON object from the model output.
func parseSuggestions(content string) ([]Suggestion, error) {
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("ai: no JSON object in response")
	}
	var wrapper struct {
		Suggestions []Suggestion `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &wrapper); err != nil {
		return nil, fmt.Errorf("ai: parse suggestions: %w", err)
	}
	return wrapper.Suggestions, nil
}
