# Vulnerability Scan Findings: TMI

Static source review of `/Users/efitz/Projects/tmi` (2026-06-12), scoped by `THREAT_MODEL.md`.
8 focus areas, 35 findings (8 HIGH / 17 MEDIUM / 10 LOW; 20 low-confidence after the
independent scoring pass). Sorted by confidence — the top of this file is the
highest-signal material. These are **static candidates, not verified vulnerabilities**;
run `/triage VULN-FINDINGS.json --repo /Users/efitz/Projects/tmi` next.

| id | conf | severity | category | location | title |
|---|---|---|---|---|---|
| F-001 | 0.9 | HIGH | result-forgery | api/result_consumer.go:175 | No per-component NATS subject scoping; any worker can publish results for any job |
| F-002 | 0.9 | HIGH | result-forgery | api/result_consumer.go:248 | Result envelope fields trusted without validation (unbounded ReasonDetail) |
| F-003 | 0.9 | MEDIUM | path-traversal | api/result_consumer.go:118 | ResultRef not bound to JobID before Object Store deletion |
| F-004 | 0.8 | HIGH | ssrf-bypass | auth/provider.go:130 | OAuth BaseProvider uses raw http.Client without redirect blocking |
| F-005 | 0.8 | HIGH | saml-validation | auth/saml/provider.go:259 | SAML LogoutRequest signature not verified |
| F-006 | 0.8 | HIGH | auth-bypass | auth/saml_manager.go:260 | SAML matches users by email only — missing cross-provider conflict check |
| F-007 | 0.8 | MEDIUM | secret-exposure | api/settings_service.go:346 | Secret-classified settings stored plaintext when encryption unconfigured |
| F-008 | 0.8 | MEDIUM | unbounded-read | auth/provider.go:227 | Unbounded io.ReadAll on OAuth provider responses |
| F-009 | 0.8 | MEDIUM | unbounded-recursion | pkg/extract/html.go:42 | Unbounded HTML recursion — duplicate copy runs in the monolith |
| F-010 | 0.7 | LOW | secret-exposure | api/config_handlers.go:503 | VisibilityInternal filtered from GET but not LIST; PUT accepts internal keys |
| F-011 | 0.7 | LOW | unbounded-recursion | pkg/extract/ooxml_common.go:233 | XML depth limit bypassed inside DecodeElement subtrees |
| F-012 | 0.6 | MEDIUM | token-exposure | api/content_token_repository.go:265 | Delegated-token GCM encryption lacks AAD row binding |
| F-013 | 0.6 | LOW | auth-bypass | api/websocket.go:2574 | Mid-session role revocation doesn't stop broadcast delivery |
| F-014 | 0.4 | MEDIUM | step-up-bypass | api/step_up_middleware.go:42 | Step-up route table is fail-open; coverage not CI-enforced |
| F-015 | 0.4 | MEDIUM | auth-bypass | cmd/server/jwt_auth.go:369 | WS ticket session_id cross-check skipped when param omitted |
| F-016 | 0.3 | HIGH | mass-assignment | api/patch_utils.go:377 | fixOwnerField fabricates User objects (downgraded: no escalation path) |
| F-017 | 0.3 | HIGH | ssrf-bypass | auth/oidc_discovery.go:60 | OIDC discovery raw client (downgraded: startup-config operator vector) |
| F-018 | 0.3 | MEDIUM | idor | api/threat_model_handlers.go:688 | Owner-change struct equality (downgraded: bypass logically impossible) |
| F-019 | 0.3 | MEDIUM | hmac-validation | api/webhook_delivery_handlers.go:256 | Status-update HMAC signs body only (replay marginal) |
| F-020 | 0.3 | MEDIUM | idor | api/websocket.go:2576 | Participants broadcast hardcodes "writer" (UI-truth only) |
| F-021 | 0.3 | MEDIUM | markdown-injection | pkg/extract/docx.go:862 | Unescaped hyperlink targets (no rendering consumer exists) |
| F-022 | 0.3 | MEDIUM | result-forgery | pkg/jobenvelope/envelope.go:68 | Reserved SourceURL fields unenforced (latent, verified dormant) |
| F-023 | 0.3 | LOW | secret-exposure | api/config_handlers.go:549 | Provider-secret masking via hardcoded suffix list (drift risk) |
| F-024 | 0.3 | LOW | unbounded-allocation | pkg/extract/xlsx.go:86 | XLSX caps decoupled (current defenses adequate) |
| F-025 | 0.2 | HIGH | cswsh | api/websocket.go:282 | Origin prefix matching (blocked by SameSite=Lax in practice) |
| F-026 | 0.2 | MEDIUM | token-exposure | api/content_oauth_provider_confluence.go:101 | Token to Confluence apiBase (hardcoded — non-finding) |
| F-027 | 0.2 | MEDIUM | toctou | api/result_consumer.go:85 | Lookup/terminal-flip ordering (verified handled safely) |
| F-028 | 0.2 | MEDIUM | hmac-validation | api/webhook_delivery_handlers.go:383 | GET HMAC signs delivery ID only (intended design) |
| F-029 | 0.2 | MEDIUM | oauth-flow | auth/handlers_oauth.go:445 | PKCE get-then-delete race (neutralized by verifier secrecy) |
| F-030 | 0.2 | MEDIUM | markdown-injection | pkg/extract/docx.go:765 | Unescaped image alt text (no rendering consumer exists) |
| F-031 | 0.2 | LOW | token-exposure | api/content_oauth_handlers.go:149 | PKCE challenge "leak" (public by design) |
| F-032 | 0.2 | LOW | step-up-bypass | api/step_up_middleware.go:40 | userAuthTime exists-flag ignored (current guards correct) |
| F-033 | 0.2 | LOW | hmac-validation | api/webhook_delivery_handlers.go:81 | Dual-auth HMAC-or-JWT (intentional, correctly implemented) |
| F-034 | 0.2 | LOW | auth-bypass | api/websocket.go:3408 | No per-op cell cap in 64KB frames (linear, authorized actor) |
| F-035 | 0.2 | LOW | sanitization-bypass | pkg/extract/html.go:39 | Raw-HTML fallback on parse error (practically unreachable) |

