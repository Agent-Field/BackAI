// SPDX-License-Identifier: Apache-2.0

// Package cdpdriver is the shared Chrome-DevTools-Protocol engine behind
// the hosted-provider browser adapters (playwright/Browserless, Steel,
// Browserbase). Those providers all expose the same primitive: a CDP
// websocket endpoint you attach a driver to. This package owns that
// attachment (via chromedp) plus the per-session lifecycle, so each
// adapter only has to answer one question — "what is the ws:// endpoint
// for this sessionID?" — through the Options.Endpoint callback.
//
// # Sessions
//
// The runtime's browser tool passes an opaque, caller-chosen sessionID
// with every verb. The driver lazily creates one live chromedp context
// per sessionID (empty string = the default session) and reuses it for
// consecutive verbs, so `navigate` followed by `extract_text` with the
// same session_id observes the same page. Idle sessions are reaped
// after Options.IdleTTL (default 5 minutes); Options.OnExpire tells the
// owning adapter so it can release the provider-side session (Steel
// bills per session-minute).
//
// # SSRF posture
//
// Unless Options.AllowPrivate is set, Endpoint URLs whose host resolves
// to loopback / RFC-1918 / link-local / CGNAT ranges are refused before
// dialing — mirroring internal/safehttp. chromedp owns its own websocket
// dialer, so the check here is a best-effort pre-dial resolution rather
// than safehttp's in-dialer Control hook; see ssrf.go for the caveat.
package cdpdriver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
)

// Defaults for Options fields left zero.
const (
	DefaultIdleTTL         = 5 * time.Minute
	DefaultNavigateTimeout = 30 * time.Second
	DefaultActionTimeout   = 10 * time.Second
)

// ErrClosed is returned by verbs invoked after Close().
var ErrClosed = errors.New("cdpdriver: driver is closed")

// Options configures a Driver.
type Options struct {
	// Endpoint returns the CDP websocket URL for sessionID, plus an
	// optional cleanup invoked exactly once when that connection is torn
	// down (idle TTL, connection death, dial failure, or Close). Called
	// once per live session (lazily, on the first verb that touches it);
	// the connection is cached until the session expires.
	//
	// Hosted providers (Steel, Browserbase) create their REST session
	// here and return its connect URL with a cleanup that releases that
	// exact provider session; fixed-endpoint backends (Browserless)
	// return a constant URL and a nil cleanup. Required.
	Endpoint func(ctx context.Context, sessionID string) (wsURL string, cleanup func(), err error)

	// AllowPrivate permits ws endpoints on loopback / private ranges
	// (a self-hosted Browserless on the compose network, localhost in
	// tests). Off by default; the factory gates it behind
	// AF_STACK_BROWSER_ALLOW_PRIVATE.
	AllowPrivate bool

	// IdleTTL is how long a session may sit unused before it is reaped.
	// Defaults to DefaultIdleTTL.
	IdleTTL time.Duration

	// NavigateTimeout bounds Navigate. Defaults to DefaultNavigateTimeout.
	NavigateTimeout time.Duration

	// ActionTimeout bounds ExtractText / Screenshot / Click / Fill.
	// Defaults to DefaultActionTimeout.
	ActionTimeout time.Duration
}

// Driver multiplexes browser verbs onto per-session remote CDP
// connections. Safe for concurrent use; verbs on the same sessionID
// serialize, verbs on different sessions run in parallel.
type Driver struct {
	opts Options

	mu       sync.Mutex
	sessions map[string]*session
	closed   bool
	stopCh   chan struct{}
	janitor  sync.Once
	wg       sync.WaitGroup // reaper + in-flight cleanup callbacks
}

// session is one live remote browser context. lastUsed is guarded by
// the owning Driver's mu; everything else by the session's own mu.
type session struct {
	mu       sync.Mutex
	taskCtx  context.Context
	cancel   context.CancelFunc
	cleanup  func() // provider-session release; nil once fired
	healthy  bool   // first successful chromedp.Run completed
	lastUsed time.Time
}

// New constructs a Driver. opts.Endpoint is required.
func New(opts Options) *Driver {
	if opts.IdleTTL <= 0 {
		opts.IdleTTL = DefaultIdleTTL
	}
	if opts.NavigateTimeout <= 0 {
		opts.NavigateTimeout = DefaultNavigateTimeout
	}
	if opts.ActionTimeout <= 0 {
		opts.ActionTimeout = DefaultActionTimeout
	}
	return &Driver{
		opts:     opts,
		sessions: make(map[string]*session),
		stopCh:   make(chan struct{}),
	}
}

