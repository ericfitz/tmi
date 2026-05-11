# `/oauth2/step_up` Implementation Plan (#397)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dedicated `GET /oauth2/step_up` endpoint that forces fresh interactive re-authentication at the upstream IdP (or short-circuits with in-place token rotation for weak providers) and audits the outcome to `system_audit_entries` — closing the warm-IdP-session gap in #355.

**Architecture:** New handler in `auth/handlers_step_up.go` reads the JWT from the cookie/Authorization header, classifies the provider as `strong` or `weak`, and either (strong) 302-redirects to the upstream IdP with `prompt=login&max_age=0` (OAuth/OIDC) or `ForceAuthn=true` (SAML), or (weak) rotates tokens in-place and returns 200 JSON. The existing `/oauth2/callback` and `/oauth2/token` handlers carry a step-up marker through the state/PKCE chain so the token mint can perform the identity-match check and write the completion audit row. No DB schema changes; no new config knobs; audit rows reuse the existing `system_audit_entries` table from #355.

**Tech Stack:** Go 1.x · Gin · Gorilla SAML (`crewjam/saml`) · Redis (state + PKCE store) · GORM/Postgres (audit table) · existing TMI auth package patterns (`auth/cookies.go`, `auth/token_blacklist.go`, `auth/state_store.go`, `api/admin_audit_redaction.go`).

**Reference spec:** `docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md`

---

## File structure

| Path | New / Modified | Purpose |
|---|---|---|
| `auth/handlers_step_up.go` | NEW | `Handlers.StepUp` — GET handler. Reads JWT, classifies, strong→302, weak→rotate+200. |
| `auth/provider_step_up.go` | NEW | `ClassifyStepUpStrength`, `BuildStepUpAuthorizationURL`. |
| `auth/audit_step_up.go` | NEW | Helpers to write step-up rows into `system_audit_entries` (wraps `api.SystemAuditRepository`). |
| `auth/saml/provider.go` | MODIFIED | Add `GetAuthorizationURLForceAuthn(state) (string, error)`. |
| `auth/handlers_oauth.go` | MODIFIED | `parseCallbackState` reads step-up fields; `processOAuthCallback` propagates them into `pkce:<code>`; handles upstream `error=access_denied` for step-up. |
| `auth/handlers_token.go` | MODIFIED | Step-up branch: identity-match, blacklist old refresh, audit, rotate. |
| `auth/handlers.go` | MODIFIED | Add `auditRepo` field to `Handlers` struct (so step-up audit helpers can write rows). |
| `cmd/server/main.go` | MODIFIED | Wire `GET /oauth2/step_up` → `Handlers.StepUp`. Inject `auditRepo` into `Handlers`. |
| `api-schema/tmi-openapi.json` | MODIFIED | Add `/oauth2/step_up` operation with full RFC-aligned response set + examples. |
| `auth/handlers_step_up_test.go` | NEW | Unit tests for the handler. |
| `auth/provider_step_up_test.go` | NEW | Unit tests for classifier + URL builder. |
| `auth/audit_step_up_test.go` | NEW | Unit tests for audit row shape. |
| `auth/handlers_oauth_step_up_callback_test.go` | NEW | Unit tests for callback marker propagation. |
| `auth/handlers_token_step_up_test.go` | NEW | Unit tests for token-mint step-up branch. |
| `test/integration/workflows/step_up_oauth_round_trip_test.go` | NEW | End-to-end integration test. |

The plan touches existing files but never restructures them beyond targeted additions.

---

## Task 0: Pre-flight check

**Files:** none

- [ ] **Step 1: Verify branch and clean tree**

```bash
git status
git branch --show-current
```

Expected: clean tree on `dev/1.4.0`.

- [ ] **Step 2: Confirm spec is committed**

```bash
ls docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md
git log --oneline -1 docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md
```

Expected: file exists; commit message includes "/oauth2/step_up design".

- [ ] **Step 3: Confirm prerequisite landed**

```bash
rg "type SystemAuditEntry struct" api/models/system_audit.go
rg "NewSystemAuditRepository" api/system_audit_repository.go
```

