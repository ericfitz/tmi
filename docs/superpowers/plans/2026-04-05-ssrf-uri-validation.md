# SSRF URI Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add SSRF protections with wildcard allowlists and configurable schemes to all user-provided URI fields, replacing Timmy's SSRFValidator.

**Architecture:** New `URIValidator` in `api/ssrf_validator.go` with wildcard-aware allowlist matching and per-type scheme configuration. Four instances (issue_uri, document_uri, repository_uri, timmy) constructed from a new `SSRFConfig` section and injected into the `Server` struct. Validation runs after sanitization, before storage. Timmy's existing `SSRFValidator` is deleted.

**Tech Stack:** Go, `net/url`, `net` (DNS resolution), structured logging via `internal/slogging`

**Spec:** `docs/superpowers/specs/2026-04-05-ssrf-uri-validation-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `api/ssrf_validator.go` | Create | `URIValidator` struct, constructor, `Validate()`, allowlist matching, IP checks |
| `api/ssrf_validator_test.go` | Create | Unit tests for all validator behavior |
| `api/uri_validation_helpers.go` | Create | `validateURI`, `validateOptionalURI`, `ValidateURIPatchOperations` helpers |
| `api/uri_validation_helpers_test.go` | Create | Unit tests for helper functions |
| `internal/config/config.go` | Modify | Add `SSRFConfig` and `SSRFURIConfig` to `Config` struct |
| `internal/config/timmy.go` | Modify | Remove `SSRFAllowlist` field |
| `cmd/server/main.go` | Modify | Construct validators from config, inject into Server and Timmy providers |
| `api/server.go` | Modify | Add validator fields to `Server` struct, setter method |
| `api/threat_model_handlers.go` | Modify | Add URI validation in Create, Update, Patch |
| `api/threat_sub_resource_handlers.go` | Modify | Add URI validation in Create, Update, Patch, Bulk Create, Bulk Update, Bulk Patch |
| `api/document_sub_resource_handlers.go` | Modify | Add URI validation in Create, Update, Patch, Bulk Create, Bulk Update |
| `api/repository_sub_resource_handlers.go` | Modify | Add URI validation in Create, Update, Patch, Bulk Create, Bulk Update |
| `api/timmy_content_provider_http.go` | Modify | Change `*SSRFValidator` to `*URIValidator` |
| `api/timmy_content_provider_pdf.go` | Modify | Change `*SSRFValidator` to `*URIValidator` |
| `api/timmy_content_provider_test.go` | Modify | Update constructor calls |
| `api/timmy_ssrf.go` | Delete | Replaced by `api/ssrf_validator.go` |
| `api/timmy_ssrf_test.go` | Delete | Replaced by `api/ssrf_validator_test.go` |

---

### Task 1: Create URIValidator Core — Allowlist Parsing

**Files:**
- Create: `api/ssrf_validator.go`
- Create: `api/ssrf_validator_test.go`

- [ ] **Step 1: Write failing tests for allowlist parsing**

In `api/ssrf_validator_test.go`:

```go
package api

import (
	"testing"
)

func TestNewURIValidator_ValidAllowlistEntries(t *testing.T) {
	// Valid entries should be accepted without panic
	v := NewURIValidator([]string{"mycompany.com", "*.example.org"}, nil)
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	if len(v.exactHosts) != 1 {
		t.Errorf("expected 1 exact host, got %d", len(v.exactHosts))
	}
	if len(v.wildcardHosts) != 1 {
		t.Errorf("expected 1 wildcard host, got %d", len(v.wildcardHosts))
	}
}

func TestNewURIValidator_InvalidAllowlistEntries(t *testing.T) {
	// Invalid entries should be skipped (not cause panic)
	v := NewURIValidator([]string{
		"*mycompany.com",      // wildcard without dot
		"foo.*.mycompany.com", // wildcard not at start
		"",                    // empty string
		"valid.com",           // this one is valid
	}, nil)
	if len(v.exactHosts) != 1 {
		t.Errorf("expected 1 exact host (only valid.com), got %d", len(v.exactHosts))
	}
	if len(v.wildcardHosts) != 0 {
		t.Errorf("expected 0 wildcard hosts, got %d", len(v.wildcardHosts))
	}
}

func TestNewURIValidator_CaseInsensitiveAllowlist(t *testing.T) {
	v := NewURIValidator([]string{"MyCompany.COM"}, nil)
	if !v.exactHosts["mycompany.com"] {
		t.Error("expected case-insensitive storage of exact host")
	}
}

func TestNewURIValidator_DefaultSchemes(t *testing.T) {
	v := NewURIValidator(nil, nil)
	if !v.schemes["https"] {
		t.Error("expected https as default scheme")
	}
	if v.schemes["http"] {
		t.Error("did not expect http as default scheme")
	}
}

