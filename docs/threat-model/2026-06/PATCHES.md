# Candidate Patches

> **Static review only.** These diffs were authored and reviewed by
> independent agents reading source. They were NOT compiled, run, or
> re-attacked. Read each diff yourself before applying — see
> `docs/patching.md#reviewing-generated-patches` for what to look for.

**Input:** TRIAGE.json (verified findings) · **Repo:** /Users/efitz/Projects/tmi · 10 findings → 10 diffs, 10/10 reviewer-ACCEPTed

Apply order note: bug_01 and bug_02 touch overlapping regions of `api/result_consumer.go` (different hunks: subject/ref binding vs envelope validation). Apply bug_02 first, then bug_01, and resolve the small context drift in `makeCallback` by hand — the validation check and the subject check both belong immediately after the `json.Unmarshal`. bug_03 and bug_08 both touch `auth/provider.go` in disjoint regions (client construction vs body reads) and compose cleanly.

---

## bug_00: [HIGH] SAML email-only matching — cross-provider account takeover  (f006)

`auth/saml_manager.go:260` · auth-bypass · owner: auth/ (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 9/10
**Diff:** `PATCHES/bug_00/patch.diff` (2 files: tiered-matching rewrite + regression test)

**Rationale:** Ports the OAuth path's #290 tiered matcher to SAML — (provider, sub) → linked identity → (provider, email) → email-only restricted to sparse records, rejecting cross-provider matches with the existing `errCrossProviderConflict`.
**Variants:** logout NameID lookup (no minting), OAuth path (reference), api/identity.go resolveByEmail (post-auth), saml_user_handlers (own check) — all safe.
**Bypass:** rogue second IdP → Tier 3 rejects; case-skewed email → fail-closed; sparse record → bound on first contact (OAuth-identical semantics).
**Test:** `TestProcessSAMLUser_CrossProviderEmailMatchRejected` (fails pre-fix by returning the victim's user).

---

## bug_01: [HIGH] NATS result forgery — subject/ref binding + per-component creds hook  (f001)

`api/result_consumer.go:175` · result-forgery · owner: api/ + deployments (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 8/10
**Diff:** `PATCHES/bug_01/patch.diff` (4 files)

**Rationale:** (1) Terms any result whose `jobs.result.<id>` subject ≠ payload JobID; (2) `worker.PayloadRefForJob` binds blob refs to the owning job before any DeletePayload (result + DLQ paths); (3) `TMI_NATS_CREDS` hook so deployments can issue per-component NATS identities.
**Residual (explicit):** a forger publishing on the victim's exact subject with a matching payload still succeeds until NATS authorization is enabled server-side — the creds hook is the enabler; pair this patch with a NATS `authorization` block in `deployments/k8s/platform/nats.yml`. Blob deletion is fully closed regardless.
**Test:** subject-mismatch Term, foreign-ref skip, DLQ variant, adversarial ref-aliasing table. Note: two existing test fixtures used blob names matching no real producer and were corrected.

---

## bug_02: [HIGH] Result envelope validation + ReasonDetail sanitization  (f002)

`api/result_consumer.go:248` · result-forgery · owner: api/ + pkg/jobenvelope (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 8/10
**Diff:** `PATCHES/bug_02/patch.diff` (3 files)

**Rationale:** New `jobenvelope.ValidateResult` (status enum, ReasonCode ≤64B + charset, ResultRef bound) + `SanitizeResult` (ReasonDetail → valid UTF-8, control-stripped, 4 KB rune-boundary truncation) called before `handleResult`; Term-on-invalid; DLQ path gated with `Validate(job)`. Truncate-not-reject avoids redelivery-loop wedging; invalid UTF-8 previously guaranteed a Nak loop via the Postgres write.
**Bypass:** semantic forgery (plausible fake diagnostics) is inherent to the channel — this removes the unbounded-persistence, status-confusion, loop, and escape-injection primitives.
**Test:** ValidateResult table + 4 sanitizer tests in `pkg/jobenvelope/envelope_test.go`.

---

## bug_03: [HIGH] OAuth clients refuse redirects; oauth2/oidc pinned; lint extended to auth/  (f004)

`auth/provider.go:130` · ssrf-bypass · owner: auth/ + scripts/ (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 8/10
**Diff:** `PATCHES/bug_03/patch.diff` (5 files)

**Rationale:** `refuseRedirects` CheckRedirect on the provider client; `oauth2.HTTPClient` injected so `Exchange` never falls back to `http.DefaultClient` (no timeout!); bonus third hole closed — `oidc.NewProvider`/JWKS pinned via `oidc.ClientContext` (verified present in go-oidc v3.18.0); TestProvider + DiscoveryClient hardened; lint now scans `auth/*.go`.
**Residual (explicit):** direct 200-response SSRF via an admin-set internal token_url is NOT stopped (no redirect involved) — follow-up: extract SafeHTTPClient to `internal/safehttp` so auth/ gets dial-time IP pinning (also defeats DNS rebinding). Reviewer also flagged: lint glob is non-recursive, so `auth/saml/provider.go:439` metadata fetch remains unscanned (separate finding).
**Test:** six redirect-refusal tests with hit-counting target servers — the counter is the load-bearing assertion.

---

## bug_04: [MEDIUM] Iterative HTML traversal — removes the monolith stack-exhaustion crash  (f009)

`pkg/extract/html.go:42` · unbounded-recursion · owner: pkg/extract/ (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 9/10
**Diff:** `PATCHES/bug_04/patch.diff` (3 files)

**Rationale:** Recursive closure → iterative pre-order walk via the tree's own Parent/NextSibling links (O(1) extra memory; an explicit stack would re-introduce depth-proportional allocation). Reviewer independently traced semantic equivalence and climb-loop nil/termination safety. Both copies fixed (the api/ copy is test-only dead code — deleting it is a reasonable follow-up).
**Test:** 100k-deep nesting (fatally crashes the process pre-fix — a true regression guard) + 50k-sibling wide tree.

---

## bug_05: [MEDIUM] SAML LogoutRequest signature verification  (f005)

`auth/saml/provider.go:259` · saml-validation · owner: auth/saml/ (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 8/10
**Diff:** `PATCHES/bug_05/patch.diff` (3 files)

**Rationale:** Mandatory enveloped-signature verification against IdP metadata certs (etree+goxmldsig, both already in the module graph; crewjam v0.5.1 exports no LogoutRequest validator), unmarshaling only the verified subtree (anti-wrapping); IssueInstant freshness (±5 min); Destination check; 100 KB cap; fixes the false "includes signature verification" comment.
**Apply-time actions:** run `go mod tidy` (etree/goxmldsig promote from `// indirect`); no positive signed-path test was feasible statically — verify against your IdP in staging. HTTP-Redirect binding is rejected (documented; the pre-patch code never supported it either).
**Test:** forged-unsigned rejection, no-certs fail-closed, wrong-root rejection.

---

## bug_06: [MEDIUM] WS heartbeat read-access re-check  (f013)

`api/websocket.go:2574` · auth-bypass · owner: api/ websocket (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 8/10
**Diff:** `PATCHES/bug_06/patch.diff` (2 files)

**Rationale:** `checkReadPermission` (mirrors `checkMutationPermission` at RoleReader, fail-closed) called from the existing 30s WritePump heartbeat next to the JWT-expiry check; closes 4403 on revocation. Latency drops from ≤3600s to ≤30s; per-tick cost is a Redis-cached Get.
**Reviewer finding worth tracking separately:** the notifications WS (`/ws/notifications`) broadcasts threat-model metadata to ALL authenticated clients with no resource-level authorization at any point, and its CheckOrigin returns true unconditionally — file as a new finding.
**Test:** grant→revoke→deny regression + nil/anonymous/missing-TM edge cases.

---

## bug_07: [MEDIUM] Write-time secret⇒encrypted gate  (f007)

`api/settings_service.go:346` · secret-exposure · owner: api/ (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 8/10
**Diff:** `PATCHES/bug_07/patch.diff` (2 files)

**Rationale:** Set() now refuses secret-classified writes in production when encryption is unavailable (nil or disabled-passthrough encryptor — one shared variable drives gate and encrypt branch), warn-and-allow in dev, mirroring the startup check's severity scaling. `isSecretSettingKey` unions the registry Secret flag with provider-suffix knowledge incl. the content_oauth family the registry's prefix classes exclude.
**Noted for follow-up:** `cmd/dbtool/config.go:127` has the same fail-open shape (deliberate offline migration — not patched).
**Test:** production rejection (registry + all three provider families), disabled-passthrough rejection, dev warn-allow, non-secret negative control.

---

## bug_08: [MEDIUM] Cap all OAuth/OIDC provider response reads  (f008)

`auth/provider.go:227` · unbounded-read · owner: auth/ (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 9/10
**Diff:** `PATCHES/bug_08/patch.diff` (4 files)

**Rationale:** All six unbounded read sites in auth/ capped: 1 MiB LimitReader (post-gzip-inflation) on the four JSON decodes, 2 KiB capped read with explicit truncation marking on both error-logging paths (which previously ~tripled peak allocation). Helper mirrors api's readCappedBody, package-local due to the import cycle. Brings the custom exchange path to parity with x/oauth2's internal 1 MiB cap.
**Test:** under/at/over-cap helper tests + fail-closed truncated-decode property.

---

## bug_09: [LOW] Registry-union secret masking  (f023)

`api/config_handlers.go:549` · secret-exposure · owner: api/ + internal/config (Eric Fitzgerald)
**Status:** static_review_only · review ACCEPT · style 9/10
**Diff:** `PATCHES/bug_09/patch.diff` (2 files)

**Rationale:** Important correction to the triage recommendation: registry-only masking would have UNMASKED every auth-provider secret (the registry deliberately prefix-classifies provider subtrees Secret:false). The fix is a union — `shouldMaskSettingValue = ClassificationFor(key).Secret || expanded prefix+suffix heuristic` — at both DB-path call sites, a verified strict superset of the old predicate. Bonus: exact-classified DB secrets (timmy.*_api_key) were also plaintext on the DB path and are now masked.
**Test:** predicate tables + end-to-end GET masking test asserting `<configured>` and plaintext absence.

---

## Skipped

None — all 10 verified findings produced reviewer-accepted diffs.
