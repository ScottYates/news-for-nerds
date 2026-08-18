package srv

import "testing"

func TestSafeReturnPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // "" means rejected
	}{
		// Pass-through
		{"empty", "", ""},
		{"root", "/", "/"},
		{"subpath", "/dashboard", "/dashboard"},
		{"nested", "/page/scott-yates", "/page/scott-yates"},
		{"with query", "/page?id=1", "/page?id=1"},
		{"with fragment", "/page#section", "/page#section"},
		{"with both", "/page?id=1#x", "/page?id=1#x"},
		{"encoded slash", "/foo%2Fbar", "/foo%2Fbar"},
		{"encoded query", "/foo?key=a%26b", "/foo?key=a%26b"},

		// Rejection: protocol-relative
		{"protocol relative", "//evil.com/path", ""},
		{"protocol relative with query", "//evil.com/?x=1", ""},

		// Rejection: absolute URL with scheme
		{"http absolute", "http://evil.com/", ""},
		{"https absolute", "https://evil.com/", ""},
		{"ftp absolute", "ftp://evil.com/", ""},

		// Rejection: javascript: / data: etc
		{"javascript scheme", "javascript:alert(1)", ""},
		{"data scheme", "data:text/html,<script>alert(1)</script>", ""},
		{"vbscript", "vbscript:msgbox(1)", ""},
		{"file scheme", "file:///etc/passwd", ""},

		// Pass-through: scheme-looking substring in a query is just a query value
		{"scheme in query string", "/path?q=http://evil.com", "/path?q=http://evil.com"},
		{"scheme-like with colon in path", "/path:foo", "/path:foo"},

		// Rejection: backslash tricks
		{"backslash to forward", "/\\evil.com", ""},
		{"backslash", "\\\\evil.com", ""},
		{"mixed slashes", "/foo\\bar", ""},

		// Rejection: not anchored (no leading /)
		{"relative no leading slash", "page/x", ""},
		{"dot-relative", "./page", ""},
		{"dotdot-relative", "../etc/passwd", ""},

		// Rejection: header injection
		{"CRLF injection", "/page\r\nLocation: https://evil.com", ""},
		{"LF only", "/page\nfoo", ""},
		{"CR only", "/page\rfoo", ""},

		// url.Parse sanity check
		{"control: simple path", "/path", "/path"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := safeReturnPath(c.in)
			if got != c.want {
				t.Errorf("safeReturnPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSafeReturnPath_NoObfuscation makes sure we don't get fooled by
// common encoding tricks. e.g. %2F is the URL-encoded form of "/" but
// stays a literal %2F in our validator, which is fine — it's not a
// literal slash, so the browser will see "/foo%2Fbar" as a single
// path segment, not as a protocol-relative URL.
func TestSafeReturnPath_NoObfuscation(t *testing.T) {
	// %2F is encoded slash; the validator should treat it as text.
	if got := safeReturnPath("/foo%2Fbar"); got != "/foo%2Fbar" {
		t.Errorf("encoded slash should pass through as text, got %q", got)
	}
	// %5C is encoded backslash; similarly should pass through.
	if got := safeReturnPath("/foo%5Cbar"); got != "/foo%5Cbar" {
		t.Errorf("encoded backslash should pass through as text, got %q", got)
	}
}
