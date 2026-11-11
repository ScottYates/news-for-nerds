package srv

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"srv.exe.dev/db/dbgen"
)

func TestSessionExpiryCheckedInGo(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	ctx := context.Background()
	q := dbgen.New(server.DB)
	now := time.Now()
	pic := ""
	if err := q.CreateUser(ctx, dbgen.CreateUserParams{ID: "u1", Email: "a@example.com", Name: "A", Picture: &pic, CreatedAt: now, LastLogin: now}); err != nil {
		t.Fatal(err)
	}
	for id, exp := range map[string]time.Time{"live": now.Add(time.Hour), "dead": now.Add(-time.Minute)} {
		if err := q.CreateSession(ctx, dbgen.CreateSessionParams{ID: id, UserID: "u1", CreatedAt: now, ExpiresAt: exp}); err != nil {
			t.Fatal(err)
		}
	}

	get := func(id string) error {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: id})
		_, err := server.GetUserFromRequest(req)
		return err
	}
	if err := get("live"); err != nil {
		t.Errorf("live session rejected: %v", err)
	}
	if err := get("dead"); err == nil {
		t.Error("expired session accepted")
	}
	if err := get("missing"); err == nil {
		t.Error("unknown session accepted")
	}
}

func TestIsHackerNewsURL_HostOnly(t *testing.T) {
	yes := []string{"https://news.ycombinator.com/", "https://news.ycombinator.com/news", "https://NEWS.ycombinator.com/newest"}
	no := []string{"https://news.ycombinator.com/rss", "https://evil.example/news.ycombinator.com", "https://news.ycombinator.com.evil.example/", "not a url"}
	for _, u := range yes {
		if !isHackerNewsURL(u) {
			t.Errorf("isHackerNewsURL(%q) = false, want true", u)
		}
	}
	for _, u := range no {
		if isHackerNewsURL(u) {
			t.Errorf("isHackerNewsURL(%q) = true, want false", u)
		}
	}
}

func TestIsDeniedIP_NAT64(t *testing.T) {
	if !isDeniedIP(net.ParseIP("64:ff9b::7f00:1")) {
		t.Error("NAT64 address embedding 127.0.0.1 should be denied")
	}
	if !isDeniedIP(net.ParseIP("64:ff9b::a9fe:a9fe")) {
		t.Error("NAT64 address embedding 169.254.169.254 should be denied")
	}
	if isDeniedIP(net.ParseIP("64:ff9b::808:808")) {
		t.Error("NAT64 address embedding 8.8.8.8 should be allowed")
	}
}

func TestMarkVisitedRejectsNonHTTP(t *testing.T) {
	server, err := New(testConfig(testDBPath(t)), "test-host")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/visited", strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: visitorCookieName, Value: "v1"})
		w := httptest.NewRecorder()
		server.HandleAPIMarkVisited(w, req)
		return w.Code
	}
	if code := post(`{"url":"javascript:alert(1)"}`); code != http.StatusBadRequest {
		t.Errorf("javascript: url got %d, want 400", code)
	}
	// Twice: the second insert must update, not fail on the unique index.
	for i := 0; i < 2; i++ {
		if code := post(`{"url":"https://example.com/a"}`); code != http.StatusOK {
			t.Errorf("valid url (attempt %d) got %d, want 200", i+1, code)
		}
	}
	var n int
	if err := server.DB.QueryRow("SELECT COUNT(*) FROM visited_links WHERE user_id = 'visitor:v1'").Scan(&n); err != nil || n != 1 {
		t.Errorf("visited rows = %d (err %v), want 1", n, err)
	}
}
