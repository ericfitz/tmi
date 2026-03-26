# Design: URL Pattern Consistency — Domain-Segregated vs Resource-Hierarchical

**Date**: 2026-03-26
**Issue**: #131
**Scope**: OpenAPI spec, Go handlers, middleware, CLAUDE.md, wiki documentation

## Problem

The TMI API uses two distinct URL pattern styles — resource-hierarchical (`/threat_models/{id}/assets/{id}`) and domain-segregated (`/admin/surveys/{id}`, `/intake/surveys/{id}`) — without documented guidance on when to use which. The patterns serve different authorization models, but this isn't formalized. Additionally, the `/admin/` prefix's authorization enforcement is inconsistent, and naming conventions have minor violations.

## Decision: Pattern Classification Framework

Both patterns are intentional and solve genuinely different authorization problems. Rather than unifying them, we formalize when to use each and improve the consistency of the existing implementation.

## URL Pattern Taxonomy

TMI has five URL pattern categories:

| Pattern | Authorization Model | Example | When to Use |
|---------|-------------------|---------|-------------|
| **Resource-hierarchical** | Ownership-based (readers/writers/owners on parent) | `/threat_models/{id}/assets/{id}` | Entity access controlled by parent resource ownership |
| **Domain-segregated** | Workflow-stage-based (admin/intake/triage) | `/admin/surveys/{id}` | Same resource needs different capabilities per workflow stage |
| **User-scoped** | Current authenticated user | `/me/preferences` | Personal resources belonging to the requesting user |
| **Cross-cutting** | Mixed (ownership + admin visibility) | `/projects/{id}`, `/addons/{id}` | Top-level resources not nested under threat models |
| **Protocol** | Public or token-based per RFC | `/oauth2/token`, `/.well-known/jwks.json` | Auth/identity protocol endpoints defined by external standards |

## Selection Criteria for New Resources

When adding a new API resource, apply these criteria in order:

1. **Is this an auth/identity protocol endpoint?** (OAuth, SAML, OIDC, well-known) → **Protocol** pattern. Follow the relevant RFC for URL structure. These use kebab-case per RFC convention.

2. **Is this a personal resource scoped to the current user?** (preferences, sessions, credentials) → **User-scoped** pattern under `/me/`.

3. **Does the resource have a natural parent that controls access?** (e.g., assets belong to a threat model) → **Resource-hierarchical** pattern. Nest under the parent.

4. **Does the resource participate in a multi-stage workflow where different roles need different capabilities on the same data?** → **Domain-segregated** pattern.
   - Default to existing prefixes (`/admin/`, `/intake/`, `/triage/`) when the workflow fits "configure → submit → review"
   - Introduce a new prefix only when the workflow stages are genuinely different (e.g., `/remediation/` for a fix-tracking workflow)

5. **Is this a top-level resource not nested under another, with its own access control?** (projects, teams, addons) → **Cross-cutting** pattern at the root level.

The key distinguishing question between resource-hierarchical and domain-segregated is: **does authorization flow from a parent entity, or from the workflow context?**

## Naming Conventions

TMI-defined path segments use **snake_case** exclusively. Kebab-case segments (e.g., `openid-configuration`, `oauth-authorization-server`) exist only under `/.well-known/` and are mandated by RFCs — not a TMI convention choice.

| Category | Convention | Status |
|----------|-----------|--------|
| Schema names | PascalCase | Consistent (168/168) |
| JSON property names | snake_case | 2 violations (see cleanup item 4) |
| Operation IDs | camelCase | 26 violations (see cleanup item 3) |
| Path parameters | snake_case | Consistent (23/23) |
| Query parameters | snake_case | Consistent (12/12) |
| Enum values | snake_case | Consistent |
| Tag names | Title Case | 1 violation (see cleanup item 5) |
| Path segments (TMI-defined) | snake_case | Consistent |
| Path segments (RFC-mandated) | kebab-case | N/A — external standard |

## Cleanup Items

### 1. Move webhooks under `/admin/` (breaking change)

All webhook endpoints require administrator access (`RequireAdministrator()` in every handler). They belong under the `/admin/` domain prefix.

| Current Path | New Path |
|-------------|----------|
| `/webhooks/subscriptions` | `/admin/webhooks/subscriptions` |
| `/webhooks/subscriptions/{webhook_id}` | `/admin/webhooks/subscriptions/{webhook_id}` |
| `/webhooks/subscriptions/{webhook_id}/test` | `/admin/webhooks/subscriptions/{webhook_id}/test` |
| `/webhooks/deliveries` | `/admin/webhooks/deliveries` |
| `/webhooks/deliveries/{delivery_id}` | `/admin/webhooks/deliveries/{delivery_id}` |

**Changes required:**
- Update 5 path entries in `api-schema/tmi-openapi.json`
- Regenerate API code (`make generate-api`)
- Update handler method names in `api/webhook_handlers.go` to match new generated signatures
- Update all webhook handler tests
- Update CLAUDE.md references

### 2. Remove redundant inline admin checks

Path-level admin enforcement already exists via `adminRouteMiddleware()` in `cmd/server/main.go:937-945`, which applies `AdministratorMiddleware` to all `/admin/*` routes. However, some handlers under `/admin/` also check admin status inline, which is redundant:

