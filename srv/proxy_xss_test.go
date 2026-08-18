package srv

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// roundTripperFunc adapts a function to the http.RoundTripper interface.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestHandleAPIProxy_SanitizesCustomCSS runs the proxy end-to-end with
// a series of malicious CSS payloads and asserts the response never
// contains the breakout sequence. Without safeCustomCSS, a payload of
// </style><script>alert(1)</script> would render in the app's
// origin and execute (the proxy is loaded into a same-origin iframe).
func TestHandleAPIProxy_SanitizesCustomCSS(t *testing.T) {
	const upstream = `<html><head><title>x</title></head><body>hello</body></html>`

	cases := []struct {
		name        string
		css         string
		mustNotHave []string // substrings that must NOT appear in the response
	}{
		{
			name:        "no css",
			css:         "",
			mustNotHave: []string{},
		},
		{
			name:        "safe css passes through",
			css:         ".ad { display: none; }",
			mustNotHave: []string{},
		},
		{
			name:        "style close tag breakout",
			css:         ".x{}</style><script>alert(1)</script>",
			mustNotHave: []string{"<script>alert(1)</script>"},
		},
		{
			name:        "uppercase style close",
			css:         ".x{}</STYLE><img src=x onerror=alert(1)>",
			mustNotHave: []string{"<img src=x onerror=alert(1)>"},
		},
		{
			name:        "html comment breakout",
			css:         ".x{}<!-- --><script>alert(1)</script>",
			mustNotHave: []string{"<!-- -->", "<script>alert(1)</script>"},
		},
		{
			name:        "javascript url",
			css:         ".x{ background: url(javascript:alert(1)); }",
			mustNotHave: []string{"javascript:alert(1)"},
		},
		{
			name:        "ie expression",
			css:         ".x{ width: expression(alert(1)); }",
			mustNotHave: []string{"expression(alert(1))"},
		},
		{
			name:        "oversize css dropped",
			css:         strings.Repeat("a", 5000),
			mustNotHave: []string{strings.Repeat("a", 100)},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, err := New(testConfig(testDBPath(t)), "test-host")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { server.DB.Close() })

			// Replace the proxyClient with a controlled transport that
			// always returns the canned upstream HTML. This bypasses
			// the safe transport so the test runs without network.
			server.proxyClient = &http.Client{
				Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: 200,
						Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
						Body:       io.NopCloser(strings.NewReader(upstream)),
						Request:    req,
					}, nil
				}),
			}

			req := httptest.NewRequest(http.MethodGet, "/api/proxy?url=https://example.com/page&css="+url.QueryEscape(c.css), nil)
			w := httptest.NewRecorder()
			server.HandleAPIProxy(w, req)

			body := w.Body.String()
			for _, bad := range c.mustNotHave {
				if strings.Contains(body, bad) {
					t.Errorf("response body contains forbidden substring %q for case %q\nbody:\n%s", bad, c.name, body)
				}
			}
		})
	}
}
