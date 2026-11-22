# Plan: fix every 5xx from CATS run 20261121T201842Z (dev/1.21/settings-fixes)

Eric (2026-11-21): fix all 5xx on this branch before the PR. Zero-500 policy; documented status codes only.

Evidence: `test/results/cats/cats-results-20261121T201842Z.db` (latest.db). Reproduced against k3s-rp on 2026-11-21.

## Human decisions

- **threat_model_framework is an enum** (Eric, 2026-11-21): CIA, STRIDE, LINDDUN, DIE, PLOT4ai, enforced on every write (spec `enum`, create, PUT, PATCH). Omitted/empty keeps today's default behavior. Recorded in `docs/superpowers/specs/2026-11-21-adr-threat-model-framework-enum.md`.
- **PATCH keeps the diagram `image.svg` maxLength (102400)** (Eric, 2026-11-21): PATCH enforces the same schema as PUT; an oversized thumbnail is a client bug (ericfitz/tmi-ux#1027).
- **PR title is `fix!:`** (Eric, 2026-11-21): the request contract tightened; server 1.22.0, schema 3.0.0.

## Implementation choices (not human decisions)

- ThreatBase.status maxLength 256 -> 128, matching the `threats.status` varchar(128) column, ThreatModelBase.status (128) and `ValidateStatusLength` (128).

## Tasks

### Task 1: reject NUL bytes in query parameters centrally (400)
Repro: `GET /teams?name=a%00b`, `/admin/users?email=a%00b`, `/admin/groups?provider=t%00mi`, `/projects?status=x%00`, `/admin/audit/system?field_path=x%00`, `/admin/audit/threat_models?actor_provider=t%00mi`, `/saml/providers/tmi/users?email=x%00` all return 500; PostgreSQL rejects 0x00 (SQLSTATE 22021) in the LIKE filter.
Fix: one central enforcement point (middleware or the existing `internal/unicodecheck` path, if it covers bodies but not query strings) that returns 400 for a NUL (and other C0 control chars if the existing body policy rejects them) in any query parameter value, before handlers run. Must be DB-independent (Oracle may accept NUL). Confirm 400 is documented on every affected operation; add it to the spec where missing.
Tests: failing test first; table test over the affected routes.

### Task 2: authorization subjects that do not resolve must be a 400 before any insert
Repro: `POST /threat_models` with `authorization:[{"provider":"tmi","provider_id":"<40+ chars, unknown>","principal_type":"user","role":"reader"}]` returns 500: `saveAuthorizationTx` inserts the raw provider_id into `threat_model_access.user_internal_uuid` varchar(36) (SQLSTATE 22001). A short unknown id gets 400 only because the FK fails.
Fix: resolve every user/group subject to an internal UUID before insert and return 400 for unresolved ones explicitly; never write a non-UUID into an *_internal_uuid column. Search all other entity types that save authorization lists (survey responses, projects, teams, etc.) for the same pattern and fix them (global rule B).
Tests: failing test first (unknown long provider_id -> 400, no row inserted).

### Task 3: spec maxLength vs column size audit; ThreatBase.status -> 128
Repro: `POST /threat_models/{id}/threats` with `status` of 256 chars returns 500 (varchar(128), SQLSTATE 22001).
Fix: set ThreatBase.status maxLength to 128. Audit every string property maxLength in `api-schema/tmi-openapi.json` against the GORM `size:` of the column it lands in (`api/models/*.go`); fix every spec value larger than its column (lower the spec; do not change columns). Write the audit table to `.superpowers/sdd/2026-11-21-cats-5xx/maxlength-audit.md`. Then `make validate-openapi` and `make generate-api` (oapi-codegen v2.7.1). No version edits.
Tests: a handler/validation test proving a 129-char threat status is a 400.

### Task 4: threat_model_framework enum + transfer must not fail on unrelated fields
Repro: CATS created a threat model with framework "ICFDENVPMLPYOVPQQIBZSXRAZSULJF" (spec allows any token; create does not validate). The BeforeUpdate hook then rejects it on every later save, so `POST /me/transfer` returns 500 ("threat_model_framework: must be one of ...").
Fix: (a) spec: `enum` on threat_model_framework wherever it appears in a request schema (keep empty/omitted behavior as today); validate on create, PUT and PATCH with 400. (b) ownership transfer updates only ownership columns (UpdateColumn / column map) so it never re-validates unrelated stored fields; an unexpected failure there must not be a 500 for a client-input reason. Regenerate API.
Tests: failing tests first: create with bad framework -> 400; transfer succeeds for a user owning a row with a legacy invalid framework.

### Task 5: DB value-too-long / bad-encoding backstop -> 400
Defense in depth after Tasks 1-4: classify PostgreSQL 22001 (string_data_right_truncation) and 22021 (character_not_in_repertoire), and Oracle ORA-12899 (value too large for column) and ORA-01401, as a client-input class in `internal/dberrors`, and map that class to 400 in the shared handler error path (wherever "constraint violation" currently becomes 500). Oracle review required.
Tests: classification unit tests for both dialects; one handler test.

### Task 6: incomplete request body must not become 503
Repro: `POST /me/identities/link/start?idp=tmi&client_callback=https%3A%2F%2Fexample.com%2Fcats` with `Content-Length: 500` and a 2-byte body hangs 5 s, then 503 "Authentication service temporarily unavailable" (log: token blacklist check fails with `context canceled`). The auth middleware maps any blacklist-check error to 503.
Fix: find which middleware blocks reading the body and what cancels the context; a short or timed-out body read is a client error (400). The auth middleware must not report a canceled request context as 503. Fix centrally.
Tests: failing test first.

## After the tasks
- oracle-db-admin review of the whole set.
- make verify; make test-integration.
- Full CATS campaign on final HEAD (ambient context k3s-rp; clear fuzzer-created webhooks for charlie first; stream server logs to scratchpad). Zero 5xx required.
- /cats:analyze plan for remaining true positives, reviewed with Eric.
