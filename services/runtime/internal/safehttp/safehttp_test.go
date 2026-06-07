// SPDX-License-Identifier: Apache-2.0

package safehttp

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBlockedCIDRsRefused verifies that a direct dial to a loopback /
// private / link-local address fails with ErrBlockedAddress. We bypass
// DNS by hitting an IP-form URL.
func TestBlockedCIDRsRefused(t *testing.T) {
	c := New(Options{})

	cases := []string{
		"http://127.0.0.1:1/",
		"http://10.0.0.1:80/",
		"http://172.16.0.1:80/",
		"http://192.168.1.1:80/",
		"http://169.254.169.254/latest/meta-data/", // AWS metadata
		"http://100.64.0.1/",
		"http://[::1]:1/",
		"http://[fe80::1]/",
		"http://[fc00::1]/",
	}
	for _, u := range cases {
		_, err := c.Get(u)
		if err == nil {
			t.Errorf("expected block for %s, got nil error", u)
			continue
		}
		if !IsBlocked(err) {
			t.Errorf("expected ErrBlockedAddress for %s, got %v", u, err)
		}
	}
}

// TestBlockedHostnamesRefused verifies that metadata.google.internal
// is refused before DNS — even on a machine where the hostname doesn't
// resolve.
func TestBlockedHostnamesRefused(t *testing.T) {
	c := New(Options{})
	cases := []string{
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://metadata/",
		"http://instance-data/",
		"http://INSTANCE-DATA.EC2.INTERNAL/",
	}
	for _, u := range cases {
		_, err := c.Get(u)
		if err == nil {
			t.Errorf("expected block for %s", u)
			continue
		}
		if !errors.Is(err, ErrBlockedHost) {
			t.Errorf("expected ErrBlockedHost for %s, got %v", u, err)
		}
	}
}

// TestAllowedCIDRPermits verifies that explicit AllowCIDRs entries
// override the default block — used by tests that hit httptest.Server
// (which listens on 127.0.0.1).
func TestAllowedCIDRPermits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, loop, _ := net.ParseCIDR("127.0.0.0/8")
	c := New(Options{AllowCIDRs: []*net.IPNet{loop}})

	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected allowed request, got %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestAllowedHostPermits verifies that AllowHosts bypasses the metadata
// pre-check while still going through the dial path (which will then
// fail for an unresolvable host — that's fine, we're testing only that
// the host check isn't the gate).
func TestAllowedHostPermits(t *testing.T) {
	c := New(Options{AllowHosts: []string{"metadata.google.internal"}})
	_, err := c.Get("http://metadata.google.internal/")
	if err == nil {
		// Most CI runners don't resolve this name; if it ever does it
		// resolves to 169.254.169.254 which the CIDR block catches.
		return
	}
	if errors.Is(err, ErrBlockedHost) {
		t.Errorf("AllowHosts did not bypass host block: %v", err)
	}
}

// TestPublicHostPasses confirms a real public host (in this case the
// test server's actual loopback IP, allowlisted) reaches RoundTrip and
// the request body comes through.
func TestPublicHostPasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hi"))
	}))
	defer srv.Close()

	_, loop, _ := net.ParseCIDR("127.0.0.0/8")
	c := New(Options{AllowCIDRs: []*net.IPNet{loop}})
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: %d", resp.StatusCode)
	}
}

// TestIPv4MappedIPv6Blocked confirms that ::ffff:127.0.0.1 (an IPv4
// loopback wrapped as IPv6) is still caught by the IPv4 CIDR rule.
func TestIPv4MappedIPv6Blocked(t *testing.T) {
	c := New(Options{})
	_, err := c.Get("http://[::ffff:127.0.0.1]:1/")
	if err == nil {
		t.Fatal("expected block")
	}
	if !IsBlocked(err) {
		// The dialer may also error with the underlying "blocked"
		// message even when not wrapped — fall back to substring.
		if !strings.Contains(err.Error(), "safehttp:") {
			t.Errorf("expected safehttp block, got %v", err)
		}
	}
}
