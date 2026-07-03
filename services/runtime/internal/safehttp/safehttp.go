// SPDX-License-Identifier: Apache-2.0

// Package safehttp wraps net/http.Client with a defensive net.Dialer
// that refuses connections to private / link-local / loopback IPs and
// well-known cloud-metadata endpoints.
//
// Why this exists
//
// User-supplied URLs flow into:
//
//   - outbound webhooks    (services/runtime/internal/webhooks/outbound.go)
//   - inbound webhook forwards to user-configured destinations
//   - MCP SSE adapter      (services/runtime/internal/mcp/adapters/sse)
//   - LLM gateway provider URLs (if ever configurable per-tenant)
//
// Without a network sandbox, the runtime would let an operator (or a
// compromised tenant) coerce the runtime into hitting the cloud
// instance metadata service (169.254.169.254), a Postgres on a docker
// internal network, or any host on the runtime's private LAN. That is
// classic SSRF.
//
// Design
//
// The Client wraps an http.Client whose Transport uses a net.Dialer
// with a Control hook. The Control hook runs after DNS resolution but
// before the TCP handshake — by inspecting the resolved IP we block
// DNS-rebinding attacks too (resolves to 8.8.8.8 the first time, then
// 127.0.0.1 the second).
//
// Blocked by default:
//   - IPv4 loopback 127.0.0.0/8
//   - IPv4 link-local 169.254.0.0/16 (catches AWS / GCP / Azure metadata)
//   - IPv4 private 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
//   - IPv4 unspec 0.0.0.0/8
//   - IPv4 carrier-grade NAT 100.64.0.0/10 (Tailscale uses this)
//   - IPv6 loopback ::1
//   - IPv6 link-local fe80::/10
//   - IPv6 unique local fc00::/7
//   - IPv6 unspec ::/128
//   - IPv4-mapped IPv6 (::ffff:...) — re-checked as IPv4
//
// Also blocks well-known metadata hostnames before DNS:
//   - metadata.google.internal
//   - metadata
//   - instance-data
//   - instance-data.ec2.internal
//
// Allowlist
//
// Callers can opt-in private IPs by passing an Allowlist of *net.IPNet
// to AllowCIDRs. Used in tests and for self-loopback delivery (the
// dashboard sometimes points a webhook at its own running runtime).
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// ErrBlockedAddress is returned by Do when the destination resolves to
// a blocked CIDR. Wraps the underlying address so callers can log it
// without leaking internal details to the wire response.
var ErrBlockedAddress = errors.New("safehttp: destination address is blocked")

// ErrBlockedHost is returned when the URL host matches a denylisted
// metadata hostname before DNS resolution.
var ErrBlockedHost = errors.New("safehttp: destination host is blocked")

// blockedHosts is checked before any DNS lookup. Lowercased.
var blockedHosts = map[string]struct{}{
	"metadata.google.internal":  {},
	"metadata":                  {},
	"instance-data":             {},
	"instance-data.ec2.internal": {},
}

// defaultBlockedCIDRs is the set of CIDRs the dialer refuses. Built
// once at init time so every dial reuses the parsed *net.IPNet values.
var defaultBlockedCIDRs = mustParseCIDRs([]string{
	"127.0.0.0/8",     // IPv4 loopback
	"10.0.0.0/8",      // RFC 1918 private
	"172.16.0.0/12",   // RFC 1918 private
	"192.168.0.0/16",  // RFC 1918 private
	"169.254.0.0/16",  // link-local (includes cloud metadata)
	"100.64.0.0/10",   // CGNAT (Tailscale, ISPs)
	"0.0.0.0/8",       // "this network" / unspecified
	"::1/128",         // IPv6 loopback
	"fc00::/7",        // IPv6 unique local
	"fe80::/10",       // IPv6 link-local
	"::/128",          // IPv6 unspecified
})

// Options configures a Client. The zero value is sensible for outbound
// webhooks: 30s timeout, default CIDR blocklist, no extra allowlist.
type Options struct {
	// Timeout bounds the entire request (dial + response). Defaults to
	// 30s when zero.
	Timeout time.Duration

	// AllowCIDRs is a per-Client allowlist of CIDRs that override the
	// default block. Used in tests; production callers should rarely
	// touch this.
	AllowCIDRs []*net.IPNet

	// AllowHosts is a per-Client allowlist of literal hostnames that
	// bypass the metadata-host check. Used in tests.
	AllowHosts []string

	// Transport, if non-nil, is wrapped instead of http.DefaultTransport.
	// The dialer of the wrapped transport is replaced with the safe
	// dialer regardless of the input.
	Transport *http.Transport
}

