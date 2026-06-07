// SPDX-License-Identifier: Apache-2.0

// Package sse implements the MCP HTTP+SSE transport. The runtime POSTs
// JSON-RPC requests to <url>/messages and receives responses on a
// long-lived Server-Sent-Events stream from <url>/sse.
//
// The MCP spec assigns a session id on the first /sse connect (sent as
// the `endpoint` event with a sessionId query param appended to the
// /messages URL); subsequent POSTs include that session id so the
// remote can route the response back over the same stream.
//
// Concurrency: ListTools / Call can fan in from many goroutines; the
// pending map fans responses back out by JSON-RPC id. Connect / Close
// are NOT safe for concurrent calls from different goroutines.
package sse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/mcp"
	"github.com/Agent-Field/backai/services/runtime/internal/safehttp"
)

// Compile-time assertion: Adapter satisfies mcp.Adapter.
var _ mcp.Adapter = (*Adapter)(nil)

// Config collects the per-adapter knobs. Env carries arbitrary headers
// (key "HTTP_X_FOO" -> "X-Foo: ..."); the env-prefix dance keeps the
// suite_mcp_servers.env column shape uniform between stdio (process
// env) and sse (request headers).
type Config struct {
	Name    string
	URL     string
	Headers map[string]string
	Client  *http.Client
	Logger  *slog.Logger
}

// Adapter is one connection to an MCP SSE server.
type Adapter struct {
	name    string
	baseURL string
	headers map[string]string
	client  *http.Client
	log     *slog.Logger

	mu         sync.Mutex
	closed     bool
	cancel     context.CancelFunc
	sessionURL string // /messages?sessionId=... after the endpoint event

	pendingMu sync.Mutex
	pending   map[int64]chan *jsonRPCMessage
	nextID    int64

	endpointReady chan struct{}
	streamErr     atomicErr
}

// New constructs an Adapter. Connect must be called before any other
// method.
func New(cfg Config) *Adapter {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	client := cfg.Client
	if client == nil {
		// SSE: no overall timeout (ctx controls cancellation); SSRF
		// defence: refuse private CIDRs + metadata hosts. The MCP
		// server URL is user-supplied (suite_mcp_servers.url is
		// dashboard-configurable) so a hostile config could otherwise
		// point at 169.254.169.254 to lift cloud creds, or at a
		// docker-internal Postgres.
		client = safehttp.New(safehttp.Options{Timeout: 0})
	}
	return &Adapter{
		name:          cfg.Name,
		baseURL:       strings.TrimRight(cfg.URL, "/"),
		headers:       cfg.Headers,
		client:        client,
		log:           log,
		pending:       map[int64]chan *jsonRPCMessage{},
		endpointReady: make(chan struct{}),
	}
}

// Connect opens the SSE stream, waits for the `endpoint` event that
// gives us the per-session /messages URL, then performs the MCP
// initialize handshake.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("sse: adapter closed")
	}
	if a.sessionURL != "" {
		a.mu.Unlock()
		return nil
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.mu.Unlock()

	// Spawn the read loop; it blocks on the long-lived /sse GET.
	go a.streamLoop(streamCtx)

	// Wait up to 15s for the endpoint event.
	endpointCtx, endpointCancel := context.WithTimeout(ctx, 15*time.Second)
	defer endpointCancel()
	select {
	case <-endpointCtx.Done():
		if err := a.streamErr.get(); err != nil {
			_ = a.Close()
			return fmt.Errorf("sse: stream failed before endpoint: %w", err)
		}
		_ = a.Close()
		return fmt.Errorf("sse: timed out waiting for endpoint event")
	case <-a.endpointReady:
	}

	// MCP handshake.
	hsCtx, hsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer hsCancel()
	if _, err := a.request(hsCtx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "af-stack",
			"version": "0.1.0",
		},
	}); err != nil {
		_ = a.Close()
		return fmt.Errorf("sse: initialize: %w", err)
	}
	if err := a.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		_ = a.Close()
		return fmt.Errorf("sse: initialized notify: %w", err)
	}
	return nil
}