func TestNewURIValidator_CustomSchemes(t *testing.T) {
	v := NewURIValidator(nil, []string{"https", "ssh", "git"})
	if !v.schemes["https"] || !v.schemes["ssh"] || !v.schemes["git"] {
		t.Error("expected all custom schemes to be stored")
	}
	if v.schemes["http"] {
		t.Error("did not expect http when not configured")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestNewURIValidator`
Expected: FAIL — `NewURIValidator` not defined

- [ ] **Step 3: Implement URIValidator struct and constructor**

In `api/ssrf_validator.go`:

```go
package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// URIValidator validates URLs to prevent SSRF attacks.
// Each instance is configured with an optional allowlist (with wildcard support)
// and a set of allowed URL schemes.
type URIValidator struct {
	exactHosts    map[string]bool // case-insensitive exact domain + single subdomain match
	wildcardHosts []string        // suffix match for *.domain entries (stored without "*." prefix)
	schemes       map[string]bool // allowed URL schemes
	hasAllowlist  bool            // true if any allowlist entries were configured
}

// NewURIValidator creates a URIValidator with optional allowlist and scheme list.
// If schemes is nil or empty, defaults to ["https"].
// Invalid allowlist entries are skipped with a warning log.
//
// Allowlist entry formats:
//   - "mycompany.com" — matches exact domain + any single subdomain
//   - "*.mycompany.com" — matches domain + any depth of subdomains
//
// Invalid forms (skipped with warning):
//   - "*mycompany.com" — wildcard without dot separator
//   - "foo.*.mycompany.com" — wildcard not at beginning
//   - "" — empty string
func NewURIValidator(allowlist []string, schemes []string) *URIValidator {
	logger := slogging.Get()

	v := &URIValidator{
		exactHosts:    make(map[string]bool),
		wildcardHosts: make([]string, 0),
		schemes:       make(map[string]bool),
	}

	// Parse allowlist
	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			logger.Warn("SSRF allowlist: skipping empty entry")
			continue
		}

		lower := strings.ToLower(entry)

		if strings.Contains(lower, "*") {
			// Wildcard entry — must be exactly "*." prefix
			if !strings.HasPrefix(lower, "*.") {
				logger.Warn("SSRF allowlist: skipping invalid wildcard entry %q (must start with '*.'')", entry)
				continue
			}
			// Check no other wildcards exist
			if strings.Contains(lower[2:], "*") {
				logger.Warn("SSRF allowlist: skipping invalid wildcard entry %q (wildcard only allowed at start)", entry)
				continue
			}
			domain := lower[2:] // strip "*."
			v.wildcardHosts = append(v.wildcardHosts, domain)
			v.hasAllowlist = true
		} else {
			v.exactHosts[lower] = true
			v.hasAllowlist = true
		}
	}

	// Parse schemes (default to https)
	if len(schemes) == 0 {
		v.schemes["https"] = true
	} else {
		for _, s := range schemes {
			v.schemes[strings.ToLower(strings.TrimSpace(s))] = true
		}
	}

	return v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestNewURIValidator`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/ssrf_validator.go api/ssrf_validator_test.go
git commit -m "feat(api): add URIValidator struct with allowlist parsing and scheme config

Closes #231 (partial)"
```

---

### Task 2: URIValidator — Hostname Matching Logic

**Files:**
- Modify: `api/ssrf_validator.go`
- Modify: `api/ssrf_validator_test.go`

- [ ] **Step 1: Write failing tests for hostname matching**

Append to `api/ssrf_validator_test.go`:

```go
func TestURIValidator_MatchHost_ExactDomain(t *testing.T) {
	v := NewURIValidator([]string{"mycompany.com"}, nil)

	tests := []struct {
		hostname string
		want     bool
	}{
		{"mycompany.com", true},         // exact match
		{"www.mycompany.com", true},     // single subdomain
		{"jira.mycompany.com", true},    // single subdomain
		{"a.b.mycompany.com", false},    // two subdomains — rejected
		{"notmycompany.com", false},     // different domain
		{"mycompany.com.evil.com", false}, // suffix trick
	}
	for _, tt := range tests {
		got := v.matchHost(tt.hostname)
		if got != tt.want {
			t.Errorf("matchHost(%q) = %v, want %v", tt.hostname, got, tt.want)
		}
	}
}

func TestURIValidator_MatchHost_Wildcard(t *testing.T) {
	v := NewURIValidator([]string{"*.mycompany.com"}, nil)

	tests := []struct {
		hostname string
		want     bool
	}{
		{"mycompany.com", true},           // base domain
		{"www.mycompany.com", true},       // single subdomain
		{"a.b.c.mycompany.com", true},     // deep subdomains
		{"notmycompany.com", false},       // different domain
		{"mycompany.com.evil.com", false}, // suffix trick
	}
	for _, tt := range tests {
		got := v.matchHost(tt.hostname)
		if got != tt.want {
			t.Errorf("matchHost(%q) = %v, want %v", tt.hostname, got, tt.want)
		}
	}
}

func TestURIValidator_MatchHost_CaseInsensitive(t *testing.T) {
	v := NewURIValidator([]string{"MyCompany.COM"}, nil)
	if !v.matchHost("MYCOMPANY.com") {
		t.Error("expected case-insensitive matching")
	}
}

func TestURIValidator_MatchHost_NoAllowlist(t *testing.T) {
	v := NewURIValidator(nil, nil)
	// No allowlist = match anything
	if !v.matchHost("anything.example.com") {
		t.Error("expected no-allowlist to match any host")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestURIValidator_MatchHost`
Expected: FAIL — `matchHost` not defined

- [ ] **Step 3: Implement matchHost method**

Add to `api/ssrf_validator.go`:

```go
// matchHost checks if a hostname matches the allowlist.
// Returns true if no allowlist is configured (open mode).
// For exact entries ("mycompany.com"): matches the domain itself and any single subdomain.
// For wildcard entries ("*.mycompany.com"): matches the domain and any depth of subdomains.
func (v *URIValidator) matchHost(hostname string) bool {
	if !v.hasAllowlist {
		return true
	}

	lower := strings.ToLower(hostname)

	// Check exact hosts (domain + single subdomain)
	if v.exactHosts[lower] {
		return true
	}
	// Check if hostname is a single subdomain of an exact host: "sub.domain.com" -> "domain.com"
	if idx := strings.Index(lower, "."); idx >= 0 {
		parent := lower[idx+1:]
		if v.exactHosts[parent] {
			// Only allow single subdomain — reject if parent itself has dots that aren't the exact host
			// e.g., "a.b.mycompany.com" -> parent = "b.mycompany.com" which is not in exactHosts
			return true
		}
	}

	// Check wildcard hosts (domain + any depth of subdomains)
	for _, domain := range v.wildcardHosts {
		if lower == domain {
			return true // base domain match
		}
		if strings.HasSuffix(lower, "."+domain) {
			return true // subdomain match at any depth
		}
	}

	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestURIValidator_MatchHost`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/ssrf_validator.go api/ssrf_validator_test.go
git commit -m "feat(api): add wildcard-aware hostname matching to URIValidator"
```

---

### Task 3: URIValidator — Validate Method with IP Checks and DNS Resolution

**Files:**
- Modify: `api/ssrf_validator.go`
- Modify: `api/ssrf_validator_test.go`

- [ ] **Step 1: Write failing tests for scheme enforcement**

Append to `api/ssrf_validator_test.go`:

```go
func TestURIValidator_Validate_DefaultSchemeRejectsHTTP(t *testing.T) {
	v := NewURIValidator(nil, nil) // default = https only
	err := v.Validate("http://example.com")
	if err == nil {
		t.Error("expected http to be rejected with default schemes")
	}
}

func TestURIValidator_Validate_DefaultSchemeAllowsHTTPS(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := v.Validate("https://example.com")
	if err != nil {
		t.Errorf("expected https to be allowed, got: %v", err)
	}
}

func TestURIValidator_Validate_CustomSchemes(t *testing.T) {
	v := NewURIValidator(nil, []string{"https", "ssh", "git"})
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://github.com/repo", false},
		{"ssh://git@github.com/repo", false},
		{"git://github.com/repo.git", false},
		{"http://github.com/repo", true},
		{"ftp://files.example.com", true},
	}
	for _, tt := range tests {
		err := v.Validate(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("Validate(%q) error = %v, wantErr = %v", tt.url, err, tt.wantErr)
		}
	}
}
```

- [ ] **Step 2: Write failing tests for IP range blocking**

Append to `api/ssrf_validator_test.go`:

```go
func TestURIValidator_Validate_BlocksPrivateIPs(t *testing.T) {
	v := NewURIValidator(nil, []string{"https", "http"})
	blocked := []string{
		"http://10.0.0.1/path",
		"http://172.16.0.1/path",
		"http://192.168.1.1/path",
		"http://127.0.0.1/path",
		"http://[::1]/path",
		"http://169.254.169.254/latest/meta-data/",
		"http://169.254.1.1/path",
	}
	for _, u := range blocked {
		if err := v.Validate(u); err == nil {
			t.Errorf("expected %q to be blocked", u)
		}
	}
}

