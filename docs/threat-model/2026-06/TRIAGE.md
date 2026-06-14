# Triage Report

35 in → 3 duplicates, 22 false positives, **10 confirmed** (4 HIGH / 5 MEDIUM / 1 LOW), 2 need manual test.

Context: interactive; environment = internet-facing web service (TMI threat-modeling platform; worker-published NATS messages are a designed-for interior untrusted boundary); scoring = derived HIGH/MEDIUM/LOW from preconditions; 3-vote adversarial verification, recall tie-break.

Target: `/Users/efitz/Projects/tmi`. All components route to **Eric Fitzgerald** (sole committer; no CODEOWNERS).

## Act on these

### [HIGH] SAML matches users by email only — cross-provider account takeover  (f006)
`auth/saml_manager.go:260` | auth-bypass | claimed HIGH (alignment +4) | confidence 9.0/10
**Owner:** component: auth/; Eric Fitzgerald (sole committer)
**Verdict:** exploitable, votes 3 TP / 0 FP
**Preconditions (4):** SAML enabled with ≥1 IdP configured; attacker can obtain a signed assertion bearing the victim's email from ANY configured IdP (multi-tenant IdP, user-editable mail attribute, less-trusted secondary IdP); victim account exists (e.g. OAuth-created); POST /saml/acs is pre-auth by design and SAML hardcodes EmailVerified=true.
**Threat-model match:** Account takeover via federated auth
**Why:** `processUser` matches existing users by bare email (`GetUserByEmail`, saml_manager.go:260) with no provider binding and mints tokens for the match (saml_manager.go:249). The OAuth path explicitly rejects this exact case as account takeover (`errCrossProviderConflict`, handlers_oauth_user.go:144-152 — the issue #290 comment describes the attack verbatim); the SAML path skips that tiered matcher entirely. A signed assertion from any configured SAML IdP bearing a victim's email yields the victim's account JWT.
**Fix shape:** mirror OAuth's tiered (provider, sub) matching + cross-provider rejection in SAML; stop hardcoding EmailVerified.
**Reachability evidence:** auth/saml_manager.go:219; auth/handlers_saml.go:240

### [HIGH] No per-component NATS scoping — any worker can forge results for any job  (f001)
`api/result_consumer.go:175` | result-forgery | claimed HIGH (alignment +4) | confidence 8.0/10
**Owner:** component: api/ + deployments/k8s/platform NATS config; Eric Fitzgerald
**Verdict:** exploitable, votes 3 TP / 0 FP
**Preconditions (3):** code execution on an extraction worker (the designed-for attacker — it parses hostile documents) or any pod with reach to the cluster-internal NATS service; NATS at shipped default config (verified: `deployments/k8s/platform/nats.yml` has no authorization block; workers connect credential-less, internal/worker/nats.go:45); victim JobIDs (free — observed on unscoped subjects).
**Threat-model match:** Cross-user threat-model data leakage (T19)
**Why:** The consumer binds `jobs.result.>` and trusts the payload JobID with no publisher identity, signature, or subject cross-check; `MarkTerminal` even bare-inserts terminal rows for unknown ids. A forged envelope flips any user's job terminal, fires their webhook, suppresses the real result (consumes the emit-once transition), and deletes arbitrary payload blobs via `Output.ResultRef`. This is the concrete instance of the threat model's T19 gap — and the control is missing at exactly the boundary the worker sandbox was built around.
**Fix shape:** per-component NATS accounts/creds scoping subjects + buckets (design-level, not deployment config); fold in f003's ResultRef↔JobID binding and f002's field validation.
**Reachability evidence:** cmd/extractor/handler.go:145; cmd/server/main.go:1075; api/result_consumer.go:254

### [HIGH] Result envelope fields persisted and served with zero validation  (f002)
`api/result_consumer.go:248` | result-forgery | claimed HIGH (alignment +4) | confidence 8.0/10
**Owner:** component: api/; Eric Fitzgerald
**Verdict:** exploitable, votes 3 TP / 0 FP
**Preconditions (4):** NATS publish access (worker compromise / unscoped reach); hostile document path to worker compromise; victim JobIDs (free once inside); valid object_ref for blob deletion.
**Threat-model match:** Worker-boundary result forgery (T19)
**Why:** No `ValidateResult` exists — `pkg/jobenvelope/validate.go` covers the Job direction only, and the isolation design spec's promised envelope validation is unimplemented. `ReasonDetail` persists unbounded into the TEXT `access_reason_detail` column (document_store_gorm.go:630, GORM hooks deliberately bypassed) and is served RAW to authenticated clients when `reason_code="other"` (access_diagnostics.go:118-120) — attacker-controlled content injection through the worker boundary into victims' API responses.
**Fix shape:** validate the Result envelope (status enum, ReasonCode allowlist, ReasonDetail size cap ~4KB, JobID↔subject cross-check) before any persistence.
**Reachability evidence:** cmd/server/main.go:1074; api/result_consumer.go:184

### [HIGH] OAuth token/userinfo fetches on raw redirect-following clients; auth/ escaped the SafeHTTPClient lint  (f004)
`auth/provider.go:130` | ssrf-bypass | claimed HIGH (alignment +4) | confidence 8.0/10
**Owner:** component: auth/ (+ scripts/check-direct-http-client.py); Eric Fitzgerald
**Verdict:** exploitable, votes 3 TP / 0 FP
**Preconditions (4):** a configured non-OIDC provider endpoint that is hostile/compromised, OR an admin-settings write to token_url/userinfo (runtime-mutable MutabilityHot DB settings, validated only for non-emptiness); trigger is any unauthenticated /oauth2/authorize→callback flow; server can reach internal targets; 307/308 re-sends the client_secret POST body.
**Threat-model match:** Account takeover via federated auth
**Why:** Verifiers found it worse than filed: beyond the redirect-following raw client (no CheckRedirect, no IP pinning) used for client_secret-bearing token POSTs (provider.go:219) and userinfo (319/339), the standard exchange at provider.go:167 uses bare `http.DefaultClient` — no timeout at all — because `oauth2.HTTPClient` is never injected. The mandatory SafeHTTPClient control exists and is lint-enforced, but `scripts/check-direct-http-client.py:66` globs only `api/*.go`, so the auth package silently escaped the #345 migration that the threat model credits as complete.
**Fix shape:** route all auth-package egress through SafeHTTPClient (or CheckRedirect-refuse + validator); extend the lint to auth/; inject oauth2.HTTPClient.
**Reachability evidence:** auth/handlers_oauth.go:33; auth/handlers_providers.go:199

### [MEDIUM] Unbounded HTML recursion runs in the monolith — one hostile page can fatally crash the API server  (f009)
`pkg/extract/html.go:42` | unbounded-recursion | claimed MEDIUM (alignment +4) | confidence 8.0/10
**Owner:** component: pkg/extract/; Eric Fitzgerald
**Verdict:** needs_manual_test, votes 3 TP / 0 FP
**Preconditions (4):** authenticated account; attacker-staged text/html endpoint; page within the 50 MiB fetch cap carrying ~10–17M nesting levels; default monolith pipeline wiring (no flag).
**Threat-model match:** none (availability)
**Why:** Verification corrected the scan: the cited `api/timmy_content_provider_http.go` copy is test-only dead code, but the identical `pkg/extract` walker is LIVE in the monolith (cmd/server/main.go:1432 → content_pipeline.go:248, reached from access_poller.go:206 and timmy_content_provider.go:106). `HTMLExtractor` implements no `BoundedExtractor`, so the deadline wrapper is skipped — and Go stack exhaustion is unrecoverable anyway. Aggravator: the access poller re-fetches registered sources, so the server can crash-loop on restart until the row is purged.
> Recommend a human build a PoC in a sandbox: whether a 50 MiB payload clears the 1 GB goroutine-stack ceiling (~64–112 B/frame × 10–17M frames) straddles the margin; static reasoning hit its limit.
**Fix shape:** iterative walk (explicit stack) or depth cap in `extractTextFromHTML`; delete the dead duplicate; implement `Bounded()` for HTML.
**Reachability evidence:** api/content_pipeline.go:248; cmd/server/main.go:1432

### [MEDIUM] SAML LogoutRequest signature not verified — unauthenticated targeted forced logout  (f005)
`auth/saml/provider.go:259` | saml-validation | claimed HIGH (alignment −2: impact is logout, not takeover) | confidence 8.0/10
**Owner:** component: auth/saml/; Eric Fitzgerald
**Verdict:** exploitable, votes 3 TP / 0 FP
**Preconditions (4):** SAML enabled + provider configured; IdP entityID (public metadata); victim email (OSINT; endpoint returns success for unknown users → enumeration side channel); victim has active sessions.
**Threat-model match:** none (availability harassment)
**Why:** `ProcessLogoutRequest` validates only an issuer string match and NameID presence — no signature, no IssueInstant, no replay protection — while the login path uses `ParseXMLResponse` with full validation. The handler comment at handlers_saml.go:320 falsely claims signature verification. GET/POST /saml/slo is unauthenticated (`security:[]` in the spec) and invalidates all sessions for the attacker-supplied NameID (handlers_saml.go:330-347).
**Fix shape:** verify LogoutRequest signatures against the IdP cert before invalidating anything; add IssueInstant freshness; fix the misleading comment.
**Reachability evidence:** auth/handlers_saml.go:321

### [MEDIUM] Role revocation doesn't stop WS broadcasts — revoked insider reads confidential edits up to JWT lifetime  (f013)
`api/websocket.go:2574` | auth-bypass | claimed LOW (alignment −1: window is 1h, not 300s) | confidence 7.7/10
**Owner:** component: api/ (websocket); Eric Fitzgerald
**Verdict:** exploitable, votes 3 TP / 0 FP
**Preconditions (5):** prior legitimate access; active WS session at revocation time; revocation mid-session; heartbeats within 300s keep the session alive (cap = remaining JWT lifetime, default 3600s); collaborators actively editing.
**Threat-model match:** Cross-user threat-model data leakage (is_confidential boundary) — raised LOW→MEDIUM
**Why:** Broadcast delivery sends full diagram_operation_event content to every client in `s.Clients` with no per-recipient re-check (explicit trust comment at websocket.go:2567-2568); the heartbeat re-validates JWT expiry only; no code path prunes WS membership on ACL change (the only CloseSession caller is the host leave-session endpoint). Mutations ARE re-checked per op — the read-side asymmetry is an oversight, and the leak fires at exactly the moment trust is withdrawn.
**Fix shape:** re-validate threat-model access + token blacklist on the heartbeat tick; close 4403 on failure (matches the threat model's planned T17 mitigation).
**Reachability evidence:** api/websocket.go:1670; api/websocket_diagram_handler.go:192; api/websocket.go:2310

### [MEDIUM] Secret-classified settings stored plaintext when encryption unconfigured (fail-open, log-only)  (f007)
`api/settings_service.go:346` | secret-exposure | claimed MEDIUM (alignment +4) | confidence 7.0/10
**Owner:** component: api/ + internal/crypto; Eric Fitzgerald
**Verdict:** exploitable, votes 2 TP / 1 FP (dissent: documented opt-in design)
**Preconditions (4):** deployment without the encryption key despite the production ERROR log (fail-open; encryptor init failure also fails open, main.go:628-633); secrets actually written via admin API or dbtool import; separate DB/backup compromise; auth.jwt.secret DB-resident for the maximal chain.
**Threat-model match:** Cross-user threat-model data leakage (via JWT-secret token forgery)
**Why:** `Set` encrypts only when the encryptor is enabled; the Secret classification drives API masking, not write-time enforcement. The startup check is documented as "must not abort startup"; ReEncryptAll is manual and errors unless encryption was pre-enabled, so plaintext rows persist silently. At stake: auth.jwt.secret, OAuth client secrets, SAML SP keys, vault token, LLM keys.
**Fix shape:** refuse (or force-encrypt) Secret-classified writes when encryption is off; fail startup in production with plaintext secret rows; auto-re-encrypt on key configuration.
**Reachability evidence:** api/config_handlers.go:638

### [MEDIUM] Unbounded reads of OAuth provider responses (gzip-amplified, drivable by unauthenticated flows)  (f008)
`auth/provider.go:227` | unbounded-read | claimed MEDIUM (alignment +4) | confidence 7.0/10
**Owner:** component: auth/; Eric Fitzgerald
**Verdict:** exploitable, votes 3 TP / 0 FP
**Preconditions (3):** a configured provider is hostile/compromised/MITM'd; attacker self-initiates callback flows (own PKCE verifier — no TMI auth needed); gzip bombs and/or concurrency to beat the 10s bandwidth×time bound.
**Threat-model match:** none (availability)
**Why:** `io.ReadAll` with no LimitReader at provider.go:227/327, uncapped JSON decode at 233/333, body duplicated into log+error strings (~3× peak), no transport cap, and no explicit Accept-Encoding so Go transparently gunzips — a small compressed body expands to multi-GB allocation in-window. The repo's own `readCappedBody` helper (api/safe_http_client.go:338) is unused here.
**Fix shape:** `io.LimitReader` (1 MiB) at all four sites; same SafeHTTPClient migration as f004.
**Reachability evidence:** auth/handlers_token.go:141

### [LOW] Provider-secret masking list diverges from the classification registry  (f023)
`api/config_handlers.go:549` | secret-exposure | claimed LOW (alignment +4) | confidence 7.0/10
**Owner:** component: api/ + internal/config; Eric Fitzgerald
**Verdict:** needs_manual_test, votes 2 TP / 1 FP (dissent: the DB row may be inert dead data)
**Preconditions (3):** step-up-gated admin session (or admin response logs/HAR/proxy captures); a `content_oauth.providers.<id>.client_secret` row manually PUT into the DB; the row holds a live secret (runtime loads content OAuth from config only, so a DB copy may never be consumed).
**Threat-model match:** none
**Why:** `isProviderSecretKey` (config_handlers.go:27-72) masks only `auth.oauth.providers.` / `auth.saml.providers.` prefixes with four hardcoded suffixes, and is the only mask on the DB-sourced GET/LIST path. `content_oauth.providers.<id>.client_secret` is Secret:true (migratable_settings.go:664-668) and VisibilityAdminOnly but escapes the prefix list — returned plaintext, violating the registry's own contract that the Secret flag drives API-response masking.
> Recommend checking a running deployment for live secrets in that key family; static reasoning can't settle whether the row exists/is live.
**Fix shape:** drive masking from `ClassificationFor(key).Secret` instead of a parallel hardcoded list.
**Reachability evidence:** api/config_handlers.go:27; api/config_handlers.go:549

## Dropped

| id | title | file:line | why dropped |
|---|---|---|---|
| f030 | Unescaped image alt text in DOCX markdown | pkg/extract/docx.go:765 | duplicate of f021 |
| f032 | userAuthTime exists-flag ignored | api/step_up_middleware.go:40 | duplicate of f014 |
| f033 | Dual-auth HMAC-or-JWT delivery-status | api/webhook_delivery_handlers.go:81 | duplicate of f028 |
| f003 | ResultRef not bound to JobID before blob delete | api/result_consumer.go:118 | not_actionable (rule 13, 2-1): compromised worker already has direct unrestricted bucket delete; deputy adds no capability. Fold the binding into f001's fix |
| f010 | VisibilityInternal LIST/PUT asymmetry | api/config_handlers.go:503 | implausible_trigger/not_actionable (rule 13): bootstrap keys never legitimately reach the DB; PUT of real secrets 409-blocked; admin reads back own value |
| f011 | XML depth bypass in DecodeElement subtrees | pkg/extract/ooxml_common.go:233 | already_handled (rule 13): string-target decodes use iterative Decoder.Skip — no recursion exists; 10k stdlib cap backstops |
| f012 | Delegated-token GCM lacks AAD binding | api/content_token_repository.go:265 | implausible_trigger (rule 13, 2-1): requires a key-less DB-write primitive that doesn't exist (all GORM-parameterized). Still cheap hardening: AAD = userID:providerID |
| f014 | Step-up route table fail-open | api/step_up_middleware.go:42 | already_handled (rule 13): router and step-up spec are the same oapi-codegen artifact — drift structurally impossible |
| f015 | WS ticket session_id cross-check optional | cmd/server/jwt_auth.go:369 | already_handled (rule 13): post-upgrade ValidateSessionAccess re-authorizes against the URL resource; tickets 32B/single-use/30s |
| f016 | fixOwnerField fabricates User objects | api/patch_utils.go:377 | intentional_behavior (rule 3): owner-only string→User shim; store resolves against existing users and rolls back; effects fire post-store only |
| f017 | OIDC discovery raw http.Client | auth/oidc_discovery.go:60 | implausible_trigger (rule 8): startup-only fetch of file/env-config issuers; DB-mutable issuer never reaches the discovery client |
| f018 | Owner-change struct equality | api/threat_model_handlers.go:688 | misread_code (rule 13): a real owner change can never compare struct-equal; only fail-closed over-triggering possible |
| f019 | Status-update HMAC signs body only | api/webhook_delivery_handlers.go:256 | implausible_trigger (rule 13): replay confined to the secret-holder's own subscription; they can mint signatures anyway |
| f020 | Participants broadcast hardcodes "writer" | api/websocket.go:2576 | nuisance (rule 12): write-only display metadata; mutations independently authorized per op |
| f021 | Unescaped hyperlink targets in DOCX markdown | pkg/extract/docx.go:862 | LLM-prompt-only sink (rule 6): no markdown→HTML renderer exists; content never returned by any endpoint. Cheap immunization: escape + scheme-validate at extraction |
| f022 | Reserved SourceURL envelope fields | pkg/jobenvelope/envelope.go:68 | not_actionable (rule 13): zero consumers; documented forward-compat; already a threat-model tripwire |
| f024 | XLSX caps decoupled | pkg/extract/xlsx.go:86 | already_handled (rule 13, 2-1): all allocations bounded by configured constants. Dissent flagged possible shared-string amplification (excelize internals) — engineering note |
| f025 | WS Origin prefix matching | api/websocket.go:282 | already_handled (rule 13): WS auth is ticket-only (?ticket= param; cookies/Bearer unreachable), tickets unobtainable cross-site. Fix HasPrefix→exact match anyway — one cookie-policy change from load-bearing |
| f026 | Token to Confluence apiBase | api/content_oauth_provider_confluence.go:101 | doesnt_exist: apiBase is a compile-time constant; the configurability premise is false |
| f027 | Result-consumer TOCTOU | api/result_consumer.go:85 | already_handled (rule 16): emit-once is DB-enforced; sentinel handling deliberate (#438); worst case is one lost webhook on crash |
| f028 | Delivery-status GET HMAC signs ID only | api/webhook_delivery_handlers.go:383 | intentional_behavior (rule 13): no second consumer of the scheme (format-disjoint payloads); secret-holder is the authorized party |
| f029 | PKCE get-then-delete race | auth/handlers_oauth.go:445 | theoretical-only (rule 16): in-memory mutex store; duplicated challenge is a public hash; verifier secrecy + single-use codes neutralize |
| f031 | PKCE challenge "leak" | api/content_oauth_handlers.go:149 | intentional_behavior (rule 3): challenge is public per RFC 7636; error paths emit static codes/bodyless 500s; verifier never exposed |
| f034 | No per-op cell cap in 64KB WS frames | api/websocket.go:3408 | volumetric by authorized writer (rule 1, 2-1). Engineering note from dissent: per-cell-op full-diagram save under the HUB-GLOBAL updateMutex (websocket.go:52,341) is O(N²) write amplification that serializes all diagram updates server-wide — worth fixing as a perf bug |
| f035 | Raw-HTML fallback on parse error | pkg/extract/html.go:39 | implausible_trigger (rule 13): html.Parse never errors on content with in-memory readers — dead code; consumers non-HTML-sensitive |
