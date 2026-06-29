package srv

import "html"

// decodeFeedEntities decodes HTML character references (&amp;, &#8217;,
// &#x2019;, &rsquo;, etc.) into their actual Unicode characters.
//
// Many RSS/Atom feeds embed entities like &#8217; in titles and
// descriptions — either because the feed author HTML-encoded the text,
// or because something along the way double-encoded it
// (e.g. <title>foo&amp;#8217;bar</title>). Either way, after the XML
// parser is done, the raw title string can still contain entities
// that need to be turned back into real characters before we ship the
// text to the browser.
//
// Loops until stable so double-encoded (&amp;#8217; -> &#8217; -> ')
// and beyond also normalize. html.UnescapeString is a no-op on strings
// with no entities, so this terminates on the first pass for clean
// input. Also a no-op on inputs like "rock & roll" where '&' isn't
// followed by a valid entity pattern.
//
// Safe to apply to any plain-text field (title, description, author
// name, etc.). DO NOT apply to intentionally-HTML fields like the
// TinyMCE widget content — that would mangle user-authored markup.
func decodeFeedEntities(s string) string {
	if s == "" {
		return s
	}
	for {
		decoded := html.UnescapeString(s)
		if decoded == s {
			return s
		}
		s = decoded
	}
}
