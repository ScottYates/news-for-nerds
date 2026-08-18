package srv

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCSRF_IssuesTokenOnSafeRequest verifies the middleware sets the
// csrf cookie on a safe (GET) request when none is present.
func TestCSRF_IssuesTokenOnSafeRequest(t *testing.T) {
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	var ck *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == csrfCookieName {
			ck = c
			break
		}
	}
	if ck == nil {
		t.Fatal("expected csrf cookie to be set on GET, got none")
	}
	if ck.Value == "" {
		t.Error("csrf cookie has empty value")
	}
	if len(ck.Value) < 20 {
		t.Errorf("csrf cookie value too short (%d chars), want >=20", len(ck.Value))
	}
	if ck.HttpOnly {
		t.Error("csrf cookie must be JS-readable (HttpOnly=false) so the JS can echo it in the header")
	}
}

// TestCSRF_PreservesExistingToken verifies the middleware does NOT
// rotate the token on every safe request. Multi-tab UX requires the
// same token to persist across requests until the user logs in/out.
func TestCSRF_PreservesExistingToken(t *testing.T) {
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	const want = "this-is-a-stable-token"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: want})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	// No Set-Cookie should have been emitted because the token was
	// already there.
	for _, c := range w.Result().Cookies() {
		if c.Name == csrfCookieName {
			if c.Value != want {
				t.Errorf("middleware rotated existing token: got %q, want %q", c.Value, want)
			}
		}
	}
}

// TestCSRF_RejectsStateChangeWithoutToken verifies the middleware
// rejects a POST that has no csrf cookie.
func TestCSRF_RejectsStateChangeWithoutToken(t *testing.T) {
	called := false
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/widgets/1", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if called {
		t.Error("next handler was called despite missing csrf cookie")
	}
}

// TestCSRF_RejectsStateChangeWithMismatchedHeader verifies the
// middleware rejects a POST whose cookie and header differ.
func TestCSRF_RejectsStateChangeWithMismatchedHeader(t *testing.T) {
	called := false
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/widgets/1", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "real-token"})
	req.Header.Set(csrfHeaderName, "attacker-supplied-token")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if called {
		t.Error("next handler was called despite mismatched token")
	}
}

// TestCSRF_RejectsStateChangeWithMissingHeader verifies the middleware
// rejects a POST whose cookie is present but the X-CSRF-Token
// header is missing.
func TestCSRF_RejectsStateChangeWithMissingHeader(t *testing.T) {
	called := false
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/widgets/1", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "real-token"})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if called {
		t.Error("next handler was called despite missing header")
	}
}

// TestCSRF_AllowsStateChangeWithMatchingToken verifies the happy
// path: cookie + matching header lets the request through.
func TestCSRF_AllowsStateChangeWithMatchingToken(t *testing.T) {
	called := false
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	const tok = "matching-token-value"
	req := httptest.NewRequest(http.MethodPost, "/api/widgets/1", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: tok})
	req.Header.Set(csrfHeaderName, tok)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !called {
		t.Error("next handler was not called with valid token")
	}
}

// TestCSRF_AllowsExemptPath verifies the OAuth callback and proxy
// paths bypass the CSRF check (they're exempted for legitimate
// reasons: OAuth flow has its own state-cookie CSRF; proxy is a
// GET-only iframe embed).
func TestCSRF_AllowsExemptPath(t *testing.T) {
	cases := []string{
		"/auth/login",
		"/auth/callback",
		"/auth/logout",
		"/api/proxy?url=https://example.com",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			called := false
			mw := csrfMiddleware([]string{"/auth/", "/api/proxy"}, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(200)
			}))

			req := httptest.NewRequest(http.MethodPost, path, nil)
			// No cookie, no header.
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)

			if !called {
				t.Errorf("exempt path %q was blocked (status=%d)", path, w.Code)
			}
		})
	}
}

// TestCSRF_AllowsHeadAndOptions verifies HEAD and OPTIONS are
// treated as safe methods.
func TestCSRF_AllowsHeadAndOptions(t *testing.T) {
	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			called := false
			mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(200)
			}))

			req := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)

			if !called {
				t.Errorf("method %s was blocked", method)
			}
		})
	}
}

// TestCSRF_RejectsAllStateChangingMethods verifies every
// state-changing method requires a token.
func TestCSRF_RejectsAllStateChangingMethods(t *testing.T) {
	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			called := false
			mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			}))

			req := httptest.NewRequest(m, "/api/x", nil)
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("%s without token: expected 403, got %d", m, w.Code)
			}
			if called {
				t.Errorf("%s without token: next handler was called", m)
			}
		})
	}
}

