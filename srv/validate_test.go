package srv

import "testing"

func TestIsValidColor(t *testing.T) {
	good := []string{"#fff", "#FFFF", "#16213e", "#16213eff"}
	bad := []string{"", "red", "#12", "#12345", "#16213e\"><img src=x onerror=alert(1)>", "#16213e;background:url(x)"}
	for _, c := range good {
		if !isValidColor(c) {
			t.Errorf("isValidColor(%q) = false, want true", c)
		}
	}
	for _, c := range bad {
		if isValidColor(c) {
			t.Errorf("isValidColor(%q) = true, want false", c)
		}
	}
}

func TestSafeLinkURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a?b=c": "https://example.com/a?b=c",
		"http://example.com":        "http://example.com",
		" https://example.com ":     "https://example.com",
		"javascript:alert(1)":       "",
		"data:text/html,<script>":   "",
		"/relative/path":            "",
		"//example.com/x":           "",
		"":                          "",
	}
	for in, want := range cases {
		if got := safeLinkURL(in); got != want {
			t.Errorf("safeLinkURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsJSONObject(t *testing.T) {
	for _, s := range []string{"{}", ` {"a":1} `} {
		if !isJSONObject(s) {
			t.Errorf("isJSONObject(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "[]", "null", "{", `"x"`, "{}x"} {
		if isJSONObject(s) {
			t.Errorf("isJSONObject(%q) = true, want false", s)
		}
	}
}
