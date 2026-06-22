package srv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDedupByID(t *testing.T) {
	tests := []struct {
		name string
		in   []FeedItem
		want []FeedItem
	}{
		{
			name: "no duplicates",
			in: []FeedItem{
				{ID: "1", Title: "first"},
				{ID: "2", Title: "second"},
				{ID: "3", Title: "third"},
			},
			want: []FeedItem{
				{ID: "1", Title: "first"},
				{ID: "2", Title: "second"},
				{ID: "3", Title: "third"},
			},
		},
		{
			name: "duplicates collapsed, first occurrence wins",
			in: []FeedItem{
				{ID: "1", Title: "first", Points: 10},
				{ID: "2", Title: "second", Points: 20},
				{ID: "1", Title: "first stale", Points: 5}, // dup of #1
				{ID: "3", Title: "third", Points: 30},
				{ID: "2", Title: "second stale", Points: 1}, // dup of #2
			},
			want: []FeedItem{
				{ID: "1", Title: "first", Points: 10},
				{ID: "2", Title: "second", Points: 20},
				{ID: "3", Title: "third", Points: 30},
			},
		},
		{
			name: "empty IDs pass through (signals parse failures, don't silently merge)",
			in: []FeedItem{
				{ID: "", Title: "no id 1"},
				{ID: "1", Title: "real"},
				{ID: "", Title: "no id 2"},
			},
			want: []FeedItem{
				{ID: "", Title: "no id 1"},
				{ID: "1", Title: "real"},
				{ID: "", Title: "no id 2"},
			},
		},
		{
			name: "empty input",
			in:   nil,
			want: nil,
		},
		{
			name: "single item passthrough",
			in: []FeedItem{
				{ID: "1", Title: "only"},
			},
			want: []FeedItem{
				{ID: "1", Title: "only"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupByID(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dedupByID() =\n%#v\nwant\n%#v", got, tt.want)
			}
		})
	}
}

// TestFetchHackerNewsDedupAcrossPages verifies that the pagination loop
// collapses duplicate HN IDs across consecutive pages. This guards against
// the real-world bug where HN front-page rank churn between paginated
// fetches causes the same story to appear on both page N and page N+1.
//
// We stub the HTTP client transport to serve two canned HN listing pages
// from a local httptest server, then redirect news.ycombinator.com
// requests to it via a RoundTripper shim.
func TestFetchHackerNewsDedupAcrossPages(t *testing.T) {
	page1 := `<html><head><title>Hacker News</title></head><body><table>` +
		`<tr class="athing" id="100"><td><span class="titleline"><a href="https://a.example/">Story A</a></span></td></tr>` +
		`<tr><td><span class="score">10 points</span><span class="age" title="2026-06-22T10:00:00 1">1h</span></td></tr>` +
		`<tr class="athing" id="200"><td><span class="titleline"><a href="https://b.example/">Story B</a></span></td></tr>` +
		`<tr><td><span class="score">20 points</span><span class="age" title="2026-06-22T11:00:00 1">1h</span></td></tr>` +
		`<tr class="athing" id="300"><td><span class="titleline"><a href="https://c.example/">Story C</a></span></td></tr>` +
		`<tr><td><span class="score">30 points</span><span class="age" title="2026-06-22T12:00:00 1">1h</span></td></tr>` +
		`<tr><td><a class="morelink" href="?p=2">More</a></td></tr>` +
		`</table></body></html>`
	// Page 2 intentionally re-contains story 300 (simulating front-page
	// rank churn between the two fetches) plus a new story 400.
	page2 := `<html><head><title>Hacker News</title></head><body><table>` +
		`<tr class="athing" id="300"><td><span class="titleline"><a href="https://c.example/">Story C</a></span></td></tr>` +
		`<tr><td><span class="score">35 points</span><span class="age" title="2026-06-22T12:00:00 1">1h</span></td></tr>` +
		`<tr class="athing" id="400"><td><span class="titleline"><a href="https://d.example/">Story D</a></span></td></tr>` +
		`<tr><td><span class="score">40 points</span><span class="age" title="2026-06-22T13:00:00 1">1h</span></td></tr>` +
		`</table></body></html>`

	mux := http.NewServeMux()
	mux.HandleFunc("/news", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page1))
	})
	mux.HandleFunc("/news_p2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page2))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Build a transport that redirects every news.ycombinator.com request
	// to our test server. The "More" link in page1 resolves to
	// news.ycombinator.com/news?p=2 (relative ?p=2 against the /news base),
	// so we rewrite that path to /news_p2 since the test mux doesn't have
	// a query-aware router.
	tr := &redirectTransport{
		target: srv.URL,
		rewrite: func(req *http.Request) {
			if req.URL.Path == "/news" && req.URL.RawQuery == "p=2" {
				req.URL.Path = "/news_p2"
				req.URL.RawQuery = ""
			}
		},
		inner: http.DefaultTransport,
	}
	client := &http.Client{Transport: tr}

	tempDB := filepath.Join(t.TempDir(), "test.sqlite3")
	server, err := New(testConfig(tempDB), "test-host")
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })
	server.httpClient = client

	title, items, err := server.fetchHackerNews(context.Background(), "https://news.ycombinator.com/news")
	if err != nil {
		t.Fatalf("fetchHackerNews: %v", err)
	}
	if title == "" {
		t.Error("expected non-empty page title")
	}

	// Expect exactly 4 unique stories, in the order they first appeared.
	if got, want := len(items), 4; got != want {
		t.Fatalf("got %d items, want %d (items=%+v)", got, want, items)
	}
	wantIDs := []string{"100", "200", "300", "400"}
	for i, want := range wantIDs {
		if items[i].ID != want {
			t.Errorf("items[%d].ID = %q, want %q", i, items[i].ID, want)
		}
	}
}

// redirectTransport is an http.RoundTripper that redirects requests to
// news.ycombinator.com at a target test server, optionally rewriting the
// path/query via the rewrite hook.
type redirectTransport struct {
	target  string
	rewrite func(*http.Request)
	inner   http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "news.ycombinator.com") {
		u, err := url.Parse(t.target)
		if err != nil {
			return nil, err
		}
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		if t.rewrite != nil {
			t.rewrite(req)
		}
	}
	return t.inner.RoundTrip(req)
}