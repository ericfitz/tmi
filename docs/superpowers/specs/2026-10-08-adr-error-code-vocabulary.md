# ADR: One documented error-code vocabulary for `Error.error`

- Status: Accepted
- Date: 2026-10-08
- Decision maker: Eric Fitzgerald (human decisions, 2026-10-08)
- Issue: #1048
- Related: `2026-09-28-adr-versioning-docs-skip-and-schema-decoupling.md` (schema version rules), `2026-10-03-adr-adopt-version-bump-bot.md`

## Context

The `error` field of the `Error` response schema (`api-schema/tmi-openapi.json`, `#/components/schemas/Error`) is documented only as "Error code" with a single example (`invalid_request`). On `main` at d5bdfb1e the server emits 90 distinct codes from 679 literal sites in `api/`, `auth/` and `cmd/server/`, several of which name the same condition: `invalid_input` / `invalid_request` / `validation_failed` / `validation_error` / `invalid_format`; `invalid_id` / `invalid_uuid`; `server_error` / `internal_error` / `internal_server_error`; `too_many_requests` / `rate_limit_exceeded`. `api/admin_quota_handlers.go` (21 sites) and `internal/slogging/middleware.go` put human sentences in the code slot, and about 42 spec response examples do the same. Clients cannot rely on the codes, and code, spec and wiki disagree.

Two constraints shaped the options. `api` imports `auth` (31 files), so shared constants cannot live in `api`. Protocol endpoints (`/oauth2/*`, `/saml/*`, `/.well-known/*`) must keep RFC 6749/7009/6750/9470 codes, and `invalid_request` on those routes means something different from a REST validation failure.

## Human-made decisions (Eric, 2026-10-08)

1. **Two tiers.** `Error.error` becomes a closed enum of REST codes, each naming one HTTP semantic (20 in the proposal Eric approved; planning found seven production sites that return 422, so `unprocessable_entity` is added as the 21st, since statuses do not change). A domain-specific reason a client can act on goes in `details.code` (already in the schema), keeping its current string. The HTTP status of an existing response never changes as part of this work.
2. **Not a breaking change.** The field had no documented vocabulary, and no tmi-ux code path or Postman assertion references a renamed code. The PR title carries no `!`: schema 2.0.0 -> 2.1.0, server MINOR.
3. **Separate `OAuthError` schema for protocol routes.** It carries the RFC codes, the documented TMI extension codes, and the transport codes route-agnostic middleware can emit. The only normalization on protocol routes is `too_many_requests` -> `rate_limit_exceeded` (and `service_unavailable` -> RFC `temporarily_unavailable` in `auth/handlers_token_helpers.go`).
4. **WebSocket codes are a separate follow-up.** `ErrorMessage.error` in the AsyncAPI spec has the same problem and an existing client/server mismatch (tmi-ux expects `permission_denied` / `validation_error`; the server sends `insufficient_permissions` / `invalid_message`). A server issue plus a paired tmi-ux issue are filed after this lands; nothing in #1048's PR changes WebSocket message codes.
5. **`invalid_patch` stays distinct from `invalid_input`.** A client receiving it should resync and rebuild the patch rather than fix a field.

## Vocabulary

### Tier 1: REST `Error.error` (enum)

| Code | Status | Meaning |
|---|---|---|
| `invalid_input` | 400 | Body, field, header or query value fails validation (incl. pagination bounds, JSON syntax) |
| `invalid_id` | 400 | A path or query identifier is malformed (UUID, slug) |
| `invalid_patch` | 400 | JSON Patch document malformed, targets a disallowed path, or cannot be applied |
| `unauthorized` | 401 | Missing, expired or invalid credentials |
| `insufficient_user_authentication` | 401 | Step-up required (RFC 9470) |
| `forbidden` | 403 | Authenticated but not permitted (incl. protected objects, self-deletion) |
| `not_found` | 404 | Resource or route does not exist, or is hidden by authorization |
| `method_not_allowed` | 405 | |
| `not_acceptable` | 406 | |
| `conflict` | 409 | State conflict: duplicate, in use, wrong lifecycle state |
| `gone` | 410 | Permanently removed (audit retention) |
| `version_mismatch` | 409 | `If-Match` does not match the current version; reload |
| `payload_too_large` | 413 | |
| `unsupported_media_type` | 415 | Content-Type not accepted |
| `unprocessable_entity` | 422 | Well-formed request that cannot be processed in the current state (provider not registered, embedding dimension mismatch) |
| `if_match_required` | 428 | `If-Match` header missing |
| `rate_limit_exceeded` | 429 | Transient limit; honor `Retry-After` |
| `quota_exceeded` | 403 or 429 | Hard cap reached; retrying does not help |
| `server_error` | 500 | Unexpected failure, including panic recovery |
| `not_implemented` | 501 | Operation not supported by this server |
| `service_unavailable` | 503 | Dependency (DB, cache, provider) unavailable; retry later |

