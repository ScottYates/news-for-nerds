package srv

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// testDBPath returns a fresh per-test SQLite path so handlers that
// touch the DB can be exercised in isolation.
func testDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.sqlite3")
}

// TestHandleLogin_RejectsOpenRedirectInReturn exercises the full HTTP
// flow: a hostile `return` query string must not make it into the
// oauth_return cookie, because that cookie would later be turned into
// a post-login Location: header in HandleCallback.
func TestHandleLogin_RejectsOpenRedirectInReturn(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })
	// Skip the actual Google URL by configuring a client ID so the
	// handler runs past its OAuth-not-configured guard.
	server.Config.GoogleClientID = "test-client-id.apps.googleusercontent.com"

	cases := []struct {
		name      string
		returnVal string
		referer   string
		wantOK    bool // expect oauth_return cookie to be set
	}{
		{"safe relative", "/dashboard", "", true},
		{"safe relative with query", "/page?id=1", "", true},
		{"absolute https", "https://evil.com/", "", false},
		{"protocol relative", "//evil.com/", "", false},
		{"javascript scheme", "javascript:alert(1)", "", false},
		{"data scheme", "data:text/html,<script>", "", false},
		{"backslashes", "/\\evil.com", "", false},
		{"header injection", "/page\r\nLocation: https://evil.com", "", false},
		{"no leading slash", "evil.com", "", false},
		// If both query and referer are hostile, neither cookie is set.
		{"both hostile", "https://evil.com/", "https://other.com/", false},
		// Hostile query but safe referer — cookie should use the referer.
		{"hostile query safe referer", "https://evil.com/", "/safe-page", true},
		// Safe query but hostile referer — cookie should use the query value.
		{"safe query hostile referer", "/safe-page", "https://evil.com/", true},
		// Both safe — query wins (since it's checked first).
		{"both safe", "/from-query", "/from-referer", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/login?return="+url.QueryEscape(c.returnVal), nil)
			if c.referer != "" {
				req.Header.Set("Referer", c.referer)
			}
			w := httptest.NewRecorder()
			server.HandleLogin(w, req)

			// The handler will redirect to Google's OAuth URL. We don't
			// care about that — we care whether the oauth_return cookie
			// was set and to what value.
			var gotCookie *http.Cookie
			for _, ck := range w.Result().Cookies() {
				if ck.Name == "oauth_return" {
					gotCookie = ck
					break
				}
			}
			if c.wantOK {
				if gotCookie == nil {
					t.Fatalf("expected oauth_return cookie to be set, got none. cookies: %v", w.Result().Cookies())
				}
				if !strings.HasPrefix(gotCookie.Value, "/") {
					t.Errorf("cookie value %q is not a relative path", gotCookie.Value)
				}
				// It must not contain any of the rejection patterns.
				if strings.HasPrefix(gotCookie.Value, "//") {
					t.Errorf("cookie value %q looks protocol-relative", gotCookie.Value)
				}
			} else {
				if gotCookie != nil {
					t.Errorf("expected oauth_return cookie NOT to be set, got %q", gotCookie.Value)
				}
			}
		})
	}
}

// TestHandleCallback_RejectsTamperedReturnCookie verifies that even
// if a user hand-edits the oauth_return cookie (e.g. via DevTools)
// to an absolute URL, the callback handler's re-validation prevents
// the redirect.
//
// We can't easily reach the post-validation redirect branch without
// mocking the Google token exchange, so the test exercises the helper
// the handler delegates to (safeReturnPath) for the values the cookie
// might hold, and also confirms the handler's fallback behavior ("" → "/")
// at the same time.
func TestHandleCallback_RejectsTamperedReturnCookie(t *testing.T) {
	cases := []struct {
		name      string
		cookieVal string
		wantSafe  string // expected safeReturnPath output
		wantLoc   string // expected Location after handler's "/" fallback
	}{
		{"safe relative", "/dashboard", "/dashboard", "/dashboard"},
		{"empty cookie", "", "", "/"},
		{"absolute https", "https://evil.com/", "", "/"},
		{"protocol relative", "//evil.com/", "", "/"},
		{"javascript scheme", "javascript:alert(1)", "", "/"},
		{"backslashes", "/\\evil.com", "", "/"},
		{"header injection", "/page\r\nLocation: https://evil.com", "", "/"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := safeReturnPath(c.cookieVal)
			if got != c.wantSafe {
				t.Errorf("safeReturnPath(%q) = %q, want %q", c.cookieVal, got, c.wantSafe)
			}
			// Simulate the handler's fallback: if safeReturnPath returned "",
			// the handler would set returnURL = "/".
			loc := got
			if loc == "" {
				loc = "/"
			}
			if loc != c.wantLoc {
				t.Errorf("handler's eventual redirect for cookie %q -> %q, want %q", c.cookieVal, loc, c.wantLoc)
			}
		})
	}
}
