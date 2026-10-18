# Bugs #1099-#1102 and Documentation Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix four server bugs found by the 2026-10-17 documentation audit, then bring the specs, README, design records and wiki in line with the fixed code.

**Architecture:** Each bug is an independent branch off `main` with its own failing-test-first cycle and PR. The documentation branch is the only one that edits `api-schema/tmi-openapi.json` and regenerates `api/api.go`, so the bug branches never conflict on generated code. Wiki edits are local commits in `/Users/efitz/Projects/tmi.wiki`, pushed only after the matching fixes merge.

**Tech Stack:** Go, Gin, GORM (PostgreSQL + Oracle ADB), oapi-codegen v2.7.1, testify.

**Spec:** the GitHub issues themselves (#1089-#1102; text saved at `/private/tmp/claude-501/-Users-efitz-Projects-tmi/44b92830-f998-4e5e-946b-91153c6dccb7/scratchpad/issues.txt`) plus the code investigation in `/private/tmp/claude-501/-Users-efitz-Projects-tmi/44b92830-f998-4e5e-946b-91153c6dccb7/scratchpad/investigation.md`.

## Human decisions (Eric, 2026-10-17)

- **#1100:** triage endpoints allow only Security Reviewers (non-confidential responses) and Confidential Project Reviewers (confidential responses), regardless of ownership. Ownership grants no triage access.
- **Q14 (#1089):** option A. Drop "or administrators" from the seven restore descriptions; the code (owner only) is the intended behavior.
- **POSTGRES_PASSWORD** in `deployments/k8s/dev/{k3s,docker-desktop}/postgres.yml` is an intended local-dev default; no change.

## Global Constraints

- Make targets only (`make test-unit name=TestX count1=true`, `make lint`, `make verify`); never `go test`/`go run` directly.
- Done gate: `make verify`. Show its last lines.
- Bug branches must not edit `api-schema/tmi-openapi.json`, `api/api.go`, `.version`, `api/version.go`.
- Zero 500s; only status codes the spec documents for that operation.
- Logging via `internal/slogging` only; log identifiers and outcomes, never values.
- Add or update `SEM@` markers on functions you add or change (`/sem-annotate --update <files>` afterward is fine).
- Conventional commit, PR title `fix(<scope>): ...` (PATCH bump). Commit body `Fixes #NNNN`.
- American English.

## Review Focus

1. #1100: a user in **both** reviewer groups sees both confidential and non-confidential responses; a user in neither sees an empty list (200), never another user's response.
2. #1100: the submitter keeps full intake access (`/intake/survey_responses/*`, including intake triage_notes reads) after the change.
3. #1100: reviewer group membership that comes only from an IdP group claim (mapped, not a direct `group_members` row) still grants triage access.
4. #1102: a production server whose DB already holds an unknown `timmy.llm_provider` refuses to start with an actionable error, rather than failing later per request.
5. #1099: a duplicate-key violation on feedback create returns 400 (409 is not documented for those operations), with the raw DB error logged but not returned.

---

### Task 1: #1099 map feedback constraint violations to 400

Branch `fix/1099-feedback-constraint-400`.

**Files:**
- Modify: `api/usability_feedback_handlers.go:273-285` (`mapDBError`)
- Test: the existing feedback handler test file (`api/usability_feedback_handlers_test.go` / `api/content_feedback_handlers_test.go`; create a focused `mapDBError` test if none fits)

**Behavior:** `mapDBError` gains `errors.Is(err, dberrors.ErrConstraint)` → `InvalidInputError("request violates a data constraint")` (400 `invalid_input`) before the 500 fallback. `ErrDuplicate` wraps `ErrConstraint`, so one branch covers both. Log the classified error at Warn with request context; never put the raw DB message in the response. Do not reuse `StoreErrorToRequestError` (`api/request_utils.go:589`): its 409 branch is undocumented for these operations.

- [ ] Write a failing table test: `mapDBError` on `dberrors.ErrConstraint`, on an error wrapping `dberrors.ErrDuplicate`, plus the existing NotFound→404, Transient→503 and an unclassified error→500. Add a handler-level test for `POST /usability_feedback` and `POST /threat_models/{id}/feedback` with a store stub returning `ErrDuplicate`, asserting 400 `invalid_input`.
- [ ] Run `make test-unit name=TestMapDBError count1=true` (and the handler test name): FAIL.
- [ ] Implement the branch.
- [ ] Re-run: PASS.
- [ ] **Fixes (B):** the investigation lists handler-local mappers with no constraint branch: `api/admin_user_credentials_handlers.go:93,:338`; `api/client_credentials_handlers.go:176,:262,:365`; `api/config_handlers.go:843`; `api/survey_handlers.go:1635`. For each, decide whether a constraint error is reachable and whether a 400 (or 409) is documented for that operation (`jq` on the spec). Fix the reachable ones, with a test each; list the rest with a one-line reason in the final report.
- [ ] `make verify`; commit `fix(api): map feedback constraint violations to 400 instead of 500` with `Fixes #1099`.

### Task 2: #1100 triage access by reviewer group and confidentiality

Branch `fix/1100-triage-reviewer-authz`.

**Files:**
- Create: `api/survey_triage_access.go` (+ `_test.go`)
- Modify: `api/survey_handlers.go` (triage list ~1044-1105, get ~1129, patch ~1141, create_threat_model ~1486), `api/server_survey.go:145-182` (triage metadata and triage_notes routes), `api/triage_note_handlers.go:44` (only for triage routes; intake reads keep `RequireSurveyResponseAccess`), and the survey response store list query if a confidentiality filter is added
- Create: `docs/superpowers/specs/2026-10-17-adr-triage-access-by-reviewer-group.md`

**Interfaces (produces):**
- `func triageReviewerGroup(isConfidential bool) BuiltInGroup`: returns `GroupConfidentialProjectReviewers` or `GroupSecurityReviewers` (pure).
- `func RequireTriageAccess(c *gin.Context, responseID openapi_types.UUID) (*SurveyResponse, bool)`: loads the response; if missing or the caller is not a member (`IsGroupMemberFromContext`, `api/group_membership.go:106`) of `triageReviewerGroup(resp.IsConfidential)`, writes 404 `not_found` (the existing non-disclosure convention, T5 #357) and returns false. Ownership and the row ACL are ignored. No admin bypass. Match the real signatures of the existing helpers it replaces.
- For the list: resolve the caller's membership in both groups once. Neither → 200 with an empty page. One → only responses whose `is_confidential` matches. Both → no confidentiality filter. Prefer pushing the `is_confidential` filter into the store query so pagination totals stay correct; that store change needs `oracle-db-admin` review.

**Out of scope:** response ACL seeding (`api/survey_response_store_gorm.go:137-190`) stays unchanged, because intake depends on the submitter's owner row. No spec change: 403/404 are already documented and the list returns 200.

- [ ] Write failing tests with users: submitter `alice` (no reviewer group), `sr` (Security Reviewers), `cpr` (Confidential Project Reviewers), `both`. Use non-confidential response N and confidential response C, both submitted by alice. Assert:
  - alice: list empty; get/patch/create_threat_model/metadata/triage_notes on N → 404.
  - sr: sees N not C; can patch N; C → 404.
  - cpr: sees C not N.
  - both: sees N and C.
  - alice's intake get/patch on N still succeeds, and so does her intake triage_notes read.
  - A reviewer whose membership comes from a mapped IdP group claim passes (use the membership-context path, not a direct row, if the test infrastructure allows; otherwise note why).
- [ ] Run the tests: FAIL.
- [ ] Implement `survey_triage_access.go`, switch every triage handler to it, and add the list filter.
- [ ] Re-run: PASS. Run the existing survey tests too, and fix any that encoded the old owner-based triage access, keeping intake assertions.
- [ ] Write the ADR, labeled "Human decision (Eric, 2026-10-17)": the rule, the 404 non-disclosure choice, no admin bypass, the submitter's owner ACL kept for intake only, and why a triage guard was chosen over an `x-tmi-authz` role (a static role gate cannot see per-row confidentiality).
- [ ] `oracle-db-admin` review if the store query changed.
- [ ] `make verify`; commit `fix(api): restrict survey triage to reviewer groups by confidentiality` with `Fixes #1100`.

### Task 3: #1101 rate limit GET /oauth2/step_up

Branch `fix/1101-step-up-rate-limit`.

**Files:**
- Modify: `api/rate_limit_middleware.go:119-132` (`isAuthFlowEndpoint`)
- Test: `api/rate_limit_middleware_test.go` (`TestIsAuthFlowEndpoint` ~706, `TestAuthFlowRateLimitMiddleware` ~568)

- [ ] Add `/oauth2/step_up` cases: `TestIsAuthFlowEndpoint` expects true, and `TestAuthFlowRateLimitMiddleware` drives step_up from one IP past the limit and expects 429 `rate_limit_exceeded`.
- [ ] Run: FAIL.
- [ ] Add `/oauth2/step_up` to the auth-flow path set. The set is the centralized list; if it is a switch, keep it one list. Confirm that `AuthFlowRateLimitMiddleware` (`api/ip_and_auth_rate_limit_middleware.go:92`) consults that function. If it has its own list, unify them.
- [ ] Run: PASS.
- [ ] **Do not** change the per-user limiter's `user_id` key. That limiter is dead on all REST routes (the JWT middleware sets `userID`/`userInternalUUID`), and turning it on changes behavior for every client, so it gets its own issue (the orchestrator files it). Note it in the commit body.
- [ ] `make verify`; commit `fix(auth): rate limit GET /oauth2/step_up` with `Fixes #1101`.

### Task 4: #1102 enforce validateTimmy

Branch `fix/1102-enforce-validate-timmy`.

**Files:**
- Modify: `internal/config/config.go:1077-1095` (comment; export or wrap as needed), `cmd/server/main.go` (boot after `config.Load` ~1944 and after the settings service is ready ~952; dumper install ~1773-1785, fix the false comment), `api/settings_service.go` (`Set` ~499-567: per-key semantic validation hook)
- Test: `internal/config/*_test.go`, `api/settings_service_test.go`, and a test for the boot check (extract it into a testable function, for example `validateTimmyAtStartup(cfg, timmyCfg, buildMode) error`)

**Behavior:**
- **Boot:** run `validateTimmy` on the env/YAML config right after `Load`. After the settings service is up, run the same checks on the effective Timmy config from `TimmyConfigProvider.Get` (DB-resolved). On error: `slogging.Get().Error(...)` naming the key, then `os.Exit(1)`. This covers the production dump flag (env/YAML or DB) and an unknown `llm_provider` (from either source).
- **Runtime writes:** `SettingsService.Set` rejects `timmy.dump_extracted_text_to_note=true` when `isProductionMode()`, and any `timmy.llm_provider` value that `ValidateLLMProvider` rejects. The error returns through the existing path to `400 invalid_input` (`api/config_handlers.go:706-712`). Implement it as one small key→validator map in the settings service (single enforcement point), not handler checks.
- Development builds keep accepting the dump flag.

- [ ] Write failing tests:
  - startup check: production + dump flag (env/YAML) → error; production + dump flag (DB-resolved) → error; development + dump flag → nil; unknown provider → error.
  - settings `Set`: production + dump=true → error; development + dump=true → ok; provider "bogus" → error; "openai"/"anthropic"/"" → ok.
  - handler `PUT` system setting → 400 for those cases.
- [ ] Run: FAIL.
- [ ] Implement.
- [ ] Run: PASS.
- [ ] `make verify`; commit `fix(config): enforce Timmy validation at startup and on settings writes` with `Fixes #1102`.

### Task 5: repo documentation (#1089, #1090, #1091)

Branch `docs/1089-1091-doc-alignment`, started after Tasks 1-4 are committed so the docs describe the fixed behavior. Rebase onto main after they merge, and re-run `make generate-api` instead of hand-resolving `api/api.go`.

- **#1089 (spec):** Q2 status enum; Q13 DELETE collaborate description; Q14 option A (drop "or administrators" in the 7 restore descriptions); Q16 `ClientCallbackQueryParam` description; Q10 spec part, add `/api/server-info` to the OpenAPI spec (read the handler for its schema and auth) and `/ws/notifications` to the AsyncAPI spec; Q11 add the interim note only (vocabulary decision stays with #1076); Q44 AsyncAPI channel. Then `make validate-openapi`, `make validate-asyncapi`, `make generate-api`.
- **#1090 (design records in `docs/superpowers/specs`):** add as-built status notes per Q1, Q3, Q4, Q6, Q7, Q8, Q9, Q12, reflecting Tasks 1-4. For Q1, migrate `docs/survey-api-design.md` to the wiki (Task 6) and delete it from `docs/`. For Q12, document reality.
- **#1091 (README):** Q10 rewrite Data Storage, Configuration and WebSocket URL sections. Also fix the stale `Store[T]` sentence in `.claude/CLAUDE.md` (Storage section).
- [ ] `make verify`; commit `docs(api): align specs, README and design records with server behavior` with `Fixes #1089`, `Fixes #1090`, `Fixes #1091`.

### Task 6: wiki follow-ups (#1095 and the wiki parts of #1089-#1091)

Local commits in `/Users/efitz/Projects/tmi.wiki`, pushed only after the matching PRs merge.

- #1095 (Configuration Model, Timmy): describe the enforcement from Task 4.
- #1090 Q1: Survey/Intake wiki page from `docs/survey-api-design.md`, corrected to the live paths and the Task 2 triage rule.
- #1089 Q10/Q13: the four `/ws/diagrams/` URL examples, the Database-Setup diagram, and the REST-API-Reference collaborate DELETE sentence (Q13).
- Feedback-APIs: 400 for constraint violations (Task 1), 413 row for content feedback (Q6).
