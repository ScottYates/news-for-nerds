package srv

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

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
		http.Error(w, "failed to fetch url: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

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
				if (e.data && e.data.type === 'nfn-visited') {
					visitedSet = new Set(e.data.urls || []);
					markAllVisited();
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
		
		// Inject after <head> tag
		headRegex := regexp.MustCompile(`(?i)(<head[^>]*>)`)
		if headRegex.MatchString(html) {
			html = headRegex.ReplaceAllString(html, "${1}"+injection)
		} else {
			// No head tag, try to inject at start of html
			htmlRegex := regexp.MustCompile(`(?i)(<html[^>]*>)`)
			if htmlRegex.MatchString(html) {
				html = htmlRegex.ReplaceAllString(html, "${1}<head>"+injection+"</head>")
			} else {
				// Just prepend
				html = injection + html
			}
		}

		// Rewrite relative URLs to absolute
		html = rewriteRelativeURLs(html, parsed)

		w.WriteHeader(resp.StatusCode)
		w.Write([]byte(html))
		return
	}

	// For non-HTML content, just proxy as-is
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// rewriteRelativeURLs converts relative URLs to absolute
func rewriteRelativeURLs(html string, base *url.URL) string {
	baseURL := base.Scheme + "://" + base.Host

	// Rewrite src and href attributes that start with /
	// This is a simple approach - won't catch everything but handles common cases
	re := regexp.MustCompile(`((?:src|href|action)=["'])(/[^"']*)`)
	html = re.ReplaceAllString(html, "${1}"+baseURL+"${2}")

	return html
}