// TestHasPathPrefix covers the boundary cases of the prefix matcher
// used by the exempt list.
func TestHasPathPrefix(t *testing.T) {
	cases := []struct {
		path, prefix string
		want         bool
	}{
		{"/auth/login", "/auth/", true},
		{"/auth/callback", "/auth/", true},
		{"/auth/logout", "/auth/", true},
		{"/api/auth/status", "/auth/", false},
		{"/api/proxy", "/api/proxy", true},
		{"/api/proxy?url=...", "/api/proxy", true},
		{"/api/proxy/sub", "/api/proxy", true},
		{"/api/proxybar", "/api/proxy", false}, // boundary
		{"/api/", "/api/proxy", false},
		{"/", "", true},
		{"/anything", "", true},
		{"", "/api/", false},
	}
	for _, c := range cases {
		got := hasPathPrefix(c.path, c.prefix)
		if got != c.want {
			t.Errorf("hasPathPrefix(%q, %q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}

// TestConstantTimeEqual covers the length and content cases of the
// constant-time comparison used in the token check.
func TestConstantTimeEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "abcd", false},
		{"abcd", "abc", false},
		{"", "", true},
		{"a", "", false},
		{"", "a", false},
		// Real-world tokens.
		{"abcdefghijklmnop", "abcdefghijklmnop", true},
		{"abcdefghijklmnop", "abcdefghijklmnoq", false},
	}
	for _, c := range cases {
		if got := constantTimeEqual(c.a, c.b); got != c.want {
			t.Errorf("constantTimeEqual(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestNewCSRFToken_GeneratesUniqueValues sanity-checks that each
// call produces a different token (probabilistic — false-negative
// rate is 2^-256 per pair).
func TestNewCSRFToken_GeneratesUniqueValues(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		tok, err := newCSRFToken()
		if err != nil {
			t.Fatalf("newCSRFToken: %v", err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token on iteration %d: %q", i, tok)
		}
		seen[tok] = true
	}
}

// TestCSRF_ErrorBodyIsGeneric checks that the error response doesn't
// leak any of the secret token value (defense in depth in case a
// future change to the response shape accidentally echoes input).
func TestCSRF_ErrorBodyIsGeneric(t *testing.T) {
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	const secret = "the-real-csrf-token-12345"
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: secret})
	req.Header.Set(csrfHeaderName, secret+"tampered")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), secret) {
		t.Errorf("error body leaks the secret token: %q", w.Body.String())
	}
}

// TestCSRF_RequestIsSecure_DefaultIsTrue verifies that the default
// (nil) requestIsSecure function returns true, so Secure cookies
// are set when the caller doesn't override.
func TestCSRF_RequestIsSecure_DefaultIsTrue(t *testing.T) {
	mw := csrfMiddleware(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	var ck *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == csrfCookieName {
			ck = c
			break
		}
	}
	if ck == nil {
		t.Fatal("no csrf cookie")
	}
	if !ck.Secure {
		t.Error("default requestIsSecure should set Secure=true")
	}
}

// TestCSRF_RequestIsSecure_RespectsFunction verifies the per-request
// function is consulted. This is the path used in production
// (r.TLS / X-Forwarded-Proto).
func TestCSRF_RequestIsSecure_RespectsFunction(t *testing.T) {
	check := func(r *http.Request) bool {
		if r.TLS != nil {
			return true
		}
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			return true
		}
		return false
	}
	mw := csrfMiddleware(nil, check, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	cases := []struct {
		name       string
		setupReq   func(*http.Request)
		wantSecure bool
	}{
		{"http no proxy header", func(r *http.Request) {}, false},
		{"https via tls", func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, true},
		{"https via proxy header", func(r *http.Request) {
			r.Header.Set("X-Forwarded-Proto", "https")
		}, true},
		{"http with non-https proxy header", func(r *http.Request) {
			r.Header.Set("X-Forwarded-Proto", "http")
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			c.setupReq(req)
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)
			var ck *http.Cookie
			for _, x := range w.Result().Cookies() {
				if x.Name == csrfCookieName {
					ck = x
					break
				}
			}
			if ck == nil {
				t.Fatal("no csrf cookie")
			}
			if ck.Secure != c.wantSecure {
				t.Errorf("Secure = %v, want %v", ck.Secure, c.wantSecure)
			}
		})
	}
}