### Tier 1b: protocol routes (`OAuthError.error`)

Routes: `/oauth2/{authorize,token,refresh,revoke,introspect,userinfo,callback,step_up}`, `/saml/*`, `/.well-known/*`. `/oauth2/content_callback`, `/oauth2/providers*` and `/me/identities/link/*` are TMI-defined and use Tier 1.

- RFC codes: `invalid_request`, `invalid_client`, `invalid_grant`, `unauthorized_client`, `unsupported_grant_type`, `invalid_scope`, `access_denied`, `unsupported_response_type`, `server_error`, `temporarily_unavailable` (RFC 6749), `unsupported_token_type` (RFC 7009), `insufficient_user_authentication` (RFC 9470). `invalid_token` and `insufficient_scope` are `WWW-Authenticate` header values (RFC 6750, `internal/wwwauth`), not body codes.
- TMI extensions (RFC 6749 §8.5 permits them): `identity_mismatch`, `account_conflict`, `email_not_verified`, `provider_unreachable`, `provider_response_invalid`, `invalid_provider`.
- Transport codes emitted by route-agnostic middleware before dispatch: `unauthorized`, `not_found`, `method_not_allowed`, `not_acceptable`, `rate_limit_exceeded`, `server_error`. `unicode_validation_middleware.go` already chooses `invalid_request` on `/oauth2/token` and `invalid_input` elsewhere by path; that stays.

### Tier 2: `details.code` (open list, lowercase snake_case)

Current strings are kept and nested under the Tier 1 code for their existing status (statuses verified on `main` d5bdfb1e):

| Status / Tier 1 code | `details.code` values |
|---|---|
| 400 `invalid_input` | `picker_file_id_mismatch`, `invalid_picker_registration`, `invalid_challenge`, `invalid_provider_type`, `client_callback_required`, `client_callback_not_allowed`, `invalid_if_match`, `invalid_version`, `duplicate_header`, `unsupported_encoding`, `reserved_key` |
| 401 `unauthorized` | `token_not_linked_or_failed`, `invalid_token` (identity-link token) |
| 403 `forbidden` | `protected_group`, `provider_mismatch` |
| 404 `not_found` | `session_not_found`, `feature_not_available` |
| 409 `conflict` | `duplicate_group`, `duplicate_membership`, `self_deletion`, `protected_user`, `deletion_blocked`, `encryption_not_enabled`, `unreadable_settings_limit`, `session_not_active` |
| 422 `unprocessable_entity` | `provider_not_registered`, `provider_not_configured`, `content_token_provider_not_configured`, `dimension_mismatch`, `inconsistent_dimensions`, `no_source`, `access_request_not_supported` |
| 429 `rate_limit_exceeded` | `too_many_connections`, `session_full`, `message_rate_limit`, `duplicate_invocation` |
| 429 `quota_exceeded` | `session_limit_exceeded` |
| 500 `server_error` | `websocket_upgrade_failed` |
| 503 `service_unavailable` | `llm_busy`, `llm_not_configured` |

New reasons may be added without a schema version bump; the schema example `COLLABORATION_SESSION_NOT_FOUND` is corrected to snake_case.

## Mapping (production sites on `main` d5bdfb1e)

| Old | New | Sites |
|---|---|---|
| `invalid_request` on REST routes | `invalid_input` | 40 |
| `invalid_uuid` | `invalid_id` | 11 |
| `validation_failed`, `validation_error` | `invalid_input` | 7 + 2 |
| `invalid_format`, `patch_failed` | `invalid_patch` | 2 + 7 |
| `invalid_limit`, `invalid_offset` | `invalid_input` | 4 + 4 |
| `internal_error`, `internal_server_error` | `server_error` | 6 + 2 |
| identity link `invalid_token` (401), `invalid_provider`, `temporarily_unavailable` | `unauthorized` + `details.code`, `invalid_input`, `service_unavailable` | 6 + 1 + 1 |
| `too_many_requests` (protocol) | `rate_limit_exceeded` | 4 |
| `too_many_connections`, `session_full`, `message_rate_limit` | `rate_limit_exceeded` + `details.code` | 3 |
| `session_limit_exceeded` | `quota_exceeded` + `details.code` | 1 |
| 36 other domain codes | Tier 1 code for the current status + `details.code` | ~36 |
| sentence-as-code bodies | proper code + `error_description` | 21 + 1 |

About 160 sites change wire value; about 510 keep their value and only switch to constants. tmi-ux branches only on `insufficient_user_authentication`, `identity_mismatch` and OAuth `access_denied`, all unchanged; `src/app/generated/api-types.d.ts` and the tmi-clients SDKs are regenerated from the spec.

