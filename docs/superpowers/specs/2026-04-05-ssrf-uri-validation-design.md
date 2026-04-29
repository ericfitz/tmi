# SSRF Protections for User-Provided URI Fields

**Issue:** [#231](https://github.com/ericfitz/tmi/issues/231)
**Date:** 2026-04-05
**Branch:** dev/1.4.0

## Problem

TMI accepts user-provided URIs in four fields — threat model `issue_uri`, threat `issue_uri`, document `uri`, and repository `uri` — with no server-side SSRF protection. These URIs are stored as-is after text sanitization. While TMI does not fetch these URIs server-side today, storing unvalidated internal URIs creates risk: future features that resolve stored URIs would inherit SSRF vulnerabilities, and displaying internal URIs to other users leaks internal infrastructure topology.

An existing `SSRFValidator` in `api/timmy_ssrf.go` protects Timmy content providers but is not used for the four user-facing URI fields and lacks wildcard allowlist support.

## Solution

Replace the Timmy-specific `SSRFValidator` with a shared `URIValidator` that supports wildcard allowlists and per-type scheme configuration. Wire it into all handlers that accept URI fields.

## Design

### 1. URIValidator (`api/ssrf_validator.go`)

A new `URIValidator` struct, constructed with an optional allowlist (comma-separated hostnames with optional `*.` prefix) and an optional scheme list.

#### Constructor

```go
type URIValidator struct {
    exactHosts    map[string]bool      // case-insensitive exact + single-subdomain match
    wildcardHosts []string             // suffix match for *.domain entries
    schemes       map[string]bool      // allowed URL schemes
}

func NewURIValidator(allowlist []string, schemes []string) *URIValidator
```

- Allowlist entries are validated at construction. Invalid entries are skipped with a warning log.
- If `schemes` is empty, defaults to `["https"]`.

#### Validation Flow

1. **URL parsing** — `url.Parse(rawURL)`, reject malformed URLs.
2. **Scheme check** — validate against the instance's configured scheme list. Default: `https` only.
3. **Allowlist check** — if an allowlist is configured and the hostname matches an entry, **skip steps 4-6** (allowlisted hosts bypass IP restrictions, since the operator has explicitly trusted them). If an allowlist is configured and the hostname does not match, reject immediately.
4. **Localhost string check** — block `localhost`, `ip6-localhost`, `ip6-loopback` (case-insensitive).
5. **DNS resolution** — resolve hostname to IP addresses via `net.LookupHost`.
6. **IP range check** — block all resolved IPs against:
   - Loopback (`127.0.0.0/8`, `::1`)
   - Private (RFC 1918: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`)
   - Link-local (`169.254.0.0/16`, `fe80::/10`)
   - Cloud metadata (`169.254.169.254`)

Steps 4-6 only run when no allowlist is configured (open mode). In this mode, any domain that passes the IP range checks is accepted.

#### Allowlist Matching Rules

| Entry Format | Matches | Does Not Match |
|---|---|---|
| `mycompany.com` | `mycompany.com`, `www.mycompany.com`, `jira.mycompany.com` | `a.b.mycompany.com` |
| `*.mycompany.com` | `mycompany.com`, `www.mycompany.com`, `a.b.c.mycompany.com` | — |

**Valid entry forms:**
- `mycompany.com` — exact domain plus any single subdomain
- `*.mycompany.com` — domain plus any depth of subdomains

**Invalid entry forms (skipped with warning):**
- `*mycompany.com` — wildcard without dot separator
- `foo.*.mycompany.com` — wildcard not at beginning
- Empty string

Matching is case-insensitive. Port numbers in URLs are ignored (only hostname is checked).

#### Behavior Matrix

| Allowlist configured? | Schemes configured? | Behavior |
|---|---|---|
| No | No | Any domain passing IP checks, `https` only |
| No | Yes | Any domain passing IP checks, configured schemes only |
| Yes | No | Only matching hostnames, `https` only |
| Yes | Yes | Only matching hostnames, configured schemes only |

### 2. Configuration (`internal/config/config.go`)

New top-level config section added to the `Config` struct:

```go
type SSRFConfig struct {
    IssueURI      SSRFURIConfig `yaml:"issue_uri"`
    DocumentURI   SSRFURIConfig `yaml:"document_uri"`
    RepositoryURI SSRFURIConfig `yaml:"repository_uri"`
    Timmy         SSRFURIConfig `yaml:"timmy"`
}

type SSRFURIConfig struct {
    Allowlist string `yaml:"allowlist" env:"TMI_SSRF_<TYPE>_ALLOWLIST"`
    Schemes   string `yaml:"schemes"   env:"TMI_SSRF_<TYPE>_SCHEMES"`
}
```

#### Environment Variables

| URI Type | Allowlist | Schemes |
|---|---|---|
| Issue URI | `TMI_SSRF_ISSUE_URI_ALLOWLIST` | `TMI_SSRF_ISSUE_URI_SCHEMES` |
| Document URI | `TMI_SSRF_DOCUMENT_URI_ALLOWLIST` | `TMI_SSRF_DOCUMENT_URI_SCHEMES` |
| Repository URI | `TMI_SSRF_REPOSITORY_URI_ALLOWLIST` | `TMI_SSRF_REPOSITORY_URI_SCHEMES` |
| Timmy | `TMI_SSRF_TIMMY_ALLOWLIST` | `TMI_SSRF_TIMMY_SCHEMES` |

All values are comma-separated strings.

#### YAML Example

```yaml
ssrf:
  issue_uri:
    allowlist: "jira.mycompany.com, *.atlassian.net"
    schemes: "https"
  document_uri:
    allowlist: "*.mycompany.com, confluence.internal"
    schemes: "https, http"
  repository_uri:
    allowlist: "gitlab.internal, *.github.com"
    schemes: "https, ssh, git"
  timmy:
    allowlist: "wiki.internal"
    schemes: "https, http"
```

### 3. Handler Integration

Validation runs in handlers **after** text sanitization, **before** storage. If validation fails, the handler returns **400 Bad Request** with the validator's error message.

#### Helper Functions

```go
func validateURI(validator *URIValidator, fieldName, uri string) error
func validateOptionalURI(validator *URIValidator, fieldName string, uri *string) error
```

Both return nil if the validator is nil or the URI is empty/nil.

#### Affected Handlers

| Handler File | Operations | Field | Validator |
|---|---|---|---|
| `api/threat_model_handlers.go` | Create, Update, Patch | `issue_uri` | `issueURIValidator` |
| `api/threat_sub_resource_handlers.go` | Create, Update, Patch, Bulk Create, Bulk Update, Bulk Patch | `issue_uri` | `issueURIValidator` |
| `api/document_sub_resource_handlers.go` | Create, Update, Bulk Create, Bulk Update | `uri` | `documentURIValidator` |
| `api/repository_sub_resource_handlers.go` | Create, Update, Bulk Create, Bulk Update | `uri` | `repositoryURIValidator` |

#### Error Response Format

```json
{
  "error": "invalid issue_uri: blocked: private address 10.0.0.5"
}
```

### 4. Server Wiring

The `Server` struct gains three new fields:

```go
issueURIValidator      *URIValidator
documentURIValidator   *URIValidator
repositoryURIValidator *URIValidator
```

Constructed at startup in `cmd/server/main.go` from `SSRFConfig`. Timmy's content providers receive their own `URIValidator` from `ssrf.timmy` config.

### 5. Migration from Timmy's SSRFValidator

| File | Change |
|---|---|
| `api/timmy_ssrf.go` | Deleted — replaced by `api/ssrf_validator.go` |
| `api/timmy_ssrf_test.go` | Deleted — tests move to `api/ssrf_validator_test.go` |
| `api/timmy_content_provider_http.go` | Field type `*SSRFValidator` → `*URIValidator` |
| `api/timmy_content_provider_pdf.go` | Field type `*SSRFValidator` → `*URIValidator` |
| `api/timmy_content_provider_test.go` | Constructor calls updated |
| `internal/config/timmy.go` | `SSRFAllowlist` field removed |
| `cmd/server/main.go` | Old Timmy SSRF wiring replaced |

`TMI_TIMMY_SSRF_ALLOWLIST` was never in a shipped release and is deleted with no migration path.

### 6. Testing

#### Unit Tests (`api/ssrf_validator_test.go`)

**Allowlist parsing & validation:**
- Valid entries: `mycompany.com`, `*.mycompany.com`
- Invalid entries skipped with warning: `*mycompany.com`, `foo.*.mycompany.com`, empty string
- Case insensitivity

**Scheme enforcement:**
- Default (no schemes): only `https` allowed
- Custom schemes: configured list respected, others rejected

**Hostname matching without wildcard (`mycompany.com`):**
- Exact match — allowed
- Single subdomain — allowed
- Two+ subdomains — rejected

**Hostname matching with wildcard (`*.mycompany.com`):**
- Exact match — allowed
- Any depth subdomains — allowed

**No allowlist configured:**
- Public URL — allowed
- Private IP URL — blocked

**IP range blocking:**
- RFC 1918, loopback, link-local, cloud metadata — all blocked
- Public IPs — allowed

**DNS resolution:**
- Hostname resolving to private IP — blocked
- Unresolvable hostname — rejected

**Helper functions:**
- Nil validator — returns nil
- Nil/empty URI — returns nil

#### Integration Tests

Existing tests continue to pass. New integration tests confirm 400 responses for private-range URIs in create/update operations.

#### Timmy Content Provider Tests

Existing tests updated to use `NewURIValidator` — behavior identical.
