package srv

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// colorRe matches the hex colours the UI's colour pickers produce
// (#rgb, #rgba, #rrggbb, #rrggbbaa).
var colorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// isValidColor reports whether s is a hex colour. Colour fields are
// shown to other users on shared pages, so anything else is rejected
// before it is stored.
func isValidColor(s string) bool {
	return colorRe.MatchString(s)
}

// validWidgetTypes are the widget types the front-end can render.
var validWidgetTypes = map[string]bool{"rss": true, "iframe": true, "html": true}

// isJSONObject reports whether s is a syntactically valid JSON object.
// Widget and page config are stored as text and parsed by the browser;
// one unparseable value stops the whole dashboard from rendering.
func isJSONObject(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") && json.Valid([]byte(t))
}

// safeLinkURL returns raw if it is an absolute http(s) URL and ""
// otherwise. Feed item links end up in href attributes in every
// viewer's dashboard, so schemes such as javascript: and data: must
// not reach the cache.
func safeLinkURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return raw
}
