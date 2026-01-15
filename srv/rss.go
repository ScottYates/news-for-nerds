package srv

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
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
}

type FeedData struct {
	Title   string     `json:"title"`
	Items   []FeedItem `json:"items"`
	Error   string     `json:"error,omitempty"`
	Fetched time.Time  `json:"fetched"`
}

func (s *Server) StartFeedRefresher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
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
	// Get feeds older than 4.5 minutes
	staleTime := time.Now().Add(-4*time.Minute - 30*time.Second)
	feeds, err := q.GetStaleFeeds(ctx, &staleTime)
	if err != nil {
		slog.Warn("failed to get stale feeds", "error", err)
		return
	}

	if len(feeds) == 0 {
		return
	}

	slog.Info("refreshing feeds", "count", len(feeds))

	// Refresh in parallel with limit
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // max 5 concurrent

	for _, feed := range feeds {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.fetchAndStoreFeed(ctx, url)
		}(feed.Url)
	}
	wg.Wait()
}

func (s *Server) fetchAndStoreFeed(ctx context.Context, url string) {
	parser := gofeed.NewParser()
	parser.UserAgent = "FeedDeck/1.0 (+https://github.com/feeddeck)"
	parser.Client = s.httpClient

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	feed, err := parser.ParseURLWithContext(url, ctx)

	now := time.Now()
	q := dbgen.New(s.DB)

	if err != nil {
		slog.Warn("failed to fetch feed", "url", url, "error", err)
		errStr := err.Error()
		_ = q.UpsertFeed(ctx, dbgen.UpsertFeedParams{
			Url:         url,
			Title:       "",
			Content:     "[]",
			LastFetched: &now,
			LastError:   &errStr,
			CreatedAt:   now,
		})
		return
	}

	items := make([]FeedItem, 0, len(feed.Items))
	for i, item := range feed.Items {
		if i >= 50 { // limit items
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
		Url:         url,
		Title:       feed.Title,
		Content:     string(content),
		LastFetched: &now,
		LastError:   nil,
		CreatedAt:   now,
	})
	if err != nil {
		slog.Warn("failed to store feed", "url", url, "error", err)
	}
}

func (s *Server) GetFeed(ctx context.Context, url string) (*FeedData, error) {
	q := dbgen.New(s.DB)

	// Ensure feed exists
	err := q.CreateFeedIfNotExists(ctx, dbgen.CreateFeedIfNotExistsParams{
		Url:       url,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return nil, err
	}

	feed, err := q.GetFeedByURL(ctx, url)
	if err != nil {
		return nil, err
	}

	// If never fetched, fetch now
	if feed.LastFetched == nil {
		s.fetchAndStoreFeed(ctx, url)
		feed, err = q.GetFeedByURL(ctx, url)
		if err != nil {
			return nil, err
		}
	}

	var items []FeedItem
	_ = json.Unmarshal([]byte(feed.Content), &items)

	data := &FeedData{
		Title: feed.Title,
		Items: items,
	}
	if feed.LastFetched != nil {
		data.Fetched = *feed.LastFetched
	}
	if feed.LastError != nil {
		data.Error = *feed.LastError
	}
	return data, nil
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
