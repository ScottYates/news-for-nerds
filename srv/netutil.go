package srv

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// defaultTrustedProxyCIDRs is what TRUSTED_PROXIES=private means: the
// address ranges a reverse proxy on the same host or the same private
// network connects from.
var defaultTrustedProxyCIDRs = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"100.64.0.0/10", "169.254.0.0/16",
	"fc00::/7", "fe80::/10",
}

// proxyTrust decides whether X-Forwarded-* headers on a request may
// be believed. They are only meaningful when the direct peer is a
// reverse proxy we trust; from any other client they are attacker
// input.
type proxyTrust struct {
	all  bool
	nets []*net.IPNet
}

// parseTrustedProxies reads the TRUSTED_PROXIES setting: "private"
// (default), "all", "none", or a comma-separated list of CIDRs/IPs.
func parseTrustedProxies(spec string) (proxyTrust, error) {
	spec = strings.TrimSpace(strings.ToLower(spec))
	var cidrs []string
	switch spec {
	case "", "private":
		cidrs = defaultTrustedProxyCIDRs
	case "all", "*":
		return proxyTrust{all: true}, nil
	case "none":
		return proxyTrust{}, nil
	default:
		for _, part := range strings.Split(spec, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, "/") {
				if strings.Contains(part, ":") {
					part += "/128"
				} else {
					part += "/32"
				}
			}
			cidrs = append(cidrs, part)
		}
	}
	var pt proxyTrust
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return proxyTrust{}, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP or CIDR", c)
		}
		pt.nets = append(pt.nets, n)
	}
	return pt, nil
}

func (pt proxyTrust) trusts(ip net.IP) bool {
	if pt.all {
		return true
	}
	if ip == nil {
		return false
	}
	for _, n := range pt.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// peerIP returns the IP of the directly connected client.
func peerIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// forwardedHeader returns the named X-Forwarded-* header if the
// request came from a trusted proxy, and "" otherwise.
func (s *Server) forwardedHeader(r *http.Request, name string) string {
	if !s.proxyTrust.trusts(peerIP(r)) {
		return ""
	}
	return strings.TrimSpace(r.Header.Get(name))
}

// clientIP returns the address to rate-limit on: the last entry of
// X-Forwarded-For when a trusted proxy supplied it (that entry is the
// one the proxy itself added), otherwise the direct peer.
func (s *Server) clientIP(r *http.Request) string {
	if fwd := s.forwardedHeader(r, "X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
			return ip.String()
		}
	}
	if ip := peerIP(r); ip != nil {
		return ip.String()
	}
	return r.RemoteAddr
}

// rateLimiter is a per-key token bucket. It is small on purpose: the
// app runs as a single process, so an in-memory map is enough.
type rateLimiter struct {
	mu      sync.Mutex
	perMin  float64
	burst   float64
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	seen   time.Time
}

// newRateLimiter allows perMin requests a minute per key, with a
// burst of the same size so a dashboard full of widgets can load at
// once. perMin <= 0 disables limiting.
func newRateLimiter(perMin int) *rateLimiter {
	return &rateLimiter{
		perMin:  float64(perMin),
		burst:   float64(perMin),
		buckets: make(map[string]*bucket),
		lastGC:  time.Now(),
	}
}

func (rl *rateLimiter) allow(key string, now time.Time) bool {
	if rl == nil || rl.perMin <= 0 {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Drop idle buckets now and then so the map cannot grow forever.
	if now.Sub(rl.lastGC) > 5*time.Minute {
		for k, b := range rl.buckets {
			if now.Sub(b.seen) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.lastGC = now
	}

	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: rl.burst, seen: now}
		rl.buckets[key] = b
	}
	b.tokens += now.Sub(b.seen).Minutes() * rl.perMin
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.seen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// rateLimited wraps the handlers that make the server fetch remote
// content on a caller's behalf.
func (s *Server) rateLimited(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow(s.clientIP(r), time.Now()) {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
