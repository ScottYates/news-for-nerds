package srv

import (
	"net/url"
	"strings"
)

// safeReturnPath returns s if it's a safe relative path to use as a
// post-login redirect target, or an empty string if it's not. The
// caller is expected to fall back to a default path ("/") when this
// returns empty.
//
// A safe path is one that:
//   - starts with a single "/" (a same-origin path)
//   - does NOT start with "//" (a protocol-relative URL that browsers
//     resolve as //evil.com -> https://evil.com/)
//   - contains no backslash (some browsers normalize \ to /, turning
//     \/evil.com into //evil.com)
//   - contains no CR/LF (header-injection guards)
//   - parses with url.Parse as a path-only URL with no host
//
// Use this for the OAuth `return` URL and any other post-auth redirect
// target. Without it, a hostile referer (or query param) can bounce
// the user to an attacker-controlled domain right after a successful
// sign-in, which is a textbook open-redirect phishing primitive.
func safeReturnPath(s string) string {
	if s == "" {
		return ""
	}
	// Trim surrounding whitespace; a leading space turns the URL into
	// an absolute form in some browsers.
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Must be anchored to the same origin with a single leading "/".
	if !strings.HasPrefix(s, "/") {
		return ""
	}
	// Protocol-relative: //evil.com/path.
	if strings.HasPrefix(s, "//") {
		return ""
	}
	// Backslash tricks: some browsers treat \ as /.
	if strings.Contains(s, "\\") {
		return ""
	}
	// Header injection.
	if strings.ContainsAny(s, "\r\n") {
		return ""
	}
	// Belt and suspenders: confirm url.Parse sees it as a path-only URL
	// with no host. Absolute URLs like "http://evil.com/" parse out a
	// host, so this catches anything the prefix checks missed.
	if u, err := url.Parse(s); err != nil || u.Host != "" {
		return ""
	}
	return s
}
