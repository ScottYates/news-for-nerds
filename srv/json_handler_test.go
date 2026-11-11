package srv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleAPIMarkVisited_RejectsOversizeBody exercises the JSON
// body cap against a real handler. HandleAPIMarkVisited decodes the
// body before doing any DB lookups, so an oversize body is the
// first thing that gets rejected.
func TestHandleAPIMarkVisited_RejectsOversizeBody(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	big := strings.Repeat("x", 2<<20) // 2 MiB
	body := `{"url":"https://example.com/` + big + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/visited", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAPIMarkVisited(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status %d, got %d (body: %s)", http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	}
}

// TestHandleAPIMarkVisited_AcceptsNormalBody sanity-checks that a
// normal-sized body does NOT trigger the body cap.
func TestHandleAPIMarkVisited_AcceptsNormalBody(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	body := `{"url":"https://example.com/hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/visited", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAPIMarkVisited(w, req)

	if w.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("normal-sized body should not trigger 413, got %d", w.Code)
	}
}