func TestURIValidator_Validate_BlocksLocalhost(t *testing.T) {
	v := NewURIValidator(nil, []string{"https", "http"})
	blocked := []string{
		"http://localhost/path",
		"http://LOCALHOST/path",
		"http://ip6-localhost/path",
		"http://ip6-loopback/path",
	}
	for _, u := range blocked {
		if err := v.Validate(u); err == nil {
			t.Errorf("expected %q to be blocked", u)
		}
	}
}

func TestURIValidator_Validate_AllowsPublicURLs(t *testing.T) {
	v := NewURIValidator(nil, []string{"https", "http"})
	allowed := []string{
		"https://github.com/ericfitz/tmi",
		"https://www.google.com",
		"http://example.com",
	}
	for _, u := range allowed {
		if err := v.Validate(u); err != nil {
			t.Errorf("expected %q to be allowed, got: %v", u, err)
		}
	}
}

func TestURIValidator_Validate_RejectsMalformedURLs(t *testing.T) {
	v := NewURIValidator(nil, nil)
	if err := v.Validate("://not-a-url"); err == nil {
		t.Error("expected malformed URL to be rejected")
	}
}

func TestURIValidator_Validate_AllowlistBypassesIPChecks(t *testing.T) {
	// Allowlisted hosts should bypass IP range checks
	// (they might resolve to private IPs, which is intentional for internal hosts)
	v := NewURIValidator([]string{"internal.corp.com"}, []string{"https", "http"})
	// We can't control DNS in unit tests, but we CAN verify that
	// an allowlisted hostname doesn't fail on the allowlist check itself.
	// DNS resolution may still fail, which is a different error.
	err := v.Validate("https://internal.corp.com/path")
	// This may succeed or fail depending on DNS — but it should NOT fail with "not in allowlist"
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("allowlisted host should not fail allowlist check: %v", err)
	}
}

