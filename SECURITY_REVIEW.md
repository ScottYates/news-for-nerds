# Security Review — NewsForNerds

**Scope:** `srv/*.go` (handlers, auth, proxy, RSS/HN scrapers, favicon, templates), `srv/templates/dashboard.html`, `srv/static/app.js`, `go.mod`, `db/migrations/*.sql`.

**Date:** 2026-08-18
**Reviewer:** Mavis (automated code review pass — manual reading, no SAST tool installed)

---

## Severity Summary

| # | Finding | Severity | Location |
|---|---|---|---|
| 1 | **SSRF in proxy handler** — accepts arbitrary URLs incl. localhost/metadata IPs | **Critical** | `srv/proxy.go:13` `HandleAPIProxy` |
| 2 | **SSRF in feed/favicon fetchers** — no host allow/deny list | **High** | `srv/rss.go:120+`, `srv/favicon.go:17` |
| 3 | **Stored XSS via proxy `css` parameter** | **High** | `srv/proxy.go:69-71` |
| 4 | **No CSRF protection** on any state-changing endpoint | **High** | All `POST/PATCH/DELETE` routes |
| 5 | **`return` URL is open redirect / SSRF precursor** | **High** | `srv/auth.go:102-117, 297-308` |
| 6 | **No security headers** (CSP, X-Frame-Options, X-Content-Type-Options, HSTS, Referrer-Policy) | **High** | `srv/server.go:1057` `Serve` |
| 7 | **TLS termination delegated entirely to reverse proxy** — README and `Serve` assume the operator fronts with TLS; no enforcement | **High** | `srv/server.go:1138` `http.ListenAndServe` |
| 8 | **`Secure: true` cookies break HTTP local dev** — by design but worth flagging | Low | `srv/auth.go:96, 113, 291, 446` |
| 9 | **No HTTP server timeouts** — slowloris / connection-exhaustion DoS | **Medium** | `srv/server.go:1138` `http.ListenAndServe` |
| 10 | **No rate limiting on auth, proxy, feed endpoints** | **Medium** | All handlers |
| 11 | **Visitor-cookie identity migration is unverified** — anyone who can set a `visitor_id` cookie can claim pages from that ID after a victim's Google login | **Medium** | `srv/auth.go:209-229` |
| 12 | **`httpClient` has no timeout on default request** | Low | `srv/server.go:115-117` |
| 13 | **Excessive `X-Forwarded-*` trust** — auth + cookie domain derived from upstream-set headers | **Medium** | `srv/auth.go:47-79, 1110-1135` |
| 14 | **`go.sum` not committed / `go mod verify` not run in CI** — supply chain risk | Low | repo hygiene |
| 15 | **`goquery` is currently `v1.8.0`** — old, last release 2020; check for advisories | Low | `go.mod:10` |
| 16 | **Inline event handler / `eval`-like usage in `app.js`** — none found, but no CSP to back-stop | N/A | `srv/static/app.js` |
| 17 | **HTML widget renders `config.html_content` as raw `innerHTML`** — by design, but worth documenting as the intentional XSS sink | By design | `srv/static/app.js:776` |
| 18 | **No body-size limit on JSON request bodies** — large payloads accepted | Low | All `json.NewDecoder` call sites |
| 19 | **OAuth state is stored in a cookie, not signed/tied to session** — replay only prevented by short `MaxAge`; OK but worth noting | Low | `srv/auth.go:90-99, 130-136` |
| 20 | **Session ID is a UUIDv4 with no rotation on privilege change** — no rotation on login | Low | `srv/auth.go:271-294` |

The Critical and High items are real and exploitable today. They should be fixed before this app is exposed to the public internet.

---

## 1. SSRF in proxy handler — Critical

**File:** `srv/proxy.go:13-136` (`HandleAPIProxy`)

The endpoint `GET /api/proxy?url=...` validates only that the URL parses and uses `http`/`https`. It does **not** block:

