package srv

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
)

// csrfCookieName is the name of the cookie used for the
// double-submit CSRF token. JS reads the value and echoes it back
// in the X-CSRF-Token header on every state-changing request.
const csrfCookieName = "csrf"

// csrfHeaderName is the request header that must carry the same
// value as csrfCookieName.
const csrfHeaderName = "X-CSRF-Token"

// csrfTokenBytes is the entropy of the CSRF token. 32 bytes / 256
// bits is well over what's needed to make guessing infeasible.
const csrfTokenBytes = 32

// newCSRFToken returns a fresh random CSRF token suitable for
// setting in a cookie and echoing in the X-CSRF-Token header.
func newCSRFToken() (string, error) {
	b := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// csrfCookie returns a Set-Cookie header value for the given token.
// The cookie is intentionally NOT HttpOnly — the JS must be able to
// read it to echo it in the X-CSRF-Token header. SameSite=Lax so the
// browser still sends it on top-level navigations (defense in depth;
// the header check is the primary control).
func csrfCookie(token, domain string, secure bool) *http.Cookie {
	c := &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return c
}

// csrfMiddleware returns a handler that issues a CSRF cookie on
// safe (GET/HEAD/OPTIONS) requests and rejects state-changing
// requests that don't carry a matching X-CSRF-Token header.
//
// The token is read from the csrf cookie and rotated only on safe
// requests that don't already have one (so multiple tabs share a
// stable token; login/logout can call ensureCSRFToken to rotate).
//
// Safe paths (OAuth, the proxy, etc.) can be exempted with the
// exempt prefix list. Anything matching one of these prefixes is
// allowed through without a CSRF check.
//
// requestIsSecure is called per-request to determine whether the
// cookie's Secure flag should be set. Pass nil to always set
// Secure=true (production default); pass a function that inspects
// r.TLS and r.Header.Get("X-Forwarded-Proto") for behind-a-proxy
// deployments.
func csrfMiddleware(exemptPrefixes []string, requestIsSecure func(*http.Request) bool, domainFn func(*http.Request) string, next http.Handler) http.Handler {
	exempt := make(map[string]bool, len(exemptPrefixes))
	for _, p := range exemptPrefixes {
		exempt[p] = true
	}
	if requestIsSecure == nil {
		requestIsSecure = func(*http.Request) bool { return true }
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always issue a token on safe requests if the cookie is
		// missing. The response writer is wrapped so we can set the
		// cookie before any handler writes.
		if isSafeMethod(r.Method) {
			// Make sure the token cookie is set so the next POST
			// has something to compare against.
			if _, err := r.Cookie(csrfCookieName); err != nil {
				tok, terr := newCSRFToken()
				if terr != nil {
					slog.Warn("csrf token generation failed", "error", terr)
				} else {
					domain := ""
					if domainFn != nil {
						domain = domainFn(r)
					}
					http.SetCookie(w, csrfCookie(tok, domain, requestIsSecure(r)))
				}
			}
			next.ServeHTTP(w, r)
			return
		}

		// State-changing request. Check exempt list.
		for prefix := range exempt {
			if hasPathPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Must have the CSRF cookie AND a matching X-CSRF-Token header.
		ck, err := r.Cookie(csrfCookieName)
		if err != nil || ck.Value == "" {
			writeCSRFError(w, "csrf token missing")
			return
		}
		headerVal := r.Header.Get(csrfHeaderName)
		if headerVal == "" {
			writeCSRFError(w, "csrf header missing")
			return
		}
		// Constant-time compare to avoid timing leaks.
		if !constantTimeEqual(ck.Value, headerVal) {
			writeCSRFError(w, "csrf token mismatch")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isSafeMethod returns true for HTTP methods that don't modify state.
func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// hasPathPrefix reports whether path begins with prefix, anchored
// at a path-segment boundary. A prefix that ends with "/" is treated
// as a directory and matches anything beneath it; a prefix that
// doesn't end with "/" requires the path's next char (if any) to be
// "/". This avoids "/api/proxy" matching "/api/proxybar" while still
// allowing "/auth/" to match "/auth/login".
func hasPathPrefix(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	if len(path) < len(prefix) {
		return false
	}
	if path[:len(prefix)] != prefix {
		return false
	}
	if prefix[len(prefix)-1] == '/' {
		// Directory-style: anything beneath matches, including
		// nothing more (path == prefix).
		return true
	}
	// Exact-style: the path's next char (if any) must be a path
	// boundary: '/', '?' (query string), or '#' (fragment).
	if len(path) == len(prefix) {
		return true
	}
	switch path[len(prefix)] {
	case '/', '?', '#':
		return true
	}
	return false
}

// constantTimeEqual compares two strings in constant time to avoid
// timing-based token recovery. Returns false for length-mismatched
// inputs.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// writeCSRFError writes a 403 response and logs the rejection.
func writeCSRFError(w http.ResponseWriter, reason string) {
	http.Error(w, "csrf rejected: "+reason, http.StatusForbidden)
}