func TestURIValidator_Validate_NonAllowlistedRejected(t *testing.T) {
	v := NewURIValidator([]string{"allowed.com"}, nil)
	err := v.Validate("https://notallowed.com/path")
	if err == nil {
		t.Error("expected non-allowlisted host to be rejected")
	}
	if !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("expected 'not in allowlist' error, got: %v", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `make test-unit name=TestURIValidator_Validate`
Expected: FAIL — `Validate` method not defined

- [ ] **Step 4: Implement Validate method and checkIP**

Add to `api/ssrf_validator.go`:

```go
// Validate checks if the URL is safe (not targeting internal resources).
//
// Validation order:
//  1. Parse URL
//  2. Check scheme against allowed schemes
//  3. If allowlist configured and host matches: allow (bypass IP checks)
//  4. If allowlist configured and host does NOT match: reject
//  5. Block localhost string variants
//  6. DNS resolution + IP range checks
func (v *URIValidator) Validate(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Step 2: scheme check
	scheme := strings.ToLower(parsed.Scheme)
	if !v.schemes[scheme] {
		allowed := make([]string, 0, len(v.schemes))
		for s := range v.schemes {
			allowed = append(allowed, s)
		}
		return fmt.Errorf("unsupported scheme: %s (allowed: %s)", parsed.Scheme, strings.Join(allowed, ", "))
	}

	hostname := strings.ToLower(parsed.Hostname())

	// Step 3/4: allowlist check
	if v.hasAllowlist {
		if v.matchHost(hostname) {
			return nil // allowlisted hosts bypass all further checks
		}
		return fmt.Errorf("host %q is not in allowlist", hostname)
	}

	// Steps 5-6: no allowlist — block dangerous destinations
	// Block localhost variants
	if hostname == "localhost" || hostname == "ip6-localhost" || hostname == "ip6-loopback" {
		return fmt.Errorf("blocked: localhost is not allowed")
	}

	// Check if hostname is already a literal IP
	if ip := net.ParseIP(hostname); ip != nil {
		return v.checkIP(ip)
	}

	// DNS resolution
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname: %s", hostname)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if err := v.checkIP(ip); err != nil {
			return err
		}
	}

	return nil
}

// checkIP verifies an IP address is not in a blocked range.
func (v *URIValidator) checkIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("blocked: loopback address %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("blocked: private address %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked: link-local address %s", ip)
	}
	// Cloud metadata endpoint
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("blocked: cloud metadata endpoint %s", ip)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit name=TestURIValidator_Validate`
Expected: PASS

- [ ] **Step 6: Run full test suite to make sure nothing is broken**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/ssrf_validator.go api/ssrf_validator_test.go
git commit -m "feat(api): add Validate method with scheme, allowlist, DNS, and IP checks"
```

---

### Task 4: URI Validation Helper Functions

**Files:**
- Create: `api/uri_validation_helpers.go`
- Create: `api/uri_validation_helpers_test.go`

- [ ] **Step 1: Write failing tests for helper functions**

In `api/uri_validation_helpers_test.go`:

```go
package api

import (
	"testing"
)

func TestValidateURI_NilValidator(t *testing.T) {
	err := validateURI(nil, "uri", "https://example.com")
	if err != nil {
		t.Errorf("expected nil validator to return nil, got: %v", err)
	}
}

func TestValidateURI_EmptyURI(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateURI(v, "uri", "")
	if err != nil {
		t.Errorf("expected empty URI to return nil, got: %v", err)
	}
}

func TestValidateURI_ValidURI(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateURI(v, "uri", "https://example.com")
	if err != nil {
		t.Errorf("expected valid URI to pass, got: %v", err)
	}
}

func TestValidateURI_InvalidURI(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateURI(v, "issue_uri", "http://10.0.0.1/path")
	if err == nil {
		t.Error("expected private IP URI to fail")
	}
}

func TestValidateOptionalURI_NilValue(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateOptionalURI(v, "issue_uri", nil)
	if err != nil {
		t.Errorf("expected nil value to return nil, got: %v", err)
	}
}

func TestValidateOptionalURI_EmptyStringValue(t *testing.T) {
	v := NewURIValidator(nil, nil)
	empty := ""
	err := validateOptionalURI(v, "issue_uri", &empty)
	if err != nil {
		t.Errorf("expected empty string to return nil, got: %v", err)
	}
}

func TestValidateOptionalURI_ValidValue(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := "https://jira.example.com/issue/123"
	err := validateOptionalURI(v, "issue_uri", &uri)
	if err != nil {
		t.Errorf("expected valid URI to pass, got: %v", err)
	}
}

func TestValidateOptionalURI_InvalidValue(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := "http://192.168.1.1/admin"
	err := validateOptionalURI(v, "issue_uri", &uri)
	if err == nil {
		t.Error("expected private IP URI to fail")
	}
}

func TestValidateURIPatchOperations_ValidatesURIFields(t *testing.T) {
	v := NewURIValidator(nil, nil)
	operations := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: "http://10.0.0.1/path"},
		{Op: "replace", Path: "/name", Value: "safe name"},
	}
	err := ValidateURIPatchOperations(v, operations, []string{"/issue_uri"})
	if err == nil {
		t.Error("expected patch operation with private IP URI to fail")
	}
}

func TestValidateURIPatchOperations_IgnoresNonURIFields(t *testing.T) {
	v := NewURIValidator(nil, nil)
	operations := []PatchOperation{
		{Op: "replace", Path: "/name", Value: "new name"},
		{Op: "replace", Path: "/description", Value: "new desc"},
	}
	err := ValidateURIPatchOperations(v, operations, []string{"/issue_uri"})
	if err != nil {
		t.Errorf("expected non-URI fields to pass, got: %v", err)
	}
}

func TestValidateURIPatchOperations_SkipsNonReplaceAdd(t *testing.T) {
	v := NewURIValidator(nil, nil)
	operations := []PatchOperation{
		{Op: "remove", Path: "/issue_uri"},
	}
	err := ValidateURIPatchOperations(v, operations, []string{"/issue_uri"})
	if err != nil {
		t.Errorf("expected remove operation to be skipped, got: %v", err)
	}
}

