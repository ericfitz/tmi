# SECURITY_REGRESSION_SKILL.md

> **Status:** draft. This document accumulates regression-prevention rules as we close threats from `docs/THREAT_MODEL.md` and `docs/THREAT_MODEL_RESPONSE_PLAN.md`. It will eventually be transformed into a Claude Code skill that runs against any change touching security-sensitive code paths.

## Purpose

When TMI fixes a security threat, the *fix* is one PR but the *regression risk* is forever — a future refactor, a new caller, or a copy-paste of an old pattern can re-open the hole. This skill is the durable memory of every closed threat. For each closed threat, it records:

1. **What the threat was** — the underlying class of bug, in plain language.
2. **The dangerous pattern** — the code shape that creates it.
3. **The required pattern** — the single sanctioned way to do the safe thing.
4. **Detection signals** — `rg` patterns, file globs, AST shapes that the reviewer agent can search for.
5. **Tests that pin the fix** — names of tests that exist specifically to break if the regression returns.

## How to use this skill (reviewer agent)

When invoked against a diff or branch, do the following:

1. For every section below, run the **Detection signals** queries against the changed files. If a signal fires, surface it with the section title as the issue label.
2. For every section, verify the **Tests that pin the fix** are still present and pass. A deleted or skipped pinning test is a regression even if no other code changed.
3. Output a report grouped by section, marking each as `OK`, `REVIEW` (signal fired but might be intentional), or `BLOCK` (signal fired in a way that matches the dangerous pattern verbatim).

## How to extend this skill

When closing a new security issue, append a new section using the template at the bottom of this document. Keep sections focused on **one threat class** — if a single issue closes multiple threats, write one section per threat.

---

## Closed threats (regression rules)

<!-- New sections are appended here as threats are closed. Order: highest-severity first within each batch, then by issue number. -->

### T3 — Server-side request forgery via user-supplied URLs (#345)

**Threat class:** SSRF / DNS rebinding
**Closed by:** #345
**Threat-model reference:** `docs/THREAT_MODEL.md` §4 T3 (also partial T26)

#### What was wrong

Outbound HTTP from TMI handlers (Timmy HTTP fetch, Timmy PDF fetch, webhook delivery, webhook challenge, Timmy HTTP source) called `client.Do(req)` after a hostname-based SSRF check. The validator resolved the hostname, walked the IPs through a blocklist (private/loopback/link-local/`169.254.169.254`), and approved the URL — but the actual `client.Do(req)` then re-resolved the hostname at dial time. An attacker DNS server with TTL=0 could return a public IP at validation time and a private IP a moment later, walking the request to AWS/GCP metadata, RFC1918 networks, or Redis on localhost.

Each caller also instantiated its own `http.Client`, so there was no single egress point at which to add IP-pinning, response-header timeouts, or response body caps.

#### Dangerous pattern (do NOT reintroduce)

```go
// BROKEN: validator resolves once, but client.Do re-resolves at dial time.
import (
	"net/http"
	"time"
)

type Provider struct {
	ssrfValidator *URIValidator
	client        *http.Client
}

func New(v *URIValidator) *Provider {
	return &Provider{
		ssrfValidator: v,
		client:        &http.Client{Timeout: 30 * time.Second}, // <-- not pinned
	}
}

func (p *Provider) Fetch(ctx context.Context, rawURL string) error {
	if err := p.ssrfValidator.Validate(rawURL); err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	resp, err := p.client.Do(req) // <-- DNS rebinding window
	...
}
```

Any new file that builds its own `*http.Client` and routes outbound TMI traffic through it reintroduces this bug.

#### Required pattern (use THIS instead)

All server-originated outbound HTTP MUST flow through `api.SafeHTTPClient` (`api/safe_http_client.go`). The helper resolves once via a configurable `HostResolver`, walks every IP through `URIValidator.checkIP`, and pins the dialed IP via a custom `Transport.DialContext` that ignores the address argument:

```go
import "time"

type Provider struct {
	client *SafeHTTPClient
}

func New(v *URIValidator) *Provider {
	return &Provider{
		client: NewSafeHTTPClient(
			v,
			WithUserAgent("TMI-...."),
			WithDefaultTimeouts(30*time.Second, 5*time.Second, 10*1024*1024),
		),
	}
}

func (p *Provider) Fetch(ctx context.Context, rawURL string) error {
	res, err := p.client.Fetch(ctx, rawURL, SafeFetchOptions{
		MaxBodyBytes: 10 * 1024 * 1024,
	})
	...
}
```

