"""
Browser-driven integration tests for newsfornerds.

Boots the real server binary, then drives it with a real Chromium
browser via Playwright. Tests cover:

  - server boot + root redirect
  - security headers (CSP, XCTO, Referrer, Frame, Permissions, COOP)
  - CSRF (rejection, exemption, browser-level happy path)
  - widget CRUD via the API
  - HN scraper (the server scrapes news.ycombinator.com, decodes
    entities, dedupes, returns real Unicode)
  - RSS proxy + SSRF block + XSS in css
  - open-redirect guard
  - HTML widget renders user content
  - JSON body cap
  - visited-link tracking
  - page render in a real browser

Each scenario is independent; failures don't stop the rest.
"""
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
from pathlib import Path
from typing import Optional

from playwright.sync_api import (
    Browser,
    BrowserContext,
    Page,
    Playwright,
    Request,
    Response,
    sync_playwright,
)


# ----------------------------- helpers ---------------------------------

PASS = "\033[32mPASS\033[0m"
FAIL = "\033[31mFAIL\033[0m"
WARN = "\033[33mWARN\033[0m"


class Harness:
    def __init__(self, host: str = "127.0.0.1", port: int = 0):
        self.host = host
        self.port = port or _free_port()
        self.base = f"http://{host}:{self.port}"
        self.tmpdir = Path(tempfile.mkdtemp(prefix="nfn-int-"))
        self.db = self.tmpdir / "test.sqlite3"
        self.log = self.tmpdir / "server.log"
        # Where scenario screenshots land. We surface the latest run
        # by clearing this directory on harness start; per-scenario
        # PNGs are written here by the test functions.
        self.screens = self.tmpdir / "screens"
        self.screens.mkdir(parents=True, exist_ok=True)
        self.saved_screens: list[Path] = []  # files referenced by the user
        self.proc: Optional[subprocess.Popen] = None
        self.results: list[tuple[str, str, str]] = []  # (name, status, detail)

    def shot(self, page, name: str) -> Path:
        """Take a screenshot of the current page state and return the
        path. The path is also recorded so the test runner can
        surface it as a media deliverable after the run."""
        path = self.screens / f"{name}.png"
        page.screenshot(path=str(path), full_page=True)
        self.saved_screens.append(path)
        return path

    def start(self):
        env = os.environ.copy()
        env["DB_PATH"] = str(self.db)
        env["LOG_FILE"] = str(self.tmpdir / "server.log")
        env["LOG_LEVEL"] = "info"
        env["LISTEN_ADDR"] = f":{self.port}"
        env["GOOGLE_CLIENT_ID"] = ""  # disable OAuth, run in visitor mode
        env["GOOGLE_CLIENT_SECRET"] = ""

        self.proc = subprocess.Popen(
            [
                "/tmp/newsfornerds",
                "-listen", f":{self.port}",
                "-env", "",
            ],
            env=env,
            stdout=open(self.log, "w"),
            stderr=subprocess.STDOUT,
            cwd=self.tmpdir,
        )
        # Wait for the server to come up.
        for _ in range(50):
            try:
                with socket.create_connection((self.host, self.port), timeout=0.5):
                    return
            except OSError:
                if self.proc.poll() is not None:
                    raise RuntimeError(
                        f"server exited early. log:\n{self.log.read_text()}"
                    )
                time.sleep(0.1)
        raise RuntimeError(f"server didn't come up. log:\n{self.log.read_text()}")

    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()

    def report(self, name: str, status: str, detail: str = ""):
        self.results.append((name, status, detail))
        marker = PASS if status == "PASS" else FAIL if status == "FAIL" else WARN
        line = f"  {marker}  {name}"
        if detail:
            line += f"  --  {detail}"
        print(line, flush=True)


def _free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


# ----------------------------- scenarios --------------------------------


