package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata" // embed the IANA tz database so America/Los_Angeles works in minimal builds

	"github.com/PuerkitoBio/goquery"
	"srv.exe.dev/db/dbgen"
)

// hnBaseURL is the canonical Hacker News base used for resolving relative links.
const hnBaseURL = "https://news.ycombinator.com/"

// losAngeles is the IANA timezone Hacker News renders its "age" titles in.
// Used to disambiguate the naive wall-clock timestamps in HN's HTML
// (e.g. "2026-06-02T18:47:07"), which carry no zone suffix.
var losAngeles = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		// Fall back to a fixed UTC-7 offset (PDT, no DST handling). The
		// _ "time/tzdata" import above should make LoadLocation succeed in
		// minimal builds where /usr/share/zoneinfo isn't present; this is
		// belt-and-suspenders for environments that strip tzdata anyway.
		return time.FixedZone("PT", -7*60*60)
	}
	return loc
}()

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
//
// HN's front page is ranked live, so a story can straddle a page boundary
// between consecutive paginated fetches (e.g. a story that was rank 30 on
// page 1 is now rank 31 on page 2). Without dedup the same story would show
// up twice in the widget. We dedup by HN story ID as we accumulate items.
func (s *Server) fetchHackerNews(ctx context.Context, feedURL string) (string, []FeedItem, error) {
	const maxPages = 12 // ~360 stories max, plenty for any widget

	var items []FeedItem
	pageTitle := ""
	pageURL := feedURL
	seen := make(map[string]bool) // HN IDs already collected across pages

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
		for _, it := range pageItems {
			// Skip IDs we've already collected. Rows with no ID (parses
			// gone wrong / malformed HTML) are kept as-is — they're rare
			// and at worst harmless.
			if it.ID != "" {
				if seen[it.ID] {
					continue
				}
				seen[it.ID] = true
			}
			items = append(items, it)
		}
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

// fetchHNDocument fetches a single HN HTML page, retrying with exponential
// backoff on HTTP 429 (rate limited). HN aggressively rate-limits rapid
// multi-page crawls, so we must be patient to paginate reliably.
func (s *Server) fetchHNDocument(ctx context.Context, pageURL string) (*goquery.Document, error) {
	const maxAttempts = 4
	backoff := 2 * time.Second

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NewsForNerds/1.0)")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts-1 {
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("hacker news returned status %d", resp.StatusCode)
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return doc, nil
	}
}