// ListTools issues tools/list. See stdio adapter for shape rationale.
func (a *Adapter) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	res, err := a.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description *string        `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(res, &payload); err != nil {
		return nil, fmt.Errorf("sse: decode tools/list: %w", err)
	}
	out := make([]mcp.Tool, 0, len(payload.Tools))
	for _, t := range payload.Tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{}
		}
		out = append(out, mcp.Tool{
			ID:          a.name + "/" + t.Name,
			Server:      a.name,
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out, nil
}

// Call issues tools/call.
func (a *Adapter) Call(ctx context.Context, tool string, args map[string]any) (mcp.CallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := a.request(ctx, "tools/call", map[string]any{
		"name":      tool,
		"arguments": args,
	})
	if err != nil {
		return mcp.CallResult{}, err
	}
	var payload struct {
		Content []map[string]any `json:"content"`
		IsError bool             `json:"isError"`
	}
	if err := json.Unmarshal(res, &payload); err != nil {
		return mcp.CallResult{}, fmt.Errorf("sse: decode tools/call: %w", err)
	}
	content := payload.Content
	if content == nil {
		content = []map[string]any{}
	}
	return mcp.CallResult{Content: content, IsError: payload.IsError}, nil
}

// Close cancels the SSE read loop and drops every pending request.
// Safe to call multiple times.
func (a *Adapter) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.pendingMu.Lock()
	for id, ch := range a.pending {
		close(ch)
		delete(a.pending, id)
	}
	a.pendingMu.Unlock()
	return nil
}

// ─── JSON-RPC plumbing ────────────────────────────────────────────────────

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (a *Adapter) request(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	id := a.nextRequestID()
	ch := make(chan *jsonRPCMessage, 1)
	a.pendingMu.Lock()
	a.pending[id] = ch
	a.pendingMu.Unlock()
	defer func() {
		a.pendingMu.Lock()
		delete(a.pending, id)
		a.pendingMu.Unlock()
	}()

	idBytes, _ := json.Marshal(id)
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("sse: encode params: %w", err)
	}
	msg := jsonRPCMessage{
		JSONRPC: "2.0",
		ID:      idBytes,
		Method:  method,
		Params:  paramsBytes,
	}
	if err := a.postMessage(ctx, msg); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("sse: connection closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("sse: rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if len(resp.Result) == 0 {
			return json.RawMessage("null"), nil
		}
		return resp.Result, nil
	}
}

func (a *Adapter) notify(ctx context.Context, method string, params map[string]any) error {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("sse: encode notify params: %w", err)
	}
	return a.postMessage(ctx, jsonRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	})
}

func (a *Adapter) postMessage(ctx context.Context, msg jsonRPCMessage) error {
	a.mu.Lock()
	sessionURL := a.sessionURL
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return errors.New("sse: adapter closed")
	}
	if sessionURL == "" {
		return errors.New("sse: no session URL (endpoint event not yet received)")
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("sse: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sse: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyHeaders(req)
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("sse: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("sse: post status %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))
	}
	// Drain body so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// streamLoop performs the long-lived GET /sse and dispatches incoming
// events. The MCP SSE spec defines two event types:
//
//	event: endpoint   data: <url>            (sent first; gives us the
//	                                          per-session /messages URL)
//	event: message    data: <json-rpc>       (every subsequent response)
func (a *Adapter) streamLoop(ctx context.Context) {
	streamURL := a.baseURL + "/sse"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		a.streamErr.set(fmt.Errorf("sse: build stream req: %w", err))
		a.signalEndpoint()
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	a.applyHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		a.streamErr.set(fmt.Errorf("sse: stream connect: %w", err))
		a.signalEndpoint()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		a.streamErr.set(fmt.Errorf("sse: stream status %d: %s", resp.StatusCode, strings.TrimSpace(string(preview))))
		a.signalEndpoint()
		return
	}

	reader := bufio.NewReader(resp.Body)
	var event, data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if event.Len() > 0 || data.Len() > 0 {
					a.handleEvent(event.String(), data.String())
					event.Reset()
					data.Reset()
				}
			case strings.HasPrefix(trimmed, ":"):
				// comment / heartbeat
			case strings.HasPrefix(trimmed, "event:"):
				event.WriteString(strings.TrimSpace(trimmed[len("event:"):]))
			case strings.HasPrefix(trimmed, "data:"):
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(strings.TrimSpace(trimmed[len("data:"):]))
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
				a.streamErr.set(err)
			}
			a.signalEndpoint() // unblocks Connect if still waiting
			return
		}
	}
}

func (a *Adapter) handleEvent(event, data string) {
	switch event {
	case "endpoint":
		// Data is either a full URL or a relative path. Resolve
		// against the base URL.
		sessionURL := data
		if !strings.HasPrefix(sessionURL, "http://") && !strings.HasPrefix(sessionURL, "https://") {
			u, err := url.Parse(a.baseURL)
			if err == nil {
				ref, err := url.Parse(data)
				if err == nil {
					sessionURL = u.ResolveReference(ref).String()
				}
			}
		}
		a.mu.Lock()
		a.sessionURL = sessionURL
		a.mu.Unlock()
		a.signalEndpoint()
	case "message", "":
		var msg jsonRPCMessage
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			a.log.Warn("sse: malformed message", "server", a.name, "error", err)
			return
		}
		if len(msg.ID) == 0 {
			return // server-originated notification; ignored in v1
		}
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			return
		}
		a.pendingMu.Lock()
		ch, ok := a.pending[id]
		a.pendingMu.Unlock()
		if !ok {
			return
		}
		select {
		case ch <- &msg:
		default:
		}
	}
}

func (a *Adapter) signalEndpoint() {
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.endpointReady:
		// already closed
	default:
		close(a.endpointReady)
	}
}

func (a *Adapter) applyHeaders(req *http.Request) {
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *Adapter) nextRequestID() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	return a.nextID
}

// atomicErr is a tiny mutex-guarded error holder. The streamLoop writes
// it before signalling endpointReady; Connect reads it on timeout.
type atomicErr struct {
	mu  sync.Mutex
	err error
}

func (a *atomicErr) set(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err == nil {
		a.err = err
	}
}

func (a *atomicErr) get() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}