## Enforcement

- `internal/errcode`: a dependency-free leaf package (like `internal/wwwauth`) holding `type Code string`, one constant per Tier 1, 1b and 2 value, and the three sets (`REST`, `Protocol`, `Details`). `RequestError.Code` is an `errcode.Code`; every Tier 1 code has a constructor in `api/request_utils.go`.
- `scripts/check-error-codes/main.go` (`go/ast`), wired into `make lint` via `scripts/lint.py`: fails on any string literal in `Error{Error: ...}`, `RequestError{Code: ...}`, `gin.H{"error": ...}`, `map[string]string{"error": ...}` or `RespondWithError(_, _, lit, ...)` in non-generated, non-test Go files. Codes must be `errcode` constants.
- `internal/errcode/spec_test.go`: parses `api-schema/tmi-openapi.json` and asserts `Error.error.enum` equals the REST set, `OAuthError.error.enum` equals the protocol set, every response-example `error` value is a member of the schema it is declared under, and `details.code` examples are snake_case.
- Build gates unchanged: `make verify`, `make validate-openapi`, `make generate-api`, `make test-integration`.

## Consequences

- oapi-codegen generates `ErrorError` / `OAuthErrorError` string types with type-prefixed constants (`ErrorErrorServerError`). Handlers use `internal/errcode`, not those constants, so agreement between code and spec rests on the drift test; an untyped string literal still compiles against either type, which is why the lint exists.
- openapi-typescript narrows `Error['error']` to a union in tmi-ux; a comparison against a code outside the set becomes a compile error there, which is the intended signal.
- The 30 protocol responses that `$ref` `Error` move to `OAuthError`; the 132 inline protocol error schemas are pointed at it too.
- Adding a Tier 1 code is a spec change (schema MINOR); adding a `details.code` reason is not.
- Wiki pages `API-Integration` (Error Handling), `API-Overview` (Error Responses) and `REST-API-Reference` get the Tier 1 and 1b tables after merge.
- Follow-ups: WebSocket `ErrorMessage.error` enum (server issue) and the paired tmi-ux issue for `dfd-collaboration.service.ts` and `websocket-message.types.ts`; a tmi-ux PR that regenerates `api-types.d.ts`; regenerate tmi-clients.

## Amendment (planning, 2026-10-08): implementation-time decisions

Labeled as implementation-time decisions, not among Eric's human-made decisions above.

1. **Two legacy domain codes stay at top level.** tmi-ux branches on top-level `error` for `feature_not_available` (404, `content-token.service.ts:34`) and `content_token_provider_not_configured` (422, `content-token.service.ts:151`). Moving them to `details.code` alone would break those paths, contradicting decision 2 (non-breaking). They stay as top-level `error` values, are added to the `Error.error` enum as documented exceptions (23 members: 21 Tier 1 plus these two), and are also set in `details.code` so clients can migrate. The 422 body keeps its top-level `provider_id`. They move to `details.code` only in a future breaking change; a paired tmi-ux issue is filed after merge.
2. **Unicode validation middleware.** The ADR stated that `unicode_validation_middleware.go` already chose `invalid_request` on `/oauth2/token` and `invalid_input` elsewhere. It emitted `invalid_request` on every route. Implementation adds the route-class switch: REST routes return `invalid_input`, protocol routes (`/oauth2/{authorize,token,refresh,revoke,introspect,userinfo,callback,step_up}`, `/saml/*`, `/.well-known/*`) keep `invalid_request`.
3. **Sentence-as-code bodies** were more numerous than counted (webhook handlers, `auth/` protocol handlers, `http.StatusText` in `api/middleware.go`); all now carry a vocabulary code with the sentence in `error_description`. Protocol routes use `invalid_request` (400), `not_found` (404), `server_error` (500) and `temporarily_unavailable` (503).
4. **Review fixes (implementation-time).** (a) `version_mismatch` is HTTP 409 in the server (`api/optimistic_locking.go`), not 412; the table above is corrected and the unused 412 constructor removed. (b) The eight `saml_*` codes emitted on `/saml/*` (`saml_error`, `saml_not_enabled`, `saml_unavailable`, `saml_provider_not_found`, `saml_metadata_error`, `saml_init_error`, `saml_invalid_logout_request`, `saml_logout_error`) are kept (non-breaking) and documented in `OAuthError` as TMI extension codes under RFC 6749 section 8.5. (c) Route-agnostic middleware can also emit `payload_too_large` (413) and `unsupported_media_type` (415) on protocol routes, so both join the transport codes in `OAuthError`. (d) Protocol operations no longer `$ref` the REST shared responses: each shared error response used by a protocol route has an `OAuth*` variant bound to `OAuthError`, and a drift test requires every 4xx/5xx JSON response on a protocol route to resolve to `OAuthError`.