// fetchHackerNewsPage scrapes a single Hacker News listing page and returns the
// page title, parsed story items, and the absolute URL of the next page ("" if
// there is no "More" link).
func (s *Server) fetchHackerNewsPage(ctx context.Context, pageURL string) (string, []FeedItem, string, error) {
	doc, err := s.fetchHNDocument(ctx, pageURL)
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
		// title="2026-06-02T18:47:07 1780426027". HN renders these in
		// US/Pacific time without a zone suffix; parsing them as UTC (Go's
		// default for a naive timestamp) shifts the wall-clock time by 7-8
		// hours and makes day labels off by one for non-PT viewers. Parse
		// in PT and convert to UTC for storage so clients in any timezone
		// see the right local date.
		published := ""
		if ageTitle, ok := sub.Find("span.age").First().Attr("title"); ok {
			ts := strings.Fields(ageTitle)
			if len(ts) > 0 {
				if t, err := time.ParseInLocation("2006-01-02T15:04:05", ts[0], losAngeles); err == nil {
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

		// Parse points / comments as integers for structured filtering
		// (Top 10 / Top 20 / Top 50% / Homepage) on the client side.
		pointsNum, _ := strconv.Atoi(leadingInt(points))
		commentsNum, _ := strconv.Atoi(leadingInt(comments))
		// "discuss" stories have 0 comments; Atoi above handles that.

		// If the story has no external URL (Ask/Launch HN), fall back to the
		// HN discussion link so the title is still clickable.
		if link == "" {
			link = commentLink
		}

		items = append(items, FeedItem{
			// goquery's .Text() usually decodes HTML entities already,
			// but we run decodeFeedEntities defensively in case a future
			// scrape path changes (e.g. reading attr/title), or in case
			// a title text node was double-encoded. Idempotent on clean
			// text.
			Title:       decodeFeedEntities(title),
			Link:        link,
			Description: decodeFeedEntities(desc),
			Author:      commentLink,
			Published:   published,
			ID:          hnID,
			Points:      pointsNum,
			Comments:    commentsNum,
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
	//
	// We also track each story's PeakPoints — the highest points value it's
	// ever had — so the "Top 10 / Top 20" filters keep showing legendary
	// stories that hit the front page a while ago and have since decayed.
	var prev []FeedItem
	firstSeen := map[string]string{}
	peakByID := map[string]int{}
	if existing, e := q.GetFeedByURL(ctx, feedURL); e == nil {
		if json.Unmarshal([]byte(existing.Content), &prev) == nil {
			// Defensive dedup: if any prior run left duplicate IDs in the
			// cached JSON (e.g. from before pagination-overlap dedup was
			// added), collapse them here so the anti-regression merge
			// below starts from a clean list. First occurrence wins,
			// preserving original ordering.
			prev = dedupByID(prev)
			for _, p := range prev {
				if p.ID != "" {
					if p.FirstSeen != "" {
						firstSeen[p.ID] = p.FirstSeen
					}
					peakByID[p.ID] = p.PeakPoints
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
		// Bump the peak if the current scrape shows a higher score than
		// any previous refresh. This is the local equivalent of hckrnews's
		// "reached top X" tracking.
		peak := peakByID[items[i].ID]
		if items[i].Points > peak {
			peak = items[i].Points
		}
		items[i].PeakPoints = peak
	}

	// Anti-regression merge: HN rate-limits multi-page crawls, so a refresh may
	// legitimately scrape fewer pages than last time. Never let the stored list
	// shrink — carry forward any previously cached stories that this scrape
	// didn't return, so the widget keeps its accumulated history (matching
	// hckrnews.com, which retains older stories). Fresh scrapes win on ordering
	// and metadata; stale entries are appended after.
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		if it.ID != "" {
			seen[it.ID] = true
		}
	}
	pointsFromDesc := regexp.MustCompile(`(\d+)\s*points?`)
	for _, p := range prev {
		if p.ID == "" || seen[p.ID] {
			continue
		}
		// Backfill peak_points from the legacy description field for items
		// cached before the structured Points/PeakPoints fields existed,
		// so they aren't dropped from the "top N" filters on first refresh
		// after this code ships.
		if p.PeakPoints == 0 {
			if m := pointsFromDesc.FindStringSubmatch(p.Description); len(m) > 1 {
				if n, err := strconv.Atoi(m[1]); err == nil && n > p.PeakPoints {
					p.PeakPoints = n
				}
			}
		}
		items = append(items, p)
		seen[p.ID] = true
	}
	if len(items) > s.Config.FeedMaxItems {
		items = items[:s.Config.FeedMaxItems]
	}

	content, _ := json.Marshal(items)
	if err := q.UpsertFeed(ctx, dbgen.UpsertFeedParams{
		Url: feedURL,
		// Decode HTML entities in the HN page title defensively (the
		// <title> tag is unusual but if it ever had entities they'd
		// leak through goquery's text-only extraction).
		Title:       decodeFeedEntities(title),
		Content:     string(content),
		LastFetched: &now,
		LastError:   nil,
		CreatedAt:   now,
	}); err != nil {
		slog.Warn("failed to store hacker news feed", "url", feedURL, "error", err)
	}
}

// leadingInt pulls the first run of digits out of a string. HN shows
// points as "123 points" and comments as "45 comments" (or "discuss"
// for zero-comment Ask/Show HN posts), so this gives us the numbers
// without dragging in a full regex.
func leadingInt(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r < '0' || r > '9' {
			return s[:i]
		}
	}
	return s
}

// dedupByID returns items with duplicate IDs collapsed, keeping the
// first occurrence and preserving original order. Items with an empty
// ID are passed through unchanged (HN rows always have IDs; an empty
// ID signals a parse failure we don't want to silently merge).
//
// Used by the HN scraper to defend against front-page rank churn that
// can put the same story on two consecutive paginated fetches, and to
// clean up any duplicate entries that older versions of the scraper
// may have persisted into the feed cache.
func dedupByID(items []FeedItem) []FeedItem {
	if len(items) < 2 {
		return items
	}
	seen := make(map[string]bool, len(items))
	out := make([]FeedItem, 0, len(items))
	for _, it := range items {
		if it.ID != "" {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
		}
		out = append(out, it)
	}
	return out
}
