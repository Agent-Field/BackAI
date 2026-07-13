// SPDX-License-Identifier: Apache-2.0

package cdpdriver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Validation contract ---
//
// 1. Endpoints whose host is loopback / RFC-1918 / link-local / CGNAT /
//    metadata are refused unless AllowPrivate is set.
// 2. Verbs after Close() fail with ErrClosed.
// 3. An Endpoint error is surfaced to the caller verbatim (wrapped).
// 4. When the endpoint is dialed but unreachable, the verb fails AND the
//    provider cleanup fires exactly once (no leaked provider sessions);
//    a subsequent verb retries Endpoint (the session was dropped).

func TestCheckEndpointBlocksPrivateRanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	blocked := []string{
		"ws://127.0.0.1:9222",
		"ws://10.1.2.3:3000",
		"ws://172.16.0.9:3000",
		"ws://192.168.1.10:3000",
		"ws://169.254.169.254/latest",
		"ws://100.64.0.1:3000",
		"ws://[::1]:9222",
		"ws://[fe80::1]:9222",
		"ws://[::ffff:127.0.0.1]:9222", // IPv4-mapped IPv6
		"wss://metadata.google.internal/x",
	}
	for _, u := range blocked {
		if err := CheckEndpoint(ctx, u); err == nil {
			t.Errorf("CheckEndpoint(%q) = nil, want blocked", u)
		}
	}

	// Public literal IPs pass without DNS.
	allowed := []string{"wss://93.184.216.34:443/cdp", "ws://8.8.8.8:9222"}
	for _, u := range allowed {
		if err := CheckEndpoint(ctx, u); err != nil {
			t.Errorf("CheckEndpoint(%q) = %v, want nil", u, err)
		}
	}

	// Non-websocket schemes are refused outright.
	if err := CheckEndpoint(ctx, "http://93.184.216.34/json"); err == nil {
		t.Error("CheckEndpoint(http://...) = nil, want scheme error")
	}
}

func TestVerbsAfterCloseReturnErrClosed(t *testing.T) {
	t.Parallel()
	d := New(Options{
		AllowPrivate: true,
		Endpoint: func(context.Context, string) (string, func(), error) {
			return "ws://127.0.0.1:1", nil, nil
		},
	})
	d.Close()
	if _, err := d.Navigate(context.Background(), "s", "https://example.com"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Navigate after Close = %v, want ErrClosed", err)
	}
	d.Close() // idempotent
}

func TestEndpointErrorSurfacesToCaller(t *testing.T) {
	t.Parallel()
	boom := errors.New("no capacity")
	d := New(Options{
		AllowPrivate: true,
		Endpoint: func(context.Context, string) (string, func(), error) {
			return "", nil, boom
		},
	})
	defer d.Close()
	if _, err := d.ExtractText(context.Background(), "s"); !errors.Is(err, boom) {
		t.Fatalf("ExtractText = %v, want wrapped %v", err, boom)
	}
}

func TestDialFailureFiresCleanupOnceAndRetriesEndpoint(t *testing.T) {
	t.Parallel()

	// A TCP listener that accepts and immediately closes: the websocket
	// dial reliably fails without a real browser.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	var endpointCalls, cleanups atomic.Int32
	d := New(Options{
		AllowPrivate:    true,
		NavigateTimeout: 5 * time.Second,
		Endpoint: func(context.Context, string) (string, func(), error) {
			endpointCalls.Add(1)
			return fmt.Sprintf("ws://%s", ln.Addr()), func() { cleanups.Add(1) }, nil
		},
	})

	for i := 1; i <= 2; i++ {
		_, err := d.Navigate(context.Background(), "sess-a", "https://example.com")
		if err == nil {
			t.Fatalf("Navigate #%d succeeded against a dead endpoint", i)
		}
		if strings.Contains(err.Error(), "not configured") {
			t.Fatalf("Navigate #%d returned unexpected error class: %v", i, err)
		}
	}
	d.Close() // waits for async cleanups

	if got := endpointCalls.Load(); got != 2 {
		t.Errorf("Endpoint called %d times, want 2 (session dropped after dial failure)", got)
	}
	if got := cleanups.Load(); got != 2 {
		t.Errorf("cleanup fired %d times, want 2 (once per failed connect)", got)
	}
}