For streaming downloads (e.g. PDF that goes to a temp file) use `FetchStreaming`, which returns an `*http.Response` whose body is wrapped in `LimitReader` bound by `MaxBodyBytes`.

#### Detection signals

- **rg pattern (block):** `rg -nP '\bhttp\.Client\s*\{' --type go -- api/ auth/` — fires when any non-helper file constructs an `http.Client`. Generated code (`api/api.go`) and the helper itself (`api/safe_http_client.go`) are the only legitimate hits.
- **rg pattern (block):** `rg -nP 'http\.NewRequestWithContext\(.*,\s*(http\.MethodGet|"GET"|"POST"|http\.MethodPost)' --type go -- api/ | rg -v 'safe_http_client'` — fires when a handler builds a request directly. Acceptable if the request is then handed to a SafeHTTPClient (rare); otherwise this is the bug.
- **rg pattern (review):** `rg -n 'net\.LookupHost|net\.DefaultResolver' --type go -- api/ auth/` — fires when code does its own DNS lookup. Should only appear inside `safe_http_client.go` and `ssrf_validator.go`.
- **Files of interest:** `api/timmy_content_provider_*.go`, `api/webhook_*_worker.go`, `api/content_source_http.go`, `api/safe_http_client.go`. Any new file that fits the pattern "fetch user-supplied URL" must be reviewed.
- **Manual check:** confirm `webhookHTTPClient(...)` from `api/webhook_base_worker.go` is not used by new code paths — it is a legacy shape and should be removed once unused.

#### Tests that pin the fix

- `api/safe_http_client_test.go::TestSafeHTTPClient_PinsResolvedIP` — confirms exactly one DNS resolution per Fetch.
- `api/safe_http_client_test.go::TestSafeHTTPClient_BlocksRebindToPrivateIP` — confirms ALL resolved IPs are checked, not just the first.
- `api/safe_http_client_test.go::TestSafeHTTPClient_BlocksLiteralPrivateIP` — confirms RFC1918, loopback, link-local, and cloud-metadata are blocked.
- `api/safe_http_client_test.go::TestSafeHTTPClient_BlocksLocalhostHostname` — confirms symbolic local hostnames are blocked.
- `api/safe_http_client_test.go::TestSafeHTTPClient_RedirectNotFollowed` — confirms redirects are not auto-followed (defense for redirect-to-private-IP).
- `api/safe_http_client_test.go::TestSafeHTTPClient_BodyCapTruncates` — confirms body cap.
- `api/safe_http_client_test.go::TestSafeHTTPClient_ResponseHeaderTimeout` — confirms slow-loris-on-headers defense (T26).
- `api/safe_http_client_test.go::TestSafeHTTPClient_DialAddressIgnored` — confirms the dial uses the pinned IP, not the URL host.

#### Notes

- A future `make check-no-direct-http-client` lint rule should grep for `http.Client{` in `api/` and fail the build for non-allowlisted files. Until that lint exists, the rg patterns above are the tripwire.
- The `URIValidator.Validate` method remains for *reference* URI validation (e.g. `issue_uri` stored on a threat model) where no fetch happens. Reuse there is fine; what we forbid is `Validate(...) → custom http.Client.Do(...)`.

---

### T16 — Open redirect / OAuth phishing (#343)

**Threat class:** open redirect / OAuth phishing
**Closed by:** #343
**Threat-model reference:** `docs/THREAT_MODEL.md` §4 T16

#### What was wrong

`/oauth2/authorize` accepted an arbitrary `client_callback` query parameter, stored it in Redis under the OAuth state, and at callback time issued `c.Redirect(http.StatusFound, redirectURL)` to that exact URL. Any attacker could send a victim a link containing `client_callback=https://evil.com/grab`, and after the victim authenticated TMI would redirect them — with the authorization code or session token attached — to the attacker.

The content-OAuth flow (`/me/content_oauth/...`) already used `api.ClientCallbackAllowList`. The main `/oauth2/authorize` flow had no equivalent.

#### Dangerous pattern (do NOT reintroduce)

```go
// BROKEN: client_callback flows from query → state → redirect with no allowlist.
clientCallback := c.Query("client_callback")
// ... store in Redis, eventually:
c.Redirect(http.StatusFound, clientCallback+"?code="+authCode+"&state="+state)
```

Any new auth handler that accepts a redirect target from the request and uses it without an exact-match (or wildcard-suffix) allowlist re-opens this hole.

#### Required pattern (use THIS instead)

