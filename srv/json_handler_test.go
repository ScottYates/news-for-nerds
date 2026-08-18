package srv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleAPISubmitFeed_RejectsOversizeBody exercises the JSON
// body cap against a real handler. HandleAPISubmitFeed decodes the
// body before doing any DB lookups, so an oversize body is the
// first thing that gets rejected.
func TestHandleAPISubmitFeed_RejectsOversizeBody(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	big := strings.Repeat("x", 2<<20) // 2 MiB
	body := `{"url":"https://example.com","title":"` + big + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/feed/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAPISubmitFeed(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status %d, got %d (body: %s)", http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	}
}

// TestHandleAPISubmitFeed_AcceptsNormalBody sanity-checks that a
// normal-sized body does NOT trigger the body cap.
func TestHandleAPISubmitFeed_AcceptsNormalBody(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	body := `{"url":"https://example.com","title":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/feed/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAPISubmitFeed(w, req)

	if w.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("normal-sized body should not trigger 413, got %d", w.Code)
	}
}
