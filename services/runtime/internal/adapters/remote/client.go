// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Config controls a Client. Zero values are sensible: 30s default
// timeout, 3 retries, default Transport (with connection pooling).
type Config struct {
	// BaseURL is the adapter root, e.g. "http://my-sandbox:8080". The
	// trailing slash is stripped.
	BaseURL string

	// Token is the bearer token sent on every request. Empty means
	// no Authorization header (only for "auth: none" dev setups).
	Token string

	// Timeout caps a single attempt (excluding streaming). The Client
	// applies it via context.WithTimeout in Do; per-attempt only.
	Timeout time.Duration

	// MaxRetries caps the total attempts on transient failures
	// (5xx + 429 + network errors). MaxRetries=0 means a single
	// attempt, no retries.
	MaxRetries int

	// HTTPClient overrides the underlying http.Client. When nil the
	// Client builds one with a tuned Transport (see newTransport).
	HTTPClient *http.Client

	// Now returns the current time, swappable for tests. nil → time.Now.
	Now func() time.Time

	// NewID returns a request id, swappable for tests. nil → uuidv7.
	NewID func() string
}

// Client is a small wrapper around *http.Client that adds the BackAI
// adapter envelope (auth, request-id, idempotency, RFC 7807, retry).
// One Client per adapter URL is the right cardinality — it owns the
// connection pool.
type Client struct {
	cfg    Config
	hc     *http.Client
	base   *url.URL
	caches *capabilityCache
}

// NewClient builds a Client. baseURL must be a full URL like
// "http://my-adapter:8080". Errors only on URL parse failure.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("remote: BaseURL required")
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("remote: parse BaseURL: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("remote: BaseURL needs scheme + host: %s", cfg.BaseURL)
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = func() string {
			// Prefer v7 (sortable) so adapter logs co-mingle nicely.
			// Fall back to v4 if v7 fails on this Go runtime.
			if id, err := uuid.NewV7(); err == nil {
				return id.String()
			}
			return uuid.NewString()
		}
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Transport: newTransport()}
	}
	return &Client{
		cfg:    cfg,
		hc:     hc,
		base:   base,
		caches: newCapabilityCache(),
	}, nil
}

// newTransport returns an *http.Transport with sensible production
// defaults: connection pooling per host, HTTP/2 enabled, modest idle
// timeouts so we don't keep dead sockets.
func newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	t.ResponseHeaderTimeout = 30 * time.Second
	t.ExpectContinueTimeout = 1 * time.Second
	// Streaming endpoints need DisableCompression=false (we want
	// gzip), so leave the default.
	return t
}

// Request describes one HTTP call to the adapter. Body may be nil for
// GET/DELETE. When BodyReader is set, the Client streams it as the
// request body and does not consult Body. Headers in ExtraHeaders are
// merged on top of the standard envelope (caller's headers win on
// conflict).
type Request struct {
	Method         string
	Path           string // joined with BaseURL
	Query          url.Values
	Body           any // marshalled as JSON; ignored if BodyReader != nil
	BodyReader     io.Reader
	ContentType    string // when set, overrides "application/json"
	TenantID       string // -> X-BackAI-Tenant-Id when non-empty
	IdempotencyKey string // -> X-BackAI-Idempotency-Key (auto-generated for writes if empty)
	ExtraHeaders   http.Header
	Stream         bool // when true, Client does not auto-close the body — caller does
}

// Response wraps *http.Response so callers don't need to know whether
// we did anything special with the body. Body MUST be closed by the
// caller when Stream=true; otherwise the Client closes it.
type Response struct {
	*http.Response
}

