package srv

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// proxyCSP is the Content-Security-Policy sent on every /api/proxy
// response. No allow-same-origin: the document gets an opaque origin.
// maxProxyBodyBytes caps a non-HTML proxied response.
const maxProxyBodyBytes = 20 << 20 // 20 MiB

const proxyCSP = "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox; frame-ancestors 'self'"

// HandleAPIProxy proxies iframe content and injects <base target="_blank"> and optional CSS
func (s *Server) HandleAPIProxy(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	customCSS := r.URL.Query().Get("css")

	// Reject URLs that point at internal/cloud-metadata services before
	// we even try to fetch them. The transport's DialContext also
	// enforces this on the actual connect (and pins to the first
	// resolved IP to defeat DNS rebinding), so a hostname that flips
	// to a private IP between here and the dial is still blocked.
	if err := validateOutboundURL(targetURL); err != nil {
		http.Error(w, "invalid url: "+err.Error(), http.StatusBadRequest)
		return
	}
	parsed, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	// Create request
	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	// Set a reasonable user agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NewsForNerds/1.0)")

	resp, err := s.proxyClient.Do(req)
	if err != nil {
		slog.Warn("proxy fetch failed", "url", targetURL, "error", err)
		http.Error(w, "failed to fetch url", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Everything this endpoint returns was written by a third party.
	// The sandbox directive gives the response an opaque origin, also
	// when the URL is opened directly instead of in a widget iframe, so
	// its scripts cannot read the app's cookies or call the API as the
	// visitor. This replaces the app's default policy, which would
	// otherwise block the proxied page's own styles and images.
	w.Header().Set("Content-Security-Policy", proxyCSP)

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	
	// Copy relevant headers
	for _, h := range []string{"Content-Type", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}

	// If it's HTML, inject base target="_blank"
	if strings.Contains(contentType, "text/html") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
		if err != nil {
			http.Error(w, "failed to read response", http.StatusBadGateway)
			return
		}

		html := string(body)
		
		// Build injection: <base target="_blank">, visited link styling, and optional custom CSS
		injection := `<base target="_blank">`
		injection += `<style>.nfn-visited, .nfn-visited * { color: #666 !important; opacity: 0.6; }</style>`
		// Sanitize user-supplied CSS: any input containing "</" or an
		// HTML-comment marker could break out of the <style> block and
		// inject HTML/JS into the proxy response. The proxy is served
		// from the app's origin and is loaded into a same-origin
		// iframe, so a successful breakout is a full XSS. safeCustomCSS
		// rejects anything that could escape the style context.
		if css := safeCustomCSS(customCSS); css != "" {
			injection += `<style>` + css + `</style>`
		}
		// Inject script to request visited links from parent and mark them
		injection += `<script>
		(function() {
			var visitedSet = new Set();
			
			function markAllVisited() {
				document.querySelectorAll('a[href]').forEach(function(a) {
					if (visitedSet.has(a.href)) {
						a.classList.add('nfn-visited');
					} else {
						a.classList.remove('nfn-visited');
					}
				});
			}
			
			// Listen for visited links from parent
			window.addEventListener('message', function(e) {
				if (e.source !== window.parent || !e.data) return;
				if (e.data.type === 'nfn-visited') {
					visitedSet = new Set(e.data.urls || []);
					markAllVisited();
				} else if (e.data.type === 'nfn-list-links') {
					var urls = [];
					document.querySelectorAll('a[href]').forEach(function(a) { urls.push(a.href); });
					window.parent.postMessage({ type: 'nfn-links', id: e.data.id, urls: urls }, '*');
				}
			});
			
			// Track link clicks and notify parent
			document.addEventListener('click', function(e) {
				var link = e.target.closest('a[href]');
				if (link && link.href) {
					visitedSet.add(link.href);
					link.classList.add('nfn-visited');
					window.parent.postMessage({ type: 'nfn-link-clicked', url: link.href }, '*');
				}
			});
			
			// Ask parent for visited links
			window.parent.postMessage({ type: 'nfn-get-visited' }, '*');
		})();
		</script>`
		
		html = injectIntoHead(html, injection)

		// Rewrite relative URLs to absolute
		html = rewriteRelativeURLs(html, parsed)

		w.WriteHeader(resp.StatusCode)
		w.Write([]byte(html))
		return
	}

	// For non-HTML content, proxy as-is, up to a size cap so the
	// endpoint cannot be used to relay arbitrarily large downloads.
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, maxProxyBodyBytes))
}

var (
	// headTagRe matches the opening <head> tag only. The earlier
	// pattern <head[^>]*> also matched <header ...>.
	headTagRe = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
	htmlTagRe = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)
	// rootRelativeRe matches src/href/action values that start with "/".
	rootRelativeRe = regexp.MustCompile(`((?:src|href|action)=["'])(/[^"']*)`)
)

// injectIntoHead inserts injection once, right after the opening
// <head> tag (or creates a head after <html>, or prepends). The text
// is spliced in as-is; it is never used as a regexp replacement
// template, so "$" sequences in a user's custom CSS survive.
func injectIntoHead(html, injection string) string {
	if loc := headTagRe.FindStringIndex(html); loc != nil {
		return html[:loc[1]] + injection + html[loc[1]:]
	}
	if loc := htmlTagRe.FindStringIndex(html); loc != nil {
		return html[:loc[1]] + "<head>" + injection + "</head>" + html[loc[1]:]
	}
	return injection + html
}

// rewriteRelativeURLs makes root-relative src/href/action values
// absolute against the proxied site. Protocol-relative values
// ("//cdn.example/x.js") already name a host and are left alone.
func rewriteRelativeURLs(html string, base *url.URL) string {
	baseURL := base.Scheme + "://" + base.Host
	return rootRelativeRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := rootRelativeRe.FindStringSubmatch(m)
		if strings.HasPrefix(sub[2], "//") {
			return m
		}
		return sub[1] + baseURL + sub[2]
	})
}