---

### F-001 — No per-component NATS subject scoping (HIGH, 0.9)
`api/result_consumer.go:175` · result-forgery

The result consumer subscribes to `jobs.result.>` and worker `Publish` (internal/worker/nats.go:127) has no authorization wrapper. No NATS accounts/creds restrict a worker to its own jobs' subjects, and `handleResult`/`MarkTerminal` accept any JobID with no publisher-identity check. Any compromised component can publish `jobs.result.{any_job_id}`, flipping other jobs' terminal state and firing their webhooks. This is the concrete code anchor for threat-model T19.

**Exploit:** a compromised chunk-embed worker publishes a Result for another user's in-flight job; the monolith marks it terminal and emits its webhook.
**Fix:** per-component NATS accounts/credentials scoping subjects (and Object Store buckets) per worker; make it a design requirement, not deployment config.
**Confidence:** code-verified; triage should merge with T19.

### F-002 — Result envelope fields trusted without validation (HIGH, 0.9)
`api/result_consumer.go:248` · result-forgery

The Result envelope is unmarshaled from untrusted NATS data with no field validation: `ReasonDetail` persists into `documents.access_reason_detail` (NullableDBText, **no size cap** — models.go:400, document_store_gorm.go:604-631); `ReasonCode`'s size:64 exists only at schema level; `JobID` is unbound to the publisher. NATS's default 1 MiB message cap is the only practical bound and is not enforced in code.

**Exploit:** repeated ~1 MiB ReasonDetail strings bloat the DB; forged status claims inject attacker text into diagnostics and webhook-visible state.
**Fix:** cap ReasonDetail (~4 KB), enum-validate ReasonCode, reject JobIDs not issued to the publishing component.
**Confidence:** all three unvalidated fields confirmed; matches the owner-confirmed "no result-blob validation" gap with concrete fields.

### F-003 — ResultRef not bound to JobID before Object Store deletion (MEDIUM, 0.9)
`api/result_consumer.go:118` · path-traversal

