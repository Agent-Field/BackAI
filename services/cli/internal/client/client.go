// SPDX-License-Identifier: Apache-2.0

// Package client is the thin REST helper that powers `af-stack <subcommand>`.
//
// The CLI never talks to the database or any internal package directly —
// every action is a single HTTP call to the runtime, exactly the way an
// operator's curl would do it. This keeps the CLI a fair test client: if
// it works, the REST surface works.
//
// Base URL resolution mirrors the SDKs:
//
//   - AF_STACK_URL (default "http://localhost:8080")
//
// Auth: AF_STACK_API_KEY sets the bearer token when present.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "http://localhost:8080"
	apiPathPrefix   = "/api/v1"
	defaultTimeoutS = 30
)

// Client is a tiny REST helper. One instance per CLI invocation is fine.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New constructs a Client using env defaults. Pass overrides via the
// returned struct fields if you need to (tests do this).
func New() *Client {
	base := strings.TrimRight(envOr("AF_STACK_URL", defaultBaseURL), "/")
	return &Client{
		BaseURL: base,
		APIKey:  os.Getenv("AF_STACK_API_KEY"),
		HTTP:    &http.Client{Timeout: defaultTimeoutS * time.Second},
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// ErrorEnvelope is the structured error shape returned by the runtime.
type ErrorEnvelope struct {
	Error struct {
		Code      string          `json:"code"`
		Message   string          `json:"message"`
		RequestID string          `json:"request_id"`
		Details   json.RawMessage `json:"details"`
	} `json:"error"`
}

// APIError is what we return on non-2xx responses.
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("[%s] %s (status=%d)", e.Code, e.Message, e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
}

// Probe issues a GET to a ROOT-level runtime path (NOT prefixed with
// /api/v1) — used for liveness/readiness probes at /health and /ready,
// which the runtime serves off the root. It returns the HTTP status and
// the raw body. A transport error (runtime unreachable) is returned as
// err with status 0, so callers can distinguish "down" from "responded
// non-2xx" and degrade gracefully.
func (c *Client) Probe(ctx context.Context, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+path, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

// Do issues an HTTP request, sending JSON when body is non-nil, and
// decodes the response into out (pass nil to ignore the body).
func (c *Client) Do(
	ctx context.Context,
	method, path string,
	body any,
	out any,
) error {
	url := c.BaseURL + apiPathPrefix + path
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		apiErr := &APIError{
			Status:  resp.StatusCode,
			Message: string(respBody),
		}
		var env ErrorEnvelope
		if jsonErr := json.Unmarshal(respBody, &env); jsonErr == nil && env.Error.Code != "" {
			apiErr.Code = env.Error.Code
			apiErr.Message = env.Error.Message
			apiErr.RequestID = env.Error.RequestID
		}
		return apiErr
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w (body=%q)", err, string(respBody))
	}
	return nil
}