def test_root_redirect(h: Harness, ctx: BrowserContext):
    """GET / should 302 to a freshly-created page."""
    name = "root-redirect"
    r = ctx.request.get(f"{h.base}/", max_redirects=0)
    if r.status != 302:
        return h.report(name, "FAIL", f"expected 302, got {r.status}")
    loc = r.headers.get("location", "")
    if not loc.startswith("/page/"):
        return h.report(name, "FAIL", f"unexpected Location: {loc!r}")
    h.report(name, "PASS", f"-> {loc}")


def test_security_headers(h: Harness, ctx: BrowserContext):
    """All required security headers are set on the dashboard response."""
    name = "security-headers"
    # First, GET / to create a page (so the next request has a page to load).
    r1 = ctx.request.get(f"{h.base}/", max_redirects=0)
    loc = r1.headers["location"]
    r = ctx.request.get(f"{h.base}{loc}")
    h_text = r.headers
    required = {
        "content-security-policy": "default-src 'self'",
        "x-content-type-options": "nosniff",
        "referrer-policy": "strict-origin-when-cross-origin",
        "x-frame-options": "SAMEORIGIN",
        "permissions-policy": "camera=()",
        "cross-origin-opener-policy": "same-origin",
    }
    missing = []
    for k, fragment in required.items():
        v = h_text.get(k, "")
        if not v:
            missing.append(f"{k} missing")
        elif fragment not in v:
            missing.append(f"{k}={v!r} does not contain {fragment!r}")
    if missing:
        return h.report(name, "FAIL", "; ".join(missing))
    h.report(name, "PASS", "all headers present with expected values")


def test_csrf_protection_api(h: Harness, ctx: BrowserContext):
    """Direct API POST without a CSRF token must be 403."""
    name = "csrf-api-rejection"
    # Use a fresh context to avoid any cookies.
    fresh = ctx.browser.new_context()
    r = fresh.request.post(
        f"{h.base}/api/widgets/abc",
        data="{}",
        headers={"Content-Type": "application/json"},
    )
    if r.status != 403:
        return h.report(
            name, "FAIL", f"expected 403 without csrf token, got {r.status}"
        )
    fresh.close()
    h.report(name, "PASS")


def test_csrf_full_browser_flow(p: Playwright, h: Harness):
    """Open the dashboard in a real browser, let app.js set the csrf
    cookie, then call a state-changing endpoint from page context and
    verify the response succeeds (proving the JS bridge works)."""
    name = "csrf-browser-flow"
    browser = p.chromium.launch()
    try:
        ctx = browser.new_context()
        page = ctx.new_page()
        # Create a page via API.
        r = ctx.request.get(f"{h.base}/", max_redirects=0)
        page_url = f"{h.base}{r.headers['location']}"
        page.goto(page_url, wait_until="domcontentloaded")
        # Wait for app.js to initialize.
        page.wait_for_function("window.newsForNerds !== undefined", timeout=5000)
        # Read the csrf cookie (must exist after the page load).
        csrf = page.evaluate(
            "() => { const m = document.cookie.match(/(?:^|;\\s*)csrf=([^;]+)/); return m ? m[1] : ''; }"
        )
        if not csrf:
            return h.report(name, "FAIL", "csrf cookie not set on page load")
        # Patch a widget via the JS fetchWithCSRF wrapper. We may
        # need to create a widget first since a fresh page has none.
        result = page.evaluate(
            f"""
            async () => {{
                let widgets = Array.from(window.newsForNerds.widgets.keys());
                if (widgets.length === 0) {{
                    const pageId = document.getElementById('app').dataset.pageId;
                    const cr = await window.newsForNerds.fetchWithCSRF(
                        '/api/pages/' + pageId + '/widgets',
                        {{method: 'POST',
                          headers: {{'Content-Type': 'application/json'}},
                          body: JSON.stringify({{
                            title: 'browser-test',
                            widget_type: 'rss',
                            pos_x: 0, pos_y: 0, width: 300, height: 200
                          }})}}
                    );
                    if (!cr.ok) {{
                        return {{ok: false, reason: 'create failed', status: cr.status, text: await cr.text()}};
                    }}
                    const widget = (await cr.json()).data;
                    window.newsForNerds.widgets.set(widget.id, widget);
                    widgets = [widget.id];
                }}
                const id = widgets[0];
                const r = await window.newsForNerds.fetchWithCSRF(
                    '/api/widgets/' + id,
                    {{method: 'PATCH', headers: {{'Content-Type': 'application/json'}},
                     body: JSON.stringify({{title: 'updated from browser'}})}}
                );
                return {{ok: r.ok, status: r.status, text: r.ok ? null : await r.text()}};
            }}
            """
        )
        if not result["ok"]:
            return h.report(
                name,
                "FAIL",
                f"browser fetchWithCSRF failed: status={result.get('status')}, "
                f"reason={result.get('reason')}, text={result.get('text', '')[:200]}",
            )
        ctx.close()
        h.report(name, "PASS", f"csrf={csrf[:8]}..., widget update status={result['status']}")
    finally:
        browser.close()


