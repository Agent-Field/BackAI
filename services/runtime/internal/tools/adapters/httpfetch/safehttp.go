// SPDX-License-Identifier: Apache-2.0

// Package httpfetch implements the HTTP-fetch ToolAdapter. It wraps
// internal/safehttp so SSRF guards apply: private CIDRs, link-local IPs,
// cloud-metadata endpoints, and DNS-rebinding attacks are blocked.
//
// Two verbs: get(url, headers?) and post(url, headers?, body).
package httpfetch

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

	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
)

// ErrInvalidArgs is returned when the caller's args don't form a valid
// request (missing URL, bad method, etc.).
var ErrInvalidArgs = errors.New("httpfetch: invalid arguments")

// Adapter wraps a safehttp client.
type Adapter struct {
	client *http.Client
}

// New constructs an Adapter with the given timeout. Pass 0 for the
// default (30s).
func New(timeout time.Duration) *Adapter {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Adapter{client: safehttp.New(safehttp.Options{Timeout: timeout})}
}

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "safehttp" }

// Configured is always true — safehttp ships with the runtime.
func (a *Adapter) Configured() bool { return true }

// Response is the canonical return shape from httpfetch verbs.
type Response struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// Get issues a GET. headers is optional.
func (a *Adapter) Get(ctx context.Context, url string, headers map[string]string) (Response, error) {
	return a.do(ctx, http.MethodGet, url, headers, nil)
}

// Post issues a POST. body is JSON-marshalled; pass nil to send no body.
func (a *Adapter) Post(ctx context.Context, url string, headers map[string]string, body any) (Response, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("%w: body marshal: %v", ErrInvalidArgs, err)
		}
		reader = bytes.NewReader(raw)
	}
	return a.do(ctx, http.MethodPost, url, headers, reader)
}

func (a *Adapter) do(ctx context.Context, method, url string, headers map[string]string, body io.Reader) (Response, error) {
	if strings.TrimSpace(url) == "" {
		return Response{}, fmt.Errorf("%w: url required", ErrInvalidArgs)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Response{}, err
	}
	flat := map[string]string{}
	for k, vals := range resp.Header {
		flat[k] = strings.Join(vals, ",")
	}
	return Response{
		StatusCode: resp.StatusCode,
		Headers:    flat,
		Body:       string(raw),
	}, nil
}
