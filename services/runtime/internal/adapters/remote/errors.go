// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Problem is the RFC 7807 envelope every adapter returns on non-2xx
// responses (see docs/adapters/PROTOCOL.md §5). It also implements
// error so callers can return it from interface methods directly.
type Problem struct {
	Type              string `json:"type,omitempty"`
	Title             string `json:"title,omitempty"`
	Status            int    `json:"status,omitempty"`
	Detail            string `json:"detail,omitempty"`
	Code              string `json:"code,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`

	// HTTPStatus carries the actual response status code as observed
	// over the wire, which can disagree with Status (a misbehaving
	// adapter might leave Status zero). Use HTTPStatus for branching.
	HTTPStatus int `json:"-"`
}

func (p *Problem) Error() string {
	if p == nil {
		return "<nil problem>"
	}
	if p.Detail != "" {
		return fmt.Sprintf("adapter error %d %s: %s", p.HTTPStatus, p.Code, p.Detail)
	}
	if p.Title != "" {
		return fmt.Sprintf("adapter error %d %s: %s", p.HTTPStatus, p.Code, p.Title)
	}
	return fmt.Sprintf("adapter error %d %s", p.HTTPStatus, p.Code)
}

// IsTransient reports whether the problem should be retried.
// Retryable: 408 Request Timeout, 425 Too Early, 429 Too Many Requests,
// 500 Internal Server Error, 502 Bad Gateway, 503 Service Unavailable,
// 504 Gateway Timeout. Everything else is not retried.
func (p *Problem) IsTransient() bool {
	if p == nil {
		return false
	}
	switch p.HTTPStatus {
	case http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// AsProblem extracts a *Problem from an error chain. Returns nil and
// false when the error isn't an adapter problem.
func AsProblem(err error) (*Problem, bool) {
	var p *Problem
	if errors.As(err, &p) {
		return p, true
	}
	return nil, false
}

// IsCode reports whether err is an adapter problem with the given
// machine-readable code. Convenience for switch-like branching.
//
//	if remote.IsCode(err, "object_not_found") { ... }
func IsCode(err error, code string) bool {
	p, ok := AsProblem(err)
	return ok && p.Code == code
}

// decodeProblem reads up to 64 KiB of response body and parses it as a
// Problem. Status is filled from the HTTP response status code. If the
// body isn't valid JSON we fabricate a Problem from the status line so
// callers still get something they can branch on.
func decodeProblem(resp *http.Response) *Problem {
	p := &Problem{HTTPStatus: resp.StatusCode}

	// Cap the body so a misbehaving adapter can't OOM the runtime.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if len(body) > 0 {
		_ = json.Unmarshal(body, p)
	}

	if p.Status == 0 {
		p.Status = resp.StatusCode
	}
	if p.Code == "" {
		p.Code = codeFromStatus(resp.StatusCode)
	}
	if p.Title == "" {
		p.Title = http.StatusText(resp.StatusCode)
	}
	return p
}

// codeFromStatus is the fallback machine code we synthesise when the
// adapter forgot to set one. Keeps callers from special-casing nil/empty.
func codeFromStatus(s int) string {
	switch s {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "adapter_unavailable"
	case http.StatusBadGateway:
		return "upstream_error"
	case http.StatusGatewayTimeout:
		return "upstream_timeout"
	}
	if s >= 500 {
		return "internal_error"
	}
	if s >= 400 {
		return "client_error"
	}
	return ""
}
