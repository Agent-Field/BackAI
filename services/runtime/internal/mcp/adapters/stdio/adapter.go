// Package stdio implements the MCP stdio transport: a child process
// spawned via os/exec, talking JSON-RPC 2.0 framed with the LSP-style
// `Content-Length: N\r\n\r\n<json>` header per the MCP wire spec.
//
// Lifecycle:
//
//   Connect: spawn the process, pipe stdin/stdout, launch a stderr
//            scraper goroutine (first chunk is surfaced as last_error
//            on the next Connect failure), perform the "initialize"
//            handshake, send the "notifications/initialized" notice.
//   ListTools: tools/list request -> Tool array.
//   Call: tools/call request -> CallResult (MCP's content array passed
//            through unchanged).
//   Close: SIGTERM the child, give it a short grace window, then SIGKILL
//            if still alive. Cancels the reader goroutine.
//
// Concurrency: Connect / Close are NOT safe for concurrent calls from
// different goroutines (the Pool serialises them). ListTools / Call ARE
// safe; the request mutex serialises writes to the child's stdin while
// the reader goroutine fan-outs responses by JSON-RPC id.
package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/mcp"
)

// Compile-time assertion: Adapter satisfies mcp.Adapter.
var _ mcp.Adapter = (*Adapter)(nil)

// Config collects the per-adapter knobs. Command + Env come from the
// resolved suite_mcp_servers row (env values prefixed "secret:<key>"
// have already been resolved by the Pool via mcp.ResolveEnv).
type Config struct {
	Name    string
	Command []string
	Env     map[string]string
	Logger  *slog.Logger
}

// Adapter is one connection to an MCP stdio server.
type Adapter struct {
	name string
	cmd  *exec.Cmd
	env  map[string]string
	args []string
	log  *slog.Logger

	mu      sync.Mutex // serialises Connect/Close + the requestID counter
	closed  bool
	cancel  context.CancelFunc
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  io.ReadCloser
	doneCh  chan struct{}

	// Response routing — the reader goroutine fans out incoming JSON
	// frames to the goroutine that issued the matching request.
	pendingMu sync.Mutex
	pending   map[int64]chan *jsonRPCMessage
	nextID    int64

	// First non-empty stderr line, surfaced through the last connect
	// error so operators see "uvx: command not found" instead of a
	// generic EOF.
	stderrFirst string
	stderrMu    sync.Mutex

	// writeMu serialises stdin writes (the framing is interleave-sensitive).
	writeMu sync.Mutex
}

// New constructs an Adapter. Connect must be called before any other
// method.
func New(cfg Config) *Adapter {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Adapter{
		name:    cfg.Name,
		args:    cfg.Command,
		env:     cfg.Env,
		log:     log,
		pending: map[int64]chan *jsonRPCMessage{},
	}
}

// Connect spawns the child and performs the MCP handshake. Idempotent
// when already connected (returns nil).
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("stdio: adapter closed")
	}
	if a.cmd != nil {
		a.mu.Unlock()
		return nil
	}
	if len(a.args) == 0 {
		a.mu.Unlock()
		return errors.New("stdio: empty command")
	}

	// Spawn under a cancellable context so Close() can SIGKILL via
	// cancel() if the child ignores SIGTERM.
	cmdCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, a.args[0], a.args[1:]...)

	// Env: merge the row's env on TOP of a minimal passthrough (PATH +
	// HOME) so utilities like `uvx` can find their interpreter. Tests
	// can clobber by setting PATH explicitly in the row's env.
	cmd.Env = mergeEnv(a.env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		a.mu.Unlock()
		return fmt.Errorf("stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		a.mu.Unlock()
		return fmt.Errorf("stdio: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		a.mu.Unlock()
		return fmt.Errorf("stdio: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		a.mu.Unlock()
		return fmt.Errorf("stdio: start %q: %w", a.args[0], err)
	}

	a.cmd = cmd
	a.cancel = cancel
	a.stdin = stdin
	a.stdout = bufio.NewReader(stdout)
	a.stderr = stderr
	a.doneCh = make(chan struct{})
	a.mu.Unlock()

	// Reader goroutines: stdout dispatches JSON-RPC responses; stderr
	// captures the first line for surfacing through last_error.
	go a.readLoop()
	go a.stderrLoop()

	// Handshake: MCP requires initialize -> initialized notification
	// before any tool calls.
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
		// Roll back the connection so a retry can re-spawn cleanly.
		_ = a.Close()
		first := a.firstStderr()
		if first != "" {
			return fmt.Errorf("stdio: initialize failed: %w (stderr: %s)", err, first)
		}
		return fmt.Errorf("stdio: initialize failed: %w", err)
	}
	if err := a.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		_ = a.Close()
		return fmt.Errorf("stdio: initialized notify: %w", err)
	}
	return nil
}

