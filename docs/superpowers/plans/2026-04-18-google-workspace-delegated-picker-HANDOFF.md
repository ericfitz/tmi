# Handoff — Google Workspace Delegated Picker (#249)

**Branch:** `feature/google-workspace-picker` (pushed to `origin`)
**Worktree:** `/Users/efitz/.config/superpowers/worktrees/tmi/feature-google-workspace-picker`
**Main project path:** `/Users/efitz/Projects/tmi` (still on `dev/1.4.0`; do NOT work there)
**Status at handoff:** 10 commits landed, 1593 unit tests passing, lint and build clean. OpenAPI regen not yet done. Server wiring not yet done. No integration tests yet.

## Spec and plan

- **Spec:** `docs/superpowers/specs/2026-04-18-google-workspace-delegated-picker-design.md`
- **Plan:** `docs/superpowers/plans/2026-04-18-google-workspace-delegated-picker.md`

Both documents live on `dev/1.4.0`; the worktree checks them out as part of that branch's history.

## Execution skill

Continue using `superpowers:subagent-driven-development`. The workflow has been: dispatch an implementer agent per task (or combined tasks), then run spec-compliance and code-quality review agents, loop back if issues found, then advance.

## Commits landed (in order)

| Task | Commit | Description |
|---|---|---|
| 1.1 | `725ee759` | Document model: `PickerProviderID`, `PickerFileID`, `PickerMimeType`, `AccessReasonCode`, `AccessReasonDetail`, `AccessStatusUpdatedAt` on the GORM model |
| 1.2/1.3 | `7d549c17` | `DocumentStore.UpdateAccessStatusWithDiagnostics` (GORM impl + mocks) |
| 2.1+2.2 | `b04bada4` | `api/access_diagnostics.go` with 8 reason codes, 7 remediation actions, `BuildAccessDiagnostics` pure builder |
| 2.3 (scoped) | `17c39cb7` | `DocumentStore.GetAccessReason(ctx, id)` store method |
| 3.1 | `3d1c3658` | `DelegatedGoogleWorkspaceSource` skeleton |
| 3.2 | `f5ad6288` | Drive API DoFetch + ValidateAccess + `NewDelegatedGoogleWorkspaceSource` constructor |
| 4.1 | `faff1a18` | `ContentSourceRegistry.FindSourceForDocument` document-aware dispatch |
| 5.1 | `d1d001de` | `GoogleWorkspaceConfig` with `PickerDeveloperKey` + `PickerAppID` |
| 5.2 | `2ee0dcd2` | `POST /me/picker_tokens/{provider_id}` handler |
| 7.1 | `f80dc196` | `ClearPickerMetadataForOwner` un-link cascade on `DELETE /me/content_tokens/{provider_id}` |

## Important plan deviations (already incorporated)

1. **Task 2.3 scope reduced.** The plan's Task 2.3 had the implementer wire `BuildAccessDiagnostics` into the document GET handler. This was split: only `GetAccessReason` store method is done. **Handler wiring is deferred to Task 8.2** (after OpenAPI regen adds `access_diagnostics` to the generated `Document` type).

2. **Task 4.2 fully deferred.** The plan's Task 4.2 had the access poller consume picker metadata. This requires picker fields on the API-level `Document` type, which only arrives after OpenAPI regen in Task 8. **Deferred to post-regen**, with the current poller still using plain URL-based dispatch (which is correct for non-picker documents; picker docs currently fall through as "not handleable by this dispatch path" and get skipped, which is acceptable).

3. **Task 6.1 (attach handler) deferred.** Same reason — picker_registration is not on the generated request body until regen. **Deferred to Task 8.2**.

4. **`ProviderGoogleWorkspace` lives in `api/content_pipeline.go`** (not `content_source_google_workspace.go` as the plan originally said) — moved there to sit alongside `ProviderGoogleDrive`. Do not redeclare.

