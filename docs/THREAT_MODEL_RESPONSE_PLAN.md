# Threat Model Response Plan

**Source threat model:** [docs/THREAT_MODEL.md](THREAT_MODEL.md) (target: TMI server @ b94886a3, dated 2026-04-30)
**Plan date:** 2026-05-01
**Owner:** efitz
**Scope:** every numbered threat T1–T27 in §4.

For each threat, this plan records either (a) the GitHub issue tracking the planned fix, or (b) why the threat is not being addressed in milestone 1.4.0 along with the evidence behind that decision.

All planned-fix issues are filed in milestone 1.4.0, assigned to ericfitz, with priority **Must Have** and project status **This milestone** in the TMI project.

## Summary

| Disposition | Threats |
|---|---|
| Planned fix in 1.4.0 | T1, T2, T3, T4, T5, T6, T7, T10, T11, T12, T13, T14, T15, T16, T18, T19, T20, T21, T23, T25, T26, T27 (22 threats / 20 issues — some issues close multiple threats) |
| Not addressed | T8 (already mitigated), T9 (low likelihood, deferred), T17 (per design — server stores raw, client sanitizes), T22 (already risk-accepted), T24 (already risk-accepted) |

## Per-threat dispositions

### T1 — Account takeover via federated-identity confusion
**Status:** planned fix
**Issue:** [#346](https://github.com/ericfitz/tmi/issues/346) — feat(auth): bind local accounts to (idp, sub) and add interactive identity-link flow
**Notes:** Builds on closed [#290](https://github.com/ericfitz/tmi/issues/290). Requires `email_verified=true` for sparse-record completion; introduces an interactive `/me/identities/link` flow so a second IdP can only attach to an existing account with explicit consent. Closes the §6 open question on `ResolveUser` fuzzy lookup.

### T2 — Privilege escalation via missing/mis-ordered authz checks and PATCH path handling
**Status:** planned fix
**Issues:**
- [#341](https://github.com/ericfitz/tmi/issues/341) — feat(api): default-deny authz middleware via `x-tmi-authz` OpenAPI extension
- [#342](https://github.com/ericfitz/tmi/issues/342) — fix(api): replace PATCH `prohibitedPaths` blocklist with per-resource allowlist

**Notes:** Two complementary fixes. Verified gap: `/owner`, `/authorization`, `/status` are not in the current PATCH `prohibitedPaths` ([api/threat_model_handlers.go:582-590](../api/threat_model_handlers.go#L582-L590)) and sub-resource handlers (asset, threat, diagram) have no allowlist at all. The authz middleware closes the §6 owner-states open question on "server is sole authz enforcer on every write".

### T3 — Server-side request forgery via user-supplied URLs
**Status:** planned fix
**Issues:**
- [#345](https://github.com/ericfitz/tmi/issues/345) — feat(api): single egress helper with DNS-pinned fetch
- [#347](https://github.com/ericfitz/tmi/issues/347) — feat(infra): isolate document/content extractors in a sandboxed worker container (defense in depth)

**Notes:** Verified DNS-rebinding gap: `api/ssrf_validator.go` resolves and checks IPs but the subsequent `client.Do(req)` calls in [api/timmy_content_provider_http.go:56](../api/timmy_content_provider_http.go#L56), [api/timmy_content_provider_pdf.go:58](../api/timmy_content_provider_pdf.go#L58), and [api/webhook_delivery_worker.go:117](../api/webhook_delivery_worker.go#L117) do not pin the resolved IP. The egress helper closes the rebinding window; the extractor sandbox is a second layer in case parsing reaches the network.

### T4 — Compromise via vulnerable third-party Go module
**Status:** planned fix
**Issue:** [#352](https://github.com/ericfitz/tmi/issues/352) — ci: gate builds on critical Dependabot alerts in security-sensitive paths

**Notes:** Owner-reported alert fatigue suggests turning the noisy fan into a narrow CI gate that blocks only on alerts touching `auth/`, `api/content_extractor*`, `api/ssrf_validator.go`, `api/webhook*.go`, `goxmldsig`, `golang-jwt/jwt`, or `gorm.io/gorm`. Includes vendoring/pinning `goxmldsig`.

### T5 — Cross-user IDOR for `is_confidential` resources via deep paths
**Status:** planned fix
**Issues:**
- [#341](https://github.com/ericfitz/tmi/issues/341) — `x-tmi-authz` middleware (covers role-gates including `confidential_reviewer`)
- [#357](https://github.com/ericfitz/tmi/issues/357) — audit(api): confidentiality coverage on nested and batch endpoints

**Notes:** Verified that the Confidential Project Reviewers group is auto-added on top-level threat-model create at [api/threat_model_handlers.go:265-271](../api/threat_model_handlers.go#L265-L271) but sub-resource and batch coverage is unverified. Issue #357 is a per-route audit with the five-actor test matrix.

### T6 — Secrets disclosure via deployment artifacts
**Status:** planned fix
**Issue:** [#344](https://github.com/ericfitz/tmi/issues/344) — ops: encrypted Terraform state + External Secrets Operator + tfvars secret scanner

**Notes:** Owner-confirmed in §6 that local tfstate is operational reality. Fix enables remote encrypted state per cloud, replaces `secrets.yml` placeholders with ESO references, and adds gitleaks (or equivalent) as a CI + pre-commit gate.

### T7 — Full-system compromise via admin-settings tampering
**Status:** planned fix
**Issue:** [#355](https://github.com/ericfitz/tmi/issues/355) — feat(api): step-up authentication and out-of-band audit alerts on `/admin/*` writes

**Notes:** Step-up auth (re-auth or TOTP/WebAuthn) on every admin write, out-of-band alert on every settings change, and an optional two-admin approval queue for the highest-risk fields (provider configs, JWT key rotation, SSRF allowlists).

### T8 — SQL injection via list-filter query parameters
**Status:** **not addressed in 1.4.0** — already mitigated
**Evidence:**
- Investigation found GORM uses parameterized queries (`?` placeholders) consistently across all `*_store_gorm.go` files.
- Specific check on `api/database_store_gorm.go`: filter values are bound (e.g., `.Where("LOWER(threat_models.name) LIKE LOWER(?)", "%"+*filters.Name+"%")`) — user input is a parameter, not concatenated.
- No `db.Raw(` with string-concatenated user input was found in the stores.

The threat model's "rare" likelihood reflects this: GORM's default is safe and no concrete risky site exists. We will revisit if a future PR introduces a `Raw()` or `fmt.Sprintf`-built predicate; the per-PR review process should catch that.

### T9 — Cache poisoning via Redis
**Status:** **not addressed in 1.4.0** — deferred to a later milestone
**Evidence:**
- Threat-model classification is "critical/rare/partially_mitigated" — Redis password is set; cache is invalidated on writes.
- Verified: cached values have no integrity MAC ([api/cache_service.go:59-98](../api/cache_service.go#L59-L98) plain `json.Marshal`/`json.Unmarshal`).
- Recommended mitigation (§8 #16) is M effort and only **partial** close of the threat (HMAC raises the bar but Redis network access still permits replay/key enumeration).

The threat requires Redis network access (or SSRF to the Redis port). The SSRF fix (T3, [#345](https://github.com/ericfitz/tmi/issues/345)) plus standard Redis network controls (private VPC, password) cover the realistic attack paths. HMAC on cached values is a sound defense-in-depth addition but does not warrant a Must-Have slot in 1.4.0 given the existing controls and the recommendation's "partial" close. Revisit when scaling or moving to multi-tenant.

### T10 — Lateral movement via pod-identity over-grant
**Status:** planned fix
**Issue:** [#348](https://github.com/ericfitz/tmi/issues/348) — ops: enforce `automountServiceAccountToken=false` consistently and scope IRSA/WIF to exact secret ARNs

**Notes:** Threat-model evidence: static manifest sets the field to `false` but the Terraform module sets it to `true`. The fix unifies the setting and tightens IAM policies to per-secret-ARN scope on each cloud.

### T11 — CI/CD pipeline compromise
**Status:** planned fix
**Issues:**
- [#344](https://github.com/ericfitz/tmi/issues/344) — encrypted Terraform state + ESO (covers the deploy-creds-on-disk angle)
- [#352](https://github.com/ericfitz/tmi/issues/352) — Dependabot CI gate (covers the malicious-dep-via-PR angle)

**Notes:** Branch protection and replacing developer-local Heroku CLI creds with OIDC-based federated deploy are operationally orthogonal and can be tracked under #344 (the deploy-creds work). If branch protection turns into a meaningful sub-task, it can be split off without re-filing.

### T12 — DoS / path traversal via malformed OOXML/zip/PDF/HTML
**Status:** planned fix
**Issue:** [#347](https://github.com/ericfitz/tmi/issues/347) — feat(infra): isolate document/content extractors in a sandboxed worker container

**Notes:** Existing controls (50 MiB caps, XML depth limiter, per-member stream caps) are good defaults but extraction runs in the main server process. Isolating extractors gives a parser CVE a finite blast radius — the extractor pod cannot reach the DB, Redis, or `169.254.169.254`, and a cgroup OOM kills it before it kills the API.

### T13 — Prompt injection via attacker-hosted document content
**Status:** planned fix
**Issue:** [#353](https://github.com/ericfitz/tmi/issues/353) — feat(timmy): per-threat-model retrieval isolation and prompt-injection guards

**Notes:** Verified `timmy_embeddings` carries a `threat_model_id` FK, but the retrieval query was not located (#214 backend incomplete). Fix lands once the retrieval handler exists: (a) `WHERE threat_model_id = ?` predicate is mandatory, (b) tool-call dispatch enforces schemas and routes outbound HTTP via the egress helper, (c) system-prompt guard frames fetched content as untrusted data.

### T14 — Race conditions in concurrent writes
**Status:** planned fix
**Issue:** [#354](https://github.com/ericfitz/tmi/issues/354) — feat(api): optimistic locking via version column and serialized ACL writes

**Notes:** SAVEPOINT recovery and unique constraints exist (verified at [api/survey_response_store_gorm.go:219-263](../api/survey_response_store_gorm.go#L219-L263)) but no `version` column anywhere. Adds optimistic-lock with `If-Match` semantics and SERIALIZABLE isolation for ACL writes. Migration requires `oracle-db-admin` review per CLAUDE.md gate.

### T15 — Brute-force of client_credentials/refresh tokens
**Status:** planned fix
**Issue:** [#350](https://github.com/ericfitz/tmi/issues/350) — feat(auth): exponential backoff lockout on `/oauth2/token` keyed on `client_id`

**Notes:** Per-IP and per-user limits exist (commits abb42e57, b56734ca); per-`client_id` lockout for `client_credentials` grants is missing, so an attacker rotating IPs can grind through `client_secret` candidates. Adds Redis-backed counter with exponential backoff per `client_id`.

### T16 — Open redirect / OAuth phishing via attacker-controlled `client_callback`
**Status:** planned fix
**Issue:** [#343](https://github.com/ericfitz/tmi/issues/343) — fix(auth): allowlist `client_callback` on `/oauth2/authorize`

**Notes:** Verified open: [auth/handlers_oauth.go:73](../auth/handlers_oauth.go#L73) reads `client_callback` raw and uses it as a redirect target at line 378. Fix promotes the existing `ClientCallbackAllowList` (used by content-OAuth at [api/content_oauth_handlers.go:121](../api/content_oauth_handlers.go#L121)) to a shared allowlist used by both flows. The §6 open question framed it as "owner says intentionally open" — this issue treats it as a bug since no ADR documenting the decision was found.

### T17 — Stored XSS via diagram cell content / threat descriptions / extracted text
**Status:** **not addressed in 1.4.0** — server-side per-design behavior; client-side concern
**Evidence:**
- Threat-model entry explicitly states: "server stores raw text by design; sanitization is client responsibility".
- The companion front-end ([tmi-ux](https://github.com/ericfitz/tmi-ux)) is the rendering surface. Token theft via stored XSS is tracked in tmi-ux issue [#164](https://github.com/ericfitz/tmi/issues/164) (closed-investigated).
- TMI's design principle (THREAT_MODEL.md §1) is: server is the sole authz enforcer; output sanitization is the client's responsibility.

The right server-side hardening (CSP-pre-flight, output-encoding-via-content-type, an HTML-stripping path on extraction outputs) belongs in the front-end and the webhook payload spec. If a future webhook subscriber (downstream UI) renders threat-model content unsafely, that is the subscriber's bug.

### T18 — Confused-deputy privilege escalation via addons
**Status:** planned fix
**Issue:** [#358](https://github.com/ericfitz/tmi/issues/358) — feat(api): scoped delegation tokens for addon write-back

**Notes:** Closes the §6 open question. Step 1 is a code-walk that documents the current authority model in the wiki; step 2 forces invoker authority on writes via a short-lived scoped delegation token rather than the addon's broad service-account credentials.

### T19 — Audit-trail tampering or bypass
**Status:** planned fix
**Issues:**
- [#341](https://github.com/ericfitz/tmi/issues/341) — `x-tmi-authz` middleware (forces audit emission as a per-route property)
- [#342](https://github.com/ericfitz/tmi/issues/342) — PATCH allowlist (rejects PATCH on `audit_entries` paths)
- [#356](https://github.com/ericfitz/tmi/issues/356) — feat(db): append-only DB-level constraint on `audit_entries` and `version_snapshots`

**Notes:** Three layers: (1) the middleware ensures every route declares whether it audits, (2) the PATCH allowlist removes the JSON-Patch path into audit tables, (3) the DB-level trigger is the last-line defense against direct DML. Hash-chain is documented as an option but not required for the first pass.

### T20 — WebSocket session abuse
**Status:** planned fix
**Issue:** [#351](https://github.com/ericfitz/tmi/issues/351) — feat(api): WebSocket JWT heartbeat re-validation and per-user connection caps

**Notes:** Verified: `SetReadLimit(65536)` is configured (per-message 64 KiB cap exists), but no per-user connection cap and no JWT re-validation after upgrade. Fix adds heartbeat-driven JWT/group-claim re-validation and hub-level connection caps. Closes the §6 open question on "Server-side WS auth path". Includes filing a tmi-ux bug to remove `?token=` from WS URLs (server already ignores it; client should stop leaking it to logs).

### T21 — Long-lived credential survival after deprovisioning
**Status:** planned fix
**Issue:** [#351](https://github.com/ericfitz/tmi/issues/351) — same WebSocket heartbeat re-validation work covers the long-lived-WS angle

**Notes:** REST-side already has `TokenBlacklist` and refresh-token rotation. The WebSocket gap (sessions outliving JWT revocation) is closed by the heartbeat re-validation. A separate larger effort to add IdP-deprovision webhooks / periodic re-validation of upstream SSO state is deliberately deferred — current controls plus heartbeat re-validation address the immediate exposure.

### T22 — Tenant data exfiltration via webhook payloads
**Status:** **not addressed in 1.4.0** — already risk-accepted in the threat model
**Evidence:**
- Threat-model column `status` is `risk_accepted`.
- Reasoning in row: "by-design integration channel; `webhook_url_validator`; HMAC signing proves origin not confidentiality".
- Webhooks are an opt-in feature configured by writers, who already have read access to the data. The exfiltration is not a confidentiality breach so much as an authorized export to a destination of the writer's choosing.

If we later restrict webhook subscription to owners (rather than writers), that decision belongs in a separate authz scoping discussion, not this threat model.

### T23 — Sensitive-data leak via observability
**Status:** planned fix
**Issue:** [#349](https://github.com/ericfitz/tmi/issues/349) — feat(observability): redact sensitive attributes in OTLP spans before export

**Notes:** Verified: `RedactSensitiveInfo` is applied to slogging ([internal/slogging/redaction.go:317](../internal/slogging/redaction.go#L317)) but **not** to OTLP spans. Adds a span processor that redacts known-sensitive attribute keys before export. The compose-stack component (Grafana/Jaeger/Prometheus on `0.0.0.0`) is already risk-accepted in §5 (compose is dev-only); this issue keeps it tidy by requiring auth in compose anyway.

### T24 — Server misconfiguration via hostile environment/config
**Status:** **not addressed in 1.4.0** — already risk-accepted in the threat model
**Evidence:**
- Threat-model column `status` is `risk_accepted`.
- Reasoning: "standard 12-factor pattern; relies on platform isolating env".

This threat is mitigated by deployment-platform isolation (Kubernetes secrets, Heroku config vars, etc.) which is the platform's job. The improvements to Terraform state and ESO under #344 (T6) tighten the 12-factor inputs incidentally.

### T25 — Information disclosure via verbose error responses
**Status:** planned fix
**Issue:** [#359](https://github.com/ericfitz/tmi/issues/359) — fix(api): eliminate CATS-discovered HTTP 500s on `/admin/surveys/{survey_id}`

**Notes:** Concrete CATS finding: 8× HTTP 500 on `/admin/surveys/{survey_id}` from the ExamplesFields fuzzer. Issue covers reproduction, error classification through `dberrors.Classify` and `StoreErrorToRequestError`, and regression tests. Addresses the Zero 500-Error Policy too. Other T25 evidence (43× schema-mismatch warnings on auth endpoints) was already closed by #295 / #298 / #299.

### T26 — Webhook/addon response-handling abuse
**Status:** planned fix
**Issues:**
- [#345](https://github.com/ericfitz/tmi/issues/345) — egress helper provides body caps and timeouts as defaults
- [#360](https://github.com/ericfitz/tmi/issues/360) — feat(api): webhook response-side caps and per-target circuit breaker

**Notes:** Verified gaps: no `Content-Length` check before stream, no `ResponseHeaderTimeout`, no per-endpoint concurrency cap. Fix adds response-side caps in the egress helper and per-target circuit-breaker logic in the webhook delivery worker.

### T27 — Workflow-authorization bypass (status / threat mutations)
**Status:** planned fix
**Issues:**
- [#341](https://github.com/ericfitz/tmi/issues/341) — `x-tmi-authz` middleware (gates on `security_reviewer` and `automation` roles)
- [#342](https://github.com/ericfitz/tmi/issues/342) — PATCH allowlist (gates `/status` and threat-sub-resource paths)

**Notes:** Verified: `IsSecurityReviewer` ([auth/service.go:178](../auth/service.go#L178)) and `IsServiceAccountRequest()` ([api/service_account_logging.go:43](../api/service_account_logging.go#L43)) exist but no handler gates threat-model `status` or `threat` mutations on them. `applyThreatModelBusinessRules` ([api/threat_model_handlers.go:1187](../api/threat_model_handlers.go#L1187)) only protects the reviewer's authorization slot. Both issues together close this gap.

## Issue index

| # | Title | Threats addressed |
|---|---|---|
| [#341](https://github.com/ericfitz/tmi/issues/341) | feat(api): default-deny authz middleware via `x-tmi-authz` OpenAPI extension | T2, T5, T18, T19, T27 |
| [#342](https://github.com/ericfitz/tmi/issues/342) | fix(api): replace PATCH `prohibitedPaths` blocklist with per-resource allowlist | T2, T19, T27 |
| [#343](https://github.com/ericfitz/tmi/issues/343) | fix(auth): allowlist `client_callback` on `/oauth2/authorize` | T16 |
| [#344](https://github.com/ericfitz/tmi/issues/344) | ops: encrypted Terraform state + External Secrets Operator + tfvars secret scanner | T6, T11 |
| [#345](https://github.com/ericfitz/tmi/issues/345) | feat(api): single egress helper with DNS-pinned fetch | T3, T26 |
| [#346](https://github.com/ericfitz/tmi/issues/346) | feat(auth): bind local accounts to (idp, sub) and add interactive identity-link flow | T1 |
| [#347](https://github.com/ericfitz/tmi/issues/347) | feat(infra): isolate document/content extractors in a sandboxed worker container | T12, T3 |
| [#348](https://github.com/ericfitz/tmi/issues/348) | ops: enforce `automountServiceAccountToken=false` consistently and scope IRSA/WIF | T10 |
| [#349](https://github.com/ericfitz/tmi/issues/349) | feat(observability): redact sensitive attributes in OTLP spans before export | T23 |
| [#350](https://github.com/ericfitz/tmi/issues/350) | feat(auth): exponential backoff lockout on `/oauth2/token` keyed on `client_id` | T15 |
| [#351](https://github.com/ericfitz/tmi/issues/351) | feat(api): WebSocket JWT heartbeat re-validation and per-user connection caps | T20, T21 |
| [#352](https://github.com/ericfitz/tmi/issues/352) | ci: gate builds on critical Dependabot alerts in security-sensitive paths | T4, T11 |
| [#353](https://github.com/ericfitz/tmi/issues/353) | feat(timmy): per-threat-model retrieval isolation and prompt-injection guards | T13 |
| [#354](https://github.com/ericfitz/tmi/issues/354) | feat(api): optimistic locking via version column and serialized ACL writes | T14 |
| [#355](https://github.com/ericfitz/tmi/issues/355) | feat(api): step-up authentication and out-of-band audit alerts on `/admin/*` writes | T7 |
| [#356](https://github.com/ericfitz/tmi/issues/356) | feat(db): append-only DB-level constraint on `audit_entries` and `version_snapshots` | T19 |
| [#357](https://github.com/ericfitz/tmi/issues/357) | audit(api): confidentiality coverage on nested and batch endpoints | T5 |
| [#358](https://github.com/ericfitz/tmi/issues/358) | feat(api): scoped delegation tokens for addon write-back | T18 |
| [#359](https://github.com/ericfitz/tmi/issues/359) | fix(api): eliminate CATS-discovered HTTP 500s on `/admin/surveys/{survey_id}` | T25 |
| [#360](https://github.com/ericfitz/tmi/issues/360) | feat(api): webhook response-side caps and per-target circuit breaker | T26 |