// Navigate loads url in the session's page and reports the final URL,
// title, and — when CDP surfaces the main-document response — the HTTP
// status code (0 otherwise, e.g. data: URLs).
func (d *Driver) Navigate(ctx context.Context, sessionID, url string) (browser.Result, error) {
	var out browser.Result
	err := d.withSession(ctx, sessionID, d.opts.NavigateTimeout, func(runCtx context.Context) error {
		resp, err := chromedp.RunResponse(runCtx, chromedp.Navigate(url))
		if err != nil {
			return fmt.Errorf("cdpdriver: navigate: %w", err)
		}
		if resp != nil {
			out.StatusCode = int(resp.Status)
		}
		var title, loc string
		if err := chromedp.Run(runCtx, chromedp.Title(&title), chromedp.Location(&loc)); err != nil {
			return fmt.Errorf("cdpdriver: navigate readback: %w", err)
		}
		out.Title = title
		out.URL = loc
		return nil
	})
	if err != nil {
		return browser.Result{}, err
	}
	return out, nil
}

// ExtractText returns the rendered inner text of the page body.
func (d *Driver) ExtractText(ctx context.Context, sessionID string) (browser.Result, error) {
	var text string
	err := d.withSession(ctx, sessionID, d.opts.ActionTimeout, func(runCtx context.Context) error {
		const script = `document.body ? document.body.innerText : ""`
		if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &text)); err != nil {
			return fmt.Errorf("cdpdriver: extract_text: %w", err)
		}
		return nil
	})
	if err != nil {
		return browser.Result{}, err
	}
	return browser.Result{Text: text}, nil
}

// Screenshot returns a base64-encoded PNG of the current viewport.
func (d *Driver) Screenshot(ctx context.Context, sessionID string) (browser.Result, error) {
	var png []byte
	err := d.withSession(ctx, sessionID, d.opts.ActionTimeout, func(runCtx context.Context) error {
		if err := chromedp.Run(runCtx, chromedp.CaptureScreenshot(&png)); err != nil {
			return fmt.Errorf("cdpdriver: screenshot: %w", err)
		}
		return nil
	})
	if err != nil {
		return browser.Result{}, err
	}
	return browser.Result{ScreenshotBase64: base64.StdEncoding.EncodeToString(png)}, nil
}

// Click clicks the first visible element matching the CSS selector.
func (d *Driver) Click(ctx context.Context, sessionID, selector string) (browser.Result, error) {
	err := d.withSession(ctx, sessionID, d.opts.ActionTimeout, func(runCtx context.Context) error {
		if err := chromedp.Run(runCtx, chromedp.Click(selector, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
			return fmt.Errorf("cdpdriver: click %q: %w", selector, err)
		}
		return nil
	})
	if err != nil {
		return browser.Result{}, err
	}
	return browser.Result{}, nil
}

// Fill sets the value of the input matching the CSS selector.
func (d *Driver) Fill(ctx context.Context, sessionID, selector, value string) (browser.Result, error) {
	err := d.withSession(ctx, sessionID, d.opts.ActionTimeout, func(runCtx context.Context) error {
		if err := chromedp.Run(runCtx, chromedp.SetValue(selector, value, chromedp.ByQuery)); err != nil {
			return fmt.Errorf("cdpdriver: fill %q: %w", selector, err)
		}
		return nil
	})
	if err != nil {
		return browser.Result{}, err
	}
	return browser.Result{}, nil
}

// Close tears down every live session (running each provider-session
// cleanup synchronously) and stops the reaper. Idempotent; verbs after
// Close return ErrClosed.
func (d *Driver) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	close(d.stopCh)
	victims := make([]*session, 0, len(d.sessions))
	for _, s := range d.sessions {
		victims = append(victims, s)
	}
	d.sessions = make(map[string]*session)
	d.mu.Unlock()

	for _, s := range victims {
		s.mu.Lock() // wait for any in-flight verb
		if s.cancel != nil {
			s.cancel()
		}
		cleanup := s.cleanup
		s.cleanup = nil
		s.mu.Unlock()
		if cleanup != nil {
			cleanup()
		}
	}
	d.wg.Wait()
}

// withSession runs fn against the live chromedp context for sessionID,
// creating (and dialing) it first if needed. fn's context carries the
// verb timeout and is additionally canceled if the caller's ctx is.
func (d *Driver) withSession(ctx context.Context, sessionID string, timeout time.Duration, fn func(runCtx context.Context) error) error {
	var s *session
	for {
		var err error
		s, err = d.getOrCreate(sessionID)
		if err != nil {
			return err
		}
		s.mu.Lock()
		// The sweeper may have expired this entry between getOrCreate and
		// the lock; reconnecting an orphaned entry would leak the browser
		// connection. Retry with a fresh entry instead.
		d.mu.Lock()
		current := d.sessions[sessionID] == s
		d.mu.Unlock()
		if current {
			break
		}
		s.mu.Unlock()
	}
	defer s.mu.Unlock()

	if s.taskCtx == nil { // fresh session: resolve endpoint + dial
		if err := d.connect(ctx, sessionID, s, timeout); err != nil {
			d.drop(sessionID, s)
			return err
		}
	}

	d.touch(sessionID)
	runCtx, cancel := context.WithTimeout(s.taskCtx, timeout)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	runErr := fn(runCtx)
	d.touch(sessionID)
	if runErr != nil && (s.taskCtx.Err() != nil || !s.healthy) {
		// The underlying browser connection is gone (or never came up):
		// drop the session so the next verb reconnects fresh.
		s.cancel()
		s.taskCtx = nil
		d.drop(sessionID, s)
		return runErr
	}
	if runErr == nil {
		s.healthy = true
	}
	return runErr
}

