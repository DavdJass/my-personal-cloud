// Package ai provides an opt-in semantic search module backed by an
// OpenAI-compatible embeddings API (DeepSeek by default). It is intentionally
// isolated from the rest of the codebase: nothing in internal/files or
// internal/photos imports this package, and the cloud functions normally
// when the API key is missing or the upstream service is unreachable.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal HTTP wrapper around an OpenAI-compatible embeddings
// endpoint. Defaults target DeepSeek but the base URL is configurable so the
// same code can talk to OpenAI, a local Ollama instance, etc.
type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// NewClient builds a Client. Pass empty strings to fall back to defaults:
//   - baseURL: https://api.deepseek.com/v1
//   - model:   text-embedding-3-small
func NewClient(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		// 30s is generous for embeddings; the rest of the cloud is unaffected
		// even if requests hang because the AI module runs in its own goroutine.
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Model returns the embedding model name in use (exposed for /api/ai/status).
func (c *Client) Model() string { return c.model }

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed sends a single text string to the embeddings endpoint and returns
// the resulting vector. Network and decoding errors are wrapped with context.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, errors.New("ai: empty text")
	}
	body, err := json.Marshal(embedRequest{Model: c.model, Input: []string{text}})
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: request failed: %w", err)
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("ai: upstream %d: %s", res.StatusCode, string(raw))
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ai: decode response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("ai: api error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, errors.New("ai: empty embedding in response")
	}
	return parsed.Data[0].Embedding, nil
}