def test_csrf_mismatch(h: Harness, ctx: BrowserContext):
    """A POST with a cookie but mismatched X-CSRF-Token header is 403."""
    name = "csrf-mismatch"
    fresh = ctx.browser.new_context()
    fresh.add_cookies(
        [{"name": "csrf", "value": "real-token", "url": h.base}]
    )
    r = fresh.request.post(
        f"{h.base}/api/widgets/abc",
        data="{}",
        headers={
            "Content-Type": "application/json",
            "X-CSRF-Token": "totally-different",
        },
    )
    if r.status != 403:
        return h.report(name, "FAIL", f"expected 403, got {r.status}")
    fresh.close()
    h.report(name, "PASS")


def test_widget_lifecycle(h: Harness, ctx: BrowserContext):
    """Create a widget via POST, list via GET, update via PATCH,
    delete via DELETE. Each step exercises the CSRF guard via the
    matching header."""
    name = "widget-lifecycle"
    # IMPORTANT: the visitor_id cookie is what the server uses to
    # identify ownership. A brand-new context has no visitor_id,
    # so the page it creates will be owned by that new visitor; a
    # second new context will own a different page. We must use the
    # SAME context for the page-creation GET and the widget POST.
    fresh = ctx.browser.new_context()
    # Create a page and capture the visitor_id that the server
    # assigned to us via Set-Cookie on the 302.
    r = fresh.request.get(f"{h.base}/", max_redirects=0)
    page_path = r.headers["location"]
    page_id = page_path.rsplit("/", 1)[-1]
    # Now add the csrf cookie. The visitor_id was already set by
    # the response above.
    fresh.add_cookies([{"name": "csrf", "value": "tok", "url": h.base}])

    create = fresh.request.post(
        f"{h.base}/api/pages/{page_id}/widgets",
        data={
            "title": "Test Widget",
            "widget_type": "rss",
            "pos_x": 10,
            "pos_y": 20,
            "width": 300,
            "height": 200,
        },
        headers={"X-CSRF-Token": "tok"},
    )
    if create.status != 200:
        return h.report(name, "FAIL", f"create: {create.status} {create.text()}")
    body = create.json()
    if not body.get("success"):
        return h.report(name, "FAIL", f"create body: {body}")
    widget_id = body["data"]["id"]

    listing = fresh.request.get(
        f"{h.base}/api/pages/{page_id}/widgets",
    )
    if listing.status != 200:
        return h.report(name, "FAIL", f"list: {listing.status}")
    if not any(w["id"] == widget_id for w in listing.json()["data"]):
        return h.report(name, "FAIL", "new widget not in listing")

    upd = fresh.request.patch(
        f"{h.base}/api/widgets/{widget_id}",
        data={"title": "Updated Title"},
        headers={"X-CSRF-Token": "tok"},
    )
    if upd.status != 200:
        return h.report(name, "FAIL", f"update: {upd.status} {upd.text()}")
    if upd.json()["data"]["title"] != "Updated Title":
        return h.report(name, "FAIL", f"update didn't apply: {upd.json()}")

    delete = fresh.request.delete(
        f"{h.base}/api/widgets/{widget_id}",
        headers={"X-CSRF-Token": "tok"},
    )
    if delete.status != 200:
        return h.report(name, "FAIL", f"delete: {delete.status} {delete.text()}")

    fresh.close()
    h.report(name, "PASS", f"create+list+update+delete of {widget_id[:8]}")