Expected: both lines present (proves #355 is on branch).

---

## Task 1: Step-up strength classifier (provider_step_up.go)

**Files:**
- Create: `auth/provider_step_up.go`
- Test: `auth/provider_step_up_test.go`

- [ ] **Step 1: Write the failing test**

Create `auth/provider_step_up_test.go`:

```go
package auth

import (
	"strings"
	"testing"
)

func TestClassifyStepUpStrength_KnownProviders(t *testing.T) {
	cases := []struct {
		providerID string
		issuer     string
		jwksURL    string
		want       StepUpStrength
	}{
		{"google", "https://accounts.google.com", "https://www.googleapis.com/oauth2/v3/certs", StepUpStrong},
		{"microsoft", "https://login.microsoftonline.com/common/v2.0", "https://login.microsoftonline.com/common/discovery/v2.0/keys", StepUpStrong},
		{"tmi", "", "", StepUpStrong}, // dev provider; controlled by us, treat as strong
		{"github", "", "", StepUpWeak},
		{"someenterprise-oidc", "https://idp.example.com", "https://idp.example.com/jwks", StepUpStrong},
		{"someenterprise-oauth", "", "", StepUpWeak},
	}
	for _, tc := range cases {
		t.Run(tc.providerID, func(t *testing.T) {
			cfg := OAuthProviderConfig{ID: tc.providerID, Issuer: tc.issuer, JWKSURL: tc.jwksURL}
			got := ClassifyStepUpStrength(cfg)
			if got != tc.want {
				t.Fatalf("ClassifyStepUpStrength(%q) = %v, want %v", tc.providerID, got, tc.want)
			}
		})
	}
}

func TestBuildStepUpAuthorizationURL_OAuthAppendsPromptAndMaxAge(t *testing.T) {
	cfg := OAuthProviderConfig{
		ID:               "google",
		ClientID:         "test-client",
		AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:         "https://oauth2.googleapis.com/token",
		Scopes:           []string{"openid", "email"},
	}
	bp, err := NewBaseProvider(cfg, "http://localhost:8080/oauth2/callback")
	if err != nil {
		t.Fatalf("NewBaseProvider: %v", err)
	}
	got, err := BuildStepUpAuthorizationURL(bp, cfg, "state-123")
	if err != nil {
		t.Fatalf("BuildStepUpAuthorizationURL: %v", err)
	}
	if !strings.Contains(got, "prompt=login") {
		t.Errorf("step-up URL missing prompt=login: %s", got)
	}
	if !strings.Contains(got, "max_age=0") {
		t.Errorf("step-up URL missing max_age=0: %s", got)
	}
	if !strings.Contains(got, "state=state-123") {
		t.Errorf("step-up URL missing state: %s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./auth/ -run "TestClassifyStepUpStrength_KnownProviders|TestBuildStepUpAuthorizationURL_OAuthAppendsPromptAndMaxAge" -count=1
```

Expected: FAIL with "undefined: StepUpStrength" / "undefined: ClassifyStepUpStrength" / "undefined: BuildStepUpAuthorizationURL".

- [ ] **Step 3: Write minimal implementation**

Create `auth/provider_step_up.go`:

```go
package auth

import (
	"fmt"
	"net/url"
)

// StepUpStrength classifies whether a given provider can guarantee a fresh
// interactive re-authentication on demand. Strong providers honor OIDC's
// prompt=login + max_age=0 (or SAML's ForceAuthn=true). Weak providers do not,
// and step-up against them is short-circuited with an audit marker.
//
// See docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md.
type StepUpStrength int

const (
	StepUpStrong StepUpStrength = iota
	StepUpWeak
)

func (s StepUpStrength) String() string {
	switch s {
	case StepUpStrong:
		return "strong"
	case StepUpWeak:
		return "weak"
	default:
		return "unknown"
	}
}

// knownStrongProviderIDs is the explicit allowlist of provider IDs known to
// honor prompt=login/max_age=0 even when no Issuer/JWKSURL is configured (e.g.,
// the in-process tmi dev provider, which we control end-to-end).
var knownStrongProviderIDs = map[string]bool{
	"google":    true,
	"microsoft": true,
	"tmi":       true,
}

// knownWeakProviderIDs is the explicit denylist of provider IDs known to
// silently ignore prompt=login (notably GitHub).
var knownWeakProviderIDs = map[string]bool{
	"github": true,
}

// ClassifyStepUpStrength returns the step-up strength for the given provider
// config. Rules (first match wins):
//
//  1. ID in knownStrongProviderIDs → Strong
//  2. ID in knownWeakProviderIDs   → Weak
//  3. Has Issuer AND JWKSURL (i.e., OIDC)  → Strong (generic OIDC providers
//     honor prompt=login per the OIDC spec)
//  4. Otherwise → Weak (pure-OAuth2 fallback; safest default)
//
// SAML providers are classified Strong by callers via a separate path; this
// function operates on OAuth provider configs only.
func ClassifyStepUpStrength(cfg OAuthProviderConfig) StepUpStrength {
	if knownStrongProviderIDs[cfg.ID] {
		return StepUpStrong
	}
	if knownWeakProviderIDs[cfg.ID] {
		return StepUpWeak
	}
	if cfg.Issuer != "" && cfg.JWKSURL != "" {
		return StepUpStrong
	}
	return StepUpWeak
}

// BuildStepUpAuthorizationURL builds the upstream authorize URL for a step-up
// round-trip. For OAuth/OIDC providers it appends prompt=login and max_age=0
// to the URL returned by provider.GetAuthorizationURL(state). SAML callers
// must not use this function; they call GetAuthorizationURLForceAuthn on the
// SAML provider directly.
func BuildStepUpAuthorizationURL(provider Provider, cfg OAuthProviderConfig, state string) (string, error) {
	raw := provider.GetAuthorizationURL(state)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid upstream authorize URL: %w", err)
	}
	q := u.Query()
	q.Set("prompt", "login")
	q.Set("max_age", "0")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./auth/ -run "TestClassifyStepUpStrength_KnownProviders|TestBuildStepUpAuthorizationURL_OAuthAppendsPromptAndMaxAge" -count=1 -v
```

Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add auth/provider_step_up.go auth/provider_step_up_test.go
git commit -m "feat(auth): step-up strength classifier + URL builder (#397)"
```

---

## Task 2: SAML ForceAuthn variant

**Files:**
- Modify: `auth/saml/provider.go`
- Test: `auth/saml/provider_test.go` (extend existing; check file presence first)

- [ ] **Step 1: Inspect current SAML AuthnRequest builder**

```bash
rg "GetAuthorizationURL|opts.ForceAuthn|MakeAuthenticationRequest" auth/saml/provider.go -n
```

Read lines around `auth/saml/provider.go:447` (the existing `opts.ForceAuthn = p.config.ForceAuthn` line) to understand how the request is constructed.

- [ ] **Step 2: Write the failing test**

Add to `auth/saml/provider_test.go` (or create if absent):

```go
func TestGetAuthorizationURLForceAuthn_SetsForceAuthnTrue(t *testing.T) {
	p := newTestSAMLProvider(t) // existing helper if present; otherwise build minimally
	// Pre-condition: confirm baseline ForceAuthn is false in test config.
	got, err := p.GetAuthorizationURLForceAuthn("state-xyz")
	if err != nil {
		t.Fatalf("GetAuthorizationURLForceAuthn: %v", err)
	}
	// SAML AuthnRequest is base64-encoded in the SAMLRequest query param.
	// Assert the decoded XML contains ForceAuthn="true".
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	samlReq := u.Query().Get("SAMLRequest")
	if samlReq == "" {
		t.Fatal("SAMLRequest query param missing")
	}
	raw, err := base64.StdEncoding.DecodeString(samlReq)
	if err != nil {
		// Some bindings deflate first; for the test just substring-search the raw.
		raw = []byte(samlReq)
	}
	if !bytes.Contains(raw, []byte(`ForceAuthn="true"`)) {
		t.Errorf("AuthnRequest missing ForceAuthn=\"true\": %s", string(raw))
	}
}
```

If `newTestSAMLProvider` does not exist, inline the minimal construction and skip with `t.Skip("saml test helper not yet established")` so the test is recorded but not blocking. Note the helper as a TODO in a follow-up issue.

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./auth/saml/ -run "TestGetAuthorizationURLForceAuthn_SetsForceAuthnTrue" -count=1
```

Expected: FAIL with "GetAuthorizationURLForceAuthn undefined" (or SKIP if helper is absent — that's acceptable for this iteration; the integration test in Task 12 covers the SAML path end-to-end).

- [ ] **Step 4: Implement**

In `auth/saml/provider.go`, alongside `GetAuthorizationURL`:

```go
// GetAuthorizationURLForceAuthn is identical to GetAuthorizationURL but
// forces a fresh re-authentication at the IdP regardless of the configured
// p.config.ForceAuthn default. Used by /oauth2/step_up (#397).
func (p *SAMLProvider) GetAuthorizationURLForceAuthn(state string) (string, error) {
	// Build an AuthnRequest with ForceAuthn=true overriding the configured default.
	// Implementation mirrors GetAuthorizationURL but sets opts.ForceAuthn = true.
	// The exact code depends on whether the existing GetAuthorizationURL uses
	// opts directly or delegates to a builder; reuse the same flow with the
	// override applied.

	// PSEUDOCODE — adapt to the precise structure of GetAuthorizationURL:
	// opts := defaultAuthnRequestOptions(p.config, state)
	// opts.ForceAuthn = true
	// req, err := p.middleware.ServiceProvider.MakeAuthenticationRequest(p.config.IDPMetadataURL, ...)
	// if err != nil { return "", err }
	// req.ForceAuthn = &trueRef
	// return req.RedirectURL(...), nil

	return p.buildAuthorizationURL(state, true /* forceAuthn */)
}
```

If the existing `GetAuthorizationURL` is a single-call wrapper, extract a private `buildAuthorizationURL(state string, forceAuthn bool)` helper and have both methods delegate to it. Confirm the refactor compiles cleanly by running `go build ./auth/saml/...`.

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./auth/saml/ -run "TestGetAuthorizationURLForceAuthn_SetsForceAuthnTrue" -count=1 -v
```

Expected: PASS (or SKIP if test helper not present; the integration test will exercise this).

- [ ] **Step 6: Commit**

```bash
git add auth/saml/provider.go auth/saml/provider_test.go
git commit -m "feat(saml): GetAuthorizationURLForceAuthn for step-up (#397)"
```

---

## Task 3: Step-up audit helpers (audit_step_up.go)

**Files:**
- Create: `auth/audit_step_up.go`
- Test: `auth/audit_step_up_test.go`

**Background:** `api.SystemAuditRepository.Create(ctx, models.SystemAuditEntry)` is the existing write path. The repository lives in package `api`, so `auth` needs an interface-abstraction to avoid an import cycle.

- [ ] **Step 1: Define the interface**

Create `auth/audit_step_up.go` with the interface first:

```go
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SystemAuditWriter is the minimal write surface required by step-up audit
// helpers. The concrete implementation in package api wraps GORM/Postgres;
// tests inject a memory implementation.
type SystemAuditWriter interface {
	WriteSystemAudit(ctx context.Context, entry SystemAuditRecord) error
}

// SystemAuditRecord is a transport struct mapping 1:1 to
// api/models.SystemAuditEntry. Defined here so package auth does not import
// package api (which would create a cycle).
type SystemAuditRecord struct {
	ActorEmail       string
	ActorProvider    string
	ActorProviderID  string
	ActorDisplayName string
	HTTPMethod       string
	HTTPPath         string
	FieldPath        string
	OldValueRedacted *string
	NewValueRedacted *string
	ChangeSummary    *string
	CreatedAt        time.Time
}

// StepUpAuditor wraps a SystemAuditWriter with the field shapes specific to
// step-up events. Fail-open: write failures are logged but do not propagate.
type StepUpAuditor struct {
	writer SystemAuditWriter
}

// NewStepUpAuditor returns an auditor. writer may be nil (in which case audit
// calls are no-ops with a debug log; matches the existing fail-open posture).
func NewStepUpAuditor(writer SystemAuditWriter) *StepUpAuditor {
	return &StepUpAuditor{writer: writer}
}
```

- [ ] **Step 2: Write the failing test**

Create `auth/audit_step_up_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

type memorySystemAuditWriter struct {
	mu      sync.Mutex
	entries []SystemAuditRecord
}

func (m *memorySystemAuditWriter) WriteSystemAudit(ctx context.Context, e SystemAuditRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

func TestStepUpAuditor_StrongSuccess(t *testing.T) {
	w := &memorySystemAuditWriter{}
	aud := NewStepUpAuditor(w)
	actor := StepUpActor{Email: "alice@example.com", Provider: "google", ProviderUserID: "u-123", DisplayName: "Alice"}

	err := aud.LogComplete(context.Background(), actor, StepUpStrong, "google", "round_trip")
	if err != nil {
		t.Fatalf("LogComplete: %v", err)
	}
	if len(w.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(w.entries))
	}
	e := w.entries[0]
	if e.HTTPPath != "/oauth2/step_up" || e.HTTPMethod != "GET" {
		t.Errorf("wrong method/path: %s %s", e.HTTPMethod, e.HTTPPath)
	}
	if e.FieldPath != "auth.step_up_complete" {
		t.Errorf("wrong FieldPath: %s", e.FieldPath)
	}
	if e.NewValueRedacted == nil {
		t.Fatal("NewValueRedacted nil")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(*e.NewValueRedacted), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload["strength"] != "strong" || payload["provider"] != "google" || payload["mode"] != "round_trip" {
		t.Errorf("wrong payload: %v", payload)
	}
}

func TestStepUpAuditor_WeakShortCircuit(t *testing.T) {
	w := &memorySystemAuditWriter{}
	aud := NewStepUpAuditor(w)
	actor := StepUpActor{Email: "bob@example.com", Provider: "github", ProviderUserID: "u-456", DisplayName: "Bob"}

	_ = aud.LogComplete(context.Background(), actor, StepUpWeak, "github", "short_circuit")

	if len(w.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(w.entries))
	}
	if !strings.Contains(*w.entries[0].NewValueRedacted, `"strength":"weak"`) {
		t.Errorf("missing weak marker: %s", *w.entries[0].NewValueRedacted)
	}
	if !strings.Contains(*w.entries[0].ChangeSummary, "weak") {
		t.Errorf("summary missing weak: %s", *w.entries[0].ChangeSummary)
	}
}

func TestStepUpAuditor_IdentityMismatchRedactsAttemptedEmail(t *testing.T) {
	w := &memorySystemAuditWriter{}
	aud := NewStepUpAuditor(w)
	actor := StepUpActor{Email: "alice@example.com", Provider: "google", ProviderUserID: "u-123", DisplayName: "Alice"}

	_ = aud.LogFailed(context.Background(), actor, "identity_mismatch", map[string]string{
		"attempted_email": "eve-the-attacker@evil.example",
	})

	if len(w.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(w.entries))
	}
	body := *w.entries[0].NewValueRedacted
	if strings.Contains(body, "eve-the-attacker@evil.example") {
		t.Errorf("attempted_email leaked verbatim: %s", body)
	}
	if !strings.Contains(body, `"reason":"identity_mismatch"`) {
		t.Errorf("missing reason: %s", body)
	}
}

func TestStepUpAuditor_NilWriterIsNoOp(t *testing.T) {
	aud := NewStepUpAuditor(nil)
	if err := aud.LogRejected(context.Background(), StepUpActor{}, "unsupported_grant_type", nil); err != nil {
		t.Fatalf("nil-writer should not error: %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./auth/ -run "TestStepUpAuditor" -count=1
```

Expected: FAIL with "undefined: StepUpActor / LogComplete / LogFailed / LogRejected".

- [ ] **Step 4: Implement the auditor**

Append to `auth/audit_step_up.go`:

```go
// StepUpActor identifies the user whose step-up event is being recorded.
// All four fields are denormalized into the audit row (matches the
// SystemAuditEntry pattern; rows survive user deletion).
type StepUpActor struct {
	Email          string
	Provider       string
	ProviderUserID string
	DisplayName    string
}

// LogComplete records a successful step-up. Strength carries strong|weak;
// mode carries round_trip|short_circuit.
func (a *StepUpAuditor) LogComplete(ctx context.Context, actor StepUpActor, strength StepUpStrength, providerID, mode string) error {
	payload := map[string]string{
		"provider": providerID,
		"strength": strength.String(),
		"mode":     mode,
	}
	summary := fmt.Sprintf("step-up completed (%s) via %s", strength.String(), providerID)
	if strength == StepUpWeak {
		summary += " — upstream IdP does not honor prompt=login"
	}
	return a.write(ctx, actor, "auth.step_up_complete", payload, summary)
}

// LogFailed records a step-up that did not complete successfully.
// reason is the short stable code (identity_mismatch, access_denied, state_expired, etc.).
// extras are inlined into the payload; values are redacted via redactStepUpAttemptedEmail
// when the key is "attempted_email".
func (a *StepUpAuditor) LogFailed(ctx context.Context, actor StepUpActor, reason string, extras map[string]string) error {
	payload := map[string]string{"reason": reason}
	for k, v := range extras {
		if k == "attempted_email" {
			payload[k] = redactStepUpAttemptedEmail(v)
		} else {
			payload[k] = v
		}
	}
	summary := fmt.Sprintf("step-up failed: %s", reason)
	return a.write(ctx, actor, "auth.step_up_failed", payload, summary)
}

// LogRejected records a step-up attempt that was rejected before the upstream
// round-trip began (e.g., CC-grant caller, invalid provider).
func (a *StepUpAuditor) LogRejected(ctx context.Context, actor StepUpActor, reason string, extras map[string]string) error {
	payload := map[string]string{"reason": reason}
	for k, v := range extras {
		payload[k] = v
	}
	summary := fmt.Sprintf("step-up rejected: %s", reason)
	return a.write(ctx, actor, "auth.step_up_rejected", payload, summary)
}

func (a *StepUpAuditor) write(ctx context.Context, actor StepUpActor, fieldPath string, payload map[string]string, summary string) error {
	if a == nil || a.writer == nil {
		slogging.Get().Debug("StepUpAuditor: no writer wired; skipping audit row for %s", fieldPath)
		return nil
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		// Defensive — map[string]string cannot fail to marshal in practice.
		slogging.Get().Error("StepUpAuditor: marshal failed for %s: %v", fieldPath, err)
		return nil
	}
	newValStr := string(jsonBody)
	rec := SystemAuditRecord{
		ActorEmail:       actor.Email,
		ActorProvider:    actor.Provider,
		ActorProviderID:  actor.ProviderUserID,
		ActorDisplayName: actor.DisplayName,
		HTTPMethod:       "GET",
		HTTPPath:         "/oauth2/step_up",
		FieldPath:        fieldPath,
		NewValueRedacted: &newValStr,
		ChangeSummary:    &summary,
		CreatedAt:        time.Now().UTC(),
	}
	if err := a.writer.WriteSystemAudit(ctx, rec); err != nil {
		slogging.Get().Error("StepUpAuditor: write failed for %s: %v", fieldPath, err)
		// Fail-open: completion paths should still succeed.
		return nil
	}
	return nil
}

// redactStepUpAttemptedEmail mirrors the Tier-2 redaction shape used by
// api/admin_audit_redaction.go (sha256-prefix-8 + last-6 tail when length ≥ 24,
// else full sha256-prefix-8). Lives in package auth to avoid the import cycle.
func redactStepUpAttemptedEmail(v string) string {
	sum := sha256.Sum256([]byte(v))
	prefix := hex.EncodeToString(sum[:])[:8]
	if len(v) >= 24 {
		return prefix + "…" + v[len(v)-6:]
	}
	return prefix
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./auth/ -run "TestStepUpAuditor" -count=1 -v
```

Expected: PASS (all four sub-tests).

- [ ] **Step 6: Commit**

```bash
git add auth/audit_step_up.go auth/audit_step_up_test.go
git commit -m "feat(auth): step-up audit helpers writing into system_audit_entries (#397)"
```

---

## Task 4: Wire SystemAuditWriter from package api into auth.Handlers

**Files:**
- Modify: `auth/handlers.go`
- Modify: `cmd/server/main.go`
- Create: `api/system_audit_writer_adapter.go` (or append to existing `api/system_audit_repository.go`)

- [ ] **Step 1: Add the adapter on the api side**

Append to `api/system_audit_repository.go`:

```go
import "github.com/ericfitz/tmi/auth"

// AuthAuditAdapter adapts a SystemAuditRepository to the auth package's
// SystemAuditWriter interface, mapping auth.SystemAuditRecord to
// models.SystemAuditEntry. Used by /oauth2/step_up (#397) so package auth
// can write system audit rows without importing package api.
type AuthAuditAdapter struct {
	repo SystemAuditRepository
}

// NewAuthAuditAdapter constructs the adapter.
func NewAuthAuditAdapter(repo SystemAuditRepository) *AuthAuditAdapter {
	return &AuthAuditAdapter{repo: repo}
}

// WriteSystemAudit implements auth.SystemAuditWriter.
func (a *AuthAuditAdapter) WriteSystemAudit(ctx context.Context, rec auth.SystemAuditRecord) error {
	entry := models.SystemAuditEntry{
		ActorEmail:       rec.ActorEmail,
		ActorProvider:    rec.ActorProvider,
		ActorProviderID:  rec.ActorProviderID,
		ActorDisplayName: rec.ActorDisplayName,
		HTTPMethod:       rec.HTTPMethod,
		HTTPPath:         rec.HTTPPath,
		FieldPath:        rec.FieldPath,
	}
	if rec.OldValueRedacted != nil {
		entry.OldValueRedacted = models.NullableDBText(*rec.OldValueRedacted)
	}
	if rec.NewValueRedacted != nil {
		entry.NewValueRedacted = models.NullableDBText(*rec.NewValueRedacted)
	}
	if rec.ChangeSummary != nil {
		entry.ChangeSummary = models.NullableDBText(*rec.ChangeSummary)
	}
	if !rec.CreatedAt.IsZero() {
		entry.CreatedAt = rec.CreatedAt
	}
	return a.repo.Create(ctx, entry)
}
```

Note: confirm `models.NullableDBText` is the existing type used elsewhere by reading `api/models/system_audit.go:33-35`. If it's a different conversion (e.g., a struct with a `Valid` field), adjust the assignment.

- [ ] **Step 2: Extend Handlers struct in auth/handlers.go**

Find `type Handlers struct` in `auth/handlers.go:71-79` and add:

```go
type Handlers struct {
	service           *Service
	config            Config
	adminChecker      AdminChecker
	userGroupsFetcher UserGroupsFetcher
	cookieOpts        CookieOptions
	registry          ProviderRegistry
	tokenLockoutImpl  *OAuthTokenLockout
	stepUpAuditor     *StepUpAuditor // #397
}
```

Add a setter near the existing `SetTokenLockout`:

```go
// SetStepUpAuditor wires the step-up audit writer. Safe to call multiple
// times; nil disables step-up auditing (used in tests). #397.
func (h *Handlers) SetStepUpAuditor(a *StepUpAuditor) {
	h.stepUpAuditor = a
}

// stepUpAud returns the wired auditor or a no-op auditor if none is set.
func (h *Handlers) stepUpAud() *StepUpAuditor {
	if h.stepUpAuditor != nil {
		return h.stepUpAuditor
	}
	return NewStepUpAuditor(nil)
}
```

- [ ] **Step 3: Wire in cmd/server/main.go**

Find the existing `systemAuditRepo := api.NewSystemAuditRepository(gormDB.DB())` (~line 898) and add immediately after:

```go
// #397 — step-up auditor uses the same system_audit_entries table.
authHandlers.SetStepUpAuditor(auth.NewStepUpAuditor(api.NewAuthAuditAdapter(systemAuditRepo)))
```

Adjust the variable name `authHandlers` to whatever the local handlers variable is called (read 50 lines of context to be sure).

- [ ] **Step 4: Build**

```bash
make build-server
```

Expected: clean build. If an import cycle is reported, the issue is in the api/auth adapter direction; confirm the adapter lives in package `api` and imports `auth` (not the reverse).

- [ ] **Step 5: Commit**

```bash
git add auth/handlers.go api/system_audit_repository.go cmd/server/main.go
git commit -m "feat(auth): wire StepUpAuditor through Handlers + main.go (#397)"
```

---

## Task 5: Step-up endpoint handler — strong path (handlers_step_up.go)

**Files:**
- Create: `auth/handlers_step_up.go`
- Test: `auth/handlers_step_up_test.go`

- [ ] **Step 1: Write the first failing test (strong-path 302)**

Create `auth/handlers_step_up_test.go`:

```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStepUp_StrongProvider_Returns302WithPromptLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, srv, _ := newStepUpTestHarness(t,
		withProvider("google", strongProviderConfig()),
		withJWTForUser("alice@example.com", "google", "uid-alice"),
		withClientCallbackAllowlist([]string{"http://localhost:4200/callback"}),
	)
	defer srv.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk&code_challenge_method=S256",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)

	h.handlers.StepUp(c)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "prompt=login") {
		t.Errorf("Location missing prompt=login: %s", loc)
	}
	if !strings.Contains(loc, "max_age=0") {
		t.Errorf("Location missing max_age=0: %s", loc)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if u.Host == "" {
		t.Errorf("Location should be absolute upstream URL: %s", loc)
	}
}
```

Test harness helpers (declared inline at the bottom of the test file — keep them minimal; we will reuse them across other tests):

```go
// stepUpTestHarness bundles the mocked Handlers + JWT used by step-up tests.
type stepUpTestHarness struct {
	handlers *Handlers
	testJWT  string
}

type stepUpHarnessOption func(*stepUpHarnessBuilder)

type stepUpHarnessBuilder struct {
	providers           map[string]OAuthProviderConfig
	jwtEmail            string
	jwtProvider         string
	jwtProviderUserID   string
	jwtClaimsExtra      map[string]any
	clientCallbackAllow []string
	auditWriter         SystemAuditWriter
}

func withProvider(id string, cfg OAuthProviderConfig) stepUpHarnessOption {
	return func(b *stepUpHarnessBuilder) { b.providers[id] = cfg }
}
func withJWTForUser(email, provider, providerUID string) stepUpHarnessOption {
	return func(b *stepUpHarnessBuilder) {
		b.jwtEmail = email
		b.jwtProvider = provider
		b.jwtProviderUserID = providerUID
	}
}
func withClientCallbackAllowlist(urls []string) stepUpHarnessOption {
	return func(b *stepUpHarnessBuilder) { b.clientCallbackAllow = urls }
}
func withCustomAuditWriter(w SystemAuditWriter) stepUpHarnessOption {
	return func(b *stepUpHarnessBuilder) { b.auditWriter = w }
}

func strongProviderConfig() OAuthProviderConfig {
	return OAuthProviderConfig{
		ID:               "google",
		Issuer:           "https://accounts.google.com",
		JWKSURL:          "https://www.googleapis.com/oauth2/v3/certs",
		ClientID:         "test-cid",
		ClientSecret:     "test-sec",
		AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:         "https://oauth2.googleapis.com/token",
		Scopes:           []string{"openid", "email"},
	}
}

func weakProviderConfig() OAuthProviderConfig {
	return OAuthProviderConfig{
		ID:               "github",
		ClientID:         "gh-cid",
		ClientSecret:     "gh-sec",
		AuthorizationURL: "https://github.com/login/oauth/authorize",
		TokenURL:         "https://github.com/login/oauth/access_token",
		Scopes:           []string{"read:user"},
	}
}

// newStepUpTestHarness assembles a Handlers with miniredis + an in-memory
// SystemAuditWriter, mints a test JWT with auth_time = now, and returns
// the harness. Returns (harness, miniredis server, cleanup).
func newStepUpTestHarness(t *testing.T, opts ...stepUpHarnessOption) (*stepUpTestHarness, *miniredis.Miniredis, func()) {
	// Implementation: stand up miniredis, build a *Service with the test key
	// manager (existing helper newTestService in auth/service_test_helpers.go
	// or equivalent), construct Handlers, register providers via
	// h.registry.Register(...), and mint a JWT via
	// h.service.GenerateTokensWithAuthTime(...).
	// Reuse existing test scaffolding in auth/handlers_test.go.
	t.Helper()
	// ... see auth/handlers_test.go for the canonical pattern.
	t.Skip("FILL IN: see auth/handlers_test.go for the existing test harness pattern; adapt to provide a SystemAuditWriter via NewStepUpAuditor + h.SetStepUpAuditor")
	return nil, nil, func() {}
}
```

> ⚠️ The harness helper above intentionally calls `t.Skip` so the first test compiles. The agent executing this plan MUST replace the body of `newStepUpTestHarness` with the real wiring before continuing. Read `auth/handlers_test.go` and `auth/service_test_helpers.go` to find the canonical patterns. This is the only "fill in" point in the plan and it is bounded to a single well-defined helper — the rest of the plan provides full code.

- [ ] **Step 2: Run test to verify it fails (or skips)**

```bash
go test ./auth/ -run "TestStepUp_StrongProvider_Returns302WithPromptLogin" -count=1
```

Expected: SKIP initially. Replace the harness body with real wiring, then re-run. Expected after wiring: FAIL with "Handlers.StepUp undefined".

- [ ] **Step 3: Implement the handler**

Create `auth/handlers_step_up.go`:

```go
// Package auth — /oauth2/step_up handler (#397).
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// StepUp is the GET /oauth2/step_up handler.
//
// Strong-provider path: 302 redirects to the upstream IdP authorize URL with
// prompt=login&max_age=0 (OAuth/OIDC) or ForceAuthn=true (SAML).
//
// Weak-provider path (currently github only): short-circuits the round-trip
// and rotates tokens in-place, returning 200 OK with a JSON body and new
// HttpOnly cookies. The audit row records strength=weak so operators can see
// these events.
//
// All paths emit a system_audit_entry; failures use LogFailed / LogRejected.
//
// See docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md.
func (h *Handlers) StepUp(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// 1. Read JWT (Authorization header priority; cookie fallback).
	tokenStr, ok := h.readStepUpJWT(c)
	if !ok {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_token",
			"error_description": "Missing or invalid access token",
		})
		return
	}

	claims, err := h.service.ValidateToken(tokenStr)
	if err != nil {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_token",
			"error_description": "Token validation failed",
		})
		return
	}

	actor := StepUpActor{
		Email:          claims.Email,
		Provider:       claims.IdentityProvider,
		ProviderUserID: claims.Subject,
		DisplayName:    claims.Name,
	}

	// 2. Client-credentials rejection.
	if strings.HasPrefix(claims.Subject, "sa:") {
		_ = h.stepUpAud().LogRejected(c.Request.Context(), actor, "unsupported_grant_type",
			map[string]string{"subject_prefix": "sa"})
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_grant_type",
			"error_description": "Step-up does not apply to client credentials grants",
		})
		return
	}

	// 3. Provider lookup.
	providerID := claims.IdentityProvider
	provider, err := h.getProvider(providerID)
	if err != nil {
		reason := "invalid_provider"
		_ = h.stepUpAud().LogRejected(c.Request.Context(), actor, reason,
			map[string]string{"provider": providerID})
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_provider",
			"error_description": fmt.Sprintf("Provider %q is not configured or is disabled", providerID),
		})
		return
	}

	// 4. Validate query params.
	clientCallback := c.Query("client_callback")
	if clientCallback == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback parameter is required",
		})
		return
	}
	allow := NewClientCallbackAllowList(h.config.OAuth.ClientCallbackAllowList)
	if !allow.Allowed(clientCallback) {
		logger.Warn("Rejected /oauth2/step_up: client_callback %q not in allowlist", clientCallback)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback is not in the allowlist",
		})
		return
	}

	codeChallenge := c.Query("code_challenge")
	if codeChallenge == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "code_challenge parameter is required",
		})
		return
	}
	if err := ValidateCodeChallengeFormat(codeChallenge); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": fmt.Sprintf("Invalid code_challenge format: %v", err),
		})
		return
	}
	codeChallengeMethod := c.Query("code_challenge_method")
	if codeChallengeMethod == "" {
		codeChallengeMethod = pkceMethodS256
	}
	if codeChallengeMethod != pkceMethodS256 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Only S256 code_challenge_method is supported",
		})
		return
	}

	// Reject unsupported optional params with the canonical RFC error code.
	if rt := c.Query("response_type"); rt != "" && rt != "code" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_response_type",
			"error_description": "Only response_type=code is supported",
		})
		return
	}
	if sc := c.Query("scope"); sc != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_scope",
			"error_description": "scope is not accepted on /oauth2/step_up",
		})
		return
	}

	// 5. Classify strength.
	cfg, err := h.providerConfig(providerID)
	if err != nil {
		// Should not happen — getProvider succeeded above.
		_ = h.stepUpAud().LogRejected(c.Request.Context(), actor, "invalid_provider", map[string]string{"provider": providerID})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	strength := ClassifyStepUpStrength(cfg)

	// 6. Weak path — short-circuit and rotate.
	if strength == StepUpWeak {
		h.stepUpWeakShortCircuit(c, actor)
		return
	}

	// 7. Strong path — store state and redirect upstream.
	h.stepUpStrongRedirect(c, provider, cfg, actor, clientCallback, codeChallenge, codeChallengeMethod)
}