// ListTools issues tools/list and parses the response into the runtime's
// flat Tool struct. The MCP spec returns {tools: [...]} so we descend
// one level before iterating.
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
		return nil, fmt.Errorf("stdio: decode tools/list: %w", err)
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

// Call issues tools/call. The MCP response shape is {content:[...],
// isError:bool}; we pass content through unchanged. duration_ms is
// stamped by the Pool, not here (so the SSE adapter doesn't need to
// duplicate the bookkeeping).
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
		return mcp.CallResult{}, fmt.Errorf("stdio: decode tools/call: %w", err)
	}
	content := payload.Content
	if content == nil {
		content = []map[string]any{}
	}
	return mcp.CallResult{Content: content, IsError: payload.IsError}, nil
}

// Close terminates the child. Sends SIGTERM, waits up to 2s, then
// SIGKILLs via the cancellable exec context. Safe to call multiple
// times.
func (a *Adapter) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	cmd := a.cmd
	cancel := a.cancel
	stdin := a.stdin
	doneCh := a.doneCh
	a.mu.Unlock()

	if cmd == nil {
		return nil
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	// Polite SIGTERM first — MCP servers should flush state on it.
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	graceCh := make(chan struct{}, 1)
	go func() {
		_ = cmd.Wait()
		graceCh <- struct{}{}
	}()
	select {
	case <-graceCh:
	case <-time.After(2 * time.Second):
		if cancel != nil {
			cancel() // SIGKILL via CommandContext
		}
		<-graceCh
	}
	if doneCh != nil {
		// Reader goroutine exits on EOF; nothing to wait on beyond Wait.
		select {
		case <-doneCh:
		default:
		}
	}
	// Fail any pending callers.
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

// request sends a JSON-RPC request and waits for the matching response
// id. ctx controls the wait timeout — when ctx is cancelled the pending
// entry is removed so a late response doesn't leak.
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
		return nil, fmt.Errorf("stdio: encode params: %w", err)
	}
	msg := jsonRPCMessage{
		JSONRPC: "2.0",
		ID:      idBytes,
		Method:  method,
		Params:  paramsBytes,
	}
	if err := a.writeFrame(msg); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("stdio: connection closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("stdio: rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if len(resp.Result) == 0 {
			return json.RawMessage("null"), nil
		}
		return resp.Result, nil
	}
}

// notify sends a notification (no id, no response expected).
func (a *Adapter) notify(_ context.Context, method string, params map[string]any) error {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("stdio: encode notify params: %w", err)
	}
	return a.writeFrame(jsonRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	})
}

// writeFrame serialises the message and emits an LSP-style framed
// payload on stdin. Holds writeMu so concurrent callers don't interleave
// header/body bytes.
func (a *Adapter) writeFrame(msg jsonRPCMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("stdio: marshal frame: %w", err)
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.mu.Lock()
	stdin := a.stdin
	closed := a.closed
	a.mu.Unlock()
	if closed || stdin == nil {
		return errors.New("stdio: not connected")
	}
	header := "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n"
	if _, err := io.WriteString(stdin, header); err != nil {
		return fmt.Errorf("stdio: write header: %w", err)
	}
	if _, err := stdin.Write(body); err != nil {
		return fmt.Errorf("stdio: write body: %w", err)
	}
	return nil
}