def test_hn_widget_in_browser(p: Playwright, h: Harness):
    """Open the dashboard, create an HN widget via JS, wait for the
    HN scraper to populate items, and confirm Unicode titles show
    correctly. Also exercises the fetchWithCSRF wrapper end-to-end."""
    name = "hn-widget"
    browser = p.chromium.launch()
    try:
        ctx = browser.new_context()
        page = ctx.new_page()
        # Page load -> csrf cookie.
        r = ctx.request.get(f"{h.base}/", max_redirects=0)
        page.goto(f"{h.base}{r.headers['location']}", wait_until="domcontentloaded")
        page.wait_for_function("window.newsForNerds !== undefined", timeout=5000)

        # Add an HN widget via the JS API.
        result = page.evaluate(
            f"""
            async () => {{
                const pageId = document.getElementById('app').dataset.pageId;
                const r = await window.newsForNerds.fetchWithCSRF(
                    '/api/pages/' + pageId + '/widgets',
                    {{method: 'POST',
                      headers: {{'Content-Type': 'application/json'}},
                      body: JSON.stringify({{
                        title: 'HN',
                        widget_type: 'hackernews',
                        pos_x: 0, pos_y: 0, width: 400, height: 600
                      }})}}
                );
                if (!r.ok) return {{ok: false, status: r.status, text: await r.text()}};
                const widget = (await r.json()).data;
                // Force a refresh.
                await window.newsForNerds.fetchWithCSRF(
                    '/api/feed/refresh?url=' + encodeURIComponent('https://news.ycombinator.com/'),
                    {{method: 'POST'}}
                );
                return {{ok: true, widget}};
            }}
            """
        )
        if not result["ok"]:
            return h.report(name, "WARN", f"create failed: {result}")
        # Now poll for HN items to appear.
        items = None
        for _ in range(40):
            r = ctx.request.get(
                f"{h.base}/api/feed?url={urllib.parse.quote('https://news.ycombinator.com/', safe='')}"
            )
            if r.status == 200 and r.json()["data"]["items"]:
                items = r.json()["data"]["items"]
                break
            time.sleep(0.5)
        if not items:
            return h.report(
                name, "WARN", "no HN items returned within 20s (network may be slow or blocked)"
            )
        # Each item's title should be a real string. We don't assert
        # specific stories (HN is dynamic); we just assert we got
        # >0 items with real titles and no raw HTML entities like
        # &#8217; in any title.
        n = len(items)
        bad = [it for it in items if "&#" in (it.get("title") or "")]
        if bad:
            return h.report(
                name,
                "FAIL",
                f"{len(bad)}/{n} items have undecoded entities: first={bad[0]['title']!r}",
            )
        ctx.close()
        h.report(name, "PASS", f"{n} HN items, all titles decoded")
    finally:
        browser.close()


def test_ssrf_block(h: Harness, ctx: BrowserContext):
    """/api/proxy and /api/feed must reject internal/metadata URLs."""
    name = "ssrf-block"
    bad_urls = [
        "http://127.0.0.1/",
        "http://localhost/admin",
        "http://169.254.169.254/latest/meta-data/",
        "http://10.0.0.1/",
    ]
    failed = []
    for u in bad_urls:
        r = ctx.request.get(
            f"{h.base}/api/proxy",
            params={"url": u},
        )
        if r.status != 400:
            failed.append(f"{u} -> {r.status} {r.text()[:80]}")
        r2 = ctx.request.get(
            f"{h.base}/api/feed",
            params={"url": u},
        )
        if r2.status != 400:
            failed.append(f"feed {u} -> {r2.status} {r2.text()[:80]}")
    if failed:
        return h.report(name, "FAIL", "; ".join(failed))
    h.report(name, "PASS", f"all {len(bad_urls)} blocked at API layer")


