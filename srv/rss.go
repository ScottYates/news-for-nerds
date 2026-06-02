package srv

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"srv.exe.dev/db/dbgen"
)

type FeedItem struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	Published   string `json:"published"`
	Author      string `json:"author"`
	// ID and FirstSeen are used by the Hacker News scraper to replicate
	// hckrnews.com's "sorted by time" ordering, which is based on when a
	// story first appeared on the front page (tracked locally across
	// refreshes), not its HN submission time.
	ID        string `json:"id,omitempty"`
	FirstSeen string `json:"first_seen,omitempty"`
}

type FeedData struct {
	Title            string     `json:"title"`
	Items            []FeedItem `json:"items"`
	Fetched          time.Time  `json:"fetched"`
	Pending          bool       `json:"pending,omitempty"`            // true if feed is still being fetched
	ClientFetchURL   string     `json:"client_fetch_url,omitempty"`   // if set, client should fetch this URL and submit results
	Error            string     `json:"error,omitempty"`              // last error message
}

func (s *Server) StartFeedRefresher(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(s.Config.FeedRefreshInterval) * time.Minute)
	go func() {
		// Initial fetch
		s.refreshAllFeeds(ctx)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				s.refreshAllFeeds(ctx)
			}
		}
	}()
}

func (s *Server) refreshAllFeeds(ctx context.Context) {
	q := dbgen.New(s.DB)
	
	staleTime := time.Now().Add(-time.Duration(s.Config.FeedStaleMinutes) * time.Minute)
	feeds, err := q.GetStaleFeeds(ctx, &staleTime)
	if err != nil {
		slog.Warn("failed to get stale feeds", "error", err)
		return
	}

	// Retry errored feeds with backoff
	errorStaleTime := time.Now().Add(-time.Duration(s.Config.FeedErrorBackoff) * time.Minute)
	errorFeeds, err := q.GetStaleFeedsWithErrors(ctx, &errorStaleTime)
	if err != nil {
		slog.Warn("failed to get stale feeds with errors", "error", err)
	} else {
		feeds = append(feeds, errorFeeds...)
	}

	if len(feeds) == 0 {
		return
	}

	slog.Info("refreshing feeds", "count", len(feeds))

	// Process up to N feeds per cycle to spread load
	count := len(feeds)
	if count > s.Config.FeedMaxPerCycle {
		count = s.Config.FeedMaxPerCycle
	}
	for i := 0; i < count; i++ {
		s.fetchAndStoreFeed(ctx, feeds[i].Url)
	}
}

func (s *Server) fetchAndStoreFeed(ctx context.Context, feedURL string) {
	s.fetchAndStoreFeedWithRetryAndProxy(ctx, feedURL, false, ProxyConfig{})
}

// ProxyConfig holds proxy configuration including optional authentication.
type ProxyConfig struct {
	URL      string
	Username string
	Password string
}

// fetchAndStoreFeedWithProxy fetches a feed using the specified proxy configuration.
func (s *Server) fetchAndStoreFeedWithProxy(ctx context.Context, feedURL string, proxy ProxyConfig) {
	s.fetchAndStoreFeedWithRetryAndProxy(ctx, feedURL, false, proxy)
}

