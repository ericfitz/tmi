# Expose enabled content providers in `/config` response

**Issue:** [#373](https://github.com/ericfitz/tmi/issues/373)
**Date:** 2026-05-06
**Branch:** `dev/1.4.0`

## Problem

`GET /config` returns `features`, `operator`, `limits`, and `ui`, but does not advertise which content providers the server has configured. Clients (tmi-ux today, others later) hardcode the list, drift from server reality, and offer "Link account" buttons that fail with `422 content_token_provider_not_configured`. See tmi-ux #662 for the user-visible failure.

## Goal

`GET /config` returns the content providers the server can actually serve, so clients can render only what works and gate per-user account-linking UX on the right provider kind.

## Non-goals

- Token introspection or per-user link status. `/me/content_tokens` already covers that.
- Operator-overridable display strings for service-only sources (`google_drive`, `http`). YAGNI; the lookup-table defaults are sufficient. Add a config block later if a real need appears.
- Any change to extractor, fetcher, or pipeline behavior.

## Schema

Extend `ClientConfig` in `api-schema/tmi-openapi.json` with a top-level `content_providers` array.

### `ClientConfig.content_providers`

Array of `ContentProvider` objects. Empty array (not omitted) when the server has no content sources. Order matches the registration order of `ContentSourceRegistry`.

### `ContentProvider`

```yaml
ContentProvider:
  type: object
  additionalProperties: false
  required: [id, name, kind, icon]
  properties:
    id:
      type: string
      description: Source identifier (matches ContentSource.Name()).
      example: google_workspace
    name:
      type: string
      description: Display label for the provider.
      example: Google Workspace
    kind:
      type: string
      enum: [delegated, service, direct]
      description: |
        - delegated: per-user OAuth; client must call /me/content_tokens/{id}/authorize
        - service:   operator-credentialed; no per-user link required
        - direct:    no auth (e.g., HTTP fetch)
    icon:
      type: string
      description: Font Awesome class string (matches OAuth IdP convention). Empty if no default and no override.
      example: "fa-brands fa-google"
```

### Example response fragment

```json
"content_providers": [
  { "id": "http",             "name": "HTTP",                 "kind": "direct",    "icon": "fa-solid fa-globe" },
  { "id": "google_drive",     "name": "Google Drive",         "kind": "service",   "icon": "fa-brands fa-google-drive" },
  { "id": "google_workspace", "name": "Google Workspace",     "kind": "delegated", "icon": "fa-brands fa-google" },
  { "id": "confluence",       "name": "Atlassian Confluence", "kind": "delegated", "icon": "fa-brands fa-confluence" }
]
```

## Resolution rules

For each source name returned by `ContentSourceRegistry.Names()`:

1. Look up `(kind, defaultName, defaultIcon)` from a static metadata table keyed by source name.
2. If the source name is unknown to the table: `kind = "direct"`, `defaultName = id`, `defaultIcon = ""`. (Safe fallback for future sources that haven't been added to the table yet.)
3. If `kind == "delegated"` and `cfg.ContentOAuth.Providers[id]` exists:
   - `name  = override.Name`  if non-empty, else `defaultName`.
   - `icon  = override.Icon`  if non-empty, else `defaultIcon`.
4. Otherwise: `name = defaultName`, `icon = defaultIcon`.
5. Append `{id, name, kind, icon}` to the response.

### Metadata table (initial)

| `id`               | `kind`      | `defaultName`         | `defaultIcon`               |
|--------------------|-------------|-----------------------|-----------------------------|
| `http`             | `direct`    | `HTTP`                | `fa-solid fa-globe`         |
| `google_drive`     | `service`   | `Google Drive`        | `fa-brands fa-google-drive` |
| `google_workspace` | `delegated` | `Google Workspace`    | `fa-brands fa-google`       |
| `microsoft`        | `delegated` | `Microsoft 365`       | `fa-brands fa-microsoft`    |
| `confluence`       | `delegated` | `Atlassian Confluence`| `fa-brands fa-confluence`   |

## Operator override

Add two optional fields to `internal/config/content_oauth.go.ContentOAuthProviderConfig`:

```go
Name string `yaml:"name"`
Icon string `yaml:"icon"`
```

Both are optional. Empty string preserves the lookup-table default. Validation unchanged.

Override applies **only to delegated providers** (those configured in `content_oauth.providers.*`). Service and direct sources read from the metadata table only.

## Wiring

- New `Server` field: `contentSourceRegistry *ContentSourceRegistry`.
- New setter: `SetContentSourceRegistry(*ContentSourceRegistry)` mirroring `SetDocumentContentOAuthRegistry`.
- `cmd/server/main.go` calls the setter after the source registry is built.

## File-level changes

| File | Change |
|---|---|
| `api-schema/tmi-openapi.json` | Add `ContentProvider` schema; add `content_providers` to `ClientConfig`; update example. |
| `api/api.go` | Regenerated by `make generate-api`. |
| `internal/config/content_oauth.go` | Add `Name`, `Icon` to `ContentOAuthProviderConfig`. |
| `api/content_provider_metadata.go` (new) | Static metadata table + `lookupContentProviderMeta(id)` helper. |
| `api/server.go` | Field `contentSourceRegistry`; setter `SetContentSourceRegistry`. |
| `api/config_handlers.go` | Extend `buildClientConfig`; factor `buildContentProviders(*ContentSourceRegistry, *config.Config) []ContentProvider`. |
| `cmd/server/main.go` | Call `apiServer.SetContentSourceRegistry(sources)`. |
| `api/config_handlers_test.go` | Add unit tests (cases below). |

## Testing

### Unit tests (`api/config_handlers_test.go`)

1. **Empty registry** → `content_providers: []` (empty array, not omitted).
2. **Mixed sources** registered (`http`, `google_drive`, `google_workspace`) → all three appear with correct `kind`, default `name`, default `icon`.
3. **Operator override**: `google_workspace` configured with `name: "GWS"` and `icon: "fa-custom"` → override values appear in response; other delegated providers without overrides keep defaults.
4. **Unknown source name** (`name() == "experimental"`) → entry has `kind: "direct"`, `name: "experimental"`, `icon: ""`.
5. **Override scoped to delegated**: the resolver consults `cfg.ContentOAuth.Providers[id]` only when the metadata table reports `kind == "delegated"`. A unit test asserts this by stubbing a non-delegated id with a same-name entry in `ContentOAuth.Providers` and verifying the override is ignored.

### Integration tests

None required. `/config` is public, no DB/auth surface. Unit tests with stubbed registries cover behavior.

### OpenAPI

- `make validate-openapi` must pass.
- `make generate-api` regenerates `api/api.go`; confirm `ClientConfig.ContentProviders` field exists.

## Caching

Handler already sets `Cache-Control: public, max-age=300`. Unchanged. Operator flipping a provider `enabled` flag propagates within 5 minutes; acceptable for a discovery endpoint.

## Backwards compatibility

Additive field. Existing clients ignoring unknown fields are unaffected. tmi-ux will be updated to consume the new field and gate "Link account" UX on `kind == "delegated"` (separate PR in `tmi-ux`).

## Out of scope (follow-ups, not blocking)

- Operator override for service/direct source labels.
- Per-provider link-status hint (already discoverable via `/me/content_tokens`).
- Localization of default display names.

## Reviews required

- **Oracle DB review**: not required (no DB code).
- **Security review**: run the `security-review` skill at session completion. No new auth surface; the response carries non-secret metadata only.

## Acceptance

- `GET /config` returns a populated `content_providers` array reflecting `ContentSourceRegistry.Names()`.
- Each entry has the correct `kind`; delegated entries pick up operator-supplied `name`/`icon` overrides.
- `make lint`, `make test-unit`, `make build-server`, `make validate-openapi` all pass.
- Issue #373 closed via commit referencing it (with explicit `gh issue close` since this lands on `dev/1.4.0`, not `main`).