// connect resolves the endpoint and establishes the CDP connection.
// Called with s.mu held.
func (d *Driver) connect(ctx context.Context, sessionID string, s *session, timeout time.Duration) error {
	wsURL, cleanup, err := d.opts.Endpoint(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("cdpdriver: endpoint for session %q: %w", sessionID, err)
	}
	// From here on every failure path runs through drop(), which fires
	// the cleanup — the provider session exists and must be released.
	s.cleanup = cleanup
	if !d.opts.AllowPrivate {
		if err := CheckEndpoint(ctx, wsURL); err != nil {
			return err
		}
	}

	// NoModifyURL is load-bearing: hosted providers (Steel, Browserbase,
	// Browserless cloud) hand out the exact browser endpoint with auth
	// baked into the query string; chromedp's default /json/version
	// probe-and-rewrite breaks those connections.
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), wsURL, chromedp.NoModifyURL)
	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	cancelAll := func() {
		cancelTask()
		cancelAlloc()
	}

	// Force the websocket dial now (Run with no actions allocates), so a
	// dead endpoint fails the current verb instead of poisoning the
	// session map. The allocation MUST run on the long-lived taskCtx —
	// chromedp pins the browser connection to the context used at
	// allocation time, so dialing under a shorter context would tear the
	// session down as soon as that context is canceled. The timeout is
	// therefore enforced from outside.
	dialErr := make(chan error, 1)
	go func() { dialErr <- chromedp.Run(taskCtx) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err = <-dialErr:
	case <-timer.C:
		cancelAll()
		<-dialErr
		err = fmt.Errorf("dial timeout after %s", timeout)
	case <-ctx.Done():
		cancelAll()
		<-dialErr
		err = ctx.Err()
	}
	if err != nil {
		cancelAll()
		return fmt.Errorf("cdpdriver: connect %q: %w", sessionID, err)
	}

	s.taskCtx = taskCtx
	s.cancel = cancelAll
	s.healthy = true
	return nil
}

// getOrCreate returns the session entry for id, creating a placeholder
// (not yet dialed) if absent. Starts the reaper on first use.
func (d *Driver) getOrCreate(id string) (*session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, ErrClosed
	}
	if s, ok := d.sessions[id]; ok {
		return s, nil
	}
	s := &session{lastUsed: time.Now()}
	d.sessions[id] = s
	d.janitor.Do(func() {
		d.wg.Add(1)
		go d.reap()
	})
	return s, nil
}

// touch refreshes the idle clock for id.
func (d *Driver) touch(id string) {
	d.mu.Lock()
	if s, ok := d.sessions[id]; ok {
		s.lastUsed = time.Now()
	}
	d.mu.Unlock()
}

// drop removes id from the map (if it still maps to s) and schedules
// the session's provider cleanup. Callers hold s.mu; the contexts are
// already canceled or never existed.
func (d *Driver) drop(id string, s *session) {
	d.mu.Lock()
	if cur, ok := d.sessions[id]; ok && cur == s {
		delete(d.sessions, id)
	}
	d.mu.Unlock()
	cleanup := s.cleanup
	s.cleanup = nil
	if cleanup != nil {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			cleanup()
		}()
	}
}

// reap periodically tears down sessions idle for longer than IdleTTL.
func (d *Driver) reap() {
	defer d.wg.Done()
	interval := d.opts.IdleTTL / 4
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.sweep()
		}
	}
}

// sweep expires idle sessions. Sessions with a verb in flight are
// skipped (TryLock fails) — their lastUsed is refreshed on completion.
func (d *Driver) sweep() {
	cutoff := time.Now().Add(-d.opts.IdleTTL)

	d.mu.Lock()
	type victim struct {
		id string
		s  *session
	}
	var victims []victim
	for id, s := range d.sessions {
		if s.lastUsed.Before(cutoff) {
			victims = append(victims, victim{id, s})
		}
	}
	d.mu.Unlock()

	for _, v := range victims {
		if !v.s.mu.TryLock() {
			continue // verb in flight
		}
		d.mu.Lock()
		// Re-check under the lock: the session may have been touched or
		// replaced between the scan and now.
		cur, ok := d.sessions[v.id]
		if !ok || cur != v.s || !v.s.lastUsed.Before(cutoff) {
			d.mu.Unlock()
			v.s.mu.Unlock()
			continue
		}
		delete(d.sessions, v.id)
		d.mu.Unlock()
		if v.s.cancel != nil {
			v.s.cancel()
			v.s.taskCtx = nil
		}
		cleanup := v.s.cleanup
		v.s.cleanup = nil
		v.s.mu.Unlock()
		if cleanup != nil {
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				cleanup()
			}()
		}
	}
}