// fetchAndStoreFeedWithRetryAndProxy fetches a feed with retry logic and optional proxy.
// If aggressive is true, retries on all errors (used for new feeds with no cached content).
func (s *Server) fetchAndStoreFeedWithRetryAndProxy(ctx context.Context, feedURL string, aggressive bool, proxy ProxyConfig) {
	// Hacker News listing pages are HTML, not RSS - scrape them directly.
	if isHackerNewsURL(feedURL) {
		s.fetchAndStoreHackerNews(ctx, feedURL)
		return
	}

	parser := gofeed.NewParser()
	parser.UserAgent = "NewsForNerds/1.0"
	fetchTimeout := time.Duration(s.Config.FeedFetchTimeout) * time.Second

	// Use proxy if provided, otherwise use default client
	if proxy.URL != "" {
		proxyParsed, err := url.Parse(proxy.URL)
		if err == nil {
			if proxy.Username != "" {
				proxyParsed.User = url.UserPassword(proxy.Username, proxy.Password)
			}
			parser.Client = &http.Client{
				Timeout: fetchTimeout,
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyParsed),
				},
			}
			slog.Debug("using proxy for feed fetch", "url", feedURL, "proxy", proxy.URL, "auth", proxy.Username != "")
		} else {
			slog.Warn("invalid proxy URL, using direct connection", "proxy", proxy.URL, "error", err)
			parser.Client = s.httpClient
		}
	} else {
		parser.Client = s.httpClient
	}

	var feed *gofeed.Feed
	var err error

	maxAttempts := 3
	if aggressive {
		maxAttempts = 5
	}

	// Retry with backoff
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Backoff: 1s, 2s, 3s, 4s
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
		feed, err = parser.ParseURLWithContext(feedURL, fetchCtx)
		cancel()

		if err == nil {
			break
		}

		// In aggressive mode, retry all errors
		if aggressive {
			slog.Debug("retrying feed fetch (aggressive)", "url", feedURL, "attempt", attempt+1, "error", err)
			continue
		}

		// Otherwise only retry on transient errors
		errStr := err.Error()
		isTransient := strings.Contains(errStr, "429") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "temporary") ||
			strings.Contains(errStr, "503")
		if !isTransient {
			break
		}
		slog.Debug("retrying feed fetch", "url", feedURL, "attempt", attempt+1, "error", err)
	}

	now := time.Now()
	q := dbgen.New(s.DB)

	if err != nil {
		slog.Warn("failed to fetch feed", "url", feedURL, "error", err)
		errStr := err.Error()
		// On error, just update the error field and timestamp - preserve cached content
		_ = q.UpdateFeedError(ctx, dbgen.UpdateFeedErrorParams{
			LastFetched: &now,
			LastError:   &errStr,
			Url:         feedURL,
		})
		return
	}

	items := make([]FeedItem, 0, len(feed.Items))
	for i, item := range feed.Items {
		if i >= s.Config.FeedMaxItems {
			break
		}
		fi := FeedItem{
			Title:       item.Title,
			Link:        item.Link,
			Description: truncate(stripHTML(item.Description), 300),
		}
		if item.PublishedParsed != nil {
			fi.Published = item.PublishedParsed.Format(time.RFC3339)
		} else if item.Published != "" {
			fi.Published = item.Published
		}
		if item.Author != nil {
			fi.Author = item.Author.Name
		}
		items = append(items, fi)
	}

	content, _ := json.Marshal(items)

	err = q.UpsertFeed(ctx, dbgen.UpsertFeedParams{
		Url:         feedURL,
		Title:       feed.Title,
		Content:     string(content),
		LastFetched: &now,
		LastError:   nil,
		CreatedAt:   now,
	})
	if err != nil {
		slog.Warn("failed to store feed", "url", feedURL, "error", err)
	}
}

// GetFeed returns cached feed data, optionally using a proxy for initial fetch.
func (s *Server) GetFeed(ctx context.Context, feedURL string) (*FeedData, error) {
	return s.GetFeedWithProxy(ctx, feedURL, ProxyConfig{})
}

// GetFeedWithProxy returns cached feed data, using the specified proxy for initial fetch if needed.
func (s *Server) GetFeedWithProxy(ctx context.Context, feedURL string, proxy ProxyConfig) (*FeedData, error) {
	q := dbgen.New(s.DB)

	// Ensure feed exists
	err := q.CreateFeedIfNotExists(ctx, dbgen.CreateFeedIfNotExistsParams{
		Url:       feedURL,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return nil, err
	}

	feed, err := q.GetFeedByURL(ctx, feedURL)
	if err != nil {
		return nil, err
	}

	var items []FeedItem
	_ = json.Unmarshal([]byte(feed.Content), &items)

	// If never fetched, fetch now with aggressive retry since we have no cache
	if feed.LastFetched == nil {
		s.fetchAndStoreFeedWithRetryAndProxy(ctx, feedURL, true, proxy) // aggressive retry for new feeds
		feed, err = q.GetFeedByURL(ctx, feedURL)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(feed.Content), &items)
	}

	data := &FeedData{
		Title: feed.Title,
		Items: items,
	}
	if feed.LastFetched != nil {
		data.Fetched = *feed.LastFetched
	}

	// If we have no content and there was an error, ask client to fetch
	if len(items) == 0 && feed.LastError != nil {
		data.Pending = true
		data.ClientFetchURL = feedURL
		data.Error = *feed.LastError
	}

	return data, nil
}

// StoreFeedFromClient stores feed data that was fetched by the client's browser
func (s *Server) StoreFeedFromClient(ctx context.Context, feedURL, title string, items []FeedItem) error {
	q := dbgen.New(s.DB)

	// Ensure feed exists
	err := q.CreateFeedIfNotExists(ctx, dbgen.CreateFeedIfNotExistsParams{
		Url:       feedURL,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return err
	}

	now := time.Now()
	content, _ := json.Marshal(items)

	err = q.UpsertFeed(ctx, dbgen.UpsertFeedParams{
		Url:         feedURL,
		Title:       title,
		Content:     string(content),
		LastFetched: &now,
		LastError:   nil, // Clear any previous error
		CreatedAt:   now,
	})
	return err
}

func stripHTML(s string) string {
	var result []rune
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result = append(result, r)
		}
	}
	return string(result)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
