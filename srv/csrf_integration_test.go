package srv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServe_FullStack_HappyPath wires up the same middleware chain
// used in production (security headers + CSRF) and walks a request
// through it. This catches integration bugs (cookie domain issues,
// header-ordering, CSRF exemption off-by-one) that unit tests miss.
func TestServe_FullStack_HappyPath(t *testing.T) {
	// Build a minimal handler chain that mirrors the production wrap.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the X-CSRF-Token header so the test can verify the
		// middleware let the request through with the header intact.
		w.Header().Set("Echo-CSRF", r.Header.Get(csrfHeaderName))
		w.WriteHeader(200)
	})
	handler := securityHeadersMiddleware(inner)
	handler = csrfMiddleware(nil, nil, nil, handler)

	// Step 1: GET to obtain a CSRF token cookie.
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/", nil))

	// Security headers must be set on the safe response.
	if rec1.Result().Header.Get("Content-Security-Policy") == "" {
		t.Error("CSP not set on safe response")
	}

	var csrfCookie *http.Cookie
	for _, c := range rec1.Result().Cookies() {
		if c.Name == csrfCookieName {
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil {
		t.Fatal("csrf cookie not issued on GET")
	}

	// Step 2: POST with the cookie and a matching header.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req2.AddCookie(csrfCookie)
	req2.Header.Set(csrfHeaderName, csrfCookie.Value)
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != 200 {
		t.Errorf("happy path POST: got %d, want 200 (body: %s)", rec2.Code, rec2.Body.String())
	}
	if got := rec2.Result().Header.Get("Echo-CSRF"); got != csrfCookie.Value {
		t.Errorf("inner handler did not see the CSRF header: got %q, want %q", got, csrfCookie.Value)
	}
}

// TestServe_FullStack_AttackRejected confirms a cross-origin attacker
// (who can't read our cookies and can't set our header) is rejected
// at the boundary.
func TestServe_FullStack_AttackRejected(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	})
	handler := securityHeadersMiddleware(inner)
	handler = csrfMiddleware(nil, nil, nil, handler)

	// Attacker submits a POST with a forged header but no cookie.
	// (In a real attack, the attacker would not have the cookie at all.)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/widgets/1", nil)
	req.Header.Set(csrfHeaderName, "forged-token")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("attacker request: got %d, want 403", rec.Code)
	}
	if called {
		t.Error("inner handler was called despite missing cookie")
	}
	// Even on the 403, security headers should still be set.
	if rec.Result().Header.Get("X-Content-Type-Options") == "" {
		t.Error("security headers not set on 403 response")
	}
}

// TestServe_FullStack_ExemptPathBypassesCSRF verifies that an exempt
// path (e.g. /auth/callback) can be hit with a POST without a token,
// because the OAuth flow has its own state-cookie CSRF defense.
func TestServe_FullStack_ExemptPathBypassesCSRF(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	})
	handler := securityHeadersMiddleware(inner)
	handler = csrfMiddleware([]string{"/auth/"}, nil, nil, handler)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/callback", nil))

	if !called {
		t.Error("exempt path was blocked")
	}
	if rec.Code != 200 {
		t.Errorf("got %d, want 200", rec.Code)
	}
}

// TestCSRFCookie_NotHttpOnly confirms the cookie is JS-readable. The
// fetch wrapper in app.js needs to read it.
func TestCSRFCookie_NotHttpOnly(t *testing.T) {
	c := csrfCookie("tok", "example.com", true)
	if c.HttpOnly {
		t.Error("csrf cookie must NOT be HttpOnly so JS can read it for the X-CSRF-Token header")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("csrf cookie SameSite = %v, want Lax", c.SameSite)
	}
	if !c.Secure {
		t.Error("csrf cookie should be Secure in production")
	}
	if c.Path != "/" {
		t.Errorf("csrf cookie Path = %q, want /", c.Path)
	}
	if c.Name != csrfCookieName {
		t.Errorf("csrf cookie name = %q, want %q", c.Name, csrfCookieName)
	}
}

// TestCSRFIntegration_RealHandlerBypass makes sure existing handler
// tests that call handlers directly (bypassing the middleware) still
// work — i.e. CSRF is layered ON TOP of the handlers, not part of
// them. We exercise one of the state-changing handlers via a
// direct call with no token and expect success, which proves CSRF
// is not in the direct-call path.
func TestCSRFIntegration_RealHandlerBypass(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	// Direct call to a state-changing handler with no CSRF token.
	// The handler itself doesn't know about CSRF — that's the
	// middleware's job. So the request should reach the handler
	// (which will 404 because no widget exists, but that's fine).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/widgets/abc",
		strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	server.HandleAPIUpdateWidget(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Errorf("direct handler call was CSRF-rejected (status %d); the CSRF middleware should sit above the handler, not inside it", rec.Code)
	}
}