func TestValidateURIPatchOperations_NilValidator(t *testing.T) {
	operations := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: "http://10.0.0.1/path"},
	}
	err := ValidateURIPatchOperations(nil, operations, []string{"/issue_uri"})
	if err != nil {
		t.Errorf("expected nil validator to return nil, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestValidateURI`
Expected: FAIL — functions not defined

- [ ] **Step 3: Implement helper functions**

In `api/uri_validation_helpers.go`:

```go
package api

import "fmt"

// validateURI validates a required URI field against the given validator.
// Returns nil if the validator is nil or the URI is empty.
func validateURI(validator *URIValidator, fieldName, uri string) error {
	if validator == nil || uri == "" {
		return nil
	}
	if err := validator.Validate(uri); err != nil {
		return InvalidInputError(fmt.Sprintf("invalid %s: %s", fieldName, err.Error()))
	}
	return nil
}

// validateOptionalURI validates an optional (*string) URI field against the given validator.
// Returns nil if the validator is nil, the pointer is nil, or the string is empty.
func validateOptionalURI(validator *URIValidator, fieldName string, uri *string) error {
	if validator == nil || uri == nil || *uri == "" {
		return nil
	}
	if err := validator.Validate(*uri); err != nil {
		return InvalidInputError(fmt.Sprintf("invalid %s: %s", fieldName, err.Error()))
	}
	return nil
}

// ValidateURIPatchOperations validates URI values in JSON Patch operations.
// Only "replace" and "add" operations for the specified paths are validated.
// Returns nil if the validator is nil.
func ValidateURIPatchOperations(validator *URIValidator, operations []PatchOperation, uriPaths []string) error {
	if validator == nil {
		return nil
	}
	pathSet := make(map[string]bool, len(uriPaths))
	for _, p := range uriPaths {
		pathSet[p] = true
	}
	for _, op := range operations {
		if (op.Op == string(Replace) || op.Op == string(Add)) && pathSet[op.Path] {
			if content, ok := op.Value.(string); ok && content != "" {
				if err := validator.Validate(content); err != nil {
					// Strip leading "/" from path for field name in error message
					fieldName := op.Path
					if len(fieldName) > 0 && fieldName[0] == '/' {
						fieldName = fieldName[1:]
					}
					return InvalidInputError(fmt.Sprintf("invalid %s: %s", fieldName, err.Error()))
				}
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestValidateURI`
Expected: PASS

Also run:
Run: `make test-unit name=TestValidateOptionalURI`
Expected: PASS

Also run:
Run: `make test-unit name=TestValidateURIPatchOperations`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/uri_validation_helpers.go api/uri_validation_helpers_test.go
git commit -m "feat(api): add URI validation helper functions for handlers and patch operations"
```

---

### Task 5: Add SSRFConfig to Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/timmy.go`

- [ ] **Step 1: Add SSRFConfig types to config.go**

Add to `internal/config/config.go`, after the existing type definitions (before the closing of the file):

```go
// SSRFConfig holds SSRF protection settings for URI validation
type SSRFConfig struct {
	IssueURI      SSRFURIConfig `yaml:"issue_uri"`
	DocumentURI   SSRFURIConfig `yaml:"document_uri"`
	RepositoryURI SSRFURIConfig `yaml:"repository_uri"`
	Timmy         SSRFURIConfig `yaml:"timmy"`
}

// SSRFURIConfig holds allowlist and scheme configuration for a single URI type
type SSRFURIConfig struct {
	Allowlist string `yaml:"allowlist" env:""`
	Schemes   string `yaml:"schemes" env:""`
}
```

Note: The `env` tags need to be set per-field. Since the config library may not support dynamic env tags, the env vars will be handled in the parsing logic in `cmd/server/main.go`.

- [ ] **Step 2: Add SSRF field to Config struct**

In `internal/config/config.go`, modify the `Config` struct to add the SSRF field:

```go
type Config struct {
	Server         ServerConfig          `yaml:"server"`
	Database       DatabaseConfig        `yaml:"database"`
	Auth           AuthConfig            `yaml:"auth"`
	WebSocket      WebSocketConfig       `yaml:"websocket"`
	Webhooks       WebhookConfig         `yaml:"webhooks"`
	Logging        LoggingConfig         `yaml:"logging"`
	Operator       OperatorConfig        `yaml:"operator"`
	Secrets        SecretsConfig         `yaml:"secrets"`
	Administrators []AdministratorConfig `yaml:"administrators"`
	Timmy          TimmyConfig           `yaml:"timmy"`
	SSRF           SSRFConfig            `yaml:"ssrf"`
}
```

- [ ] **Step 3: Remove SSRFAllowlist from TimmyConfig**

In `internal/config/timmy.go`, remove this line from the `TimmyConfig` struct:

```go
SSRFAllowlist             string `yaml:"ssrf_allowlist" env:"TMI_TIMMY_SSRF_ALLOWLIST"` // Comma-separated list of allowed internal hosts
```

- [ ] **Step 4: Run build to verify compilation**

Run: `make build-server`
Expected: This will likely fail because `cmd/server/main.go` still references `cfg.Timmy.SSRFAllowlist`. That's expected — we'll fix it in Task 7.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/timmy.go
git commit -m "feat(config): add SSRFConfig with per-URI-type allowlist and scheme settings

Remove SSRFAllowlist from TimmyConfig (never shipped)."
```

---

### Task 6: Add Validator Fields to Server Struct

**Files:**
- Modify: `api/server.go`

- [ ] **Step 1: Add validator fields to Server struct**

In `api/server.go`, add three fields to the `Server` struct after the `allowHTTPWebhooks` field (around line 75):

```go
	// URI validators for SSRF protection
	issueURIValidator      *URIValidator
	documentURIValidator   *URIValidator
	repositoryURIValidator *URIValidator
```

- [ ] **Step 2: Add setter method**

Add after the existing setter methods (around line 200):

```go
// SetURIValidators sets the URI validators for SSRF protection
func (s *Server) SetURIValidators(issueURI, documentURI, repositoryURI *URIValidator) {
	s.issueURIValidator = issueURI
	s.documentURIValidator = documentURI
	s.repositoryURIValidator = repositoryURI
}
```

- [ ] **Step 3: Run lint to check for issues**

Run: `make lint`
Expected: PASS (fields are used in later tasks, but unused fields in structs don't trigger lint errors in Go)

- [ ] **Step 4: Commit**

```bash
git add api/server.go
git commit -m "feat(api): add URI validator fields and setter to Server struct"
```

---

### Task 7: Migrate Timmy Content Providers and Wire Config

**Files:**
- Modify: `api/timmy_content_provider_http.go`
- Modify: `api/timmy_content_provider_pdf.go`
- Modify: `api/timmy_content_provider_test.go`
- Delete: `api/timmy_ssrf.go`
- Delete: `api/timmy_ssrf_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Update HTTPContentProvider to use URIValidator**

In `api/timmy_content_provider_http.go`, change the struct field and constructor:

Replace:
```go
	ssrfValidator *SSRFValidator
```
With:
```go
	ssrfValidator *URIValidator
```

Replace:
```go
func NewHTTPContentProvider(ssrfValidator *SSRFValidator) *HTTPContentProvider {
```
With:
```go
func NewHTTPContentProvider(ssrfValidator *URIValidator) *HTTPContentProvider {
```

- [ ] **Step 2: Update PDFContentProvider to use URIValidator**

In `api/timmy_content_provider_pdf.go`, make the same changes:

Replace:
```go
	ssrfValidator *SSRFValidator
```
With:
```go
	ssrfValidator *URIValidator
```

Replace:
```go
func NewPDFContentProvider(ssrfValidator *SSRFValidator) *PDFContentProvider {
```
With:
```go
func NewPDFContentProvider(ssrfValidator *URIValidator) *PDFContentProvider {
```

- [ ] **Step 3: Update content provider tests**

In `api/timmy_content_provider_test.go`, replace all occurrences of `NewSSRFValidator` with `NewURIValidator`.

For calls like `NewSSRFValidator(nil)`, replace with `NewURIValidator(nil, nil)`.
For calls like `NewSSRFValidator([]string{"127.0.0.1"})`, replace with `NewURIValidator([]string{"127.0.0.1"}, []string{"https", "http"})`.

- [ ] **Step 4: Delete old SSRFValidator files**

```bash
rm api/timmy_ssrf.go api/timmy_ssrf_test.go
```

- [ ] **Step 5: Update cmd/server/main.go — replace Timmy SSRF wiring**

In `cmd/server/main.go`, find the block around lines 940-949 that constructs the Timmy SSRF validator:

Replace:
```go
	var ssrfAllowlist []string
	if cfg.Timmy.SSRFAllowlist != "" {
		ssrfAllowlist = strings.Split(cfg.Timmy.SSRFAllowlist, ",")
		for i := range ssrfAllowlist {
			ssrfAllowlist[i] = strings.TrimSpace(ssrfAllowlist[i])
		}
	}
	ssrfValidator := api.NewSSRFValidator(ssrfAllowlist)
	registry.Register(api.NewHTTPContentProvider(ssrfValidator))
	registry.Register(api.NewPDFContentProvider(ssrfValidator))
```

With:
```go
	// Build URI validators from SSRF config
	issueURIValidator := buildURIValidator(cfg.SSRF.IssueURI, "TMI_SSRF_ISSUE_URI")
	documentURIValidator := buildURIValidator(cfg.SSRF.DocumentURI, "TMI_SSRF_DOCUMENT_URI")
	repositoryURIValidator := buildURIValidator(cfg.SSRF.RepositoryURI, "TMI_SSRF_REPOSITORY_URI")
	timmyURIValidator := buildURIValidator(cfg.SSRF.Timmy, "TMI_SSRF_TIMMY")

	srv.SetURIValidators(issueURIValidator, documentURIValidator, repositoryURIValidator)

	registry.Register(api.NewHTTPContentProvider(timmyURIValidator))
	registry.Register(api.NewPDFContentProvider(timmyURIValidator))
```

Note: `srv` refers to the `*api.Server` variable — check the actual variable name in `cmd/server/main.go` and adjust.

- [ ] **Step 6: Add buildURIValidator helper in cmd/server/main.go**

Add this helper function in `cmd/server/main.go`:

```go
// buildURIValidator constructs a URIValidator from config, with env var overrides.
func buildURIValidator(cfg config.SSRFURIConfig, envPrefix string) *api.URIValidator {
	// Check env var overrides
	allowlistStr := cfg.Allowlist
	if envVal := os.Getenv(envPrefix + "_ALLOWLIST"); envVal != "" {
		allowlistStr = envVal
	}
	schemesStr := cfg.Schemes
	if envVal := os.Getenv(envPrefix + "_SCHEMES"); envVal != "" {
		schemesStr = envVal
	}

	var allowlist []string
	if allowlistStr != "" {
		for _, entry := range strings.Split(allowlistStr, ",") {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				allowlist = append(allowlist, entry)
			}
		}
	}

	var schemes []string
	if schemesStr != "" {
		for _, s := range strings.Split(schemesStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				schemes = append(schemes, s)
			}
		}
	}

	return api.NewURIValidator(allowlist, schemes)
}
```

- [ ] **Step 7: Build to verify compilation**

Run: `make build-server`
Expected: PASS

- [ ] **Step 8: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add api/timmy_content_provider_http.go api/timmy_content_provider_pdf.go api/timmy_content_provider_test.go cmd/server/main.go
git rm api/timmy_ssrf.go api/timmy_ssrf_test.go
git commit -m "refactor(api): replace SSRFValidator with URIValidator, wire config

Migrate Timmy content providers to URIValidator. Delete
api/timmy_ssrf.go. Construct all validators from SSRFConfig."
```

---

### Task 8: Add URI Validation to Threat Model Handlers

**Files:**
- Modify: `api/threat_model_handlers.go`

- [ ] **Step 1: Add validation to CreateThreatModel**

In `api/threat_model_handlers.go`, after the sanitization line (line 231):
```go
request.IssueUri = SanitizeOptionalString(request.IssueUri)
```

Add immediately after:
```go
	if err := validateOptionalURI(h.issueURIValidator, "issue_uri", request.IssueUri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

Note: The `ThreatModelHandler` struct currently only has a `wsHub` field. It needs access to the validator. Add a field and setter, consistent with the pattern used for other handlers:

In the `ThreatModelHandler` struct definition, add:
```go
type ThreatModelHandler struct {
	wsHub             *WebSocketHub
	issueURIValidator *URIValidator
}
```

Add setter:
```go
func (h *ThreatModelHandler) SetIssueURIValidator(v *URIValidator) {
	h.issueURIValidator = v
}
```

Then in `api/server.go`, update `SetURIValidators` to also set the threat model handler's validator:
```go
s.threatModelHandler.SetIssueURIValidator(issueURI)
```

- [ ] **Step 2: Add validation to UpdateThreatModel**

After line 424:
```go
request.IssueUri = SanitizeOptionalString(request.IssueUri)
```

Add:
```go
	if err := validateOptionalURI(h.issueURIValidator, "issue_uri", request.IssueUri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 3: Add validation to PatchThreatModel**

After line 626:
```go
modifiedTM.IssueUri = SanitizeOptionalString(modifiedTM.IssueUri)
```

Add:
```go
	if err := validateOptionalURI(h.issueURIValidator, "issue_uri", modifiedTM.IssueUri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 4: Build to verify compilation**

Run: `make build-server`
Expected: PASS

- [ ] **Step 5: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/threat_model_handlers.go api/server.go
git commit -m "feat(api): add SSRF validation to threat model issue_uri handlers"
```

---

### Task 9: Add URI Validation to Threat Sub-Resource Handlers

**Files:**
- Modify: `api/threat_sub_resource_handlers.go`

The `ThreatSubResourceHandler` needs access to the validator. Add a field and update the constructor.

- [ ] **Step 1: Add validator field to ThreatSubResourceHandler**

Add to the struct:
```go
type ThreatSubResourceHandler struct {
	threatStore      ThreatStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	issueURIValidator *URIValidator
}
```

Update constructor:
```go
func NewThreatSubResourceHandler(threatStore ThreatStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *ThreatSubResourceHandler {
```

Don't change the constructor signature — instead, add a setter or inject via the Server after construction. Check how the Server currently initializes this handler in `NewServer` (line 105):

```go
threatHandler: NewThreatSubResourceHandler(GlobalThreatStore, nil, nil, nil),
```

Add a method to set the validator after construction. In `api/threat_sub_resource_handlers.go`:

```go
// SetIssueURIValidator sets the SSRF validator for issue_uri fields
func (h *ThreatSubResourceHandler) SetIssueURIValidator(v *URIValidator) {
	h.issueURIValidator = v
}
```

Then in `api/server.go`, update `SetURIValidators`:
```go
func (s *Server) SetURIValidators(issueURI, documentURI, repositoryURI *URIValidator) {
	s.issueURIValidator = issueURI
	s.documentURIValidator = documentURI
	s.repositoryURIValidator = repositoryURI
	s.threatModelHandler.SetIssueURIValidator(issueURI)
	s.threatHandler.SetIssueURIValidator(issueURI)
	s.documentHandler.SetDocumentURIValidator(documentURI)
	s.repositoryHandler.SetRepositoryURIValidator(repositoryURI)
}
```

- [ ] **Step 2: Add validation to CreateThreat**

After line 340 (`threat.IssueUri = SanitizeOptionalString(threat.IssueUri)`), add:
```go
	if err := validateOptionalURI(h.issueURIValidator, "issue_uri", threat.IssueUri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 3: Add validation to UpdateThreat**

After line 420 (`threat.IssueUri = SanitizeOptionalString(threat.IssueUri)`), add:
```go
	if err := validateOptionalURI(h.issueURIValidator, "issue_uri", threat.IssueUri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 4: Add validation to PatchThreat**

After line 500 (`SanitizePatchOperations(operations, ...)`), add:
```go
	if err := ValidateURIPatchOperations(h.issueURIValidator, operations, []string{"/issue_uri"}); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 5: Add validation to BulkCreateThreats**

After line 640 (`threat.IssueUri = SanitizeOptionalString(threat.IssueUri)`), add:
```go
		if err := validateOptionalURI(h.issueURIValidator, "issue_uri", threat.IssueUri); err != nil {
			HandleRequestError(c, err)
			return
		}
```

- [ ] **Step 6: Add validation to BulkUpdateThreats**

After line 737 (`threat.IssueUri = SanitizeOptionalString(threat.IssueUri)`), add:
```go
		if err := validateOptionalURI(h.issueURIValidator, "issue_uri", threat.IssueUri); err != nil {
			HandleRequestError(c, err)
			return
		}
```

- [ ] **Step 7: Add validation to BulkPatchThreats**

After line 813 (`SanitizePatchOperations(patch.Operations, ...)`), add:
```go
		if err := ValidateURIPatchOperations(h.issueURIValidator, patch.Operations, []string{"/issue_uri"}); err != nil {
			HandleRequestError(c, err)
			return
		}
```

- [ ] **Step 8: Build and test**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add api/threat_sub_resource_handlers.go api/server.go
git commit -m "feat(api): add SSRF validation to threat issue_uri handlers"
```

---

### Task 10: Add URI Validation to Document Sub-Resource Handlers

**Files:**
- Modify: `api/document_sub_resource_handlers.go`

- [ ] **Step 1: Add validator field and setter**

Add to `DocumentSubResourceHandler` struct:
```go
	documentURIValidator *URIValidator
```

Add setter:
```go
func (h *DocumentSubResourceHandler) SetDocumentURIValidator(v *URIValidator) {
	h.documentURIValidator = v
}
```

- [ ] **Step 2: Add validation to CreateDocument**

After line 176 (`document.Uri = SanitizePlainText(document.Uri)`), add:
```go
	if err := validateURI(h.documentURIValidator, "uri", document.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 3: Add validation to UpdateDocument**

After line 250 (`document.Uri = SanitizePlainText(document.Uri)`), add:
```go
	if err := validateURI(h.documentURIValidator, "uri", document.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 4: Add validation to PatchDocument**

After line 478 (`SanitizePatchOperations(operations, ...)`), add:
```go
	if err := ValidateURIPatchOperations(h.documentURIValidator, operations, []string{"/uri"}); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 5: Add validation to BulkCreateDocuments**

After line 405 (`document.Uri = SanitizePlainText(document.Uri)`), add:
```go
		if err := validateURI(h.documentURIValidator, "uri", document.Uri); err != nil {
			HandleRequestError(c, err)
			return
		}
```

- [ ] **Step 6: Add validation to BulkUpdateDocuments**

After line 565 (`documents[i].Uri = SanitizePlainText(documents[i].Uri)`), add:
```go
		if err := validateURI(h.documentURIValidator, "uri", documents[i].Uri); err != nil {
			HandleRequestError(c, err)
			return
		}
```

- [ ] **Step 7: Build and test**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add api/document_sub_resource_handlers.go
git commit -m "feat(api): add SSRF validation to document uri handlers"
```

---

### Task 11: Add URI Validation to Repository Sub-Resource Handlers

**Files:**
- Modify: `api/repository_sub_resource_handlers.go`

- [ ] **Step 1: Add validator field and setter**

Add to `RepositorySubResourceHandler` struct:
```go
	repositoryURIValidator *URIValidator
```

Add setter:
```go
func (h *RepositorySubResourceHandler) SetRepositoryURIValidator(v *URIValidator) {
	h.repositoryURIValidator = v
}
```

- [ ] **Step 2: Add validation to CreateRepository**

After line 175 (`repository.Uri = SanitizePlainText(repository.Uri)`), add:
```go
	if err := validateURI(h.repositoryURIValidator, "uri", repository.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 3: Add validation to UpdateRepository**

After line 249 (`repository.Uri = SanitizePlainText(repository.Uri)`), add:
```go
	if err := validateURI(h.repositoryURIValidator, "uri", repository.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 4: Add validation to PatchRepository**

After line 463 (`SanitizePatchOperations(operations, ...)`), add:
```go
	if err := ValidateURIPatchOperations(h.repositoryURIValidator, operations, []string{"/uri"}); err != nil {
		HandleRequestError(c, err)
		return
	}
```

- [ ] **Step 5: Add validation to BulkCreateRepositorys**

After line 390 (`repository.Uri = SanitizePlainText(repository.Uri)`), add:
```go
		if err := validateURI(h.repositoryURIValidator, "uri", repository.Uri); err != nil {
			HandleRequestError(c, err)
			return
		}
```

- [ ] **Step 6: Add validation to BulkUpdateRepositorys**

After line 550 (`repositories[i].Uri = SanitizePlainText(repositories[i].Uri)`), add:
```go
		if err := validateURI(h.repositoryURIValidator, "uri", repositories[i].Uri); err != nil {
			HandleRequestError(c, err)
			return
		}
```

- [ ] **Step 7: Build and test**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add api/repository_sub_resource_handlers.go
git commit -m "feat(api): add SSRF validation to repository uri handlers"
```

---

### Task 12: Final Integration Verification

**Files:** None (verification only)

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: PASS (no new lint issues)

- [ ] **Step 2: Run full unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Run build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: PASS — existing tests use public-looking URLs or no URIs

- [ ] **Step 5: Verify config example in development config**

Check that `config-development.yml` does NOT need changes — with no SSRF config, the default behavior is: no allowlist (any public domain allowed), https-only scheme. This matches existing behavior since no URIs were validated before.

- [ ] **Step 6: Final commit if any fixes were needed**

If any fixes were applied during verification, commit them with an appropriate message.