5. **`googleHostDocs` / `googleHostDrive`** unexported constants live in `api/content_pipeline.go`, extracted during Task 3.1 to satisfy `goconst`. Existing `GoogleDriveSource.CanHandle` and new `DelegatedGoogleWorkspaceSource.CanHandle` both reference them.

6. **NULL-clearing uses plain `nil` in GORM `Updates` maps**, not `gorm.Expr("NULL")`. The plan originally prescribed `gorm.Expr("NULL")` but the codebase uses plain `nil`. The plan file was corrected in-place in the worktree (uncommitted edit) but the plan commit on `dev/1.4.0` still shows the old prescription. Trust the code, not the original plan text.

7. **`PickerTokenHandler` constructor takes 4 args** (`tokens, registry, configs, userLookup`) instead of the plan's 3. The `userLookup` parameter is required — cleaner than the plan's implicit-default pattern. Field is unexported (`userLookup`) rather than exported like `ContentOAuthHandlers.UserLookup`. Functionally fine.

## Remaining tasks in order

### Task 8.1 — OpenAPI schema updates

**File:** `api-schema/tmi-openapi.json` (**2 MB, 62 k lines — use jq streaming**)

Add these schemas under `components.schemas`:

- **`PickerRegistration`** — `{provider_id (enum: google_workspace), file_id (1-255), mime_type (1-128)}`.
- **`PickerTokenResponse`** — `{access_token, expires_at (date-time), developer_key, app_id}`.
- **`DocumentAccessDiagnostics`** — `{reason_code (enum), reason_detail (nullable), remediations: [AccessRemediation]}`.
- **`AccessRemediation`** — `{action (enum), params (object, additionalProperties: true)}`.

Enum values — **exact**, these are the wire contract:
- `reason_code`: `token_not_linked`, `token_refresh_failed`, `token_transient_failure`, `picker_registration_invalid`, `no_accessible_source`, `source_not_found`, `fetch_error`, `other`.
- `remediation.action`: `link_account`, `relink_account`, `repick_file`, `share_with_service_account`, `repick_after_share`, `retry`, `contact_owner`.

Add the operation:

- `/me/picker_tokens/{provider_id}` POST `mintPickerToken` — JWT bearer auth, returns `PickerTokenResponse`. Response codes 200/401/404/422/429/503. Set `x-cacheable-endpoint: false`.

Extend existing schemas:

- **`Document` response schema** — add optional `access_diagnostics: DocumentAccessDiagnostics` and `access_status_updated_at: string (date-time, nullable)`.
- **Document-attach request bodies** (the POST `/threat_models/{id}/documents` body schema — may be named `Document` in the request body or a distinct `DocumentInput` schema) — add optional `picker_registration: PickerRegistration`.

**Verification commands:**
```
cd /Users/efitz/.config/superpowers/worktrees/tmi/feature-google-workspace-picker
make validate-openapi
```
Check `api-schema/openapi-validation-report.json` for new errors; address any.

**Commit:** One commit, message `feat(openapi): add picker_registration, picker-token, and access_diagnostics schemas (#249)`.

### Task 8.2 expanded — Regen + wire deferred pieces

After 8.1's spec edits land, run:
```
cd /Users/efitz/.config/superpowers/worktrees/tmi/feature-google-workspace-picker
make generate-api
```

This regenerates `api/api.go`. The regen may introduce compile errors in handler code that referenced interim helpers. Fix them, then wire the deferred pieces:

**Piece A — Diagnostics in GET handler.** Currently the GET document handler does not return `access_diagnostics`. Wire `BuildAccessDiagnostics` into the serialization path, using `GetAccessReason` to load the stored reason and the caller's linked providers to build the `remediations` array. The handler struct will need a `ContentTokenRepository` field added (to query linked providers) and a `ServiceAccountEmail` string field (from `cfg.ContentSources.GoogleDrive.ServiceAccountEmail`).