- Webhook handlers: call `RequireAdministrator()` inline (7 occurrences) — redundant after webhook move to `/admin/`
- Config/settings handlers: check `IsUserAdministrator()` inline (6 occurrences) — redundant
- Survey admin handlers: no inline check — correctly relies on path middleware
- Quota handlers: no inline check — correctly relies on path middleware

**Fix:** Remove the redundant inline admin checks from webhook handlers (after they move to `/admin/`) and config/settings handlers. The path-level middleware already handles enforcement.

### 3. Fix operationId casing (26 violations)

All 26 violations are on Project and Team endpoints, using PascalCase instead of camelCase. OpenAPI convention is camelCase.

**Complete list of affected operationIds:**
- `BulkCreateProjectMetadata` → `bulkCreateProjectMetadata`
- `BulkCreateTeamMetadata` → `bulkCreateTeamMetadata`
- `BulkReplaceProjectMetadata` → `bulkReplaceProjectMetadata`
- `BulkReplaceTeamMetadata` → `bulkReplaceTeamMetadata`
- `BulkUpsertProjectMetadata` → `bulkUpsertProjectMetadata`
- `BulkUpsertTeamMetadata` → `bulkUpsertTeamMetadata`
- `CreateProject` → `createProject`
- `CreateProjectMetadata` → `createProjectMetadata`
- `CreateTeam` → `createTeam`
- `CreateTeamMetadata` → `createTeamMetadata`
- `DeleteProject` → `deleteProject`
- `DeleteProjectMetadata` → `deleteProjectMetadata`
- `DeleteTeam` → `deleteTeam`
- `DeleteTeamMetadata` → `deleteTeamMetadata`
- `GetProject` → `getProject`
- `GetProjectMetadata` → `getProjectMetadata`
- `GetTeam` → `getTeam`
- `GetTeamMetadata` → `getTeamMetadata`
- `ListProjects` → `listProjects`
- `ListTeams` → `listTeams`
- `PatchProject` → `patchProject`
- `PatchTeam` → `patchTeam`
- `UpdateProject` → `updateProject`
- `UpdateProjectMetadata` → `updateProjectMetadata`
- `UpdateTeam` → `updateTeam`
- `UpdateTeamMetadata` → `updateTeamMetadata`

**Changes required:**
- Update 26 operationIds in `api-schema/tmi-openapi.json`
- Regenerate API code (`make generate-api`)
- Update Go handler method names in `api/project_handlers.go`, `api/team_handlers.go`, and metadata handler files to match new generated signatures
- Update corresponding tests

### 4. Fix property name violations (2)

`dataAssetIds` in `MinimalNode` and `MinimalEdge` schemas should be `data_asset_ids` to match the snake_case convention used by all other 413+ properties.

**Changes required:**
- Update property names in `api-schema/tmi-openapi.json`
- Regenerate API code
- Update any Go code referencing the generated field name
- This is a breaking change for WebSocket diagram model payloads — clients sending/receiving `MinimalNode` or `MinimalEdge` must update

### 5. Fix tag name (1)

`webhooks` tag should be `Webhooks` to match Title Case convention of all other tags.

**Changes required:**
- Update tag name in `api-schema/tmi-openapi.json` (both in the top-level `tags` array and in all endpoint `tags` references)
- No code impact — tags are documentation/UI only

### 6. Document URL pattern guidelines

Add the pattern taxonomy and selection criteria from this spec to:

- **CLAUDE.md** — under the existing "API Design Guidelines" section
- **GitHub wiki** — as a new page or section in the API design documentation

Content to add: the URL Pattern Taxonomy table, the Selection Criteria decision list, and the Naming Conventions table from this spec.

## Client Notification

After all server-side breaking changes are implemented, file a GitHub issue against `ericfitz/tmi-ux` detailing the exact changes client developers need to make:

- Webhook endpoint URL changes (`/webhooks/` → `/admin/webhooks/`)
- `dataAssetIds` → `data_asset_ids` in WebSocket diagram model payloads (`MinimalNode`, `MinimalEdge`)
- Any operationId changes that affect generated client SDKs

The issue must be:
- In the **TMI** GitHub project
- Status: **In Progress**
- Assigned to: **ericfitz**
- Milestone: **1.4.0**

## Non-Goals

- **No URL restructuring beyond webhooks** — threat_models, survey_responses, audit_trail, etc. keep their current paths
- **No underscore-to-hyphen migration** — snake_case is the established TMI convention for path segments
- **No addon endpoint relocation** — addons have a mixed access model (admin creates, all users invoke) and don't fit cleanly under `/admin/`
- **No schema name changes** — all 168 schema names already follow PascalCase correctly

## Breaking Changes Summary

| Change | Affected Consumers |
|--------|-------------------|
| Webhook URLs move to `/admin/` | API clients calling webhook endpoints |
| `dataAssetIds` → `data_asset_ids` | WebSocket diagram model consumers |
| 26 operationId casing changes | Code generators / SDK consumers using operationIds |

All breaking changes are appropriate for the 1.4.0 feature release.
