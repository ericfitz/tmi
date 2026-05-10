# Step-up authentication and in-band admin audit log (T7 Part 1)

**Issue:** [#355](https://github.com/ericfitz/tmi/issues/355)
**Threat reference:** T7 (Full-system compromise via admin-settings tampering) in `docs/THREAT_MODEL.md` §4 — implements §8 mitigation #15 (Part 1)
**Status:** Design complete, ready for implementation plan
**Date:** 2026-05-09

## Summary

This is Part 1 of a three-part T7 mitigation. Part 1 closes the worst case — single API call with no friction and no per-event audit — by:

1. Adding step-up authentication to high-blast-radius `/admin/*` writes via an OIDC-standard `auth_time` JWT claim and a 5-minute freshness window.
2. Recording every `/admin/*` write to a new system-level audit table (`system_audit_entries`) with redacted before/after values.

Three things are deliberately **out of scope** and tracked in follow-up tickets:

- Out-of-band alert sink ([#395](https://github.com/ericfitz/tmi/issues/395)) — so a hostile insider with admin can't suppress the audit signal.
- Dual-admin approval queue for the highest-risk fields ([#396](https://github.com/ericfitz/tmi/issues/396)).
- Forced-fresh re-auth via `/oauth2/step_up` with `prompt=login&max_age=0` ([#397](https://github.com/ericfitz/tmi/issues/397)) — so step-up freshness doesn't depend on warm IdP sessions.

Read API for the audit log ([#398](https://github.com/ericfitz/tmi/issues/398)), admin UI ([tmi-ux #679](https://github.com/ericfitz/tmi-ux/issues/679)), client-credentials step-up mechanism ([#399](https://github.com/ericfitz/tmi/issues/399)), and retention/pruning ([#400](https://github.com/ericfitz/tmi/issues/400)) are also tracked separately.

## Threat framing

T7 in `docs/THREAT_MODEL.md` describes an admin-role attacker — insider or post-T2 escalation — who uses a single `/admin/*` API call to land a system-wide compromise: rewriting OAuth/SAML provider config to an attacker IdP, disabling the SSRF allowlist, weakening rate limits, or rotating the JWT signing key. Today's mitigations: only the admin-role gate. No second factor, no per-event audit alert, no friction.

Part 1's value is that it adds friction (step-up requires a fresh interactive IdP authentication within a 5-minute window) and produces an immutable per-event record (a row in `system_audit_entries`). It does not close the threat — a determined insider can satisfy step-up and tamper with the in-band log — but it raises the bar from "single API call" to "fresh authentication plus audit-log tampering" and provides the evidentiary substrate that the OOB alert and dual-approval follow-ups need.

## Architecture

Three components, landable as a single PR:

1. **`auth_time` JWT claim.** The JWT is extended with an OIDC-standard `auth_time` claim (Unix seconds) representing the time of the user's last interactive IdP authentication. New IdP logins set `auth_time = now`. Refresh-token rotation preserves the existing `auth_time` (refresh proves possession of the refresh token, not freshness of the human). Client-credentials grants set `auth_time = now` on every token mint (Part 1 deliberately does not break automation; #399 will introduce a real CC step-up mechanism later).

2. **`StepUpMiddleware`.** A new Gin middleware that runs after `AuthzMiddleware` for routes resolved as step-up-required. It reads `auth_time` from the validated JWT and, if `now - auth_time > step_up_window`, returns `401 Unauthorized` with `WWW-Authenticate: Bearer error="insufficient_user_authentication", error_description="...", max_age=300` (per draft-ietf-oauth-step-up-authn-challenge). On a fresh enough `auth_time`, it passes through.

3. **System audit log + `AdminAuditMiddleware`.** A new GORM model `SystemAuditEntry` (table `system_audit_entries`, no `ThreatModelID` — separate from the existing TM-scoped `audit_entries`). A new middleware that captures the request body and current server-side state before the handler runs, then on a 2xx response writes a redacted-old/redacted-new audit row.

### Request flow for a gated admin write

```
HTTP request
  → JWT validation (existing)
  → AuthContext (existing — populates actor identity)
  → AuthzMiddleware (existing — admin role check)
  → StepUpMiddleware (new — auth_time freshness check)
  → AdminAuditMiddleware (new — capture before-state)
  → handler (existing)
  → AdminAuditMiddleware (after — write audit row on 2xx)
  → response
```

Order matters: AuthzMiddleware runs before StepUpMiddleware so non-admins get 403 (not 401 step-up challenge), avoiding info leak about which routes require step-up.

## Schema: `system_audit_entries`

New GORM model in `api/models/system_audit.go`. Table is purely additive; created by `AutoMigrate` at startup.

```go
type SystemAuditEntry struct {
    ID                string         `gorm:"primaryKey;type:varchar(36)"`

    // Actor — denormalized so audit rows survive user deletion
    ActorEmail        string         `gorm:"type:varchar(320);not null;index:idx_sysaudit_actor,priority:1"`
    ActorProvider     string         `gorm:"type:varchar(100);not null"`
    ActorProviderID   string         `gorm:"type:varchar(500);not null"`
    ActorDisplayName  string         `gorm:"type:varchar(256);not null"`

    // Request
    HTTPMethod        string         `gorm:"type:varchar(10);not null"`
    HTTPPath          string         `gorm:"type:varchar(2048);not null"`

    // Change
    FieldPath         string         `gorm:"type:varchar(1024);not null;index:idx_sysaudit_field"`
    OldValueRedacted  NullableDBText `gorm:""`
    NewValueRedacted  NullableDBText `gorm:""`
    ChangeSummary     NullableDBText `gorm:""`

    CreatedAt         time.Time      `gorm:"not null;autoCreateTime;index:idx_sysaudit_actor,priority:2;index:idx_sysaudit_created"`
}

func (SystemAuditEntry) TableName() string { return tableName("system_audit_entries") }
```

**Indexes:**
- `idx_sysaudit_actor` (`actor_email`, `created_at`) — for "what did Alice do recently"
- `idx_sysaudit_field` (`field_path`) — for "who touched the SSRF allowlist"
- `idx_sysaudit_created` (`created_at`) — for time-range queries

**Oracle compatibility — to be reviewed by oracle-db-admin subagent before merge:**

- `varchar(320)` for email is char-based on PostgreSQL but byte-based on Oracle; matches existing `AuditEntry.ActorEmail` precedent. Caught by project-wide #379.
- `NullableDBText` for redacted-value columns — large values are possible (multi-line cert blobs); existing project pattern handles CLOBs.
- `autoCreateTime` on a high-volume insert path — #380 tracks the analogous concern on feedback models. The subagent should weigh in on whether to set `CreatedAt` in the repository instead.
- Index strategy on `(actor_email, created_at)` — Oracle's index size limits and statistics gathering may want different shape than what GORM emits.
- No FKs (intentional, matches existing `AuditEntry` denormalization).

**Rollback:** drop the table. Purely additive; nothing else references it.

## JWT `auth_time` claim

`auth_time` is added to the standard claims set in the JWT. Files touched:

- `auth/handlers_token.go`, `auth/handlers_token_helpers.go` — token mint and refresh
- `auth/jwt_key_manager.go` — claim parsing/serialization
- `auth/handlers_oauth.go` — OAuth callback successful login
- `auth/handlers_saml.go` — SAML callback successful login
- `auth/default_provider_dev.go` — dev-mode `/oauth2/authorize?idp=tmi` shortcut
- `auth/client_credentials.go` — CC grant token issuance

### Mint paths (set `auth_time = now`)

- OAuth callback successful login
- SAML callback successful login
- Dev-mode TMI provider login
- Client-credentials grant (Part 1 only — #399 will revisit)

### Refresh path (carry forward)

`POST /oauth2/token` with `grant_type=refresh_token` preserves the existing `auth_time` from the previous JWT. The refresh proves possession of the refresh token, not freshness of the human; the new JWT's `auth_time` reflects the *original* interactive auth.

### Validation path

The existing JWT validator builds an auth context. We add `AuthTime *time.Time` to that context (nullable so missing-claim is distinguishable from `auth_time=0`). Existing middlewares ignore the new field; only `StepUpMiddleware` reads it.

### Backwards compatibility

Existing JWTs in flight do not have `auth_time`. The validator parses them cleanly (unknown claims are already ignored). `StepUpMiddleware` treats `AuthTime == nil` as stale, forcing step-up on the next admin write. No grace period — the user-visible cost is one extra OAuth round-trip on first admin write post-deploy. Self-corrects on next interactive IdP login.

## `StepUpMiddleware`

New file `api/step_up_middleware.go`. Runs after `AuthzMiddleware` for routes resolved as step-up-required.

```go
func StepUpMiddleware(window time.Duration, table StepUpRouteTable) gin.HandlerFunc {
    return func(c *gin.Context) {
        if !table.Required(c.Request.Method, c.FullPath()) {
            c.Next()
            return
        }

        authTime, ok := getAuthTimeFromContext(c)
        if !ok || time.Since(authTime) > window {
            c.Header("WWW-Authenticate",
                fmt.Sprintf(`Bearer error="insufficient_user_authentication", error_description="re-authentication required for this admin operation", max_age=%d`,
                    int(window.Seconds())))
            HandleRequestError(c, &RequestError{
                Status:  http.StatusUnauthorized,
                Code:    "insufficient_user_authentication",
                Message: "Recent re-authentication required",
            })
            c.Abort()
            return
        }
        c.Next()
    }
}
```

### Route resolution

Built once at server boot in `api/step_up_routes.go`:

```go
func buildStepUpRouteTable(spec *openapi3.T) StepUpRouteTable {
    table := StepUpRouteTable{}
    for path, item := range spec.Paths.Map() {
        if !strings.HasPrefix(path, "/admin/") { continue }
        for method, op := range item.Operations() {
            if !isWriteMethod(method) { continue }
            required := true
            if v, ok := op.Extensions["x-tmi-authz-step-up"].(string); ok && v == "optional" {
                required = false
            }
            table.Set(method, path, required)
        }
    }
    return table
}
```

Default for any `/admin/*` write method (POST/PUT/PATCH/DELETE) is `required = true`. Opt-out via `x-tmi-authz-step-up: optional` in the OpenAPI spec.

### Initial gating policy

**Step-up required (no opt-out):**
- `PUT /admin/settings/{key}`
- `DELETE /admin/settings/{key}`
- `POST /admin/settings/reencrypt`
- `POST /admin/users/{id}/transfer`
- `PATCH /admin/users/automation`
- `/admin/groups/*` (writes)
- `/admin/quotas/*` (writes)

**Opt-out (`x-tmi-authz-step-up: optional`):**
- `/admin/webhooks/subscriptions/*` (writes)
- `/admin/surveys/*` (writes)
- `/admin/timmy/*` (operational/diagnostic — not config-mutating)
- `/admin/users/{id}/content_tokens/*` (operational)

### `WWW-Authenticate` shape

Per draft-ietf-oauth-step-up-authn-challenge:

```
WWW-Authenticate: Bearer error="insufficient_user_authentication", error_description="re-authentication required for this admin operation", max_age=300
```

Clients aware of the draft handle it correctly. Clients that aren't see a regular `WWW-Authenticate: Bearer` and at least know auth failed.

### Configuration

One new system setting:

- `auth.step_up_window_seconds` (int, default 300, minimum 60)

Read once at middleware construction; changing it requires a server restart.

## `AdminAuditMiddleware`

New file `api/admin_audit_middleware.go`. Runs after `StepUpMiddleware`.

### Flow

1. Buffer the request body (Gin's `c.Request.Body` is single-read; restore it for the handler).
2. For routes that need a before-state, read the current server-side value.
3. Let the handler execute.
4. On 2xx response, write a `system_audit_entries` row with redacted old/new values.
5. On non-2xx, do nothing — the write didn't take effect.

```go
func AdminAuditMiddleware(repo SystemAuditRepository, redactor Redactor, descriptors map[routeKey]auditDescriptor) gin.HandlerFunc {
    return func(c *gin.Context) {
        desc, gated := descriptors[routeKey{c.Request.Method, c.FullPath()}]
        if !gated {
            c.Next()
            return
        }

        actor := mustGetActor(c)

        oldVal := desc.OldValueFn(c)

        bodyBytes, _ := io.ReadAll(c.Request.Body)
        c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

        c.Next()

        if c.Writer.Status() < 200 || c.Writer.Status() >= 300 {
            return
        }

        newVal := desc.NewValueFn(c, bodyBytes)
        fieldPath := desc.FieldPathFn(c)

        entry := models.SystemAuditEntry{
            ActorEmail:       actor.Email,
            ActorProvider:    actor.Provider,
            ActorProviderID:  actor.ProviderID,
            ActorDisplayName: actor.DisplayName,
            HTTPMethod:       c.Request.Method,
            HTTPPath:         c.FullPath(),
            FieldPath:        fieldPath,
            OldValueRedacted: redactor.Redact(fieldPath, oldVal),
            NewValueRedacted: redactor.Redact(fieldPath, newVal),
            ChangeSummary:    summarize(c.Request.Method, fieldPath),
        }

        if err := repo.Create(c.Request.Context(), entry); err != nil {
            slogging.Get().WithContext(c).Error(
                "system audit write failed: %v (actor=%s field=%s method=%s)",
                err, actor.Email, fieldPath, c.Request.Method)
            // Audit failure does NOT fail the admin write.
        }
    }
}
```

### Per-route descriptors

Each gated route has a descriptor:

```go
auditDescriptor{
    Route:       routeKey{"PUT", "/admin/settings/:key"},
    FieldPathFn: func(c *gin.Context) string { return "system_settings." + c.Param("key") },
    OldValueFn:  readSystemSettingValue,
    NewValueFn:  readRequestBodyValue,
}
```

Route-shape categories Part 1 must handle:

- **Per-key settings routes** (`/admin/settings/{key}`, `/admin/settings/reencrypt`): `field_path = "system_settings." + key`, `old_value` = current DB value, `new_value` = request body's `value` field.
- **Composite routes** (`PATCH /admin/users/automation`, `POST /admin/users/{id}/transfer`, `/admin/groups/*`, `/admin/quotas/*`): `field_path = "users.automation_grant"` / `"users.<uuid>.ownership_transfer"` / `"groups.<uuid>.<sub-resource>"` / `"quotas.<scope>.<id>"`. `old_value` = small read of relevant current state, `new_value` = request body.

A unit test asserts every gated route has a descriptor; a route without one falls back to logging just method + path with empty values (defense-in-depth, not the routine path).

### Audit-write failure policy

The admin write succeeds. The audit failure is logged at `Error` level. Trade-off: a transient DB hiccup should not become a denial-of-admin. Request logs are an additional (non-tamper-resistant) signal. The OOB alert follow-up (#395) closes the tamper-evidence gap.

## Redaction: `api/admin_audit_redaction.go`

Deny-list policy with three tiers.

### Tier 1: Total redaction

Patterns: `*.password`, `*.passphrase`

Output: `{"redacted": true}` — no hash, no tail. Hash leakage on a low-entropy password enables rainbow-table attacks.

### Tier 2: Hash + optional tail

Patterns: `*.api_key`, `*.client_secret`, `*.signing_key`, `*.private_key`, `*.public_key`, `*.bearer_token`, `*.access_token`, `*.refresh_token`, `*.token`, `*.secret`, `*.credential`, `*encryption*`

Output:
- Always: `{"redacted": true, "sha256_prefix": "<first-8-hex-of-sha256>"}` for any-length value (correlation via side-channel).
- Additionally: `"tail": "<last-6-chars>"` if the original value is **24 chars or longer**. The 24-char threshold ensures we never publish more than ~25% of a value, and only for high-entropy values where 25% is meaningless. API keys with prefix conventions (e.g., `ghp_…`, `xoxb-…`, `tmi_cc_…`) are well-handled because the predictable part is the prefix; the tail is the entropy-bearing remainder.

### Tier 3: Verbatim

Numeric, bool, null values: always verbatim regardless of field name.
String values that don't match Tier 1 or Tier 2 patterns: verbatim.

### Build-time gate

A unit test in `api/admin_audit_redaction_test.go` walks every system-settings key in `MigratableSettings` (from `api/config_provider_adapter.go`) and every `auditDescriptor.FieldPathFn` literal. For any key/path containing a substring in `["secret", "key", "password", "token", "credential", "private", "auth"]`, it MUST match a Tier 1 or Tier 2 deny-pattern. Adding a new sensitive-named setting without updating the deny-list breaks `make test-unit`.

The heuristic is intentionally over-broad. False positives are resolved by renaming or explicitly opting into the deny-list. The cost of a false positive is "redacted-but-actually-fine value in the audit log"; the cost of a false negative is "secret leaks in the audit log forever."

## Configuration

One new system setting:

- `auth.step_up_window_seconds` — int, default 300, minimum 60.

Lives in the migratable settings registry (so it's audited per Part 1 itself). Loaded once at server boot. Plumbed through the existing config provider — no new environment variable.

## OpenAPI changes

Three vendor-extension keys:

1. **`x-tmi-authz-step-up: optional`** — opt-out marker on the operations listed in the "Initial gating policy" section above.
2. **`x-tmi-audit: { kind: admin_settings_change }`** — audit emission marker on every gated route. Read by the route-table builder during boot to decide which routes get the audit middleware.
3. **`StepUpRequired` response component** — new entry in `components/responses` for `401` with the `WWW-Authenticate: Bearer error="insufficient_user_authentication"` shape. Referenced from each gated route's responses.

The OpenAPI changes go in `api-schema/tmi-openapi.json` and require regeneration via `make generate-api`. `make validate-openapi` checks the spec.

## Tests

### Unit tests

- `auth/handlers_token_test.go` — `auth_time` set on fresh login, preserved across refresh, set on CC grant.
- `auth/jwt_key_manager_test.go` — `auth_time` round-trips through JWT mint/parse.
- `api/step_up_middleware_test.go` — missing/stale/fresh `auth_time` cases; opt-out routes skipped; non-admin routes ignored; `WWW-Authenticate` header shape.
- `api/step_up_routes_test.go` — route table built correctly from the embedded OpenAPI spec; deny-by-default; opt-out parsed correctly.
- `api/admin_audit_redaction_test.go` — tier matching, tail/hash logic, build-time gate over `MigratableSettings` keys.
- `api/admin_audit_middleware_test.go` — capture-before-state, redaction applied, audit row only on 2xx, audit-write failure does not fail the admin write, every gated route has a descriptor.

### Integration tests

- Full step-up round-trip: issue JWT at T-10min via OAuth callback, attempt `PUT /admin/settings/some.key`, verify 401 + `WWW-Authenticate`, re-auth via OAuth callback (issuing fresh JWT with `auth_time = now`), retry the write, verify 2xx and a `system_audit_entries` row landed.
- Audit row contains redacted old/new for sensitive fields (e.g., `oauth.providers.google.client_secret`), verbatim for non-sensitive fields (e.g., `auth.step_up_window_seconds`).
- Non-admin user gets 403 from `AuthzMiddleware` and never sees the step-up challenge.
- CC grant successfully hits a gated admin route (per the Part 1 CC-step-up policy decision).

### CATS fuzzing

Existing `/admin/*` coverage continues. The 401 + `WWW-Authenticate: step-up` response shape is new on existing routes for stale-JWT cases; CATS may flag it. The first run will be analyzed for true-positive 500 errors per the zero-500-error policy.

## Oracle DB compatibility

Per CLAUDE.md, the **oracle-db-admin subagent must be dispatched before merge.** Specific items expected to be flagged:

- `autoCreateTime` on the high-volume insert path (#380 is the open analogous concern).
- `varchar(320)` for actor email under Oracle byte-vs-char semantics (#379).
- Index shape on `(actor_email, created_at)` — Oracle may want different layout than GORM default.
- CLOB handling for `OldValueRedacted` / `NewValueRedacted` (`NullableDBText`).

BLOCKING findings are fixed before merge. APPROVED WITH NOTES — fix what's easy, file follow-ups for the rest.

## Rollout

- **Single PR to `dev/1.4.0`.** Three components land together: shipping step-up without audit would record nothing for admins making changes during the gap; shipping audit without step-up loses half the value.
- **No feature flag.** The behavior change is the point of the ticket.
- **Migration:** GORM `AutoMigrate` creates `system_audit_entries` at startup. No data backfill — the audit log starts empty.
- **User-visible impact at deploy:** existing admin sessions without `auth_time` take one extra OAuth round-trip on first admin write post-deploy. Self-corrects.

## Out of scope (follow-up issues)

| Issue | Scope |
|---|---|
| [#395](https://github.com/ericfitz/tmi/issues/395) | Out-of-band audit alert sink (so insiders can't suppress in-band audit) |
| [#396](https://github.com/ericfitz/tmi/issues/396) | Dual-admin approval queue for highest-risk fields |
| [#397](https://github.com/ericfitz/tmi/issues/397) | `/oauth2/step_up` with `prompt=login&max_age=0` for guaranteed fresh re-auth |
| [#398](https://github.com/ericfitz/tmi/issues/398) | Admin endpoints to query system + threat-model audit logs |
| [tmi-ux #679](https://github.com/ericfitz/tmi-ux/issues/679) | Admin UI for viewing both audit logs |
| [#399](https://github.com/ericfitz/tmi/issues/399) | Step-up mechanism investigation for client-credentials grants |
| [#400](https://github.com/ericfitz/tmi/issues/400) | Retention and pruning for `system_audit_entries` |

## Open questions

None at design time. The CC step-up gap (#399) is a known accepted trade-off for Part 1.