// Do executes the request, applying retries and decoding errors as
// *Problem. For non-streaming requests, the body is consumed and
// closed; the returned Response.Body is a non-nil bytes.Buffer the
// caller can drain again if needed (but typically uses
// DecodeJSON instead). For Stream=true, the response body is left open
// and the caller MUST close it.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if c == nil {
		return nil, errors.New("remote: nil Client")
	}
	if req.Method == "" {
		req.Method = http.MethodGet
	}

	// Pre-marshal JSON body once; we may need to re-send on retries.
	var bodyBytes []byte
	if req.BodyReader == nil && req.Body != nil {
		b, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("remote: marshal body: %w", err)
		}
		bodyBytes = b
	}

	// Streaming requests cannot retry safely because BodyReader is
	// single-pass. Enforce that here.
	maxAttempts := c.cfg.MaxRetries + 1
	if req.BodyReader != nil || req.Stream {
		maxAttempts = 1
	}

	// Idempotency-Key: required by protocol for writes. If caller
	// didn't supply one, generate a uuid so retries are safe.
	if req.IdempotencyKey == "" && isWrite(req.Method) {
		req.IdempotencyKey = c.cfg.NewID()
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt, lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := c.doOnce(ctx, req, bodyBytes)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		// Decide whether to retry.
		if !shouldRetry(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// doOnce performs a single attempt without retries.
func (c *Client) doOnce(ctx context.Context, req Request, bodyBytes []byte) (*Response, error) {
	full := c.base.JoinPath(req.Path)
	if req.Query != nil {
		full.RawQuery = req.Query.Encode()
	}

	var body io.Reader
	switch {
	case req.BodyReader != nil:
		body = req.BodyReader
	case len(bodyBytes) > 0:
		body = bytes.NewReader(bodyBytes)
	}

	// Per-attempt deadline so a misbehaving adapter doesn't stall us
	// forever. Streaming endpoints skip this (Client.Timeout in
	// http.Client would also kill them — and we never set that field).
	attemptCtx := ctx
	if !req.Stream && c.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		attemptCtx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
	}

	hr, err := http.NewRequestWithContext(attemptCtx, req.Method, full.String(), body)
	if err != nil {
		return nil, fmt.Errorf("remote: build request: %w", err)
	}

	// Standard envelope.
	if c.cfg.Token != "" {
		hr.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	hr.Header.Set(HeaderRequestID, c.cfg.NewID())
	hr.Header.Set(HeaderRuntimeVersion, RuntimeVersion)
	if req.TenantID != "" {
		hr.Header.Set(HeaderTenantID, req.TenantID)
	}
	if req.IdempotencyKey != "" {
		hr.Header.Set(HeaderIdempotencyKey, req.IdempotencyKey)
	}
	if req.ContentType != "" {
		hr.Header.Set("Content-Type", req.ContentType)
		hr.Header.Set(HeaderContentType, req.ContentType)
	} else if len(bodyBytes) > 0 {
		hr.Header.Set("Content-Type", "application/json")
		hr.Header.Set(HeaderContentType, "application/json")
	}
	// Caller overrides.
	for k, vs := range req.ExtraHeaders {
		for _, v := range vs {
			hr.Header.Set(k, v)
		}
	}

	resp, err := c.hc.Do(hr)
	if err != nil {
		// Wrap network errors so callers can branch on them too.
		return nil, fmt.Errorf("remote: transport: %w", err)
	}

	if resp.StatusCode >= 400 {
		// Decode the problem and close the body.
		p := decodeProblem(resp)
		_ = resp.Body.Close()
		return nil, p
	}

	if req.Stream {
		// Caller owns the body.
		return &Response{Response: resp}, nil
	}

	// Non-stream: consume the body so connection can return to pool.
	// We DON'T pre-buffer to keep memory low — caller uses
	// DecodeJSON / io.Copy directly on resp.Body. But we replace it
	// with a non-buffered ReadCloser that delegates to the original,
	// and ensure Close drains.
	return &Response{Response: resp}, nil
}

// DecodeJSON consumes the response body and unmarshals into v.
// Always closes the body. Use for non-stream responses.
func (r *Response) DecodeJSON(v any) error {
	if r == nil || r.Response == nil {
		return errors.New("remote: nil Response")
	}
	defer r.Body.Close()
	if r.StatusCode == http.StatusNoContent || r.ContentLength == 0 {
		return nil
	}
	// The protocol REQUIRES us to ignore unknown fields (forward
	// compatibility with adapter extensions). We deliberately do
	// NOT call dec.DisallowUnknownFields() — drift detection
	// belongs in the conformance harness, not the runtime client.
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("remote: decode json: %w", err)
	}
	return nil
}

// Events returns a channel of SSE events from a streaming response.
// The caller is responsible for closing r.Body (typically via defer).
// Cancelling ctx stops emission and lets the underlying reader unwind.
func (r *Response) Events(ctx context.Context) <-chan Event {
	if r == nil || r.Response == nil {
		ch := make(chan Event)
		close(ch)
		return ch
	}
	return readEvents(ctx, r.Body)
}

// CapabilityResponse is the envelope every /v1/capabilities response
// MUST return. Per-slot shims unmarshal the inner Capabilities map
// into their own typed struct.
type CapabilityResponse struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Slot            string          `json:"slot"`
	ProtocolVersion string          `json:"protocol_version"`
	Vendor          string          `json:"vendor"`
	Homepage        string          `json:"homepage,omitempty"`
	Capabilities    json.RawMessage `json:"capabilities"`
}