`handleResult` passes `res.Output.ResultRef` to `DeletePayload`; `payloadName` (internal/worker/nats.go:117-123) checks only the `TMI_PAYLOADS/` prefix, never that the name belongs to `res.JobID`. Blob names are `{jobID}/extracted` / `{jobID}/result`, so a forged ref targets any other in-flight job's blob.

**Exploit:** `Result{JobID:"job-xyz", ResultRef:"TMI_PAYLOADS/job-123/extracted"}` deletes job-123's blob, breaking that job.
**Fix:** require the object name to start with the envelope's own JobID; combine with F-001 bucket scoping.

### F-004 — OAuth BaseProvider raw http.Client without redirect blocking (HIGH, 0.8)
`auth/provider.go:130` · ssrf-bypass

BaseProvider's client has Timeout + otel transport only — no CheckRedirect, no SafeHTTPClient validation/IP pinning. Used for token exchange (lines 167, 219, 339) and userinfo (line 278); Go follows up to 10 redirects by default, so a configured/compromised provider endpoint can redirect requests carrying `client_secret` to internal targets. Inconsistent with the #345 "all direct http.Client callers migrated to SafeHTTPClient + lint guard" control claimed in the threat model.

**Fix:** route through SafeHTTPClient or set CheckRedirect to refuse redirects; extend the #345 lint guard to the auth package.
**Confidence:** raw client confirmed; requires operator/admin endpoint config, but redirects reach targets the admin never configured.

### F-005 — SAML LogoutRequest signature not verified (HIGH, 0.8)
`auth/saml/provider.go:259` · saml-validation

`ProcessLogoutRequest` (lines 259-286) parses via `xml.Unmarshal` and checks only issuer match and NameID presence — no signature verification, unlike the login path's `ParseXMLResponse`. The handler comment in auth/handlers_saml.go (~320) falsely claims "(includes signature verification)"; sessions are invalidated from the unverified NameID (lines 330-347). No IssueInstant validation.

**Exploit:** forged LogoutRequest with the victim's email as NameID → targeted unauthenticated session revocation (forced-logout DoS).
**Fix:** verify the LogoutRequest signature against the IdP cert before invalidating anything; fix the misleading comment.

### F-006 — SAML email-only user matching, no cross-provider check (HIGH, 0.8)
`auth/saml_manager.go:260` · auth-bypass

`processUser` matches existing users via `GetUserByEmail` without checking the matched user's bound provider. The OAuth path explicitly rejects this (`errCrossProviderConflict`, handlers_oauth_user.go:146-152) with tiered (provider, sub) matching; SAML skips that flow, and #383's identity-link hardening covers OAuth only. A SAML assertion bearing a victim's email from any configured IdP yields a JWT for the victim's OAuth-created account.

**Exploit:** victim uses Google OAuth; an attacker-influenced SAML IdP (sub-realm control, compromise, or misconfigured trust) asserts the victim's email → full account takeover.
**Fix:** mirror OAuth's tiered matching and cross-provider conflict rejection in SAML.

### F-007 — Plaintext secret-classified settings; no write-time enforcement (MEDIUM, 0.8)
`api/settings_service.go:346` · secret-exposure

`Set` encrypts only when the encryptor is configured+enabled; the Secret classification gates API masking, not encryption. The startup check (`warnIfPlaintextSecretsAtRest`) is log-only — ERROR in production but never aborts. `ReEncryptAll` is a manual admin endpoint that fails unless encryption was already enabled, so secrets written before enablement persist plaintext with no automatic remediation.

**Fix:** reject/force-encrypt Secret-classified writes when encryption is off; refuse production startup with plaintext secret rows; auto-re-encrypt on key configuration.

### F-008 — Unbounded io.ReadAll on OAuth provider responses (MEDIUM, 0.8)
`auth/provider.go:227` (also :327) · unbounded-read

`customTokenExchange` and `fetchEndpoint` read entire response bodies with no LimitReader and no transport cap. A single response from a hostile/compromised provider endpoint is fully allocated in monolith memory.

**Fix:** `io.LimitReader(resp.Body, 1<<20)` at both sites; audit the auth package for siblings.

### F-009 — Unbounded HTML recursion; duplicate copy in the monolith (MEDIUM, 0.8)
`pkg/extract/html.go:42` (duplicate: `api/timmy_content_provider_http.go:82`) · unbounded-recursion