- `http://localhost:8000/api/...` — pivot through the app itself (auth bypass, internal-only endpoints)
- `http://127.0.0.1:6379/` — Redis, local services
- `http://169.254.169.254/latest/meta-data/` — AWS / GCP / Azure instance metadata
- `http://[::1]/`, `http://0.0.0.0/`
- DNS rebinding via a hostname that resolves to an internal IP

The handler is unauthenticated (no `AuthMiddleware`), so any unauthenticated visitor can probe internal network surfaces.

**Exploit example:**
```
GET /api/proxy?url=http://169.254.169.254/latest/meta-data/iam/security-credentials/
```
or
```
GET /api/proxy?url=http://localhost:5432/  (Postgres)
```

**Fix sketch:**
- Resolve the target hostname yourself, reject any IP in loopback / link-local / private / cloud-metadata ranges.
- Pin the resolution — re-resolve just before connecting (DNS rebinding).
- Optionally maintain an allow-list (e.g. only the hostnames the user has explicitly saved as widgets).
- Enforce an auth check (or at least a SameSite=Strict cookie + same-origin check) before responding.

---

## 2. SSRF in feed/favicon fetchers — High

**File:** `srv/rss.go:120+` (`fetchAndStoreFeed*`), `srv/favicon.go:17` (`GetFavicon`)

Same pattern as #1. The `url` query param to `/api/feed`, `/api/feed/refresh`, and the `feedURL` used in `GetFavicon` go straight to the server-side HTTP client with no host validation. A widget saved with a `http://169.254.169.254/...` URL will be auto-refreshed by the background ticker and silently leak data into the rendered feed.

