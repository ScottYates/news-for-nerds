package srv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_DefaultHeaders(t *testing.T) {
	mw := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	h := w.Result().Header
	cases := []struct {
		name   string
		key    string
		mustHave string
	}{
		{"CSP set", "Content-Security-Policy", "default-src 'self'"},
		{"CSP frame-ancestors", "Content-Security-Policy", "frame-ancestors 'self'"},
		{"CSP object-src none", "Content-Security-Policy", "object-src 'none'"},
		{"CSP base-uri", "Content-Security-Policy", "base-uri 'self'"},
		{"nosniff", "X-Content-Type-Options", "nosniff"},
		{"referrer policy", "Referrer-Policy", "strict-origin-when-cross-origin"},
		{"frame options", "X-Frame-Options", "SAMEORIGIN"},
		{"permissions policy", "Permissions-Policy", "camera=()"},
		{"permissions policy mic", "Permissions-Policy", "microphone=()"},
		{"permissions policy geo", "Permissions-Policy", "geolocation=()"},
		{"coep", "Cross-Origin-Opener-Policy", "same-origin"},
	}
	for _, c := range cases {
		got := h.Get(c.key)
		if got == "" {
			t.Errorf("%s: header is empty", c.name)
			continue
		}
		if !strings.Contains(got, c.mustHave) {
			t.Errorf("%s: header %q does not contain %q", c.name, got, c.mustHave)
		}
	}
}

func TestSecurityHeaders_AllHeadersPresent(t *testing.T) {
	mw := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	expected := []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"Referrer-Policy",
		"X-Frame-Options",
		"Permissions-Policy",
		"Cross-Origin-Opener-Policy",
	}
	for _, k := range expected {
		if w.Result().Header.Get(k) == "" {
			t.Errorf("header %q is missing", k)
		}
	}
}

// TestSecurityHeaders_HandlerCanOverride verifies a handler can
// override the default by setting the header itself before any
// body is written. This is important for the proxy endpoint which
// needs to forward certain content-type signals that might
// otherwise be gated by the default.
func TestSecurityHeaders_HandlerCanOverride(t *testing.T) {
	mw := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Result().Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("handler-set X-Frame-Options not honored: got %q, want %q", got, "DENY")
	}
}

// TestSecurityHeaders_CSPAllowsProxyAndInline confirms the CSP
// policy permits what the app actually needs:
//   - inline scripts (proxy-injected visited-link tracker)
//   - inline styles (per-widget colors)
//   - data: URLs (favicons)
//   - arbitrary frame-src (widget iframes via the proxy)
func TestSecurityHeaders_CSPAllowsProxyAndInline(t *testing.T) {
	mw := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	csp := w.Result().Header.Get("Content-Security-Policy")
	mustContain := []string{
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"frame-src *",
		"object-src 'none'",
		"base-uri 'self'",
		"https://cdnjs.cloudflare.com", // TinyMCE editor
		"form-action 'self'",
	}
	for _, want := range mustContain {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q\nfull CSP: %s", want, csp)
		}
	}
}

// TestSecurityHeaders_DoesNotOverrideUnrelated verifies that a
// handler can set its own headers (e.g. Content-Type) without the
// security middleware clobbering them.
func TestSecurityHeaders_DoesNotOverrideUnrelated(t *testing.T) {
	mw := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if got := w.Result().Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type clobbered: %q", got)
	}
	if got := w.Result().Header.Get("X-Custom"); got != "value" {
		t.Errorf("X-Custom clobbered: %q", got)
	}
	// Security headers should still be set.
	if got := w.Result().Header.Get("X-Content-Type-Options"); got == "" {
		t.Error("X-Content-Type-Options not set")
	}
}
