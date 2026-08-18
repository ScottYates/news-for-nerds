package srv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// safeTransport wraps base with a DialContext that rejects connections to
// loopback, link-local, private, and cloud-metadata IP ranges. This
// defends the server's outbound HTTP fetchers (proxy, RSS, HN, favicon)
// against Server-Side Request Forgery where a user-supplied URL is
// followed into the local network or the cloud metadata service.
//
// Defense layers:
//
//  1. Hostname-to-IP resolution is done inside the dialer, so any IP the
//     hostname resolves to is checked before the connect syscall.
//  2. The dial pins to the first resolved IP, defeating DNS rebinding
//     where a hostname resolves to a public IP at validation time and
//     to a private IP at connect time. The Host header (and TLS SNI)
//     still uses the original hostname, so HTTPS works correctly.
//  3. If the host is already a literal IP, it's checked directly without
//     going through DNS.
//
// To allow a self-hosted internal feed (rare), set the request context
// with safeDialBypassKey to true; production code never does this.
func safeTransport(base *http.Transport) *http.Transport {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	t := base.Clone()

	// Preserve the existing dialer (or fall back to a fresh one with a
	// sensible timeout) and wrap its DialContext with our guard.
	inner := t.DialContext
	if inner == nil {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		inner = d.DialContext
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Per-request bypass for trusted callers (none today).
		if v, ok := ctx.Value(safeDialBypassKey{}).(bool); ok && v {
			return inner(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			if isDeniedIP(ip) {
				return nil, fmt.Errorf("dial %s blocked: address in deny list", addr)
			}
			return inner(ctx, network, addr)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		for _, ipa := range ips {
			if isDeniedIP(ipa.IP) {
				return nil, fmt.Errorf("dial %s blocked: %s resolves to denied address", addr, ipa.IP)
			}
		}
		// Pin to the first IP. The http.Transport will set the Host
		// header / TLS SNI to the original hostname from the URL.
		first := ips[0].IP
		pinned := net.JoinHostPort(first.String(), port)
		return inner(ctx, network, pinned)
	}
	return t
}

// safeDialBypassKey is a context key used to opt out of the SSRF guard
// for a single request. Intentionally not exported — there are no
// legitimate in-tree uses today; reserved for future operator config
// (e.g. a flag that allows internal-only feeds).
type safeDialBypassKey struct{}

// isDeniedIP reports whether ip belongs to a range that the server's
// outbound fetchers should never connect to. The intent is to block
// SSRF pivot points: loopback, link-local, private RFC1918, ULA,
// cloud-provider metadata IPs, CGNAT, and the unspecified / multicast
// ranges that no legitimate public service would use.
func isDeniedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	// AWS / GCP / Azure metadata service. Link-local would already
	// catch 169.254.0.0/16, but make the intent explicit and future-
	// proof against any other 169.254/16 carve-outs.
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}
	// 100.64.0.0/10 — RFC6598 carrier-grade NAT. Not a normal public
	// destination for a feed reader.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	// IPv6 unique local addresses (fc00::/7).
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	// IPv4-mapped IPv6 of any of the above. To4/To16 normalization
	// already covers most cases; this is belt-and-suspenders.
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 0 || v4[0] >= 224 {
			return true
		}
	}
	return false
}

// validateOutboundURL parses a user-supplied URL and returns a
// non-nil error if the scheme is not http(s), the host is missing,
// or the host resolves to a denied IP range. Use this at the API
// boundary to surface a 400 instead of a connect-time failure.
//
// This is the *input* half of the SSRF defense. The *transport*
// half (safeTransport) is what stops DNS-rebinding mid-flight.
func validateOutboundURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (use http or https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url missing host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isDeniedIP(ip) {
			return fmt.Errorf("url host %s is in deny list", host)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %s did not resolve", host)
	}
	for _, ipa := range ips {
		if isDeniedIP(ipa.IP) {
			return fmt.Errorf("url host %s resolves to denied address %s", host, ipa.IP)
		}
	}
	return nil
}
