package srv

import "testing"

func TestDecodeFeedEntities(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "no entities passes through", in: "Sony's next-gen PlayStation", want: "Sony's next-gen PlayStation"},
		{name: "no entities with literal Unicode passes through", in: "Sony\u2019s next-gen PlayStation", want: "Sony\u2019s next-gen PlayStation"},
		{
			name: "decimal numeric entity decoded (the user-reported case)",
			in:   "Sony&#8217;s next-gen PlayStation",
			want: "Sony\u2019s next-gen PlayStation",
		},
		{
			name: "hex numeric entity decoded",
			in:   "foo &#x2014; bar",
			want: "foo \u2014 bar",
		},
		{
			name: "named entity decoded",
			in:   "AT&amp;T is rolling out &mdash; faster",
			want: "AT&T is rolling out \u2014 faster",
		},
		{
			name: "double-encoded normalizes to real char",
			in:   "Sony&amp;#8217;s next-gen",
			want: "Sony\u2019s next-gen",
		},
		{
			name: "triple-encoded also normalizes",
			in:   "Sony&amp;amp;#8217;s",
			want: "Sony\u2019s",
		},
		{
			name: "literal ampersand without entity stays",
			in:   "rock & roll",
			want: "rock & roll", // not an entity, UnescapeString leaves it
		},
		{
			name: "smart quotes + em dash + hellip",
			in:   "&ldquo;hello&rdquo; &mdash; &hellip;",
			want: "\u201chello\u201d \u2014 \u2026",
		},
		{
			name: "idempotent on already-decoded text (no extra work)",
			in:   "Sony\u2019s next-gen",
			want: "Sony\u2019s next-gen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeFeedEntities(tt.in)
			if got != tt.want {
				t.Errorf("decodeFeedEntities(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