`extractTextFromHTML` recurses over the parse tree with no depth limit; `x/net/html.Parse` builds full-depth trees, and the 10 MiB fetch cap still allows ~1M nested elements. Go stack overflow is an unrecoverable fatal error. The pkg/extract copy crashes a sandboxed worker (contained); the timmy_content_provider_http.go copy **runs in the monolith API process**.

**Exploit:** attacker-hosted deeply-nested HTML registered as a content source crashes the API server.
**Fix:** iterative walk (explicit stack) or depth counter in both copies; deduplicate the implementations.

### F-010 — VisibilityInternal LIST/PUT asymmetry (LOW, 0.7)
`api/config_handlers.go:503` · secret-exposure

GET rejects VisibilityInternal keys, but `mergeSettingsWithConfig` filters only config-derived settings — DB rows merge unfiltered into LIST responses. `UpdateSystemSetting` never checks visibility classification, so an admin can PUT a VisibilityInternal key and read it via LIST, bypassing the policy GET/DELETE enforce.

**Fix:** filter dbSettings by visibility in the merge; reject VisibilityInternal in PUT; add a regression test.

### F-011 — XML depth limit bypassed in DecodeElement subtrees (LOW, 0.7)
`pkg/extract/ooxml_common.go:233` · unbounded-recursion

`boundedXMLDecoder` enforces depth in `Token()` but `DecodeElement` consumes subtrees internally — the code's own comment admits the gap. Backstopped only by Go 1.20+'s internal ~10k XML depth limit (undocumented) and worker isolation.

**Fix:** Token() loop under the bounded decoder for untrusted subtrees, or explicit depth tracking across DecodeElement.

### F-012 — Delegated-token GCM encryption lacks AAD binding (MEDIUM, 0.6)
`api/content_token_repository.go:265` · token-exposure

`ContentTokenEncryptor` Seal/Open pass nil AAD; ciphertexts aren't bound to their row. A write-limited DB primitive (e.g. UPDATE-only SQLi that can copy but not decrypt) can graft user A's encrypted delegated token into user B's row, and B's flows then use A's Google Drive/MS365/Confluence token.

**Fix:** AAD = userID + ":" + providerID in Seal/Open; re-encrypt existing rows.

### F-013 — Role revocation doesn't stop WS broadcasts (LOW, 0.6)
`api/websocket.go:2574` · auth-bypass

Broadcast delivery never re-checks authorization after connect (explicit comment at 2567-2568); the heartbeat re-checks JWT expiry only. Mutations are re-checked per operation, but a revoked user keeps receiving diagram broadcasts (incl. confidential content) until disconnect/inactivity (300s default). Concrete instance of threat-model T17.

**Fix:** re-validate threat-model access + blacklist on the heartbeat; close 4403 on failure.

### F-014 — Step-up route table fail-open, coverage unenforced (MEDIUM, 0.4)
`api/step_up_middleware.go:42` · step-up-bypass