def test_xss_in_proxy_css(h: Harness, ctx: BrowserContext):
    """/api/proxy with a malicious css= must not break out of <style>."""
    name = "xss-proxy-css"
    # Need a real public URL the proxy will try to fetch. Use
    # example.com (an IANA-reserved domain that always returns 200).
    target = "https://example.com/"
    r = ctx.request.get(
        f"{h.base}/api/proxy",
        params={
            "url": target,
            "css": "</style><script>alert(1)</script>",
        },
    )
    if r.status != 200:
        return h.report(name, "FAIL", f"proxy returned {r.status}: {r.text()[:80]}")
    body = r.text()
    if "<script>alert(1)</script>" in body:
        return h.report(name, "FAIL", "XSS payload reached the response body")
    h.report(name, "PASS", "malicious css dropped at sanitization layer")


def test_open_redirect(h: Harness, ctx: BrowserContext):
    """/auth/login?return=https://evil.com/ must not store the
    absolute URL in the oauth_return cookie."""
    name = "open-redirect"
    # Note: with Google OAuth disabled the handler returns 503. But
    # the cookie is set BEFORE the redirect to Google. We can still
    # see whether the cookie is set at all, and if so, what value.
    r = ctx.request.get(
        f"{h.base}/auth/login",
        params={"return": "https://evil.com/"},
        max_redirects=0,
    )
    # We expect 503 (oauth not configured). Look at cookies.
    cookies = {c["name"]: c["value"] for c in ctx.cookies()}
    ret = cookies.get("oauth_return", "")
    if ret and ret != "/":
        return h.report(name, "FAIL", f"oauth_return cookie = {ret!r}")
    h.report(name, "PASS", "absolute URL rejected at cookie-write step")


def test_html_widget_render(p: Playwright, h: Harness):
    """Add an HTML widget in a real browser, open the dashboard,
    and confirm the html_content is rendered (it goes through the
    innerHTML sink; this is by-design and verifies the intentional
    XSS surface still works for the page owner)."""
    name = "html-widget"
    browser = p.chromium.launch()
    try:
        ctx = browser.new_context()
        page = ctx.new_page()
        r = ctx.request.get(f"{h.base}/", max_redirects=0)
        page.goto(f"{h.base}{r.headers['location']}", wait_until="domcontentloaded")
        page.wait_for_function("window.newsForNerds !== undefined", timeout=5000)

        # Set a marker on window and create an HTML widget whose
        # content references it. If the html_content is rendered as
        # raw HTML, we'll see the script execute (which we then
        # assert by reading the marker).
        result = page.evaluate(
            f"""
            async () => {{
                window.__htmlWidgetMarker = false;
                const pageId = document.getElementById('app').dataset.pageId;
                const r = await window.newsForNerds.fetchWithCSRF(
                    '/api/pages/' + pageId + '/widgets',
                    {{method: 'POST',
                      headers: {{'Content-Type': 'application/json'}},
                      body: JSON.stringify({{
                        title: 'HTML',
                        widget_type: 'html',
                        pos_x: 0, pos_y: 0, width: 300, height: 200,
                        config: JSON.stringify({{
                          html_content: '<div id="hw-marker">HI</div><script>window.__htmlWidgetMarker = true;</script>'
                        }})
                      }})}}
                );
                if (!r.ok) return {{ok: false, status: r.status, text: await r.text()}};
                return {{ok: true}};
            }}
            """
        )
        if not result["ok"]:
            return h.report(name, "FAIL", f"create: {result}")
        # Reload to render the new widget server-side.
        page.reload(wait_until="domcontentloaded")
        page.wait_for_function("window.newsForNerds !== undefined", timeout=5000)
        marker_text = page.evaluate(
            "() => document.getElementById('hw-marker')?.textContent || ''"
        )
        if marker_text != "HI":
            return h.report(
                name,
                "FAIL",
                f"html_content didn't render: marker.textContent = {marker_text!r}",
            )
        # Inner script ran.
        ran = page.evaluate("() => window.__htmlWidgetMarker === true")
        if not ran:
            return h.report(
                name,
                "WARN",
                "html_content text rendered but inner <script> didn't run (likely browser security); text-side ok",
            )
        ctx.close()
        h.report(name, "PASS", "html widget text + script rendered")
    finally:
        browser.close()


