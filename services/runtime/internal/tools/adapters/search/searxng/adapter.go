// SPDX-License-Identifier: Apache-2.0

// Package searxng implements the SearXNG SearchAdapter. SearXNG is a
// self-hostable metasearch engine; the operator runs an instance and
// points SEARXNG_URL at it. We do NOT ship a SearXNG container in
// docker-compose — operators with privacy requirements run their own.
//
// Wire shape: GET <base>/search?q=...&format=json
package searxng

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search"
)

// Adapter is the SearXNG client. Construct via New().
type Adapter struct {
	baseURL string
	client  *http.Client
}

// New returns a SearXNG adapter for the given instance URL. Empty URL
// produces an unconfigured adapter.
func New(baseURL string) *Adapter {
	return &Adapter{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  safehttp.New(safehttp.Options{Timeout: 30 * time.Second}),
	}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "searxng" }

// Configured reports whether the instance URL is set.
func (a *Adapter) Configured() bool { return a != nil && a.baseURL != "" }

type rawResponse struct {
	Results []struct {
		URL     string  `json:"url"`
		Title   string  `json:"title"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	} `json:"results"`
}

// Search runs one query and returns up to maxResults hits.
func (a *Adapter) Search(ctx context.Context, query string, maxResults int) ([]search.Result, error) {
	if !a.Configured() {
		return nil, search.ErrNotConfigured
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: query required", search.ErrNotConfigured)
	}
	endpoint, err := url.Parse(a.baseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("searxng: invalid url: %w", err)
	}
	qs := endpoint.Query()
	qs.Set("q", query)
	qs.Set("format", "json")
	endpoint.RawQuery = qs.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("searxng: request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng: do: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("searxng: read: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("searxng: status %d", resp.StatusCode)
	}
	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("searxng: unmarshal: %w", err)
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	out := make([]search.Result, 0, len(raw.Results))
	for i, r := range raw.Results {
		if i >= maxResults {
			break
		}
		out = append(out, search.Result{
			URL:     r.URL,
			Title:   r.Title,
			Snippet: r.Content,
			Score:   r.Score,
		})
	}
	return out, nil
}