Routes absent from the table skip step-up (fail-open). All 31 current /admin/* writes are covered by the default policy; nothing enforces spec↔router sync, so future drift silently bypasses freshness checks.

**Fix:** fail-closed default for /admin/* writes + a coverage test.

### F-015 — WS ticket session_id cross-check optional (MEDIUM, 0.4)
`cmd/server/jwt_auth.go:369` · auth-bypass

The ticket↔query session cross-check runs only when `?session_id=` is present, contradicting the ws-ticket-auth design. Mitigants verified: 32-byte random single-use 30s-TTL tickets, and post-upgrade `ValidateSessionAccess` still authorizes the user against the URL resource — residual impact is session-integrity, not cross-resource access.

**Fix:** make the cross-check mandatory (absent or mismatched → reject).

### F-016 — fixOwnerField fabricates User objects (HIGH severity as filed, 0.3)
`api/patch_utils.go:377` · mass-assignment — **downgraded by verification**: actor is already the resource owner, fabricated owner is in-memory only, FK constraints and post-store audit ordering fail closed. Robustness bug; reject string-typed /owner at validation for type hygiene.

### F-017 — OIDC discovery raw http.Client (HIGH as filed, 0.3)
`auth/oidc_discovery.go:60` · ssrf-bypass — **downgraded**: issuer URLs come only from startup config file/env (operator vector), not runtime-mutable settings. Fix for invariant consistency with #345 (CheckRedirect refusal), exploit path weak.

### F-018 — Owner-change struct equality (MEDIUM as filed, 0.3)
`api/threat_model_handlers.go:688` · idor — **downgraded**: a true owner change always differs in provider_id, so the gate-skip direction is logically impossible; fragility only over-triggers the stricter gate. Code quality fix: canonical-identity comparison helper.

### F-019 — Status-update HMAC signs body only (MEDIUM, 0.3)
`api/webhook_delivery_handlers.go:256` · hmac-validation — cross-subscription replay is blocked (secret resolved via the delivery's own subscription); secret-holders can mint signatures anyway. Marginal: bind deliveryID into the MAC payload.

### F-020 — Participants broadcast hardcodes "writer" (MEDIUM, 0.3)
`api/websocket.go:2576` · idor — UI-truth bug only; REST participant path resolves real roles and mutation authz is enforced server-side per operation. Fix the broadcast to reuse `getSessionPermissionsForUser`.

### F-021 / F-030 — DOCX markdown injection: hyperlink targets / image alt text (MEDIUM, 0.3 / 0.2)
`pkg/extract/docx.go:862` / `:765` · markdown-injection — unescaped concatenation is real, but extracted markdown feeds only the fenced LLM/RAG pipeline; document endpoints never return it and no markdown→HTML renderer exists server-side. Immunize anyway: escape `[]()` and validate URL schemes at extraction time.

### F-022 — Reserved SourceURL/SourceSecretRef envelope fields (MEDIUM, 0.3)
`pkg/jobenvelope/envelope.go:68` · result-forgery — verified dormant (no consumer code path). Matches the threat model's existing tripwire: when source-locator mode ships, validate against the component's egress class and gate SourceSecretRef per-component.

### F-023 — Provider-secret masking via hardcoded suffix list (LOW, 0.3)
`api/config_handlers.go:549` — all current secrets covered; audit redactor has broader fallback. Drive masking from the classification registry to remove the drift risk.

### F-024 — XLSX caps decoupled (LOW, 0.3)
`pkg/extract/xlsx.go:86` — three independent caps verified adequate (excelize part limits gate shared-strings; markdown cap trips first). Documentation/clarity only.

### F-025 — WS Origin prefix matching (HIGH as filed, 0.2)
`api/websocket.go:282` · cswsh — `HasPrefix` admits `example.com.attacker.com`, **but** WS auth derives from the JWT cookie which is SameSite=Lax (auth/cookies.go:54): cross-site pages can't send it, so the CSWSH chain breaks at authentication. Fix the comparison to exact scheme+host+port anyway — it's one cookie-policy change away from load-bearing.

### F-026–F-035 — Verified non-findings / hygiene notes (0.2)
- **F-026** Confluence `apiBase` is a hardcoded constant — the operator-misconfiguration premise is false.
- **F-027** Result-consumer TOCTOU — guarded UPDATE + sentinel handling verified safe.
- **F-028** Delivery-status GET HMAC — intended dual-auth design; signature scope matches the authz it implements.
- **F-029** PKCE get-then-delete race — in-memory mutex store (not shared Redis); verifier secrecy neutralizes it.
- **F-031** PKCE challenge exposure — public by design (RFC 7636); verifier never leaves the token-endpoint path.
- **F-032** userAuthTime exists-flag — current guards (`!ok || authTime == nil`) verified correct.
- **F-033** Dual-auth HMAC-or-JWT — explicit branch, no weaker-leg confusion.
- **F-034** 64KB WS frames without cell cap — linear cost by an authorized writer; deprioritized category.
- **F-035** Raw-HTML fallback on parse error — unreachable with in-memory readers; non-HTML consumers.

---

*Generated by /vuln-scan. Candidates only — `/triage` performs rigorous verification and is where false positives are actually removed. For execution-verified findings, use the vuln-pipeline (C/C++ targets) or equivalent dynamic testing.*
