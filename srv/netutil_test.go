package srv

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseTrustedProxies(t *testing.T) {
	pt, err := parseTrustedProxies("")
	if err != nil {
		t.Fatal(err)
	}
	if !pt.trusts(net.ParseIP("127.0.0.1")) || !pt.trusts(net.ParseIP("10.1.2.3")) {
		t.Error("default should trust loopback and private peers")
	}
	if pt.trusts(net.ParseIP("203.0.113.9")) {
		t.Error("default must not trust a public peer")
	}
	if pt, _ := parseTrustedProxies("none"); pt.trusts(net.ParseIP("127.0.0.1")) {
		t.Error("none must trust nobody")
	}
	if pt, _ := parseTrustedProxies("all"); !pt.trusts(net.ParseIP("203.0.113.9")) {
		t.Error("all must trust everyone")
	}
	pt, err = parseTrustedProxies("203.0.113.9, 198.51.100.0/24")
	if err != nil || !pt.trusts(net.ParseIP("203.0.113.9")) || !pt.trusts(net.ParseIP("198.51.100.7")) || pt.trusts(net.ParseIP("10.0.0.1")) {
		t.Errorf("explicit list not honoured (err=%v)", err)
	}
	if _, err := parseTrustedProxies("not-an-ip"); err == nil {
		t.Error("expected an error for an invalid entry")
	}
}

// TestForwardedHeadersOnlyFromTrustedPeer checks that a client on a
// public address cannot set the scheme or host the server believes.
func TestForwardedHeadersOnlyFromTrustedPeer(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	req := httptest.NewRequest(http.MethodGet, "http://app.example/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.example")

	req.RemoteAddr = "203.0.113.9:4444"
	if server.requestIsSecure(req) {
		t.Error("X-Forwarded-Proto believed from an untrusted peer")
	}
	if got := server.getCookieDomain(req); got != "app.example" {
		t.Errorf("X-Forwarded-Host believed from an untrusted peer: %q", got)
	}

	req.RemoteAddr = "127.0.0.1:4444"
	if !server.requestIsSecure(req) {
		t.Error("X-Forwarded-Proto ignored from a trusted peer")
	}
	if got := server.getCookieDomain(req); got != "evil.example" {
		t.Errorf("X-Forwarded-Host ignored from a trusted peer: %q", got)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(3)
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !rl.allow("a", now) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if rl.allow("a", now) {
		t.Error("fourth request in the same instant should be refused")
	}
	if !rl.allow("b", now) {
		t.Error("a different key has its own bucket")
	}
	if !rl.allow("a", now.Add(time.Minute)) {
		t.Error("tokens should refill after a minute")
	}
	if !newRateLimiter(0).allow("a", now) {
		t.Error("a limit of 0 disables limiting")
	}
}
