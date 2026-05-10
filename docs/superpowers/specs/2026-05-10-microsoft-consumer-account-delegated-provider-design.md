# Microsoft consumer-account delegated content provider — design

**Issue:** [#297](https://github.com/ericfitz/tmi/issues/297)
**Parent:** [#249](https://github.com/ericfitz/tmi/issues/249) (Confluence and OneDrive content providers)
**Sibling (shipped):** [#286](https://github.com/ericfitz/tmi/issues/286) — Microsoft delegated content provider for OneDrive-for-Business and SharePoint Online (Entra-managed work/school accounts)
**Branch:** `dev/1.4.0`
**Date:** 2026-05-10

## Goal

Extend the Microsoft delegated content provider that shipped in #286 to also accept **consumer Microsoft accounts** (personal OneDrive at `onedrive.live.com` and short-link `1drv.ms`). After this change, a TMI user who pastes a personal-OneDrive URL on a document — or picks one via the Microsoft File Picker — gets the same delegated-fetch + access-diagnostics behavior they get today for SharePoint and OneDrive-for-Business.

## Non-goals

- New OAuth provider id. The existing `microsoft` provider id is reused for both audiences.
- Service-mode (app-only) consumer access. Microsoft Graph does not support `client_credentials` against personal accounts; out of scope and not viable.
- Editing or write access to consumer files. Same scope as #286.
- Microsoft Teams / Stream / OneNote consumer content. Out of scope.

## Approach

**Single `microsoft` provider, multi-audience Entra app.** The TMI Entra application is registered with `signInAudience = AzureADandPersonalMicrosoftAccount`. OAuth uses the `/common/` (or `/consumers/` for testing) authorization endpoint, which routes both work/school and personal logins to the same TMI provider id. The same `DelegatedMicrosoftSource` handles both audiences because Microsoft Graph's `/shares/{shareId}/driveItem` resolution works uniformly across audiences once the user has consented (and per-file permission is in place).

The alternative — a separate `microsoft_consumer` provider id with its own OAuth wrapper, source registration, picker-grant handler, and link button in tmi-ux — was rejected as ~2× the code surface and 2× the operator setup, with no functional advantage for the multi-audience case. This matches the issue body's stated intent ("much smaller follow-on", "should require minimal changes").

### Picker-grant flow caveat (Experience 2)

The picker-grant handler in #286 (`api/microsoft_picker_grant_handler.go`) calls
`POST /drives/{driveId}/items/{itemId}/permissions` with a
`grantedToIdentities` body naming the configured TMI Entra app's identity to
grant `read` on the picked file. This is a tenanted-app construct.

For consumer accounts, the `grantedToIdentities` application-grantee form may
not be honored by Graph — Microsoft's documented per-file permissions on
consumer OneDrive are user-targeted (sharing links, individual recipients),
not application-targeted.
Behavior must be probed at implementation time on a real personal account.

Two outcomes are acceptable:

1. **It works as-is.** No additional code change; the picker-grant call returns
   2xx and subsequent fetches succeed under the picker-issued
   `Files.SelectedOperations.Selected` token.
2. **It does not work.** The handler detects the consumer audience (e.g., from
   the token's `tid` claim being the well-known consumer tenant
   `9188040d-6c67-4c5b-b112-36a304b66dad`, or from the `/me` userinfo shape) and
   **skips the grant call**, relying on the picker SDK's own per-file scope
   issuance. The picker file_id metadata is still written to the document so
   subsequent fetches go through the picker-aware dispatch path. The operator
   wiki notes this difference.

In both outcomes Experience 1 (paste-URL → "share with TMI app" remediation) is
unaffected by audience. Experience 2 either works identically across audiences
or degrades gracefully on consumer accounts.

## Architecture changes

The change is additive — extending host matchers and OAuth endpoint defaults.
No new files are required by the core flow. (A small audience-detection helper
may be added if Outcome 2 above materializes.)

### 1. URL pattern matcher

`api/content_pipeline.go` — `URLPatternMatcher.Identify`:

- Add cases for `host == "onedrive.live.com"` and `host == "1drv.ms"`.
- Also accept any subdomain ending in `.onedrive.live.com` for resilience to
  Microsoft's regional and tenant-specific personal-OneDrive hosts.
- All three resolve to `ProviderMicrosoft`.
- Remove the comment block that says consumer URLs are intentionally excluded.

### 2. Source `CanHandle`

`api/content_source_microsoft_graph.go` — `DelegatedMicrosoftSource.CanHandle`:

- Same three host matches added.
- Update doc comment: replace "Personal Microsoft account hosts … are
  deliberately not handled here" with "Both Entra-managed and personal Microsoft
  account hosts are handled (multi-audience Entra app, see #297)."

### 3. Share-ID encoding

`encodeMicrosoftShareID` already accepts arbitrary URLs and base64url-encodes
them; existing tests verify a `onedrive.live.com` URL encodes correctly. No
code change. A `1drv.ms` short-link encodes the same way and Graph follows the
redirect server-side. Add a test case to lock that in.

### 4. OAuth endpoint defaults

`config-development.yml` (and the wiki operator setup): default Microsoft
`authorization_url` and `token_url` to the `/common/` endpoint, which serves
both work/school and personal audiences:

- `https://login.microsoftonline.com/common/oauth2/v2.0/authorize`
- `https://login.microsoftonline.com/common/oauth2/v2.0/token`

Operators who want to lock down to a single audience may continue to use
`/consumers/`, `/organizations/`, or a specific tenant id; the code does not
care.

The `issuer` and `jwks_url` for token validation already point at the
consumer-tenant well-known values (`9188040d-…`) for compatibility; this is
correct for personal accounts and remains unchanged.

### 5. Picker-grant audience awareness (conditional)

`api/microsoft_picker_grant_handler.go` — implementation phase will probe Graph
behavior. If Outcome 2 materializes:

- Add a small helper `isConsumerMicrosoftToken(tok)` that inspects the stored
  token row (or, if necessary, the OAuth `id_token` claims persisted at link
  time) for the consumer-tenant id `9188040d-6c67-4c5b-b112-36a304b66dad`.
- In `Handle`, if `isConsumerMicrosoftToken(tok)` returns true, skip the
  Graph permissions POST, log "consumer account — relying on picker per-file
  scope", and return 200 with the same response shape (so tmi-ux is
  unaffected).

This branch is added only if probing demonstrates the unconditional path
fails on consumer accounts. The spec records the contingency so the
implementation plan can include the probe as a task.

### 6. Configuration

No change to `MicrosoftConfig` shape. `TenantID` semantics widen: operators
configuring multi-audience set `tenant_id: "common"` (or `"consumers"`).
`IsConfigured` validation already passes when these are non-empty strings.

The `picker_origin` value remains operator-supplied; for a multi-audience app
serving consumer pickers it is typically `https://onedrive.live.com`.

## Out-of-scope changes (explicitly not in this issue)

- Database schema, migrations, or token-row shape — unchanged.
- `BaseContentOAuthProvider`, `MicrosoftContentOAuthProvider` wrapper —
  unchanged.
- `picker_token` handler shape — unchanged. Operator simply emits a
  consumer-appropriate `picker_origin`.
- OpenAPI surface — no new operations or schemas.
- tmi-ux changes — tracked separately in tmi-ux. The Microsoft File Picker SDK
  already supports both audiences with a small picker-init parameter change;
  filed as a follow-on if not already tracked.

## Testing

### Unit

- `api/content_source_microsoft_graph_test.go` — extend
  `TestDelegatedMicrosoftSource_CanHandle` to assert `true` for:
  - `https://onedrive.live.com/redir?resid=1234`
  - `https://1drv.ms/abc`
  - `https://my.onedrive.live.com/personal/foo` (subdomain form)
  - existing `*.sharepoint.com` cases continue to assert `true`.
- Same file: extend `TestEncodeMicrosoftShareID` to lock in encoding for
  consumer URLs (`onedrive.live.com` already covered; add `1drv.ms`).
- `api/content_pipeline_test.go` — flip the existing assertion that
  `https://onedrive.live.com/edit.aspx?id=abc` resolves to `"http"`;
  it must now resolve to `"microsoft"`. Add a `1drv.ms` case. Update the
  comment that calls out the historical exclusion.

### Integration

- `api/microsoft_delegated_integration_test.go` — add a sub-test
  `consumer_paste_url_happy_path` that exercises `fetchByURL` against the
  existing stub Graph server with a consumer-host URL fixture. The stub does
  not differentiate audiences; this proves the routing + share-id encoding +
  fetch path work end-to-end for the consumer URL shape.
- If Outcome 2 materializes (audience-conditional skip in picker-grant): add
  a sub-test `consumer_picker_grant_skipped` that asserts the handler returns
  200 with no Graph permissions call when the stored token is consumer-tenant.

### Manual verification (operator-side)

- Wiki page updated with a "personal-account verification checklist":
  1. Register Entra app with `signInAudience =
     AzureADandPersonalMicrosoftAccount`.
  2. Set TMI config `content_oauth.providers.microsoft` to use the `/common/`
     endpoint and `content_sources.microsoft.tenant_id: "common"`.
  3. Link a personal Microsoft account via TMI's
     `POST /me/content_tokens/microsoft/authorize`.
  4. Paste a personal-OneDrive URL on a TMI document; verify the
     `microsoft_not_shared` diagnostic surfaces, then share the file with the
     TMI app and verify the access poller flips it to `accessible`.
  5. (If picker UI is wired in tmi-ux) pick a consumer file via the picker
     and verify the document attaches and is fetched successfully.

Manual verification cannot be automated against live Graph; the wiki captures
the steps an operator should run before declaring consumer support production-
ready.

## Risk and rollback

The change is additive — three host matchers, one default endpoint URL, and
optionally one audience-conditional branch.

- **Worst case (operator hasn't configured multi-audience).** The user gets an
  OAuth error at link time when they try to authorize against the wrong
  audience. This is the same failure mode as today's `/organizations/`-only
  configuration; no regression.
- **Worst case (Graph picker-grant fails on consumer accounts and we don't
  detect it).** Picker grant returns 4xx; tmi-ux already handles 4xx
  gracefully. The conditional branch in §5 prevents this case from shipping.
- **Rollback.** Revert the host matcher additions and the OAuth default URL
  changes. No data migration; no token-row shape change; tokens linked under
  the multi-audience configuration remain valid against the rolled-back
  single-audience configuration as long as the Entra app's audience setting
  is also rolled back.

## Definition of done

1. Host matchers in `URLPatternMatcher.Identify` and
   `DelegatedMicrosoftSource.CanHandle` accept the three consumer hosts.
2. Existing unit tests that asserted the historical exclusion are updated to
   assert the new inclusion. New tests cover share-id encoding for `1drv.ms`
   and the consumer integration sub-test.
3. `config-development.yml` defaults `microsoft.authorization_url` /
   `token_url` to `/common/`. Sample is uncommented and the per-audience
   alternatives are kept commented.
4. Picker-grant outcome is decided (Outcome 1 or Outcome 2). If Outcome 2,
   the audience-detection branch and integration test sub-test are in place.
5. Operator wiki page describes: multi-audience Entra app registration, the
   `/common/` config, and the personal-account verification checklist above.
6. `make lint`, `make build-server`, `make test-unit`, `make test-integration`
   all clean. `oracle-db-admin` review N/A (no DB-touching code).
7. Issue closed via `gh issue comment 297` + `gh issue close 297` (the branch
   is `dev/1.4.0`, not `main`, so a `Closes #297` trailer alone will not
   auto-close).