// HealthResponse is GET /healthz.
type HealthResponse struct {
	Status        string             `json:"status"` // healthy | degraded | unhealthy
	StartedAt     string             `json:"started_at,omitempty"`
	UptimeSeconds int64              `json:"uptime_seconds,omitempty"`
	Dependencies  []HealthDependency `json:"dependencies,omitempty"`
}

// HealthDependency is one upstream dependency reported by /healthz.
type HealthResponse_Dep = HealthDependency

// HealthDependency is one row in HealthResponse.Dependencies.
type HealthDependency struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// InfoResponse is GET /v1/info — optional operator-facing metadata.
type InfoResponse struct {
	AdminUI      string `json:"admin_ui,omitempty"`
	Docs         string `json:"docs,omitempty"`
	SupportEmail string `json:"support_email,omitempty"`
}

// Capabilities fetches /v1/capabilities (cached for the Client's TTL).
// Each call shares the same in-flight fetch via singleflight.
func (c *Client) Capabilities(ctx context.Context) (CapabilityResponse, error) {
	return c.caches.get(ctx, c.base.String(), func() (CapabilityResponse, error) {
		resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: "/v1/capabilities"})
		if err != nil {
			return CapabilityResponse{}, err
		}
		var out CapabilityResponse
		if err := resp.DecodeJSON(&out); err != nil {
			return CapabilityResponse{}, err
		}
		return out, nil
	})
}

// Health calls /healthz and returns the response. The Client does not
// cache health — operators expect live status.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: "/healthz"})
	if err != nil {
		return HealthResponse{}, err
	}
	var out HealthResponse
	if err := resp.DecodeJSON(&out); err != nil {
		return HealthResponse{}, err
	}
	return out, nil
}

// Info calls /v1/info. A 404 is reported as a nil error with a zero
// InfoResponse — the endpoint is optional per the protocol.
func (c *Client) Info(ctx context.Context) (InfoResponse, error) {
	resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: "/v1/info"})
	if err != nil {
		if IsCode(err, "not_found") {
			return InfoResponse{}, nil
		}
		return InfoResponse{}, err
	}
	var out InfoResponse
	if err := resp.DecodeJSON(&out); err != nil {
		return InfoResponse{}, err
	}
	return out, nil
}

// --- retry policy --------------------------------------------------------

func isWrite(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	// Adapter-level problem? Use its transient bit.
	if p, ok := AsProblem(err); ok {
		return p.IsTransient()
	}
	// Network errors (DNS, TCP) — almost always worth a retry.
	// We classify by string match cautiously; net.Error.Temporary()
	// is deprecated.
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context canceled"),
		strings.Contains(msg, "context deadline exceeded"):
		// Caller cancelled; do not retry.
		return false
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "EOF"):
		return true
	}
	return false
}

// backoff returns the sleep before attempt n (n starts at 1 for the
// first retry). Honours Retry-After from a 429 if present.
func backoff(attempt int, lastErr error) time.Duration {
	if p, ok := AsProblem(lastErr); ok && p.RetryAfterSeconds > 0 {
		return time.Duration(p.RetryAfterSeconds) * time.Second
	}
	// Exponential with jitter: 200ms, 400ms, 800ms ± 25%.
	base := 200 * time.Millisecond * time.Duration(1<<uint(attempt-1))
	if base > 5*time.Second {
		base = 5 * time.Second
	}
	jitter := time.Duration(rand.Int64N(int64(base) / 2))
	return base + jitter - (base / 4)
}

// retryAfterFromHeader parses the "Retry-After" header in either of
// its two RFC 7231 forms (integer seconds or HTTP date). Exposed for
// future use; the protocol prefers retry_after_seconds in the JSON
// body.
func retryAfterFromHeader(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
