# Microsoft picker_registration on document creation — design

**Issue:** [#307](https://github.com/ericfitz/tmi/issues/307)
**Branch:** `dev/1.4.0`
**Date:** 2026-04-28

## Background

Server-side support for the Microsoft delegated content provider (#286) shipped on `dev/1.4.0`: picker-token mint, `POST /me/microsoft/picker_grants`, the `DelegatedMicrosoftSource`, and `{driveId}:{itemId}` picker_file_id encoding. However, the document-creation contract still rejects Microsoft `picker_registration` payloads, blocking [tmi-ux#643](https://github.com/ericfitz/tmi-ux/issues/643).

Three concrete gaps were identified in #307:

1. `PickerRegistration.provider_id` enum in `api-schema/tmi-openapi.json` is `["google_workspace"]` only.
2. `validatePickerRegistration` in `api/document_sub_resource_handlers.go` (lines ~103–110) calls `extractGoogleDriveFileID(uri)` unconditionally and rejects with `picker_file_id_mismatch` when the URL isn't a Google Drive URL.
3. No end-to-end integration test exercises `POST /threat_models/{id}/documents` with a Microsoft `picker_registration`.

## Goals

- `POST /threat_models/{id}/documents` (and `/documents/bulk`) accepts `picker_registration` payloads with `provider_id: "microsoft"`.
- Existing Google Workspace picker_registration behavior is preserved exactly (same code paths, same error codes).
- Microsoft picker_registration is exercised end-to-end in an integration test.

## Non-goals

- Personal Microsoft accounts (`onedrive.live.com`, `1drv.ms`) — owned by #297.
- Confluence picker — Confluence sub-project (#249 sub-project 2) does not use a picker flow.
- Provider-pluggable validator registry — deferred until a third pickered provider lands.

## Design

### Approach: inline provider dispatch

`validatePickerRegistration` will branch on `pr.ProviderID` after the empty-field check and before the registry lookup. For each known provider, it performs a provider-specific URI ↔ `file_id` consistency check; for unknown providers it falls through to the existing `picker_file_id_mismatch` 400.

This matches the existing codebase pattern: `content_pipeline.go` already uses a host-based switch to route URIs to content sources rather than a per-source validator interface. With only two pickered providers today, an interface is over-abstraction; if/when a third provider lands, refactor at that point.

### Provider-specific validation

| provider_id | URI check | file_id check |
|-------------|-----------|---------------|
| `google_workspace` | none beyond `extractGoogleDriveFileID(uri)` | `extractGoogleDriveFileID(uri) == pr.FileID` (existing behavior) |
| `microsoft` | URL host has suffix `.sharepoint.com` (matches `DelegatedMicrosoftSource.CanHandle`) | `decodeMicrosoftPickerFileID(pr.FileID)` returns `ok=true` |
| anything else | n/a | rejected with existing `picker_file_id_mismatch` 400 |

**Microsoft URI ↔ file_id cross-validation is intentionally lenient.** SharePoint `webUrl`s do not deterministically encode the drive id and item id (the URL is path-based; the `{driveId}:{itemId}` is the Graph-API canonical identity), so we cannot reliably check that the URL points at the same drive item the picker returned. The Graph picker-grant call (#286) already vouched for the binding before the client gets here. We document this leniency in code so future readers understand why the check is host-only.

### OpenAPI spec changes

In `api-schema/tmi-openapi.json`, `components.schemas.PickerRegistration`:

- `provider_id.enum`: `["google_workspace"]` → `["google_workspace", "microsoft"]`
- Schema-level `description`: drop "Google Workspace Picker" framing; describe as a client-provided registration for any picker-mediated provider attachment, with the server using the fields to dispatch fetch and access-validation through the matching delegated source.
- `file_id.description`: keep the existing example wording but make it provider-neutral (mention both the Google Drive file ID and Microsoft `{driveId}:{itemId}` as examples).

After spec edit, `make generate-api` regenerates `api/api.go` so the `PickerRegistration_ProviderID` enum constants include the Microsoft value.

### Handler changes

In `api/document_sub_resource_handlers.go` `validatePickerRegistration`:

```go
// after the empty-field check, replace the unconditional Google extraction with:
switch pr.ProviderID {
case "google_workspace":
    fileID, ok := extractGoogleDriveFileID(uri)
    if !ok || fileID != pr.FileID {
        // existing 400 picker_file_id_mismatch
    }
case "microsoft":
    parsed, err := url.Parse(uri)
    if err != nil || !strings.HasSuffix(strings.ToLower(parsed.Host), ".sharepoint.com") {
        // 400 picker_file_id_mismatch — non-SharePoint URI for microsoft provider
    }
    if _, _, ok := decodeMicrosoftPickerFileID(pr.FileID); !ok {
        // 400 picker_file_id_mismatch — file_id is not "{driveId}:{itemId}"
    }
default:
    // unknown provider_id — same 400 picker_file_id_mismatch
}
```

The downstream registry-lookup and linked-token checks remain unchanged. The provider-not-registered (422) and token-not-linked (401) paths still apply uniformly across providers.

`POST /threat_models/{id}/documents/bulk` (`BulkCreateDocuments`) does **not** currently process `picker_registration` — it binds the body to `[]Document` (the response type, which lacks `picker_registration`) and never calls `validatePickerRegistration`. That's a pre-existing gap, not introduced by #307. We do not extend bulk to support `picker_registration` in this change; the picker UX is one-file-at-a-time so the single-document path is the only one tmi-ux uses for picker attachments. If bulk needs picker support later, file a separate issue.

### Error code preservation

`picker_file_id_mismatch` is reused for all URI/file_id failures regardless of provider. tmi-ux already maps this code; widening the meaning ("URI doesn't match the file_id format expected for this provider") is consistent with the existing code's intent.

### Integration test

New file: `api/microsoft_picker_registration_integration_test.go`. Reuses the harness from `api/microsoft_delegated_integration_test.go`.

Sub-tests:

1. **Happy path** — link Microsoft account; mint picker token; call `POST /me/microsoft/picker_grants`; create a document via `POST /threat_models/{id}/documents` with `uri` (SharePoint webUrl), `picker_registration: { provider_id: "microsoft", file_id: "{driveId}:{itemId}", mime_type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" }`. Assert response shows `access_status: "accessible"` and DB rows include `picker_provider_id`, `picker_file_id`, `picker_mime_type`.
2. **Non-SharePoint URI** — `provider_id: "microsoft"` with a `https://example.com/...` URI returns 400 `picker_file_id_mismatch`.
3. **Malformed file_id** — `provider_id: "microsoft"` with `file_id: "abc"` (no colon) returns 400 `picker_file_id_mismatch`.
4. **Unknown provider_id** — value not in `{google_workspace, microsoft}` (e.g. `"made_up"`) is rejected by the handler with 400 `picker_file_id_mismatch` via the `default` switch arm. Note: OpenAPI middleware does **not** gate this field; `picker_registration` is read by a body-sniff before the typed parse (the typed parse binds to `Document`, the response schema, which has no `picker_registration` field). Enforcement lives entirely in `validatePickerRegistration`.

The existing Google Workspace integration test (`api/google_workspace_delegated_integration_test.go`) continues to validate the unchanged Google branch — no edits needed.

## Files changed

- `api-schema/tmi-openapi.json` — schema enum + descriptions (3 edits in `PickerRegistration`)
- `api/api.go` — regenerated by `make generate-api` (no manual edits)
- `api/document_sub_resource_handlers.go` — replace unconditional Google extraction with provider switch (~25 lines)
- `api/microsoft_picker_registration_integration_test.go` — new (4 sub-tests)

## Verification

Per `CLAUDE.md` task-completion workflow:

1. `make lint` — clean.
2. `make validate-openapi` — clean.
3. `make generate-api` — produces clean diff in `api/api.go` (only enum-related lines changed).
4. `make build-server` — clean.
5. `make test-unit` — all green; existing 1606+ tests still pass.
6. `make test-integration` — happy path + 3 negative sub-tests pass; existing Google + Microsoft tests still pass.
7. **Oracle review**: this change does not touch DB code (only handler validation + new test). Skipping oracle-db-admin dispatch.
8. Commit with `Closes #307` trailer; on `dev/1.4.0`, follow up with `gh issue comment 307` and `gh issue close 307` (per memory: feature branches don't auto-close).

## Acceptance criteria mapping

| Issue requirement | Covered by |
|---|---|
| Gap 1: enum widened to `[google_workspace, microsoft]` | OpenAPI spec section above |
| Gap 1: schema description generalized | OpenAPI spec section above |
| Gap 2: per-provider validator dispatch | Handler section above |
| Gap 2: Microsoft URI host check + `decodeMicrosoftPickerFileID` | Provider-specific validation table |
| Gap 3: end-to-end Microsoft document-creation test | Integration test section above |
