package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
//
// Hacker News only serves 30 stories per page, so to honor a higher item cap
// we follow the "More" pagination link (?p=2, ?p=3, ...) until we have enough
// items (FeedMaxItems) or run out of pages. A hard page cap guards against
// runaway crawls.
func (s *Server) fetchHackerNews(ctx context.Context, feedURL string) (string, []FeedItem, error) {
	const maxPages = 12 // ~360 stories max, plenty for any widget

	var items []FeedItem
	pageTitle := ""
	pageURL := feedURL

	for page := 0; page < maxPages; page++ {
		title, pageItems, next, err := s.fetchHackerNewsPage(ctx, pageURL)
		if err != nil {
			// If we already have some items from earlier pages, return those
			// rather than failing the whole refresh.
			if len(items) > 0 {
				break
			}
			return "", nil, err
		}
		if pageTitle == "" {
			pageTitle = title
		}
		items = append(items, pageItems...)
		if len(items) >= s.Config.FeedMaxItems {
			break
		}
		if next == "" {
			break
		}
		pageURL = next
		// Be polite between page requests.
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	if len(items) == 0 {
		return "", nil, fmt.Errorf("no hacker news stories parsed (page layout may have changed)")
	}
	if len(items) > s.Config.FeedMaxItems {
		items = items[:s.Config.FeedMaxItems]
	}
	if pageTitle == "" {
		pageTitle = "Hacker News"
	}
	return pageTitle, items, nil
}

// fetchHackerNewsPage scrapes a single Hacker News listing page and returns the
// page title, parsed story items, and the absolute URL of the next page ("" if
// there is no "More" link).
func (s *Server) fetchHackerNewsPage(ctx context.Context, pageURL string) (string, []FeedItem, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NewsForNerds/1.0)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, "", fmt.Errorf("hacker news returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", nil, "", err
	}

	var items []FeedItem

	// Each story headline lives in a <tr class="athing"> row; the metadata
	// (points/comments/author) lives in the immediately following sibling row.
	doc.Find("tr.athing").Each(func(_ int, row *goquery.Selection) {
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

	title := strings.TrimSpace(doc.Find("title").First().Text())
	if title == "" {
		title = "Hacker News"
	}

	// Follow the "More" pagination link if present. The href is relative to
	// the current page (e.g. "?p=2"), so resolve it against pageURL rather than
	// the site root.
	next := ""
	if href, ok := doc.Find("a.morelink").First().Attr("href"); ok {
		href = strings.TrimSpace(href)
		if base, perr := url.Parse(pageURL); perr == nil {
			if ref, rerr := url.Parse(href); rerr == nil {
				next = base.ResolveReference(ref).String()
			}
		}
	}

	return title, items, next, nil
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