**Piece B — picker_registration in attach handler.** Currently `api/document_sub_resource_handlers.go` creates documents without inspecting any picker_registration payload. After regen, the generated `Document` request body (or its discriminated input type) will expose the field. In `CreateDocument` (or equivalent), validate: provider registered + enabled, caller has active linked token, `file_id` matches `extractGoogleDriveFileID(url)`. Then call a new store method `SetPickerMetadata(ctx, docID, provider, fileID, mime)` — you'll need to add this alongside the existing `UpdateAccessStatus` methods. Or if the GORM model already exposes the fields, include them in the initial `Create` call.

**Piece C — Access poller picker-aware dispatch.** The poller at `api/access_poller.go:63` currently calls `FindSource(ctx, doc.Uri)`. After regen, `doc` (API type) carries `PickerRegistration` (nilable). Build a `*PickerMetadata` from it and swap to `FindSourceForDocument(ctx, doc.Uri, pickerMeta, doc.OwnerInternalUUID, tokenChecker)`. This requires:
- Poller struct gains a `tokenChecker LinkedProviderChecker` field.
- `LinkedProviderChecker` implementation backed by `ContentTokenRepository` (trivial wrapper).
- Poller constructor wiring it through.
- Per-document user context: `WithUserID(ctx, doc.OwnerInternalUUID)`. But **doc.OwnerInternalUUID doesn't exist on the Document API type** — it's only on the parent ThreatModel. Two options: (i) the poller joins through to fetch the owner, (ii) add a helper `GetOwner(ctx, docID) string` on the DocumentStore. Option (ii) is cleaner.

Commits: one per piece, or a single commit for regen + all three if they're small. Each wire-up piece should get a focused handler/poller test (not just regen-is-clean).

### Task 9.1 — Server wiring + startup validation

In `cmd/server/main.go`, around line 1162 (where `ContentSources.GoogleDrive` is registered):

1. **Register `DelegatedGoogleWorkspaceSource` before `GoogleDriveSource`** when `cfg.ContentSources.GoogleWorkspace.IsConfigured()`:
   ```go
   if cfg.ContentSources.GoogleWorkspace.IsConfigured() {
       gw := api.NewDelegatedGoogleWorkspaceSource(
           contentTokenRepo, contentOAuthRegistry,
           cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey,
           cfg.ContentSources.GoogleWorkspace.PickerAppID,
       )
       contentSources.Register(gw)
       logger.Info("Content source enabled: google_workspace (delegated, drive.file scope)")
   }
   ```

2. **Startup validation**: if `cfg.ContentSources.GoogleWorkspace.Enabled == true` and `cfg.ContentOAuth.Providers["google_workspace"].Enabled != true`, exit with a clear error.

3. **Register picker-token route**: add `r.POST("/me/picker_tokens/:provider_id", pickerHandler.Handle)` where `pickerHandler` is constructed via `api.NewPickerTokenHandler` with the configs map populated from config. But wait — if the OpenAPI-generated `ServerInterface` already registers a `MintPickerToken` method (it will after regen), the route should be registered via the generated router path instead. Check after regen.

4. **Wire `ContentOAuthHandlers.Documents`** to the `DocumentStore` so the un-link cascade (Task 7.1) actually fires in production.

Commit: `feat(server): register google_workspace source, picker-token route, un-link cascade wiring (#249)`.

### Task 10.1 — Integration test

Create `test/integration/workflows/google_workspace_delegated_test.go`. Real Postgres + stub OAuth provider. See sub-project 1's `TestDelegatedContentProvider_EndToEnd_Integration` for the scaffolding pattern (stub provider advertises `drive.file` scope instead of the mock provider's generic scopes).

