package srv

import "strings"

// safeCustomCSS returns css if it looks like real CSS that can be
// embedded inside an existing <style>...</style> block without
// breaking out, or an empty string if it doesn't.
//
// Threat model: the proxy endpoint embeds user-supplied CSS into a
// <style> block in HTML served from the app's origin. The block can
// be escaped with </style>, after which the attacker can inject
// arbitrary HTML/JS. With sandbox="allow-same-origin" on the iframe
// the proxy is loaded into, the JS has access to the app's cookies
// and storage — a full account compromise via a crafted proxy URL.
//
// Real CSS does not contain "</" (CSS has no nested tags, and
// comments are CSS-context-only, no HTML overlap). URL data in
// url(...) is constrained to URL syntax (which can't contain "</"
// without percent-encoding, in which case the literal ">" wouldn't
// appear in a position to break out of <style>).
//
// Reject: any input containing "</" or "<!--" or "-->". CSS
// comments are "/* ... */", not HTML-style.
//
// Also enforce a max length so the proxy endpoint can't be used as a
// bandwidth/storage amplifier.
func safeCustomCSS(css string) string {
	if css == "" {
		return ""
	}
	// Conservative length cap. A normal user CSS injection to hide
	// elements on a proxied site is well under 2 KB.
	const maxLen = 4096
	if len(css) > maxLen {
		return ""
	}
	// Strip control chars (CR, LF, NUL, etc.) before scanning. They
	// can confuse downstream parsers and aren't valid in real CSS
	// outside of escaped sequences.
	for _, r := range css {
		if r < 0x20 && r != '\t' {
			return ""
		}
	}
	// The actual escape guard. Real CSS never has "</" or HTML-comment
	// markers. We check case-insensitively because the HTML parser's
	// recognition of </style> as the end-tag is case-insensitive.
	lowerCSS := strings.ToLower(css)
	if strings.Contains(lowerCSS, "</") {
		return ""
	}
	if strings.Contains(lowerCSS, "<!--") || strings.Contains(lowerCSS, "-->") {
		return ""
	}
	// Defense against expression()/javascript: that some old CSS
	// parsers honor. Modern browsers don't, but defense in depth.
	if strings.Contains(lowerCSS, "expression(") {
		return ""
	}
	if strings.Contains(lowerCSS, "javascript:") {
		return ""
	}
	return css
}
