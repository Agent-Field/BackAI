// SPDX-License-Identifier: Apache-2.0

package cdpdriver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrBlockedEndpoint is returned when a CDP endpoint's host resolves to
// a loopback / private / link-local address and AllowPrivate is off.
var ErrBlockedEndpoint = errors.New("cdpdriver: endpoint address is blocked (set AF_STACK_BROWSER_ALLOW_PRIVATE to permit private CDP endpoints)")

// blockedHosts mirrors internal/safehttp: well-known cloud-metadata
// hostnames refused before DNS resolution.
var blockedHosts = map[string]struct{}{
	"metadata.google.internal":   {},
	"metadata":                   {},
	"instance-data":              {},
	"instance-data.ec2.internal": {},
}

// blockedCIDRs mirrors internal/safehttp's default blocklist. That
// package keeps the list unexported and enforces it inside an
// http.Client dialer — chromedp owns its own websocket dialer, so the
// hosted-provider adapters re-state the ranges here and check before
// handing the URL to chromedp.
var blockedCIDRs = mustParseCIDRs([]string{
	"127.0.0.0/8",    // IPv4 loopback
	"10.0.0.0/8",     // RFC 1918 private
	"172.16.0.0/12",  // RFC 1918 private
	"192.168.0.0/16", // RFC 1918 private
	"169.254.0.0/16", // link-local (includes cloud metadata)
	"100.64.0.0/10",  // CGNAT (Tailscale, ISPs)
	"0.0.0.0/8",      // "this network" / unspecified
	"::1/128",        // IPv6 loopback
	"fc00::/7",       // IPv6 unique local
	"fe80::/10",      // IPv6 link-local
	"::/128",         // IPv6 unspecified
})

// CheckEndpoint refuses ws/wss URLs whose host is a metadata hostname
// or resolves to a blocked range. It resolves via the default resolver
// and rejects if ANY returned address is blocked (conservative w.r.t.
// dual-horizon DNS).
//
// Caveat: this is a check-then-dial guard, not an in-dialer Control
// hook like safehttp's — a DNS record that flips between the check and
// chromedp's own dial (classic rebinding) is not caught. Acceptable
// here because the endpoint host is operator-configured (an env var or
// a trusted provider API response), not tenant input.
func CheckEndpoint(ctx context.Context, wsURL string) error {
	u, err := url.Parse(wsURL)
	if err != nil {
		return fmt.Errorf("cdpdriver: parse endpoint: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("cdpdriver: endpoint scheme %q is not ws/wss", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("cdpdriver: endpoint %q has no host", wsURL)
	}
	if _, bad := blockedHosts[host]; bad {
		return fmt.Errorf("%w: %s", ErrBlockedEndpoint, host)
	}

	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return fmt.Errorf("cdpdriver: resolve endpoint host %q: %w", host, err)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4 // normalise IPv4-mapped IPv6 (::ffff:127.0.0.1)
		}
		for _, n := range blockedCIDRs {
			if n.Contains(ip) {
				return fmt.Errorf("%w: %s in %s", ErrBlockedEndpoint, ip, n)
			}
		}
	}
	return nil
}

func mustParseCIDRs(in []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(in))
	for _, s := range in {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			panic("cdpdriver: bad CIDR " + s + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}
