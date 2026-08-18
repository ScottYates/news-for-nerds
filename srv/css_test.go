package srv

import (
	"strings"
	"testing"
)

func TestSafeCustomCSS(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // "" means rejected
	}{
		// Pass-through: real CSS
		{"empty", "", ""},
		{"simple selector", ".header { display: none; }", ".header { display: none; }"},
		{"hide element", "div.advert { display: none !important; }", "div.advert { display: none !important; }"},
		{"multiple rules", "a { color: red; } p { font-size: 14px; }", "a { color: red; } p { font-size: 14px; }"},
		{"with @media", "@media (max-width: 600px) { .nav { display: none; } }", "@media (max-width: 600px) { .nav { display: none; } }"},
		{"with @keyframes", "@keyframes spin { from { transform: rotate(0); } to { transform: rotate(360deg); } }", "@keyframes spin { from { transform: rotate(0); } to { transform: rotate(360deg); } }"},
		{"with comment", "/* hide ads */ .ad { display: none; }", "/* hide ads */ .ad { display: none; }"},
		{"data URL", ".bg { background: url(data:image/png;base64,abc); }", ".bg { background: url(data:image/png;base64,abc); }"},
		{"tabs allowed", "a\t{\tdisplay:\tnone;\t}", "a\t{\tdisplay:\tnone;\t}"},

		// Rejection: </style> breakout
		{"style close tag", ".x{}</style><script>alert(1)</script>", ""},
		{"style close uppercase", ".x{}</STYLE><script>alert(1)</script>", ""},
		{"style close with space", ".x{}</ style>", ""},
		{"style close with newlines", ".x{}</\nstyle>", ""},
		{"backtick style close", ".x{}</style >", ""},
		{"style close in string", `</style>`[:6], ""}, // 6 chars: literally "</style"

		// Rejection: HTML comments
		{"html comment open", "<!--", ""},
		{"html comment close", "-->", ""},
		{"html comment both", "<!-- xyz -->", ""},

		// Rejection: legacy IE expression()
		{"expression lower", "div { width: expression(alert(1)); }", ""},
		{"expression upper", "div { WIDTH: EXPRESSION(alert(1)); }", ""},

		// Rejection: javascript: scheme
		{"javascript scheme", "div { background: url(javascript:alert(1)); }", ""},
		{"javascript mixed case", "div { background: url(JavaScript:alert(1)); }", ""},

		// Rejection: control chars
		{"null byte", "div { color: red;\x00; }", ""},
		{"CR", "div { color: red;\r; }", ""},
		{"LF", "div { color: red;\n; }", ""},
		{"form feed", "div { color: red;\x0c; }", ""},

		// Rejection: oversize
		{"max length exactly", strings.Repeat("a", 4096), strings.Repeat("a", 4096)},
		{"max length + 1", strings.Repeat("a", 4097), ""},
		{"way too long", strings.Repeat("a", 10000), ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := safeCustomCSS(c.in)
			if got != c.want {
				t.Errorf("safeCustomCSS(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