Scenarios to cover:
- Attach with picker registration → DB row has picker columns set.
- GET document → `access_diagnostics` shape correct.
- `no_accessible_source` + linked account → 2 remediations in correct order.
- `POST /me/picker_tokens/google_workspace` → valid response shape with refresh semantics.
- Un-link cascade end-to-end: authorize → attach picker doc → DELETE content token → GET doc shows cleared picker columns + unknown status.
- Multi-user view: user A picker-attaches; user B views the same threat model → per-viewer `remediations` populated correctly.

Do NOT stub the Drive API. Skip Fetch coverage at integration level (unit level already covers `exportFormatFor`, and manual test covers real Drive call).

### Task 10.2 — Picker harness + manual test

Create:
- `scripts/google-picker-harness/index.html` — ~100 lines of HTML+JS using Google Picker.
- `test/integration/manual/google_workspace_delegated_test.go` (`//go:build manual`) with helpers that drive the full flow against a real server + real Google account.
- Makefile target `test-manual-google-workspace`.

Plan file already has complete code for the harness HTML and the manual test helpers. Copy them in and adapt paths if needed.

### Task 11 — Final validation + PR

```
make lint
make validate-openapi
make build-server
make test-unit
make test-integration   # pre-existing failures documented in #249's sub-project 1 comment may recur; accept
make check-unsafe-union-methods
```

If all green, update #249's tracking comment noting sub-project 4 complete, reference the follow-up issues (tmi-ux#626, tmi#283).

Open PR: `feat(content): google_workspace delegated picker access (#249)` against `dev/1.4.0`. Body lists summary + test plan checklist.

## Follow-up issues filed (already done in prior session)

- **tmi-ux#626** — Google Drive picker integration UI. Blocks on this sub-project landing.
- **tmi#283** — OOXML export upgrade. Blocks on sub-project 5 of #249.

## How to resume

```bash
cd /Users/efitz/.config/superpowers/worktrees/tmi/feature-google-workspace-picker
git status  # should show clean tree, HEAD == f80dc196
git log --oneline dev/1.4.0..HEAD | wc -l  # should be 10
make test-unit | tail -5  # should show 1593 passed
```

Then invoke `superpowers:subagent-driven-development` and start with Task 8.1.

## Gotchas and invariants

1. **Never run tests/commands from `/Users/efitz/Projects/tmi/`** — that directory is on `dev/1.4.0` with no feature work. Always use the worktree.

2. **The `tmi-clients` symlink.** The worktree directory has a symlink `/Users/efitz/.config/superpowers/worktrees/tmi/tmi-clients -> /Users/efitz/Projects/tmi-clients` because `go.mod` uses a relative `replace` directive. Don't delete or recreate this symlink.

3. **Document GORM model has no `OwnerID`** — ownership is via `threat_models.owner_internal_uuid`. Any "document owner" query must join. See Task 7.1's `ClearPickerMetadataForOwner` implementation for the established pattern.

4. **`Table("documents")` not `Model(&models.Document{})`** for GORM writes. The `BeforeSave` hook on `models.Document` fails on empty structs. Established pattern in `UpdateAccessStatus` and its siblings.

5. **Use `nil` for NULL in GORM `Updates(map[string]interface{})`**, not `gorm.Expr("NULL")`. Codebase convention.

6. **Always use `make` targets.** Never `go test` directly. See `Makefile` for available targets.

7. **Diagnostic builder types use `Diag` suffix** (`AccessDiagnosticsDiag`, `AccessRemediationDiag`) to distinguish from OpenAPI-generated wire types. After regen, the handler will convert builder types → generated wire types at the response boundary.

## Context carried from brainstorm

- Path A (picker + `drive.file`) was chosen over Path B (narrow scopes, no picker) because enterprise Workspace admins increasingly block `drive.readonly` (restricted scope) and CASA-2 audits are disproportionate for TMI's use case.
- Export format stays plain-text/CSV for this sub-project (Phase 0); OOXML upgrade tracked as `tmi#283` after sub-project 5 lands.
- tmi-ux picker UI is tracked as `tmi-ux#626`; server is being built first so the client can consume a stable API.