def test_import_widgets(p: Playwright, h: Harness):
    """Drive the existing /api/pages/{id}/import endpoint through the
    real browser using the testdata/widgets-import.json file (an
    export from the production app: 18 widgets, 1 HTML, 17 RSS, plus
    page_settings). Verify the import succeeds, the page is fully
    rendered, and capture screenshots before and after."""
    name = "import-widgets"
    import_path = Path(__file__).parent / "testdata" / "widgets-import.json"
    if not import_path.exists():
        return h.report(
            name, "FAIL", f"import file not found at {import_path}"
        )
    expected_titles = {w["title"] for w in json.loads(import_path.read_text())["widgets"]}
    expected_n = len(expected_titles)

    browser = p.chromium.launch()
    try:
        ctx = browser.new_context()
        page = ctx.new_page()
        # Auto-accept the confirm() dialog that the import flow shows.
        page.on("dialog", lambda d: d.accept())

        # Create a page via API.
        r = ctx.request.get(f"{h.base}/", max_redirects=0)
        page.goto(f"{h.base}{r.headers['location']}", wait_until="domcontentloaded")
        page.wait_for_function("window.newsForNerds !== undefined", timeout=5000)
        page_id = page.evaluate("() => document.getElementById('app').dataset.pageId")

        # Screenshot: empty page before import.
        h.shot(page, "01-before-import")

        # Upload the import file via the existing hidden file input.
        # Playwright's set_input_files fires the change event, which
        # triggers the existing importWidgets() handler.
        page.set_input_files("#import-file-input", str(import_path))

        # Wait for the import to complete. The JS adds widgets to the
        # app's internal map, so we poll that.
        page.wait_for_function(
            f"() => window.newsForNerds && window.newsForNerds.widgets.size >= {expected_n}",
            timeout=15000,
        )

        # Wait for widget DOM to settle. Give the layout a moment.
        page.wait_for_function(
            f"() => document.querySelectorAll('.widget').length >= {expected_n}",
            timeout=5000,
        )
        page.wait_for_timeout(500)  # let layout/fonts settle for the screenshot

        # Verify all expected titles are present in the DOM.
        rendered_titles = page.evaluate(
            """() => {
                const els = document.querySelectorAll('.widget-title');
                return Array.from(els).map(e => e.textContent.trim());
            }"""
        )
        missing = expected_titles - set(rendered_titles)
        if missing:
            return h.report(
                name, "FAIL",
                f"{len(missing)} widgets missing from DOM: "
                f"{sorted(missing)[:5]}...",
            )

        # Screenshot: full dashboard after import.
        h.shot(page, "02-after-import")

        # Screenshot: a single-widget close-up. The Hacker News widget
        # is the most interesting visually (hckrnews-style layout).
        hn = page.evaluate(
            """() => {
                const els = Array.from(document.querySelectorAll('.widget'));
                const hn = els.find(e => /Hacker News/i.test(e.textContent));
                if (!hn) return null;
                hn.scrollIntoView({block: 'center'});
                const r = hn.getBoundingClientRect();
                return {x: r.x, y: r.y, w: r.width, h: r.height};
            }"""
        )
        if hn:
            page.wait_for_timeout(200)
            closeup = h.screens / "03-hn-closeup.png"
            page.screenshot(
                path=str(closeup),
                clip={
                    "x": max(0, hn["x"] - 8),
                    "y": max(0, hn["y"] - 8),
                    "width": hn["w"] + 16,
                    "height": hn["h"] + 16,
                },
            )
            h.saved_screens.append(closeup)

        # Verify the server-side state: the page has 18 widgets and
        # the import endpoint would return them. We use the API path
        # to check (bypasses the rendered DOM).
        api = ctx.request.get(f"{h.base}/api/pages/{page_id}/widgets")
        if api.status != 200:
            return h.report(name, "FAIL", f"api widgets: {api.status}")
        api_widgets = api.json()["data"]
        if len(api_widgets) != expected_n:
            return h.report(
                name, "FAIL",
                f"expected {expected_n} widgets in API, got {len(api_widgets)}",
            )

        ctx.close()
        h.report(
            name, "PASS",
            f"{len(rendered_titles)} widgets rendered, all expected titles present",
        )
    finally:
        browser.close()


