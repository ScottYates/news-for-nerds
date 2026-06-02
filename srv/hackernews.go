package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"srv.exe.dev/db/dbgen"
)

// hnBaseURL is the canonical Hacker News base used for resolving relative links.
const hnBaseURL = "https://news.ycombinator.com/"

// isHackerNewsURL reports whether the given feed URL points at a Hacker News
// HTML listing page (e.g. https://news.ycombinator.com/news) that we should
// scrape directly rather than parse as RSS.
func isHackerNewsURL(feedURL string) bool {
	u := strings.ToLower(strings.TrimSpace(feedURL))
	if !strings.Contains(u, "news.ycombinator.com") {
		return false
	}
	// The actual RSS feed lives at /rss - let gofeed handle that one.
	if strings.Contains(u, "/rss") {
		return false
	}
	return true
}

// fetchHackerNews scrapes the Hacker News front-page HTML and returns the page
// title and parsed story items, formatted to mirror the hckrnews.com widget
// (title, points, comment count, and source site).
func (s *Server) fetchHackerNews(ctx context.Context, feedURL string) (string, []FeedItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NewsForNerds/1.0)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("hacker news returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", nil, err
	}

	var items []FeedItem

	// Each story headline lives in a <tr class="athing"> row; the metadata
	// (points/comments/author) lives in the immediately following sibling row.
	doc.Find("tr.athing").Each(func(_ int, row *goquery.Selection) {
		if len(items) >= s.Config.FeedMaxItems {
			return
		}

		titleLink := row.Find("span.titleline > a").First()
		title := strings.TrimSpace(titleLink.Text())
		if title == "" {
			return
		}

		// HN story ID (used as a stable key for first-seen tracking).
		hnID, _ := row.Attr("id")

		link, _ := titleLink.Attr("href")
		link = resolveHNLink(link)

		// Source site, e.g. "github.com".
		site := strings.TrimSpace(row.Find("span.sitestr").First().Text())

		// The subtext row follows the title row.
		sub := row.Next()
		points := strings.TrimSpace(sub.Find("span.score").First().Text())

		// Published timestamp lives in the age span's title attribute, e.g.
		// title="2026-06-02T18:47:07 1780426027".
		published := ""
		if ageTitle, ok := sub.Find("span.age").First().Attr("title"); ok {
			ts := strings.Fields(ageTitle)
			if len(ts) > 0 {
				if t, err := time.Parse("2006-01-02T15:04:05", ts[0]); err == nil {
					published = t.UTC().Format(time.RFC3339)
				}
			}
		}

		comments := ""
		commentLink := ""
		sub.Find("a").Each(func(_ int, a *goquery.Selection) {
			t := strings.TrimSpace(a.Text())
			if strings.Contains(t, "comment") {
				comments = t
				if href, ok := a.Attr("href"); ok {
					commentLink = resolveHNLink(href)
				}
			} else if t == "discuss" {
				comments = "discuss"
				if href, ok := a.Attr("href"); ok {
					commentLink = resolveHNLink(href)
				}
			}
		})

		// Build an hckrnews-style description line.
		var parts []string
		if points != "" {
			parts = append(parts, points)
		}
		if comments != "" {
			parts = append(parts, comments)
		}
		if site != "" {
			parts = append(parts, site)
		}
		desc := strings.Join(parts, " • ")

		// If the story has no external URL (Ask/Launch HN), fall back to the
		// HN discussion link so the title is still clickable.
		if link == "" {
			link = commentLink
		}

		items = append(items, FeedItem{
			Title:       title,
			Link:        link,
			Description: desc,
			Author:      commentLink,
			Published:   published,
			ID:          hnID,
		})
	})

	if len(items) == 0 {
		return "", nil, fmt.Errorf("no hacker news stories parsed (page layout may have changed)")
	}

	title := strings.TrimSpace(doc.Find("title").First().Text())
	if title == "" {
		title = "Hacker News"
	}
	return title, items, nil
}

// resolveHNLink turns a possibly-relative HN href into an absolute URL.
func resolveHNLink(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	return hnBaseURL + strings.TrimPrefix(href, "/")
}

// fetchAndStoreHackerNews scrapes the HN listing page and stores the result
// using the same feed cache as RSS feeds, so the existing RSS widget can
// render it transparently.
func (s *Server) fetchAndStoreHackerNews(ctx context.Context, feedURL string) {
	now := time.Now()
	q := dbgen.New(s.DB)

	title, items, err := s.fetchHackerNews(ctx, feedURL)
	if err != nil {
		slog.Warn("failed to scrape hacker news", "url", feedURL, "error", err)
		errStr := err.Error()
		_ = q.UpdateFeedError(ctx, dbgen.UpdateFeedErrorParams{
			LastFetched: &now,
			LastError:   &errStr,
			Url:         feedURL,
		})
		return
	}

	// Replicate hckrnews.com's "sorted by time" ordering: stories are ordered
	// by when they FIRST appeared on the front page, not their HN submission
	// time. We track first-seen timestamps locally across refreshes, keyed by
	// HN story ID, by reading back the previously cached content.
	firstSeen := map[string]string{}
	if existing, e := q.GetFeedByURL(ctx, feedURL); e == nil {
		var prev []FeedItem
		if json.Unmarshal([]byte(existing.Content), &prev) == nil {
			for _, p := range prev {
				if p.ID != "" && p.FirstSeen != "" {
					firstSeen[p.ID] = p.FirstSeen
				}
			}
		}
	}
	nowStr := now.UTC().Format(time.RFC3339)
	for i := range items {
		if fs, ok := firstSeen[items[i].ID]; ok {
			items[i].FirstSeen = fs
		} else {
			items[i].FirstSeen = nowStr
		}
	}

	content, _ := json.Marshal(items)
	if err := q.UpsertFeed(ctx, dbgen.UpsertFeedParams{
		Url:         feedURL,
		Title:       title,
		Content:     string(content),
		LastFetched: &now,
		LastError:   nil,
		CreatedAt:   now,
	}); err != nil {
		slog.Warn("failed to store hacker news feed", "url", feedURL, "error", err)
	}
}
