package srv

import (
	"net/http"
)

// securityHeadersMiddleware adds a baseline set of security-related
// response headers. Defaults are tuned for this app: it serves
// HTML pages, embeds iframe widgets from arbitrary user-supplied
// URLs (proxied through /api/proxy), and includes inline scripts
// in proxied HTML, so the CSP is set up to allow that while
// still blocking the worst of the standard attack surface.
//
// Defaults:
//
//   - Content-Security-Policy:
//       default-src 'self';
//       script-src 'self' 'unsafe-inline';  // proxy-injected inline script
//       style-src  'self' 'unsafe-inline';  // per-widget inline styles
//       img-src    'self' data:;            // data: URLs for favicons
//       frame-src  *;                      // widget iframes (proxy + user iframes)
//       connect-src 'self';
//       object-src 'none';
//       base-uri   'self';
//       form-action 'self';
//       frame-ancestors 'self';            // allow self-embedding
//
//   - X-Content-Type-Options: nosniff
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - X-Frame-Options: SAMEORIGIN         (legacy defense; CSP frame-ancestors supersedes)
//   - Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()
//   - Cross-Origin-Opener-Policy: same-origin
//
// Per-handler overrides can be done by calling w.Header().Set()
// inside the handler before any body is written; the defaults only
// fire when the header is not already set.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		setIfMissing(h, "Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"frame-src *; "+
				"connect-src 'self'; "+
				"object-src 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'; "+
				"frame-ancestors 'self'")
		setIfMissing(h, "X-Content-Type-Options", "nosniff")
		setIfMissing(h, "Referrer-Policy", "strict-origin-when-cross-origin")
		setIfMissing(h, "X-Frame-Options", "SAMEORIGIN")
		setIfMissing(h, "Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		setIfMissing(h, "Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// setIfMissing sets the header k on h to v only if no value is
// already set. Allows handlers to override the default.
func setIfMissing(h http.Header, k, v string) {
	if h.Get(k) == "" {
		h.Set(k, v)
	}
}
