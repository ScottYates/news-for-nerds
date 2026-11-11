package srv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExeDevHeaderIgnoredByDefault checks that X-ExeDev-UserID does
// not identify the caller unless TRUST_EXEDEV_HEADERS is on.
func TestExeDevHeaderIgnoredByDefault(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-ExeDev-UserID", "victim-user-id")

	server.Config.TrustExeDevHeaders = false
	if got := server.GetUserID(req); got == "victim-user-id" {
		t.Errorf("header trusted with TrustExeDevHeaders=false: GetUserID = %q", got)
	}

	server.Config.TrustExeDevHeaders = true
	if got := server.GetUserID(req); got != "victim-user-id" {
		t.Errorf("header not honoured with TrustExeDevHeaders=true: GetUserID = %q", got)
	}

	// Even when trusted, the header cannot name a cookie visitor.
	req.Header.Set("X-ExeDev-UserID", "visitor:1234")
	if got := server.GetUserID(req); got == "visitor:1234" {
		t.Errorf("header was allowed to name a visitor identity")
	}
}

func TestRedactPageConfig(t *testing.T) {
	in := `{"grid_size":10,"proxy_url":"http://p:8080","proxy_user":"u","proxy_pass":"secret"}`
	out := redactPageConfig(in)
	for _, bad := range []string{"proxy_url", "proxy_user", "proxy_pass", "secret"} {
		if strings.Contains(out, bad) {
			t.Errorf("redacted config still contains %q: %s", bad, out)
		}
	}
	if !strings.Contains(out, `"grid_size":10`) {
		t.Errorf("redacted config lost other fields: %s", out)
	}
	if got := redactPageConfig("not json"); got != "{}" {
		t.Errorf("redactPageConfig(invalid) = %q, want {}", got)
	}
}

// TestFeedProxyQueryParamsIgnored checks that a caller can no longer
// name a proxy in the query string (it used to bypass the SSRF guard).
func TestFeedProxyQueryParamsIgnored(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	req := httptest.NewRequest(http.MethodGet, "/api/feed?url=https://example.com/f&proxy=http://127.0.0.1:6379&proxy_user=a&proxy_pass=b", nil)
	proxy, err := server.feedProxyForRequest(req)
	if err != nil {
		t.Fatalf("feedProxyForRequest: %v", err)
	}
	if proxy != (ProxyConfig{}) {
		t.Errorf("query-string proxy was used: %+v", proxy)
	}
}
