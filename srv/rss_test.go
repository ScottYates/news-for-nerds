package srv

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string // exact expected output
	}{
		{
			name:   "shorter than max returns as-is",
			in:     "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length returns as-is (no ellipsis)",
			in:     "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "ASCII gets ellipsis suffix",
			in:     "hello world",
			maxLen: 5,
			want:   "hello\u2026",
		},
		{
			name:   "CJK counts runes (3 bytes each in UTF-8)",
			in:     "中文标题内容很长的描述",
			maxLen: 4,
			want:   "中文标题\u2026",
		},
		{
			name:   "emoji counts as one rune (4 bytes in UTF-8)",
			in:     "test \U0001F600\U0001F4A9\U0001F680 done",
			maxLen: 7,
			want:   "test \U0001F600\U0001F4A9\u2026",
		},
		{
			name:   "accented Latin counts runes not bytes",
			in:     "caf\u00e9 caf\u00e9 caf\u00e9",
			maxLen: 9,
			want:   "caf\u00e9 caf\u00e9\u2026",
		},
		{
			name:   "zero max returns empty",
			in:     "anything",
			maxLen: 0,
			want:   "",
		},
		{
			name:   "negative max returns empty",
			in:     "anything",
			maxLen: -5,
			want:   "",
		},
		{
			name:   "empty input returns empty",
			in:     "",
			maxLen: 10,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.in, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.maxLen, got, tt.want)
			}
			// Defensive: every result must be valid UTF-8. The old
			// byte-truncating version could leave an incomplete code
			// point at the boundary, which would marshal to invalid
			// UTF-8 in the JSON sent to the client.
			if !utf8.ValidString(got) {
				t.Errorf("truncate(%q, %d) produced invalid UTF-8: %q", tt.in, tt.maxLen, got)
			}
		})
	}
}

// TestTruncate_NoSplitRune is a regression guard for the original bug:
// the old `s[:maxLen]` could slice through a multi-byte rune and produce
// invalid UTF-8. This test specifically fuzzes a few real-world multi-byte
// characters at the cut boundary.
func TestTruncate_NoSplitRune(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
	}{
		{"CJK boundary", "一二三四五六七八九十", 5},
		{"emoji boundary", "abc\U0001F600\U0001F4A9def\U0001F680ghi", 8},
		{"accented boundary", "\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9", 4},
		{"mixed Latin + CJK", "abc\u4e2d\u6587abc\u4e2d\u6587", 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncate(c.in, c.max)
			if !utf8.ValidString(got) {
				t.Errorf("truncate(%q, %d) -> %q: invalid UTF-8 (byte slice split a rune)", c.in, c.max, got)
			}
			// Count runes in result (sans ellipsis) — must be exactly max.
			want := strings.TrimSuffix(got, "\u2026")
			if n := utf8.RuneCountInString(want); n != c.max && n != len([]rune(c.in)) {
				t.Errorf("truncate kept %d runes, want %d (or all %d if shorter)", n, c.max, len([]rune(c.in)))
			}
		})
	}
}