`/oauth2/authorize` validates `client_callback` against `auth.ClientCallbackAllowList` (`auth/client_callback_allowlist.go`) before storing or redirecting. The allowlist is configured via `auth.oauth.client_callback_allowlist` in YAML or the comma-separated `TMI_OAUTH_CLIENT_CALLBACK_ALLOWLIST` env var. **An empty allowlist rejects every client_callback (fail-closed)** — the startup logs a warning so operators know they need to populate it.

```go
clientCallback := c.Query("client_callback")
if clientCallback != "" {
	allow := NewClientCallbackAllowList(h.config.OAuth.ClientCallbackAllowList)
	if !allow.Allowed(clientCallback) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback is not in the allowlist",
		})
		return
	}
}
```

The same allowlist concept exists in the api package (`api.ClientCallbackAllowList`) for the content-OAuth flow. They are intentionally duplicated rather than shared — `auth` cannot import `api` without a cycle. Keep behavior in sync if either changes.

#### Detection signals

- **rg pattern (block):** `rg -nP 'c\.Redirect\([^,]+,\s*c\.Query\(' --type go -- auth/ api/` — fires when a redirect target comes directly from a query parameter without an intervening allowlist call.
- **rg pattern (block):** `rg -nP 'c\.Redirect\([^)]+(clientCallback|client_callback|callbackURL)' --type go -- auth/ api/ | rg -v 'allow\.Allowed|CallbackAllow'` — fires when the redirect goes to a callback variable but no `Allowed(...)` check is on the same path.
- **rg pattern (review):** `rg -n 'client_callback' --type go -- auth/ api/` — every match should be near an allowlist call or a `_test.go` file.
- **Files of interest:** `auth/handlers_oauth.go`, `auth/client_callback_allowlist.go`, `api/content_oauth_handlers.go`, `api/content_oauth_callbacks.go`.

#### Tests that pin the fix

- `auth/client_callback_allowlist_test.go::TestClientCallbackAllowList_EmptyRejectsEverything` — fail-closed default.
- `auth/client_callback_allowlist_test.go::TestClientCallbackAllowList_RejectsAttackerVariants` — host-suffix smuggling, scheme mismatch, protocol-relative.
- `auth/handlers_oauth_client_callback_test.go::TestAuthorize_RejectsClientCallbackOutsideAllowlist` — end-to-end allowlist enforcement on `/oauth2/authorize`.
- `auth/handlers_oauth_client_callback_test.go::TestAuthorize_EmptyAllowlistRejectsAnyCallback` — pins fail-closed behavior for unconfigured operators.
- `auth/handlers_oauth_client_callback_test.go::TestAuthorize_AllowedClientCallbackPassesAllowlist` — pins that legitimate callbacks survive.

#### Notes

- Operators MUST configure `auth.oauth.client_callback_allowlist` in production. A startup warning is logged when the list is empty; the warning should escalate to an error in a future hardening pass once tooling guarantees no operator forgets.
- Wildcard patterns (`*` suffix only) are intentional: prefix-matching captures variable-path callbacks like `http://localhost:8079/cb?run=...` while preventing host smuggling because the prefix includes the full host.
- Dev / test config files (`config-development*.yml`, `config-test*.yml`) ship with the OAuth callback stub URL pre-populated. Do not strip those entries; new dev onramp depends on them.

---

## Section template

```markdown
### T{N} — {one-line threat name} (#{issue})

**Threat class:** {SSRF | IDOR | open-redirect | injection | auth-bypass | secrets-disclosure | ...}
**Closed by:** #{issue} (commit `{sha}`)
**Threat-model reference:** `docs/THREAT_MODEL.md` §4 T{N}

#### What was wrong
{2–4 sentences explaining the original bug in plain language. Include the verified evidence so future reviewers know this is real, not theoretical.}

#### Dangerous pattern (do NOT reintroduce)
\`\`\`go
// Example of the broken pattern that the fix removes.
\`\`\`

#### Required pattern (use THIS instead)
\`\`\`go
// Example of the sanctioned pattern, naming the helper or middleware.
\`\`\`

#### Detection signals
- **rg pattern (block):** `rg '...' --type go` — fires when the dangerous pattern returns verbatim.
- **rg pattern (review):** `rg '...' --type go` — fires when a related pattern shows up that *might* bypass the fix.
- **Files of interest:** `path/to/file.go`, `path/glob/*.go`
- **AST/manual checks:** {anything that can't be expressed as a regex}

#### Tests that pin the fix
- `path/to/test_file.go::TestRegressionForT{N}_*`
- {any other tests that must continue to pass}

#### Notes
{Optional: caveats, intentional exceptions, links to follow-up issues.}
```