// readLoop reads framed messages off stdout and dispatches them to the
// matching pending entry by JSON-RPC id. Exits on EOF or close.
func (a *Adapter) readLoop() {
	defer close(a.doneCh)
	for {
		a.mu.Lock()
		stdout := a.stdout
		closed := a.closed
		a.mu.Unlock()
		if closed || stdout == nil {
			return
		}
		body, err := readFrame(stdout)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				a.log.Debug("stdio: read frame", "server", a.name, "error", err)
			}
			return
		}
		var msg jsonRPCMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			a.log.Warn("stdio: malformed frame", "server", a.name, "error", err)
			continue
		}
		// Notification (id absent) — currently ignored. Future work
		// surfaces tools/list_changed back into the Pool.
		if len(msg.ID) == 0 {
			continue
		}
		var id int64
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			// Some servers return string ids; we only issue numeric so
			// any non-int response is a server bug. Drop it rather than
			// panic.
			continue
		}
		a.pendingMu.Lock()
		ch, ok := a.pending[id]
		a.pendingMu.Unlock()
		if !ok {
			continue
		}
		// Non-blocking send: the buffered channel always has room
		// because each pending entry is created with capacity 1.
		select {
		case ch <- &msg:
		default:
		}
	}
}

// stderrLoop captures the first non-empty line for surfacing in
// last_error. Subsequent stderr is drained to /dev/null (debug-logged
// at low verbosity).
func (a *Adapter) stderrLoop() {
	r := bufio.NewReader(a.stderr)
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			a.stderrMu.Lock()
			if a.stderrFirst == "" {
				a.stderrFirst = trimmed
			}
			a.stderrMu.Unlock()
			a.log.Debug("stdio: stderr", "server", a.name, "line", trimmed)
		}
		if err != nil {
			return
		}
	}
}

func (a *Adapter) firstStderr() string {
	a.stderrMu.Lock()
	defer a.stderrMu.Unlock()
	return a.stderrFirst
}

func (a *Adapter) nextRequestID() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	return a.nextID
}

// readFrame reads one LSP-style framed JSON-RPC message: a sequence of
// `Header: value\r\n` lines terminated by a blank line, then exactly
// Content-Length bytes of payload.
func readFrame(r *bufio.Reader) ([]byte, error) {
	var contentLength int = -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		if strings.EqualFold(key, "Content-Length") {
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("stdio: invalid Content-Length %q: %w", val, err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, errors.New("stdio: missing Content-Length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// mergeEnv returns a slice of "K=V" entries suitable for exec.Cmd.Env.
// We pass through PATH, HOME, USER, LANG by default so the child can
// find its interpreter; the row's env then overrides / adds on top.
func mergeEnv(extra map[string]string) []string {
	passthrough := []string{"PATH", "HOME", "USER", "LANG", "LC_ALL", "TMPDIR"}
	out := []string{}
	seen := map[string]struct{}{}
	for _, key := range passthrough {
		if val, ok := lookupEnv(key); ok {
			out = append(out, key+"="+val)
			seen[key] = struct{}{}
		}
	}
	for k, v := range extra {
		if _, alreadySet := seen[k]; alreadySet {
			// Replace the passthrough value.
			for i, e := range out {
				if strings.HasPrefix(e, k+"=") {
					out[i] = k + "=" + v
					break
				}
			}
			continue
		}
		out = append(out, k+"="+v)
		seen[k] = struct{}{}
	}
	return out
}

// lookupEnv is broken out so tests can stub it. Falls back to
// os.Getenv via the variable below.
var lookupEnv = func(key string) (string, bool) {
	v, ok := osLookup(key)
	return v, ok
}