**Fix sketch:** shared SSRF guard (same as #1) applied once in the feed-fetch path; reject and log on hit.

---

## 3. Stored XSS via proxy `css` parameter — High

**File:** `srv/proxy.go:69-71`

```go
if customCSS != "" {
    injection += `<style>` + customCSS + `</style>`
}
```

The `css` query parameter is interpolated directly into the response HTML, which is then rendered in an `<iframe src="/api/proxy?...">` from the dashboard. The injected content is in a CSS `<style>` block, but the surrounding script block has `</style>` opportunities — and even if the immediate context is style, a malicious `customCSS` of:

```
</style><img src=x onerror=alert(1)><style>
```

escapes the style context. The endpoint is unauthenticated and the resulting HTML is served from the app's own origin (`/api/proxy`), so this executes in the dashboard's origin.

**Fix:**
- Validate / strip `customCSS` to a real CSS subset (no `<`/`>`, or run a real CSS sanitizer).
- Or just remove the `customCSS` feature.

---

## 4. No CSRF protection — High

**Files:** all state-changing handlers — `HandleAPICreateWidget`, `HandleAPIUpdateWidget`, `HandleAPIDeleteWidget`, `HandleAPIUpdatePage`, `HandleAPIImportWidgets`, `HandleAPISubmitFeed`, `HandleAPIRefreshFeed`, `HandleAPIMarkVisited`, `HandleAPIClonePage` (if present), etc.

The session cookie is set with `SameSite: http.SameSiteLaxMode`. That blocks most cross-origin POST CSRF (browsers won't send `Lax` cookies on cross-site form POSTs), but:
- Same-site subdomains can still attack.
- GET-shaped actions (`/api/feed?url=...`, `/api/feed/refresh`) are still exploitable from a malicious site via image/script tags that issue a GET to the app.
- `POST /api/feed/refresh` is in fact the worst — an attacker can pin a victim's dashboard to repeatedly fetch an internal URL via the SSRF in #2, or blow their feed cache.

**Fix:** Add a CSRF token (double-submit cookie, or a header check on a server-issued token) and require it on all state-changing endpoints. Re-validate that no GET handler mutates state.

---

## 5. Open redirect / SSRF precursor via `return` URL — High

**File:** `srv/auth.go:102-117, 297-308`

```go
returnURL := r.URL.Query().Get("return")
if returnURL == "" { returnURL = r.Referer() }
...
http.Redirect(w, r, returnURL, http.StatusFound)
```

The `return` parameter is taken straight from the query string and used as a `Location:` header. An attacker can craft `/auth/login?return=https://evil.example/` and the post-login redirect will send the user there. This is a textbook open redirect, useful for phishing (the user just successfully signed in to the real app and then gets bounced to a fake "session expired" page).

It also compounds with #1 — once the user has a valid session, an open-redirect chain into `/api/proxy?url=...` can be used as an authenticated SSRF vector.

**Fix:** Whitelist relative paths starting with `/` and containing no `//` or `\\`. Reject anything absolute or off-host.

---

## 6. No security headers — High

**File:** `srv/server.go:1057-1138` (`Serve`)

Nothing in the response chain sets:

- `Content-Security-Policy` — should at minimum set `default-src 'self'` (TinyMCE and inline event handlers may need a more permissive policy, but baseline first).
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY` or `frame-ancestors 'none'` in CSP (the dashboard itself uses iframes for widgets, so `frame-ancestors` is per-page)
- `Referrer-Policy: no-referrer` or `strict-origin-when-cross-origin`
- `Strict-Transport-Security` when behind TLS
- `Permissions-Policy` to disable unused features (camera, mic, geolocation, etc.)

**Fix:** Add a middleware that sets these on all responses, with per-route overrides for the dashboard's own iframe usage.

---

## 7. No HTTP server timeouts — Medium

**File:** `srv/server.go:1138`

```go
return http.ListenAndServe(addr, handler)
```

The bare `http.ListenAndServe` uses `http.DefaultServeMux`-equivalent defaults, which means **no `ReadTimeout`, no `WriteTimeout`, no `IdleTimeout`**. Slowloris-style attacks and idle-socket exhaustion are unmitigated.

**Fix:** Use a configured `http.Server{...}` with `ReadHeaderTimeout: 5s`, `ReadTimeout: 30s`, `WriteTimeout: 60s`, `IdleTimeout: 120s`, and pass it to `http.Server.ListenAndServe` / `ListenAndServeTLS`.

---

## 8. No rate limiting — Medium

There's no per-IP or per-user rate limit on:

- `/auth/login` (state cookie write)
- `/api/proxy` (SSRF probe)
- `/api/feed/refresh` (cache-bust storm, also SSRF amplifier)
- `/api/feed/submit` (cache poisoning)

A logged-out attacker can hammer these to enumerate, probe SSRF, or fill the DB.

**Fix:** Add a token-bucket or sliding-window rate limiter (e.g. `golang.org/x/time/rate` per-IP) as middleware. Especially important for `/api/proxy`.

---

## 9. Visitor-cookie identity migration — Medium

**File:** `srv/auth.go:209-229`

```go
if visitorCookie, cookieErr := r.Cookie(visitorCookieName); cookieErr == nil && visitorCookie.Value != "" {
    visitorID := "visitor:" + visitorCookie.Value
    err = q.UpdatePagesOwnership(ctx, dbgen.UpdatePagesOwnershipParams{
        UserID:   user.ID,
        UserID_2: visitorID,
    })
    ...
}
```

If an attacker can plant a `visitor_id` cookie in a victim's browser (subdomain XSS, a previously-shared machine, network injection), then trigger the victim to log in, all pages owned by that visitor ID are silently migrated to the attacker's Google account. Conversely, if the attacker is the one logging in and the victim was using the same `visitor_id` (e.g. a shared link to a "scott-yates" page), the attacker now owns those pages.

**Fix:**
- Don't auto-migrate. Ask the user to confirm.
- Or, scope visitor IDs to `(IP, User-Agent, browser session)` so they're harder to forge.
- Or, sign the visitor cookie so it can't be set from outside the app.

---

## 10. Excessive `X-Forwarded-*` trust — Medium

**File:** `srv/auth.go:47-79, 1110-1135`

The OAuth redirect URI, cookie `Domain`, and canonical-domain check all read `X-Forwarded-Host` and `X-Forwarded-Proto` from the request without verifying the connection comes from a trusted reverse proxy. If the app is ever exposed without a proxy (or with a misconfigured proxy that allows these headers through), an attacker can:

- Spoof `X-Forwarded-Proto: http` to bypass the `Secure: true` cookie assumption.
- Spoof `X-Forwarded-Host: evil.com` to redirect the OAuth callback to a domain they control.
- Bypass the canonical-domain redirect (line 1110) by sending `X-Forwarded-Host: <real-canonical-domain>`.

**Fix:** Whitelist the proxy IPs / networks that may set these headers, or use a dedicated Go reverse-proxy library that strips untrusted hop headers (e.g. `chi/middleware` or a hand-rolled check).

---

## 11. Minor / informational

- **`goquery v1.8.0` (2020).** No public CVE I'm aware of, but it's an HTML parser and worth a `go list -m -u all` to see what's available.
- **No `go.sum` verified at build time.** Run `go mod verify` in CI.
- **`http.DefaultTransport` doesn't set timeouts.** Each outbound `httpClient.Do(...)` inherits the client's own `Timeout`, but if any code path uses `http.Get(...)` or `http.Post(...)` it bypasses the timeout. (`exchangeCode` in `auth.go:337` uses `http.Post` to `oauth2.googleapis.com` and relies on the default transport — that's fine for Google, but the pattern is brittle.)
- **`json.NewDecoder` with no `MaxBytes` cap.** A malicious or buggy client can post arbitrarily large JSON. Set a `http.MaxBytesReader` wrapper before decoding.
- **Session ID is not rotated on login.** `CreateSession` issues a new UUID but the old anonymous/visitor ID is left in place. Low impact (the new session is opaque), but session fixation in theory possible.
- **OAuth state cookie has no integrity protection.** Just a base64-encoded random. Fine for CSRF protection of the OAuth flow itself, but if you ever start using it for anything else, sign it.
- **HTML widget `config.html_content` rendered as raw `innerHTML`** (`srv/static/app.js:776`) is intentional — the page owner authors their own HTML. Document this in the README so users understand the trust model. If you ever add a sharing feature, sanitize.
- **No `LogOut` from a token's session list page.** Sessions are only deleted when the cookie is presented. Stolen cookies remain valid until `SessionDurationDays`. Consider adding a "log out all sessions" endpoint that deletes by user_id.

---

## Recommended Triage Order

1. **Today:** Add SSRF guard to `/api/proxy`, `/api/feed`, `/api/feed/refresh`, and `GetFavicon` (#1, #2). Validate `return` URL (#5). Sanitize or remove `customCSS` injection (#3).
2. **This week:** Add `http.Server` timeouts, rate limiting, and security headers (#6, #7, #8).
3. **Before next public deploy:** CSRF protection, TLS enforcement, `X-Forwarded-*` trust list (#4, #10, #11). Set `http.MaxBytesReader` on JSON endpoints.
4. **Backlog:** Visitor-cookie migration policy, dependency review (`go list -m -u all`), `go mod verify` in CI, session-rotation policy.

---

## What This Review Did *Not* Cover

- **Runtime fuzzing / dynamic analysis** — no SAST tool installed (`govulncheck`, `gosec`, `staticcheck`), no automated scan of dependency CVEs.
- **Browser-side XSS** beyond `app.js` static review — no DOM-based XSS scan run.
- **The HN scraper / favicon fetcher** for outbound resource exhaustion (a feed or favicon URL pointing at a slow / never-ending response) — partial DoS surface.
- **The database** — SQLite + WAL, queries are all parameterized via sqlc, so no SQLi surface; the file-based DB has no network exposure, but no backup/disaster-recovery was assessed.
- **Logging** — `logs/newsfornerds.log` may capture session IDs or tokens; not audited.
- **Build / deploy** — the systemd service file in the README, the make build target, the `build`/`start`/`stop` shell scripts were not in scope here.

A follow-up pass with `govulncheck` and a dynamic proxy-URL fuzzer would catch the dependency CVEs and confirm the SSRF mitigation end-to-end.