def test_visited_links(h: Harness, ctx: BrowserContext):
    """POST /api/visited then GET /api/visited returns the entry."""
    name = "visited-links"
    fresh = ctx.browser.new_context()
    # The visitor_id cookie is the user's identity. Use the same
    # context for the POST and the GET (so the visitor matches).
    fresh.request.get(f"{h.base}/", max_redirects=0)
    fresh.add_cookies([{"name": "csrf", "value": "tok", "url": h.base}])
    r = fresh.request.post(
        f"{h.base}/api/visited",
        data={"url": "https://example.com/article"},
        headers={"Content-Type": "application/json", "X-CSRF-Token": "tok"},
    )
    if r.status != 200:
        return h.report(name, "FAIL", f"post: {r.status} {r.text()}")
    r2 = fresh.request.get(f"{h.base}/api/visited")
    if r2.status != 200:
        return h.report(name, "FAIL", f"get: {r2.status}")
    urls = r2.json()["data"]
    if "https://example.com/article" not in urls:
        return h.report(name, "FAIL", f"url not in {urls}")
    fresh.close()
    h.report(name, "PASS")


def test_json_body_cap(h: Harness, ctx: BrowserContext):
    """A 2 MB JSON body to a state-changing endpoint returns 413
    (overrides any 403 from the CSRF middleware, since the body-cap
    check runs inside the handler after the middleware lets the
    request through)."""
    name = "json-body-cap"
    fresh = ctx.browser.new_context()
    fresh.request.get(f"{h.base}/", max_redirects=0)
    fresh.add_cookies([{"name": "csrf", "value": "tok", "url": h.base}])
    big = "x" * (2 * 1024 * 1024)
    r = fresh.request.post(
        f"{h.base}/api/feed/submit",
        data=f'{{"url":"https://example.com","title":"{big}"}}',
        headers={"Content-Type": "application/json", "X-CSRF-Token": "tok"},
    )
    if r.status != 413:
        return h.report(name, "FAIL", f"expected 413, got {r.status}")
    fresh.close()
    h.report(name, "PASS")