// New constructs a safehttp.Client honouring opts. Always non-nil.
func New(opts Options) *http.Client {
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	d := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlFn(opts.AllowCIDRs),
	}
	tr := opts.Transport
	if tr == nil {
		// Clone the default to inherit proxy/H2 settings without
		// mutating the global.
		tr = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		tr = tr.Clone()
	}
	tr.DialContext = d.DialContext
	// Disable HTTP proxy by default — proxies bypass the dial check.
	// Callers that need a proxy should set Options.Transport with their
	// proxy func and accept the SSRF trade-off.
	tr.Proxy = nil

	allowHosts := map[string]struct{}{}
	for _, h := range opts.AllowHosts {
		allowHosts[strings.ToLower(h)] = struct{}{}
	}

	return &http.Client{
		Transport: &hostGuardTransport{
			next:       tr,
			allowHosts: allowHosts,
		},
		Timeout: opts.Timeout,
	}
}

// hostGuardTransport short-circuits requests whose URL host matches a
// known cloud-metadata name. The dialer Control hook below catches the
// IP case; this catches the pre-DNS name case (e.g. an attacker who
// passes "metadata.google.internal" as a URL — the dialer might be
// happy with the resolved IP if it's not in our blocklist, depending
// on the deployment's DNS).
type hostGuardTransport struct {
	next       *http.Transport
	allowHosts map[string]struct{}
}

func (h *hostGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Hostname())
	if _, ok := h.allowHosts[host]; !ok {
		if _, blocked := blockedHosts[host]; blocked {
			return nil, fmt.Errorf("%w: %s", ErrBlockedHost, host)
		}
	}
	return h.next.RoundTrip(req)
}

// controlFn returns a net.Dialer.Control callback that inspects the
// resolved (network, address) pair and refuses blocked CIDRs.
//
// extraAllow is merged into the per-call allow check; CIDRs in
// extraAllow override defaultBlockedCIDRs.
func controlFn(extraAllow []*net.IPNet) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("safehttp: split host: %w", err)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			// The dialer should have already resolved this — refuse
			// rather than fall through and let an attacker exploit a
			// double-resolve.
			return fmt.Errorf("%w: unresolved host %q", ErrBlockedAddress, host)
		}
		// Normalise IPv4-mapped IPv6 back to IPv4 so the CIDR check
		// catches `::ffff:127.0.0.1`.
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		// Allowlist short-circuit.
		for _, n := range extraAllow {
			if n.Contains(ip) {
				return nil
			}
		}
		for _, n := range defaultBlockedCIDRs {
			if n.Contains(ip) {
				return fmt.Errorf("%w: %s in %s", ErrBlockedAddress, ip.String(), n.String())
			}
		}
		return nil
	}
}

// mustParseCIDRs panics on a bad CIDR — only called from the package
// var initialiser so a typo is caught at build/test time.
func mustParseCIDRs(in []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(in))
	for _, s := range in {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			panic("safehttp: bad CIDR " + s + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}

// LoopbackAndPrivateCIDRs returns the loopback + RFC-1918 + CGNAT ranges
// as an allowlist. Intended for dev / PoC deployments that legitimately
// deliver to a service on the same host or private network (e.g. a
// webhook subscriber running on localhost). Production callers should NOT
// pass this — it re-permits the exact ranges the SSRF guard exists to
// block. Link-local (169.254/16, fe80::/10) is deliberately excluded so
// cloud-metadata endpoints stay blocked even in dev.
func LoopbackAndPrivateCIDRs() []*net.IPNet {
	return mustParseCIDRs([]string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"::1/128",
		"fc00::/7",
	})
}

// IsBlocked reports whether err is one of the safehttp sentinel errors.
// Callers translating a transport error to a user-facing message can
// use this to distinguish "we refused to dial" from "the remote went
// away".
func IsBlocked(err error) bool {
	return errors.Is(err, ErrBlockedAddress) || errors.Is(err, ErrBlockedHost)
}

// Compile-time assertion: hostGuardTransport satisfies http.RoundTripper.
var _ http.RoundTripper = (*hostGuardTransport)(nil)

// Ensure context import used (will be once we add Stop variants).
var _ = context.Background