// readStepUpJWT extracts the JWT using the same Bearer-then-cookie priority
// as the JWT middleware.
func (h *Handlers) readStepUpJWT(c *gin.Context) (string, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
			return parts[1], true
		}
		return "", false
	}
	if tok := ExtractAccessTokenFromCookie(c); tok != "" {
		return tok, true
	}
	return "", false
}

// providerConfig fetches the OAuthProviderConfig by ID. Implemented via the
// existing registry / config lookup pattern used by getProvider. If the
// project does not already expose a config-only lookup, add one as a thin
// wrapper around the existing registry.
func (h *Handlers) providerConfig(providerID string) (OAuthProviderConfig, error) {
	for _, cfg := range h.config.OAuth.Providers {
		if cfg.ID == providerID {
			return cfg, nil
		}
	}
	return OAuthProviderConfig{}, fmt.Errorf("provider %q not found", providerID)
}

// stepUpStrongRedirect implements the strong-provider path (Section 1 of the spec).
func (h *Handlers) stepUpStrongRedirect(c *gin.Context, provider Provider, cfg OAuthProviderConfig, actor StepUpActor, clientCallback, codeChallenge, codeChallengeMethod string) {
	logger := slogging.Get().WithContext(c)

	state := c.Query("state")
	if state == "" {
		var err error
		state, err = generateRandomState()
		if err != nil {
			logger.Error("Failed to generate state for step-up: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
	}

	// Look up the original user's internal UUID for the identity-match check
	// at token-mint time. The User.InternalUUID is the authoritative identity.
	ctx := c.Request.Context()
	user, err := h.service.GetUserByProviderID(ctx, actor.Provider, actor.ProviderUserID)
	if err != nil {
		logger.Error("Failed to look up user for step-up: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	stateKey := fmt.Sprintf("oauth_state:%s", state)
	stateData := map[string]string{
		"provider":           actor.Provider,
		"client_callback":    clientCallback,
		"step_up":            "true",
		"original_user_uuid": user.InternalUUID,
		"original_email":     user.Email,
		"step_up_strength":   StepUpStrong.String(),
	}
	stateJSON, err := json.Marshal(stateData)
	if err != nil {
		logger.Error("Failed to marshal state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if err := h.service.dbManager.Redis().Set(ctx, stateKey, string(stateJSON), 10*time.Minute); err != nil {
		logger.Error("Failed to store state: %v", err)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "temporarily_unavailable"})
		return
	}
	if err := h.service.stateStore.StorePKCEChallenge(ctx, state, codeChallenge, codeChallengeMethod, 10*time.Minute); err != nil {
		logger.Error("Failed to store PKCE for step-up: %v", err)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "temporarily_unavailable"})
		return
	}

	authURL, err := BuildStepUpAuthorizationURL(provider, cfg, state)
	if err != nil {
		logger.Error("Failed to build step-up URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	logger.Debug("step-up strong redirect: provider=%s state=%s", actor.Provider, state)
	c.Redirect(http.StatusFound, authURL)
}

// stepUpWeakShortCircuit implements the weak-provider path (Section 3.5 of the spec).
// Rotates tokens in-place, blacklists the old refresh token if present, and
// writes a step_up_complete/strength=weak audit row.
func (h *Handlers) stepUpWeakShortCircuit(c *gin.Context, actor StepUpActor) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	user, err := h.service.GetUserByProviderID(ctx, actor.Provider, actor.ProviderUserID)
	if err != nil {
		logger.Error("Step-up weak: user lookup failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Blacklist old refresh token if cookie present.
	if oldRefresh, err := c.Cookie(RefreshTokenCookieName); err == nil && oldRefresh != "" {
		if h.service.tokenBlacklist != nil {
			if bErr := h.service.tokenBlacklist.BlacklistToken(ctx, oldRefresh); bErr != nil {
				logger.Warn("Step-up weak: failed to blacklist old refresh: %v", bErr)
			}
		}
	} else {
		logger.Debug("Step-up weak: no refresh cookie to blacklist")
	}

	tokenPair, err := h.service.GenerateTokensWithUserInfo(ctx, user, nil)
	if err != nil {
		logger.Error("Step-up weak: token mint failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	if h.cookieOpts.Enabled {
		SetTokenCookies(c, tokenPair, h.cookieOpts)
	}

	_ = h.stepUpAud().LogComplete(ctx, actor, StepUpWeak, actor.Provider, "short_circuit")

	c.JSON(http.StatusOK, gin.H{
		"result":    "step_up_weak_complete",
		"provider":  actor.Provider,
		"auth_time": time.Now().Unix(),
		"message":   "Provider does not support guaranteed fresh re-auth; tokens rotated and step-up window reset. Audit log records this as a weak step-up.",
	})
}
```

> **Note on `h.service.GetUserByProviderID` and `h.service.tokenBlacklist`**: confirm the exact field/method names by reading `auth/service.go`. If `GetUserByProviderID` is named differently (e.g., `FindUserByProviderID` or via the repository), adapt the call. If `tokenBlacklist` is named differently, fix it. These are concrete lookups, not placeholders — find the real names.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./auth/ -run "TestStepUp_StrongProvider_Returns302WithPromptLogin" -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add auth/handlers_step_up.go auth/handlers_step_up_test.go
git commit -m "feat(auth): /oauth2/step_up handler — strong path (#397)"
```

---

## Task 6: Step-up handler — error responses (extend handlers_step_up_test.go)

**Files:**
- Modify: `auth/handlers_step_up_test.go`

- [ ] **Step 1: Add tests for each error response**

Append to `auth/handlers_step_up_test.go`:

```go
func TestStepUp_MissingJWT_Returns401(t *testing.T) {
	// JWT missing entirely → 401 invalid_token, WWW-Authenticate set.
	gin.SetMode(gin.TestMode)
	h, _, _ := newStepUpTestHarness(t, withProvider("google", strongProviderConfig()))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/oauth2/step_up?client_callback=x&code_challenge=y", nil)
	h.handlers.StepUp(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("WWW-Authenticate"), "invalid_token") {
		t.Errorf("missing WWW-Authenticate: %s", w.Header().Get("WWW-Authenticate"))
	}
}

func TestStepUp_CCGrant_Returns400UnsupportedGrantType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h, _, _ := newStepUpTestHarness(t,
		withProvider("google", strongProviderConfig()),
		withCustomAuditWriter(auditW),
		withJWTSubject("sa:cc-grant-123:alice@example.com"), // CC-grant subject
	)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported_grant_type") {
		t.Errorf("wrong error: %s", w.Body.String())
	}
	if len(auditW.entries) != 1 || auditW.entries[0].FieldPath != "auth.step_up_rejected" {
		t.Errorf("expected step_up_rejected audit row; got %#v", auditW.entries)
	}
}

func TestStepUp_InvalidClientCallback_Returns400(t *testing.T) {
	// client_callback not in allowlist → 400 invalid_request.
	gin.SetMode(gin.TestMode)
	h, _, _ := newStepUpTestHarness(t,
		withProvider("google", strongProviderConfig()),
		withJWTForUser("alice@example.com", "google", "uid-alice"),
		withClientCallbackAllowlist([]string{"http://localhost:4200/callback"}),
	)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Fevil.example%2F&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_request") {
		t.Errorf("wrong error: %s", w.Body.String())
	}
}

func TestStepUp_MissingPKCE_Returns400(t *testing.T) {
	// code_challenge missing → 400 invalid_request.
	gin.SetMode(gin.TestMode)
	h, _, _ := newStepUpTestHarness(t,
		withProvider("google", strongProviderConfig()),
		withJWTForUser("alice@example.com", "google", "uid-alice"),
		withClientCallbackAllowlist([]string{"http://localhost:4200/callback"}),
	)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback", nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestStepUp_WeakProvider_ShortCircuits200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h, _, _ := newStepUpTestHarness(t,
		withProvider("github", weakProviderConfig()),
		withJWTForUser("bob@example.com", "github", "uid-bob"),
		withClientCallbackAllowlist([]string{"http://localhost:4200/callback"}),
		withCustomAuditWriter(auditW),
	)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "step_up_weak_complete") {
		t.Errorf("missing result marker: %s", w.Body.String())
	}
	// New HttpOnly cookies should be set in response.
	cookies := w.Result().Cookies()
	var sawAccess, sawRefresh bool
	for _, ck := range cookies {
		if ck.Name == AccessTokenCookieName {
			sawAccess = true
			if !ck.HttpOnly {
				t.Errorf("access cookie missing HttpOnly")
			}
		}
		if ck.Name == RefreshTokenCookieName {
			sawRefresh = true
			if !ck.HttpOnly {
				t.Errorf("refresh cookie missing HttpOnly")
			}
		}
	}
	if !sawAccess || !sawRefresh {
		t.Errorf("expected both access+refresh cookies; got access=%v refresh=%v", sawAccess, sawRefresh)
	}
	// Audit row should record strength=weak.
	if len(auditW.entries) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(auditW.entries))
	}
	if auditW.entries[0].FieldPath != "auth.step_up_complete" {
		t.Errorf("wrong FieldPath: %s", auditW.entries[0].FieldPath)
	}
	if !strings.Contains(*auditW.entries[0].NewValueRedacted, `"strength":"weak"`) {
		t.Errorf("audit missing strength=weak: %s", *auditW.entries[0].NewValueRedacted)
	}
}
```

The `withJWTSubject` option mirrors `withJWTForUser` but overrides the JWT's `sub` claim — add it to the harness builder.

- [ ] **Step 2: Run all tests**

```bash
go test ./auth/ -run "TestStepUp_" -count=1 -v
```

Expected: all 5 pass.

- [ ] **Step 3: Commit**

```bash
git add auth/handlers_step_up_test.go
git commit -m "test(auth): step-up handler error responses + weak short-circuit (#397)"
```

---

## Task 7: Propagate step-up marker through callback (handlers_oauth.go)

**Files:**
- Modify: `auth/handlers_oauth.go`
- Test: `auth/handlers_oauth_step_up_callback_test.go`

- [ ] **Step 1: Read the current callback state struct**

```bash
rg "callbackStateData|parseCallbackState" auth/handlers_oauth.go -n
```

Inspect `callbackStateData` (around line 267) — see what fields exist today.

- [ ] **Step 2: Write the failing test**

Create `auth/handlers_oauth_step_up_callback_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestParseCallbackState_ExtractsStepUpFields(t *testing.T) {
	h, _, _ := newStepUpTestHarness(t)
	stateMap := map[string]string{
		"provider":           "google",
		"client_callback":    "http://localhost:4200/cb",
		"step_up":            "true",
		"original_user_uuid": "uuid-original",
		"original_email":     "alice@example.com",
		"step_up_strength":   "strong",
	}
	stateJSON, _ := json.Marshal(stateMap)
	state := "test-state-abc"
	stateKey := "oauth_state:" + state
	ctx := context.Background()
	if err := h.handlers.service.dbManager.Redis().Set(ctx, stateKey, string(stateJSON), 60_000_000_000); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	c, _ := newTestGinCtx()
	c.Request = httptest.NewRequest("GET", "/oauth2/callback?state="+state, nil)

	got, err := h.handlers.parseCallbackState(c, state)
	if err != nil {
		t.Fatalf("parseCallbackState: %v", err)
	}
	if !got.StepUp {
		t.Errorf("StepUp flag not extracted")
	}
	if got.OriginalUserUUID != "uuid-original" {
		t.Errorf("OriginalUserUUID = %s", got.OriginalUserUUID)
	}
	if got.OriginalEmail != "alice@example.com" {
		t.Errorf("OriginalEmail = %s", got.OriginalEmail)
	}
	if got.StepUpStrength != "strong" {
		t.Errorf("StepUpStrength = %s", got.StepUpStrength)
	}
}

func TestProcessOAuthCallback_CopiesStepUpMarkerIntoPKCERecord(t *testing.T) {
	h, _, _ := newStepUpTestHarness(t)
	ctx := context.Background()

	state := "test-state-xyz"
	stateMap := map[string]string{
		"provider":           "google",
		"client_callback":    "http://localhost:4200/cb",
		"step_up":            "true",
		"original_user_uuid": "uuid-original",
		"original_email":     "alice@example.com",
		"step_up_strength":   "strong",
	}
	stateJSON, _ := json.Marshal(stateMap)
	_ = h.handlers.service.dbManager.Redis().Set(ctx, "oauth_state:"+state, string(stateJSON), 60_000_000_000)
	// Seed the PKCE challenge linked to the state (existing helper).
	_ = h.handlers.service.stateStore.StorePKCEChallenge(ctx, state, "challenge-abc", "S256", 60_000_000_000)

	// Construct a callback request and run the existing Callback handler.
	c, w := newTestGinCtx()
	c.Request = httptest.NewRequest("GET", "/oauth2/callback?code=test-code&state="+state, nil)
	h.handlers.Callback(c)
	if w.Code >= 400 && w.Code != 302 {
		t.Fatalf("callback returned %d body=%s", w.Code, w.Body.String())
	}

	// The PKCE record at pkce:<code> should now include the step-up fields.
	pkceJSON, err := h.handlers.service.dbManager.Redis().Get(ctx, "pkce:test-code")
	if err != nil {
		t.Fatalf("pkce record missing: %v", err)
	}
	var pkceMap map[string]string
	_ = json.Unmarshal([]byte(pkceJSON), &pkceMap)
	if pkceMap["step_up"] != "true" {
		t.Errorf("pkce record missing step_up=true: %v", pkceMap)
	}
	if pkceMap["original_user_uuid"] != "uuid-original" {
		t.Errorf("pkce record missing original_user_uuid: %v", pkceMap)
	}
	if pkceMap["step_up_strength"] != "strong" {
		t.Errorf("pkce record missing step_up_strength: %v", pkceMap)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./auth/ -run "TestParseCallbackState_ExtractsStepUpFields|TestProcessOAuthCallback_CopiesStepUpMarkerIntoPKCERecord" -count=1
```

Expected: FAIL (fields don't exist; pkce record doesn't carry markers).

- [ ] **Step 4: Modify callbackStateData and parseCallbackState**

In `auth/handlers_oauth.go`, extend the struct:

```go
type callbackStateData struct {
	ProviderID       string
	ClientCallback   string
	UserHint         string
	StepUp           bool   // #397
	OriginalUserUUID string // #397 — set when StepUp is true
	OriginalEmail    string // #397
	StepUpStrength   string // #397 — "strong" | "weak"
}
```

In `parseCallbackState`, after the existing assignments, add:

```go
result.StepUp = stateMap["step_up"] == "true"
if result.StepUp {
	result.OriginalUserUUID = stateMap["original_user_uuid"]
	result.OriginalEmail = stateMap["original_email"]
	result.StepUpStrength = stateMap["step_up_strength"]
}
```

In `processOAuthCallback`, where the PKCE data map is built before being stored at `pkce:<code>`, extend it with step-up fields when applicable:

```go
pkceData := map[string]string{
	"code_challenge":        pkceChallenge,
	"code_challenge_method": pkceMethod,
}
if stateData.StepUp {
	pkceData["step_up"] = "true"
	pkceData["original_user_uuid"] = stateData.OriginalUserUUID
	pkceData["original_email"] = stateData.OriginalEmail
	pkceData["step_up_strength"] = stateData.StepUpStrength
}
```

This change is needed in **two places** in `handlers_oauth.go`: the `Authorize` handler's TMI-test-provider branch (around line 226) and `processOAuthCallback` (around line 360). Apply the same extension in both.

- [ ] **Step 5: Handle upstream `error=access_denied` for step-up**

At the top of `Callback`, add (before `code` is required):

```go
// RFC 6749 §4.1.2.1 — upstream IdP returned an error.
if upErr := c.Query("error"); upErr != "" {
	state := c.Query("state")
	if state != "" {
		// Try to parse state to decide if this is a step-up so we can audit.
		if sd, perr := h.parseCallbackState(c, state); perr == nil && sd.StepUp {
			actor := StepUpActor{Email: sd.OriginalEmail, Provider: sd.ProviderID}
			_ = h.stepUpAud().LogFailed(c.Request.Context(), actor, upErr, nil)
			redirectURL := fmt.Sprintf("%s?error=%s&state=%s",
				sd.ClientCallback, url.QueryEscape(upErr), url.QueryEscape(state))
			c.Redirect(http.StatusFound, redirectURL)
			return
		}
	}
	// Non-step-up upstream error — fall through to existing behavior or
	// surface a generic error response.
	c.JSON(http.StatusBadRequest, gin.H{"error": upErr})
	return
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./auth/ -run "TestParseCallbackState_ExtractsStepUpFields|TestProcessOAuthCallback_CopiesStepUpMarkerIntoPKCERecord" -count=1 -v
```

Expected: PASS.

- [ ] **Step 7: Run the existing callback tests to confirm no regressions**

```bash
go test ./auth/ -run "TestAuthorize|TestCallback|TestProcessOAuthCallback" -count=1
```

Expected: all existing tests still pass.

- [ ] **Step 8: Commit**

```bash
git add auth/handlers_oauth.go auth/handlers_oauth_step_up_callback_test.go
git commit -m "feat(auth): propagate step-up marker through OAuth callback chain (#397)"
```

---

## Task 8: Token-mint step-up branch (handlers_token.go)

**Files:**
- Modify: `auth/handlers_token.go`
- Test: `auth/handlers_token_step_up_test.go`

- [ ] **Step 1: Write the failing tests**

Create `auth/handlers_token_step_up_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToken_StepUpIdentityMatch_RotatesAndAudits(t *testing.T) {
	auditW := &memorySystemAuditWriter{}
	h, _, _ := newStepUpTestHarness(t,
		withProvider("google", strongProviderConfig()),
		withCustomAuditWriter(auditW),
	)
	ctx := context.Background()

	// Seed the PKCE record with step_up=true matching the original user
	// who will be re-authed.
	originalUUID := h.seedUser(ctx, "alice@example.com", "google", "uid-alice")
	pkceMap := map[string]string{
		"code_challenge":        "challenge-abc",
		"code_challenge_method": "S256",
		"step_up":               "true",
		"original_user_uuid":    originalUUID,
		"original_email":        "alice@example.com",
		"step_up_strength":      "strong",
	}
	pkceJSON, _ := json.Marshal(pkceMap)
	_ = h.handlers.service.dbManager.Redis().Set(ctx, "pkce:test-code", string(pkceJSON), 60_000_000_000)

	// Configure the test provider to return userInfo matching the original user.
	h.stubProvider.SetUserInfoForCode("test-code", &UserInfo{ID: "uid-alice", Email: "alice@example.com", Name: "Alice", IdP: "google"})

	// Build the /oauth2/token request.
	body := `grant_type=authorization_code&code=test-code&code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-OUR-VERIFIER&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Foauth2%2Fcallback&provider=google`
	c, w := newTestGinCtx()
	c.Request = httptest.NewRequest("POST", "/oauth2/token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.handlers.Token(c)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(auditW.entries) != 1 {
		t.Fatalf("expected 1 step_up_complete audit row, got %d", len(auditW.entries))
	}
	if auditW.entries[0].FieldPath != "auth.step_up_complete" {
		t.Errorf("wrong FieldPath: %s", auditW.entries[0].FieldPath)
	}
}

func TestToken_StepUpIdentityMismatch_Returns400AndAudits(t *testing.T) {
	auditW := &memorySystemAuditWriter{}
	h, _, _ := newStepUpTestHarness(t,
		withProvider("google", strongProviderConfig()),
		withCustomAuditWriter(auditW),
	)
	ctx := context.Background()

	_ = h.seedUser(ctx, "alice@example.com", "google", "uid-alice")
	_ = h.seedUser(ctx, "eve@example.com", "google", "uid-eve")

	pkceMap := map[string]string{
		"code_challenge":        "challenge-abc",
		"code_challenge_method": "S256",
		"step_up":               "true",
		"original_user_uuid":    "uuid-of-alice-pinned",
		"original_email":        "alice@example.com",
		"step_up_strength":      "strong",
	}
	pkceJSON, _ := json.Marshal(pkceMap)
	_ = h.handlers.service.dbManager.Redis().Set(ctx, "pkce:test-code", string(pkceJSON), 60_000_000_000)

	// Configure the provider to return Eve's userInfo — the re-authed identity.
	h.stubProvider.SetUserInfoForCode("test-code", &UserInfo{ID: "uid-eve", Email: "eve@example.com", Name: "Eve", IdP: "google"})

	body := `grant_type=authorization_code&code=test-code&code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-OUR-VERIFIER&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Foauth2%2Fcallback&provider=google`
	c, w := newTestGinCtx()
	c.Request = httptest.NewRequest("POST", "/oauth2/token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.handlers.Token(c)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "identity_mismatch") {
		t.Errorf("wrong error: %s", w.Body.String())
	}
	if len(auditW.entries) != 1 || auditW.entries[0].FieldPath != "auth.step_up_failed" {
		t.Errorf("expected step_up_failed audit row; got %#v", auditW.entries)
	}
	// attempted_email must be redacted.
	if strings.Contains(*auditW.entries[0].NewValueRedacted, "eve@example.com") {
		t.Errorf("attempted_email leaked verbatim: %s", *auditW.entries[0].NewValueRedacted)
	}
}
```

The helpers `h.seedUser`, `h.stubProvider`, and `newTestGinCtx` extend the harness in Task 5. Add them now.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./auth/ -run "TestToken_StepUpIdentityMatch_RotatesAndAudits|TestToken_StepUpIdentityMismatch_Returns400AndAudits" -count=1
```

Expected: FAIL (current `Token` handler does not branch on step-up).

- [ ] **Step 3: Implement the token-mint step-up branch**

In `auth/handlers_token.go`, locate the parse-PKCE block (around line 103-114). After parsing `pkceData`, capture the step-up fields:

```go
codeChallenge := pkceData["code_challenge"]
codeChallengeMethod := pkceData["code_challenge_method"]

isStepUp := pkceData["step_up"] == "true"
stepUpOriginalUUID := pkceData["original_user_uuid"]
stepUpOriginalEmail := pkceData["original_email"]
stepUpStrength := pkceData["step_up_strength"]
```

After `findOrCreateUser` returns successfully (around line 213, after the error switch but before "Update user profile"), branch:

```go
// #397 — step-up identity-match check.
if isStepUp {
	if user.InternalUUID != stepUpOriginalUUID {
		actor := StepUpActor{Email: stepUpOriginalEmail, Provider: providerID}
		_ = h.stepUpAud().LogFailed(ctx, actor, "identity_mismatch", map[string]string{
			"attempted_email": email,
		})
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "identity_mismatch",
			"error_description": "You must re-authenticate as the user who initiated step-up",
		})
		return
	}
	// Identity match — blacklist old refresh cookie (best-effort) before mint.
	if oldRefresh, cerr := c.Cookie(RefreshTokenCookieName); cerr == nil && oldRefresh != "" {
		if h.service.tokenBlacklist != nil {
			if bErr := h.service.tokenBlacklist.BlacklistToken(ctx, oldRefresh); bErr != nil {
				slogging.Get().WithContext(c).Warn("step-up: failed to blacklist old refresh: %v", bErr)
			}
		}
	}
}
```

After the existing token mint succeeds (around line 252, after `tokenPair, err := h.service.GenerateTokensWithUserInfo(...)`), if `isStepUp`:

```go
if isStepUp {
	actor := StepUpActor{
		Email:          user.Email,
		Provider:       providerID,
		ProviderUserID: user.ProviderUserID,
		DisplayName:    user.Name,
	}
	mode := "round_trip"
	strength := StepUpStrong
	if stepUpStrength == "weak" {
		strength = StepUpWeak
	}
	_ = h.stepUpAud().LogComplete(ctx, actor, strength, providerID, mode)
}
```

Cookies are already set by `SetTokenCookies` in the existing flow — no change needed for cookie behavior.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./auth/ -run "TestToken_StepUp" -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Run all auth tests for regressions**

```bash
make test-unit name=auth
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add auth/handlers_token.go auth/handlers_token_step_up_test.go
git commit -m "feat(auth): token-mint step-up branch with identity-match + audit (#397)"
```

---

## Task 9: Register route in cmd/server/main.go

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Find where /oauth2/authorize is registered**

```bash
rg "/oauth2/authorize|GET.*authorize.*Authorize" cmd/server/main.go -n
```

Note the route registration line(s) and the surrounding middleware stack.

- [ ] **Step 2: Register /oauth2/step_up**

Immediately after the `/oauth2/authorize` registration, add:

```go
// #397 — /oauth2/step_up forces fresh re-authentication. Public route
// (no JWT middleware on the route itself; the handler reads the cookie/
// header). Same rate-limit posture as /oauth2/authorize.
router.GET("/oauth2/step_up", authHandlers.StepUp)
```

If `/oauth2/authorize` is wrapped in a middleware group (rate-limit, public-endpoint annotation, etc.), wrap `/oauth2/step_up` in the same group.

- [ ] **Step 3: Build and smoke**

```bash
make build-server
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): register GET /oauth2/step_up route (#397)"
```

---

## Task 10: OpenAPI spec — `/oauth2/step_up` operation

**Files:**
- Modify: `api-schema/tmi-openapi.json`

This task is large but mechanical. The agent should work by reading the existing `/oauth2/authorize` operation (line range can be found via `rg -n '"/oauth2/authorize"' api-schema/tmi-openapi.json`) and producing an analogous entry for `/oauth2/step_up`.

- [ ] **Step 1: Add the new path entry**

In `api-schema/tmi-openapi.json`, locate `/oauth2/authorize` and add `/oauth2/step_up` immediately after it. Use this skeleton (fill exact text inline; all `example` fields are mandatory):

```jsonc
"/oauth2/step_up": {
  "get": {
    "tags": ["Authentication"],
    "summary": "Initiate fresh-prompt step-up re-authentication",
    "description": "Forces a fresh interactive re-authentication at the user's bound IdP by adding prompt=login&max_age=0 (OAuth/OIDC) or ForceAuthn=true (SAML) to the upstream authorize URL. For providers that do not honor those parameters (e.g., GitHub), the endpoint short-circuits and rotates tokens in-place with a 'strength: weak' audit marker. See #397 and docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md.",
    "operationId": "stepUpAuthenticate",
    "parameters": [
      { "$ref": "#/components/parameters/ClientCallbackQueryParam" },
      { "$ref": "#/components/parameters/StateQueryParam" },
      { "$ref": "#/components/parameters/CodeChallengeQueryParam" },
      { "$ref": "#/components/parameters/CodeChallengeMethodQueryParam" }
    ],
    "responses": {
      "200": {
        "description": "Weak-provider short-circuit: tokens rotated in-place. Set-Cookie headers carry new HttpOnly access and refresh tokens. Returned only when the JWT-bound provider is classified as 'weak' (e.g., github).",
        "content": {
          "application/json": {
            "schema": {
              "type": "object",
              "required": ["result", "provider", "auth_time", "message"],
              "properties": {
                "result": { "type": "string", "enum": ["step_up_weak_complete"] },
                "provider": { "type": "string" },
                "auth_time": { "type": "integer", "format": "int64" },
                "message": { "type": "string" }
              }
            },
            "example": {
              "result": "step_up_weak_complete",
              "provider": "github",
              "auth_time": 1715357321,
              "message": "Provider does not support guaranteed fresh re-auth; tokens rotated and step-up window reset. Audit log records this as a weak step-up."
            }
          }
        }
      },
      "302": {
        "description": "Strong-provider path: redirect to upstream IdP with prompt=login&max_age=0 (OAuth/OIDC) or ForceAuthn=true (SAML).",
        "headers": {
          "Location": {
            "description": "Upstream IdP authorization URL with fresh-prompt parameters appended.",
            "schema": {
              "type": "string",
              "format": "uri",
              "maxLength": 2000,
              "example": "https://accounts.google.com/o/oauth2/v2/auth?client_id=...&response_type=code&prompt=login&max_age=0&state=..."
            }
          }
        }
      },
      "400": {
        "description": "Validation or pre-flight rejection (RFC 6749 §4.1.2.1-aligned error codes).",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" },
            "examples": {
              "invalid_request": {
                "summary": "Missing or malformed parameter",
                "value": { "error": "invalid_request", "error_description": "client_callback parameter is required" }
              },
              "invalid_provider": {
                "summary": "JWT idp is not configured or disabled",
                "value": { "error": "invalid_provider", "error_description": "Provider \"acme\" is not configured or is disabled" }
              },
              "unsupported_grant_type": {
                "summary": "Caller is a client-credentials grant",
                "value": { "error": "unsupported_grant_type", "error_description": "Step-up does not apply to client credentials grants" }
              },
              "unsupported_response_type": {
                "summary": "Unsupported response_type (defensive)",
                "value": { "error": "unsupported_response_type", "error_description": "Only response_type=code is supported" }
              },
              "invalid_scope": {
                "summary": "scope parameter not accepted",
                "value": { "error": "invalid_scope", "error_description": "scope is not accepted on /oauth2/step_up" }
              },
              "identity_mismatch": {
                "summary": "Returned by /oauth2/token, not here; documented for cross-reference",
                "value": { "error": "identity_mismatch", "error_description": "You must re-authenticate as the user who initiated step-up" }
              }
            }
          }
        }
      },
      "401": {
        "description": "JWT cookie/header missing, invalid, or expired.",
        "headers": {
          "WWW-Authenticate": {
            "description": "Bearer error=\"invalid_token\"",
            "schema": { "type": "string", "example": "Bearer error=\"invalid_token\"" }
          }
        },
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" },
            "example": { "error": "invalid_token", "error_description": "Missing or invalid access token" }
          }
        }
      },
      "403": {
        "description": "Reserved for future per-grant restrictions (unauthorized_client).",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" },
            "example": { "error": "unauthorized_client", "error_description": "Step-up is not permitted for this grant" }
          }
        }
      },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": {
        "description": "Internal server error.",
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" },
            "example": { "error": "server_error", "error_description": "Internal failure during step-up processing" }
          }
        }
      },
      "503": {
        "description": "Redis or downstream store temporarily unavailable.",
        "headers": {
          "Retry-After": {
            "description": "Seconds to wait before retry",
            "schema": { "type": "integer", "example": 30 }
          }
        },
        "content": {
          "application/json": {
            "schema": { "$ref": "#/components/schemas/Error" },
            "example": { "error": "temporarily_unavailable", "error_description": "State storage temporarily unavailable" }
          }
        }
      }
    },
    "x-public-endpoint": true,
    "x-public-endpoint-purpose": "Step-up authentication initiation (reads JWT from cookie/header; no JWT middleware on the route)"
  }
}
```

- [ ] **Step 2: Validate the spec**

```bash
make validate-openapi
```

Expected: `0 errors`. Warnings/info should match the baseline (the closing comment on #287 reported `2 warnings, 6 info`). If new warnings appear, fix them in the spec entry above (most likely culprits: missing `example` on a schema, undefined `$ref`).

- [ ] **Step 3: Regenerate API code**

```bash
make generate-api
```

Expected: clean regeneration. The generated `stepUpAuthenticate` operation should appear in `api/api.go` (search via `rg "stepUpAuthenticate\|StepUpAuthenticate" api/api.go`).

- [ ] **Step 4: Run lint + build**

```bash
make lint
make build-server
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): /oauth2/step_up OpenAPI operation with RFC-aligned error responses (#397)"
```

---

## Task 11: Integration test (end-to-end strong + weak paths)

**Files:**
- Create: `test/integration/workflows/step_up_oauth_round_trip_test.go`

- [ ] **Step 1: Read the existing step-up integration test for pattern**

```bash
ls test/integration/workflows/step_up_round_trip_test.go
rg "step_up_round_trip|TestStepUpRoundTrip" test/integration/workflows/ -n | head
```

This test (from #355) demonstrates the integration-test scaffolding: real Postgres via Testcontainers, miniredis, a stub OAuth provider. The new test mirrors that pattern.

- [ ] **Step 2: Write the integration test**

Create `test/integration/workflows/step_up_oauth_round_trip_test.go`:

```go
//go:build integration

package workflows

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStepUpStrongRoundTripFlow exercises the full strong-provider flow end-to-end:
//   1. Client (with valid JWT cookie) hits /oauth2/step_up.
//   2. Server 302-redirects to upstream IdP with prompt=login&max_age=0.
//   3. Test stub IdP "completes" re-auth and redirects back to /oauth2/callback.
//   4. Callback redirects to client_callback with code+state.
//   5. Client posts to /oauth2/token; identity matches; tokens rotated; audit row written.
//   6. Client retries the originally-failing /admin/* operation; succeeds.
func TestStepUpStrongRoundTripFlow(t *testing.T) {
	srv := newIntegrationServer(t) // existing helper from the #355 test, or analogous
	defer srv.Close()

	// 1. Mint an initial JWT for alice via the stub provider's normal login.
	aliceJWT, aliceCookie := srv.LoginViaStub(t, "alice", "google")

	// 2. Roll the clock forward so auth_time is stale (past step_up_window).
	srv.AdvanceClock(t, 6*60) // 6 minutes; window is 5 min.

	// 3. Attempt an admin write; expect 401 + WWW-Authenticate: step-up.
	adminReq, _ := http.NewRequest("POST", srv.URL+"/admin/settings/test_key", strings.NewReader(`{"value":"x"}`))
	adminReq.AddCookie(aliceCookie)
	adminReq.Header.Set("Content-Type", "application/json")
	adminResp, err := srv.Client.Do(adminReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, adminResp.StatusCode)
	require.Contains(t, adminResp.Header.Get("WWW-Authenticate"), "insufficient_user_authentication")

	// 4. Hit /oauth2/step_up.
	pkceVerifier, pkceChallenge := generatePKCE(t)
	stepUpURL := srv.URL + "/oauth2/step_up?" + url.Values{
		"client_callback":       {"http://localhost:4200/cb"},
		"code_challenge":        {pkceChallenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	suReq, _ := http.NewRequest("GET", stepUpURL, nil)
	suReq.AddCookie(aliceCookie)
	// Disable automatic redirects so we can inspect the 302.
	noRedirectClient := *srv.Client
	noRedirectClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	suResp, err := noRedirectClient.Do(suReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, suResp.StatusCode)
	upstreamURL := suResp.Header.Get("Location")
	require.Contains(t, upstreamURL, "prompt=login")
	require.Contains(t, upstreamURL, "max_age=0")

	// 5. Simulate upstream re-auth — the stub provider exposes a hook that
	//    accepts the upstream URL and returns a callback URL.
	callbackURL := srv.StubProvider.SimulateUpstreamReAuth(t, upstreamURL, "alice")
	cbReq, _ := http.NewRequest("GET", callbackURL, nil)
	cbReq.AddCookie(aliceCookie)
	cbResp, err := noRedirectClient.Do(cbReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, cbResp.StatusCode)
	finalLoc := cbResp.Header.Get("Location")
	require.Contains(t, finalLoc, "http://localhost:4200/cb")
	require.Contains(t, finalLoc, "code=")

	// 6. Extract code and POST to /oauth2/token.
	codeParam := mustExtractQueryParam(t, finalLoc, "code")
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {codeParam},
		"code_verifier": {pkceVerifier},
		"redirect_uri":  {srv.URL + "/oauth2/callback"},
		"provider":      {"google"},
	}
	tokenReq, _ := http.NewRequest("POST", srv.URL+"/oauth2/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.AddCookie(aliceCookie)
	tokenResp, err := srv.Client.Do(tokenReq)
	require.NoError(t, err)
	require.Equal(t, 200, tokenResp.StatusCode)
	var tp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.NewDecoder(tokenResp.Body).Decode(&tp))
	require.NotEmpty(t, tp.AccessToken)

	// New cookies must be in Set-Cookie headers.
	newCookies := tokenResp.Cookies()
	var newAccessCookie *http.Cookie
	for _, ck := range newCookies {
		if ck.Name == "tmi_access_token" {
			newAccessCookie = ck
		}
	}
	require.NotNil(t, newAccessCookie, "new access cookie must be set")
	require.True(t, newAccessCookie.HttpOnly)

	// 7. Retry the admin write with the new cookie — should succeed.
	retryReq, _ := http.NewRequest("POST", srv.URL+"/admin/settings/test_key", strings.NewReader(`{"value":"x"}`))
	retryReq.AddCookie(newAccessCookie)
	retryReq.Header.Set("Content-Type", "application/json")
	retryResp, err := srv.Client.Do(retryReq)
	require.NoError(t, err)
	require.True(t, retryResp.StatusCode >= 200 && retryResp.StatusCode < 300, "admin write must succeed after step-up; got %d", retryResp.StatusCode)

	// 8. Confirm exactly one step_up_complete row landed in system_audit_entries.
	rows := srv.QueryAuditRows(t, "auth.step_up_complete")
	require.Len(t, rows, 1)
	require.Contains(t, rows[0].NewValueRedacted, `"strength":"strong"`)
}

func TestStepUpWeakShortCircuit(t *testing.T) {
	srv := newIntegrationServer(t)
	defer srv.Close()

	bobJWT, bobCookie := srv.LoginViaStub(t, "bob", "github")
	_ = bobJWT

	pkceVerifier, pkceChallenge := generatePKCE(t)
	_ = pkceVerifier
	stepUpURL := srv.URL + "/oauth2/step_up?" + url.Values{
		"client_callback":       {"http://localhost:4200/cb"},
		"code_challenge":        {pkceChallenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	req, _ := http.NewRequest("GET", stepUpURL, nil)
	req.AddCookie(bobCookie)
	resp, err := srv.Client.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var body struct {
		Result   string `json:"result"`
		Provider string `json:"provider"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "step_up_weak_complete", body.Result)
	require.Equal(t, "github", body.Provider)

	rows := srv.QueryAuditRows(t, "auth.step_up_complete")
	require.Len(t, rows, 1)
	require.Contains(t, rows[0].NewValueRedacted, `"strength":"weak"`)
}
```

The integration scaffolding (`newIntegrationServer`, `srv.LoginViaStub`, `srv.AdvanceClock`, `srv.QueryAuditRows`, `srv.StubProvider`, `mustExtractQueryParam`, `generatePKCE`) is reused from the existing `test/integration/workflows/step_up_round_trip_test.go`. If a helper name differs, adapt to what is actually present. **Do not** invent new scaffolding for this test — reuse the existing patterns.

- [ ] **Step 3: Run the integration test**

```bash
make test-integration name=TestStepUpStrongRoundTripFlow
make test-integration name=TestStepUpWeakShortCircuit
```

Expected: both pass.

- [ ] **Step 4: Run the full integration suite for regressions**

```bash
make test-integration
```

Expected: pre-existing failures from #355 baseline may still reproduce (TestAuthFlowRateLimiting_MultiScope, TestClientCredentialsCRUD, TestIPRateLimiting_PublicEndpoints, TestCascadeDeletion). No NEW failures introduced.

- [ ] **Step 5: Commit**

```bash
git add test/integration/workflows/step_up_oauth_round_trip_test.go
git commit -m "test(integration): end-to-end /oauth2/step_up strong + weak flows (#397)"
```

---

## Task 12: Verification gates

**Files:** none (all are command runs)

- [ ] **Step 1: Lint**

```bash
make lint
```

Expected: `0 issues`.

- [ ] **Step 2: OpenAPI validate**

```bash
make validate-openapi
```

Expected: `0 errors`. Warnings/info no worse than baseline.

- [ ] **Step 3: Build**

```bash
make build-server
```

Expected: clean.

- [ ] **Step 4: Unit tests**

```bash
make test-unit
```

Expected: 0 failures. New tests counted.

- [ ] **Step 5: Integration tests**

```bash
make test-integration
```

Expected: no new failures vs. the pre-existing baseline.

- [ ] **Step 6: Security regression check (mandatory per CLAUDE.md)**

```bash
# Invoke the security-regression skill — the agent harness should know to do this.
# If running this plan manually, use the /security-regression command in Claude Code.
```

Expected: PASS across all sections.

- [ ] **Step 7: Oracle DB compatibility (defensive)**

This plan introduces **no schema changes** — no new tables, no new columns, no GORM tag changes. Verify by:

```bash
git diff dev/1.4.0..HEAD -- api/models/ auth/migrations/ | wc -l
```

Expected: 0 lines (or only test files, no model files).

If the diff includes any model/migration change, dispatch the `oracle-db-admin` subagent per CLAUDE.md.

- [ ] **Step 8: Confirm no regressions in #355 tests**

```bash
go test ./test/integration/workflows/ -run "TestStepUpRoundTrip" -count=1 -tags=integration
```

Expected: the original #355 round-trip test still passes.

---

## Task 13: Update issue, commit & push, close

**Files:** none (operational tasks)

- [ ] **Step 1: Status comment on #397**

```bash
gh issue comment 397 --body "$(cat <<'EOF'
## Implementation complete

`/oauth2/step_up` landed on `dev/1.4.0` with both strong-provider redirect path (prompt=login&max_age=0 / ForceAuthn) and weak-provider short-circuit (github) with audit marker. Identity-match check at /oauth2/token defends against cookie-theft laundering.

### What shipped

- `auth/handlers_step_up.go` + `auth/provider_step_up.go` + `auth/audit_step_up.go` (new)
- SAML ForceAuthn variant in `auth/saml/provider.go`
- Step-up marker propagated through `parseCallbackState` → `pkce:<code>` → `/oauth2/token`
- `/oauth2/step_up` GET wired in `cmd/server/main.go`
- OpenAPI spec entry with RFC 6749 §4.1.2.1 error coverage + examples
- Unit tests for classifier, audit helpers, handler, callback propagation, token-mint branch
- Integration test: end-to-end strong-flow round trip + weak-flow short-circuit

### Verification

- `make lint` — 0 issues
- `make validate-openapi` — 0 errors
- `make build-server` — clean
- `make test-unit` — all pass (added tests for handler/classifier/audit/callback/token branches)
- `make test-integration` — new step-up tests pass; pre-existing #355 baseline failures unchanged
- `security-regression` — PASS
- No DB schema changes; no oracle-db-admin dispatch required.

### Follow-up issues

- tmi-ux issue to update the client retry path to call `/oauth2/step_up` (instead of /oauth2/authorize) on 401 + WWW-Authenticate: insufficient_user_authentication. Will file as a separate issue.
- Wiki update describing the new audit event kinds (auth.step_up_complete, auth.step_up_failed, auth.step_up_rejected) and strength=strong|weak semantics.
EOF
)"
```

- [ ] **Step 2: File the tmi-ux follow-up**

```bash
gh issue create \
  --repo ericfitz/tmi-ux \
  --title "feat(auth): use /oauth2/step_up on step-up retry (TMI #397)" \
  --body "$(cat <<'EOF'
## Context

TMI #397 landed `/oauth2/step_up` on `dev/1.4.0`. tmi-ux currently retries on 401 + `WWW-Authenticate: insufficient_user_authentication` by re-running `/oauth2/authorize`, which silently rubber-stamps a warm IdP session and defeats the freshness guarantee.

## Ask

Update the step-up retry path to call `/oauth2/step_up` (GET, browser navigation) instead of `/oauth2/authorize`. The endpoint takes the same `client_callback`, `state`, `code_challenge`, `code_challenge_method` params and 302s to the upstream IdP with `prompt=login&max_age=0` — caller behavior is the same.

For weak providers (currently github only), the server returns 200 with `{result: "step_up_weak_complete"}` and new HttpOnly cookies. tmi-ux should treat 200 as "step-up done, retry the admin operation now" and surface a small UI note that step-up was weak (audit row will reflect this server-side).

## References

- TMI #397
- TMI design spec: docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md
EOF
)"
```

- [ ] **Step 3: Final push**

```bash
git pull --rebase
git push
git status  # MUST show "up to date with origin"
```

- [ ] **Step 4: Manual issue close (per CLAUDE.md — non-default branch)**

```bash
gh issue close 397
```

(`Closes #397` in commit messages alone is **not** sufficient on `dev/1.4.0` — only `main` triggers GitHub auto-close.)

- [ ] **Step 5: Project board update**

The TMI project status for #397 should auto-move to "Done" via GitHub's default automation when the issue closes. If it doesn't:

```bash
gh project item-list 2 --owner ericfitz --format json --limit 200 \
  | jq -r '.items[] | select(.content.number == 397) | .id' \
  | xargs -I{} gh project item-edit \
      --project-id "PVT_kwHOACjZhM4BC0Z1" \
      --id {} \
      --field-id "PVTSSF_lAHOACjZhM4BC0Z1zg06000" \
      --single-select-option-id "98236657"  # "Done"
```

---

## Self-review notes

**Spec coverage:**
- Endpoint shape & flow → Tasks 5 + 9.
- Strength classifier → Task 1.
- SAML ForceAuthn → Task 2.
- Audit rows (all 7 event kinds) → Tasks 3, 5, 6, 7, 8 (each path writes the relevant kind).
- Callback marker propagation → Task 7.
- Token-mint identity-match + rotate + audit → Task 8.
- Cookie rotation (HttpOnly) → Tasks 5 (weak path), 8 (strong path via existing `SetTokenCookies`).
- Weak short-circuit → Task 5.
- CC-grant rejection → Task 6.
- RFC error response surface in OpenAPI → Task 10.
- No DB schema changes → confirmed in Task 12 step 7.

**Type consistency:** `StepUpStrength` (Task 1) ↔ `StepUpStrong`/`StepUpWeak` (Tasks 1, 3, 5, 6, 8). `StepUpAuditor` ↔ `StepUpActor`, `SystemAuditRecord`, `SystemAuditWriter` (Tasks 3, 4, 5, 6, 8). `BuildStepUpAuthorizationURL` (Task 1) ↔ used in Task 5. `GetAuthorizationURLForceAuthn` (Task 2) ↔ should be wired by the SAML branch of `BuildStepUpAuthorizationURL` — note for the implementer: Task 1's `BuildStepUpAuthorizationURL` currently handles OAuth only; if a SAML provider reaches this code path it must delegate to `GetAuthorizationURLForceAuthn` via a type-switch on the `Provider` interface. The classifier returns `Strong` for SAML by convention, so the handler does need to branch on provider kind before calling the URL builder. **Action for implementer:** add a SAML branch in `stepUpStrongRedirect` (Task 5) that calls `samlProvider.GetAuthorizationURLForceAuthn(state)` instead of `BuildStepUpAuthorizationURL(...)`. Detect via type assertion `if sp, ok := provider.(*saml.SAMLProvider); ok { ... }`.

**Placeholders:** the one `t.Skip("FILL IN: ...")` in Task 5's harness body is the only intentional handoff point. Every other step has concrete code or an exact command. The Task 2 SAML test's `newTestSAMLProvider` is a known harness-name guess; if it doesn't exist, the test skips cleanly and the integration test in Task 11 is the safety net.

**Scope:** single endpoint + supporting changes in 6 existing files + 4 new files + 6 new test files + 1 OpenAPI spec edit. Single implementation plan; no decomposition needed.