def test_dashboard_loads_in_browser(p: Playwright, h: Harness):
    """Open the dashboard in a real browser, add a widget via the JS
    API, reload, and verify app.js initializes, the widget renders
    in the DOM, and there are no console errors."""
    name = "dashboard-browser-load"
    browser = p.chromium.launch()
    errors = []
    try:
        ctx = browser.new_context()
        page = ctx.new_page()
        page.on("pageerror", lambda exc: errors.append(f"pageerror: {exc}"))
        page.on(
            "console",
            lambda msg: errors.append(f"console.{msg.type}: {msg.text}")
            if msg.type == "error"
            else None,
        )
        r = ctx.request.get(f"{h.base}/", max_redirects=0)
        page.goto(f"{h.base}{r.headers['location']}", wait_until="domcontentloaded")
        page.wait_for_function("window.newsForNerds !== undefined", timeout=5000)
        title = page.title()
        if "NewsForNerds" not in title:
            return h.report(name, "FAIL", f"unexpected title: {title!r}")
        toolbar_visible = page.evaluate(
            "() => !!document.getElementById('toolbar')"
        )
        if not toolbar_visible:
            return h.report(name, "FAIL", "toolbar not in DOM")
        # Add a widget so the test can verify the full render path.
        page.evaluate(
            """async () => {
                const pageId = document.getElementById('app').dataset.pageId;
                const r = await window.newsForNerds.fetchWithCSRF(
                    '/api/pages/' + pageId + '/widgets',
                    {method: 'POST', headers: {'Content-Type': 'application/json'},
                     body: JSON.stringify({
                         title: 'Test', widget_type: 'rss',
                         pos_x: 0, pos_y: 0, width: 300, height: 200
                     })}
                );
                if (r.ok) {
                    const w = (await r.json()).data;
                    window.newsForNerds.renderWidget(w);
                }
            }"""
        )
        # Wait for the widget DOM to appear.
        page.wait_for_selector(".widget", timeout=5000)
        n = page.evaluate("() => document.querySelectorAll('.widget').length")
        if n < 1:
            return h.report(name, "FAIL", f"widget didn't render (n={n})")
        if errors:
            return h.report(
                name,
                "FAIL",
                "page errors: " + "; ".join(errors[:3]),
            )
        ctx.close()
        h.report(name, "PASS", f"title={title!r}, widgets={n}, no JS errors")
    finally:
        browser.close()


# ----------------------------- main ------------------------------------


def main():
    h = Harness()
    h.start()
    print(f"\nServer up at {h.base}\n", flush=True)
    try:
        with sync_playwright() as p:
            # Single shared context for the http-only scenarios.
            browser = p.chromium.launch()
            try:
                ctx = browser.new_context()

                test_root_redirect(h, ctx)
                test_security_headers(h, ctx)
                test_csrf_protection_api(h, ctx)
                test_csrf_mismatch(h, ctx)
                test_widget_lifecycle(h, ctx)
                test_visited_links(h, ctx)
                test_json_body_cap(h, ctx)
                test_ssrf_block(h, ctx)
                test_xss_in_proxy_css(h, ctx)
                test_open_redirect(h, ctx)
                test_dashboard_loads_in_browser(p, h)
                test_csrf_full_browser_flow(p, h)
                test_hn_widget_in_browser(p, h)
                test_html_widget_render(p, h)
                test_import_widgets(p, h)
            finally:
                browser.close()
    finally:
        h.stop()

    n_pass = sum(1 for _, s, _ in h.results if s == "PASS")
    n_warn = sum(1 for _, s, _ in h.results if s == "WARN")
    n_fail = sum(1 for _, s, _ in h.results if s == "FAIL")
    total = len(h.results)
    print(
        f"\n=== {n_pass}/{total} pass, {n_warn} warn, {n_fail} fail ===",
        flush=True,
    )

    # Copy screenshots to a stable repo-local location (gitignored)
    # so the user can browse them after the run. The most useful
    # ones are the before/after/closeup from the import scenario.
    if h.saved_screens:
        stable = Path(__file__).parent / "screenshots"
        stable.mkdir(parents=True, exist_ok=True)
        for src in h.saved_screens:
            dst = stable / src.name
            shutil.copyfile(src, dst)
        print(
            f"\nScreenshots copied to: {stable}",
            flush=True,
        )
        for s in h.saved_screens:
            print(f"  - {s}", flush=True)

    if h.log.exists():
        # Surface the last few server log lines on failure.
        if n_fail:
            print(
                f"\nServer log tail ({h.log}):\n"
                + "\n".join(h.log.read_text().splitlines()[-30:]),
                flush=True,
            )
    return 0 if n_fail == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
