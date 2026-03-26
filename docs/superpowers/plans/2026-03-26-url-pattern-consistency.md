# URL Pattern Consistency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Formalize TMI's URL pattern taxonomy, move webhooks under `/admin/`, fix naming convention violations, remove redundant admin checks, and document guidelines.

**Architecture:** Changes are concentrated in the OpenAPI spec (`api-schema/tmi-openapi.json`), with cascading updates to generated code (`api/api.go`), handler files, tests, and documentation. Each task produces a working, buildable codebase.

**Tech Stack:** OpenAPI 3.0.3, oapi-codegen (Go/Gin), jq for JSON manipulation, `make` targets for build/test/lint.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `api-schema/tmi-openapi.json` | Modify | Rename webhook paths, fix operationIds, fix property names, fix tag |
| `api/api.go` | Regenerate | Generated from OpenAPI spec |
| `api/webhook_handlers.go` | Modify | Remove inline `RequireAdministrator()` calls (now handled by path-level middleware), update method names if generated interface changes |
| `api/webhook_handlers_test.go` | Modify | Update URL paths from `/webhooks/` to `/admin/webhooks/` |
| `api/project_handlers.go` | Modify | Rename handler methods to match new camelCase operationIds |
| `api/project_handlers_test.go` | Modify | Update method name references |
| `api/team_handlers.go` | Modify | Rename handler methods to match new camelCase operationIds |
| `api/team_handlers_test.go` | Modify | Update method name references |
| `api/diagram_model_transform.go` | Modify | Update `DataAssetIds` → `DataAssetIds` field references (field name changes in generated code) |
| `api/config_handlers.go` | Modify | Remove inline `IsUserAdministrator()` checks for `/admin/settings` endpoints |
| `api/addon_handlers.go` | Modify | Remove inline `requireAdministrator()` calls for addon create/delete |
| `api/admin_quota_handlers.go` | No change needed | Already has no inline admin checks (correctly relies on path middleware) |
| `CLAUDE.md` | Modify | Add URL pattern guidelines to API Design Guidelines section |

---

### Task 1: Fix tag name `webhooks` → `Webhooks`

**Files:**
- Modify: `api-schema/tmi-openapi.json`

This is the smallest change and a good warmup to verify the build pipeline.

- [ ] **Step 1: Fix the tag definition**

Use jq to update the tag name in the top-level `tags` array:

```bash
cd /Users/efitz/Projects/tmi
jq '(.tags[] | select(.name == "webhooks")).name = "Webhooks"' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 2: Fix tag references on all webhook endpoints**

Update all operation-level tag references from `"webhooks"` to `"Webhooks"`:

```bash
jq 'walk(if type == "object" and .tags? then .tags |= map(if . == "webhooks" then "Webhooks" else . end) else . end)' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 3: Validate the spec**

```bash
make validate-openapi
```

Expected: Passes with same score as before (tag name is cosmetic).

- [ ] **Step 4: Regenerate API code and build**

```bash
make generate-api
make build-server
```

Expected: Both pass. Tag name changes don't affect generated Go code.

- [ ] **Step 5: Run unit tests**

```bash
make test-unit
```

Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "fix(api): capitalize webhooks tag name to Webhooks for consistency

All other tags use Title Case. This fixes the one exception.

Refs #131"
```

---

### Task 2: Fix `dataAssetIds` → `data_asset_ids` property name

**Files:**
- Modify: `api-schema/tmi-openapi.json` (property names in MinimalNode and MinimalEdge schemas, plus examples)
- Regenerate: `api/api.go`
- Modify: `api/diagram_model_transform.go` (field references change from `DataAssetIds` to `DataAssetIds` — verify generated name)

- [ ] **Step 1: Identify all occurrences in the spec**

```bash
grep -n 'dataAssetIds' api-schema/tmi-openapi.json
```

Expected output shows property definitions in MinimalNode and MinimalEdge schemas, plus example values. Note the line numbers for the property definitions and examples.

- [ ] **Step 2: Rename the property in the OpenAPI spec**

Use jq to rename the property in both schemas and update examples:

```bash
jq '
  # Rename property in MinimalNode
  .components.schemas.MinimalNode.properties.data_asset_ids = .components.schemas.MinimalNode.properties.dataAssetIds |
  del(.components.schemas.MinimalNode.properties.dataAssetIds) |

  # Rename property in MinimalEdge
  .components.schemas.MinimalEdge.properties.data_asset_ids = .components.schemas.MinimalEdge.properties.dataAssetIds |
  del(.components.schemas.MinimalEdge.properties.dataAssetIds) |

  # Update any examples that reference the old property name
  walk(if type == "object" and has("dataAssetIds") and (has("shape") or has("source") or has("cell_id")) then
    .data_asset_ids = .dataAssetIds | del(.dataAssetIds)
  else . end)
' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 3: Update the description referencing `dataAssetIds`**

Search for `"description": "Asset objects referenced by cells in this diagram via dataAssetIds"` and update it:

```bash
jq 'walk(if type == "string" and test("dataAssetIds") then gsub("dataAssetIds"; "data_asset_ids") else . end)' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 4: Verify no remaining references to `dataAssetIds`**

```bash
grep -c 'dataAssetIds' api-schema/tmi-openapi.json
```

Expected: `0`

- [ ] **Step 5: Validate the spec**

```bash
make validate-openapi
```

Expected: Passes.

- [ ] **Step 6: Regenerate API code**

```bash
make generate-api
```

- [ ] **Step 7: Check what the generated field name is**

```bash
grep -n 'DataAssetIds\|data_asset_ids\|DataAssetID' api/api.go | head -10
```

The generated Go struct field will likely be `DataAssetIds` (from `data_asset_ids` via oapi-codegen's snake_case → PascalCase conversion). Note the exact name.

- [ ] **Step 8: Update diagram_model_transform.go**

If the generated field name changed, update all references in `api/diagram_model_transform.go`. Search for the old field name and replace:

```bash
grep -n 'DataAssetIds' api/diagram_model_transform.go
```

Update each reference to match the new generated field name. Also update the GoJoGraph data key string from `"dataAssetIds"` to `"data_asset_ids"` at lines ~556 and ~589:

In `api/diagram_model_transform.go`, find:
```go
data = append(data, GraphData{Key: "dataAssetIds", Value: string(assetIdsJSON)})
```
Replace with:
```go
data = append(data, GraphData{Key: "data_asset_ids", Value: string(assetIdsJSON)})
```

Do this for both the node (line ~556) and edge (line ~589) versions.

- [ ] **Step 9: Build and test**

```bash
make build-server
make test-unit
```

Expected: Both pass.

- [ ] **Step 10: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go api/diagram_model_transform.go
git commit -m "fix(api): rename dataAssetIds to data_asset_ids for snake_case consistency

BREAKING CHANGE: MinimalNode and MinimalEdge WebSocket payloads now use
data_asset_ids instead of dataAssetIds. This aligns with the snake_case
convention used by all other 413+ API properties.

Refs #131"
```

---

### Task 3: Fix operationId casing on Project and Team endpoints

**Files:**
- Modify: `api-schema/tmi-openapi.json` (26 operationIds)
- Regenerate: `api/api.go`
- Modify: `api/project_handlers.go` (handler method names)
- Modify: `api/project_handlers_test.go` (method references)
- Modify: `api/team_handlers.go` (handler method names)
- Modify: `api/team_handlers_test.go` (method references)

- [ ] **Step 1: Create a jq script to fix all 26 operationIds**

```bash
cat > /tmp/fix-operation-ids.jq << 'EOF'
# Walk all paths and fix PascalCase operationIds to camelCase
.paths |= with_entries(
  .value |= with_entries(
    if .value | type == "object" and has("operationId") then
      .value.operationId |= (
        if . == "ListProjects" then "listProjects"
        elif . == "CreateProject" then "createProject"
        elif . == "GetProject" then "getProject"
        elif . == "PatchProject" then "patchProject"
        elif . == "DeleteProject" then "deleteProject"
        elif . == "UpdateProject" then "updateProject"
        elif . == "GetProjectMetadata" then "getProjectMetadata"
        elif . == "CreateProjectMetadata" then "createProjectMetadata"
        elif . == "UpdateProjectMetadata" then "updateProjectMetadata"
        elif . == "DeleteProjectMetadata" then "deleteProjectMetadata"
        elif . == "BulkCreateProjectMetadata" then "bulkCreateProjectMetadata"
        elif . == "BulkReplaceProjectMetadata" then "bulkReplaceProjectMetadata"
        elif . == "BulkUpsertProjectMetadata" then "bulkUpsertProjectMetadata"
        elif . == "ListTeams" then "listTeams"
        elif . == "CreateTeam" then "createTeam"
        elif . == "GetTeam" then "getTeam"
        elif . == "PatchTeam" then "patchTeam"
        elif . == "DeleteTeam" then "deleteTeam"
        elif . == "UpdateTeam" then "updateTeam"
        elif . == "GetTeamMetadata" then "getTeamMetadata"
        elif . == "CreateTeamMetadata" then "createTeamMetadata"
        elif . == "UpdateTeamMetadata" then "updateTeamMetadata"
        elif . == "DeleteTeamMetadata" then "deleteTeamMetadata"
        elif . == "BulkCreateTeamMetadata" then "bulkCreateTeamMetadata"
        elif . == "BulkReplaceTeamMetadata" then "bulkReplaceTeamMetadata"
        elif . == "BulkUpsertTeamMetadata" then "bulkUpsertTeamMetadata"
        else .
        end
      )
    else .
    end
  )
)
EOF
jq -f /tmp/fix-operation-ids.jq api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 2: Verify all PascalCase operationIds are gone**

```bash
jq -r '.paths | to_entries[] | .value | to_entries[] | .value.operationId // empty' api-schema/tmi-openapi.json | grep -E '^[A-Z]'
```

Expected: No output (all operationIds now start with lowercase).

- [ ] **Step 3: Validate the spec**

```bash
make validate-openapi
```

Expected: Passes.

- [ ] **Step 4: Regenerate API code**

```bash
make generate-api
```

- [ ] **Step 5: Try to build — expect compilation errors**

```bash
make build-server 2>&1 | head -40
```

Expected: Compilation errors because the generated `ServerInterface` now expects method names like `ListProjects` → still `ListProjects` (oapi-codegen PascalCases operationIds for Go method names regardless). Wait — check the actual generated names:

```bash
grep -n 'ListProjects\|listProjects\|CreateProject\|createProject' api/api.go | head -10
```

Note the exact method names oapi-codegen generated. If oapi-codegen converts `listProjects` to `ListProjects` (exported Go name), then the handler methods may not need renaming. If it generates `ListProjects` from both `ListProjects` and `listProjects`, the handlers are already correct.

- [ ] **Step 6: Rename handler methods if needed**

If the generated interface method names changed, update handler methods in `api/project_handlers.go` and `api/team_handlers.go` to match. For example, if `ListProjects` became `ListProjects` (unchanged because Go exports with PascalCase), no rename is needed.

If method names did change (unlikely with oapi-codegen), rename all affected methods:
- `api/project_handlers.go`: 12 methods (ListProjects, CreateProject, GetProject, PatchProject, DeleteProject, UpdateProject, plus 6 metadata methods)
- `api/team_handlers.go`: 12 methods (same pattern)

- [ ] **Step 7: Build and test**

```bash
make build-server
make test-unit
```

Expected: Both pass.

- [ ] **Step 8: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go api/project_handlers.go api/project_handlers_test.go api/team_handlers.go api/team_handlers_test.go
git commit -m "fix(api): use camelCase for Project and Team operationIds

26 operationIds changed from PascalCase to camelCase to match OpenAPI
convention used by all other endpoints. Examples:
- CreateProject -> createProject
- ListTeams -> listTeams

Refs #131"
```

---

### Task 4: Move webhook endpoints under `/admin/`

**Files:**
- Modify: `api-schema/tmi-openapi.json` (5 path entries)
- Regenerate: `api/api.go`
- Modify: `api/webhook_handlers.go` (update method names, remove inline admin checks)
- Modify: `api/webhook_handlers_test.go` (update URL paths)

- [ ] **Step 1: Move webhook paths in the OpenAPI spec**

```bash
jq '
  # Move /webhooks/subscriptions -> /admin/webhooks/subscriptions
  .paths["/admin/webhooks/subscriptions"] = .paths["/webhooks/subscriptions"] |
  del(.paths["/webhooks/subscriptions"]) |

  # Move /webhooks/subscriptions/{webhook_id} -> /admin/webhooks/subscriptions/{webhook_id}
  .paths["/admin/webhooks/subscriptions/{webhook_id}"] = .paths["/webhooks/subscriptions/{webhook_id}"] |
  del(.paths["/webhooks/subscriptions/{webhook_id}"]) |

  # Move /webhooks/subscriptions/{webhook_id}/test -> /admin/webhooks/subscriptions/{webhook_id}/test
  .paths["/admin/webhooks/subscriptions/{webhook_id}/test"] = .paths["/webhooks/subscriptions/{webhook_id}/test"] |
  del(.paths["/webhooks/subscriptions/{webhook_id}/test"]) |

  # Move /webhooks/deliveries -> /admin/webhooks/deliveries
  .paths["/admin/webhooks/deliveries"] = .paths["/webhooks/deliveries"] |
  del(.paths["/webhooks/deliveries"]) |

  # Move /webhooks/deliveries/{delivery_id} -> /admin/webhooks/deliveries/{delivery_id}
  .paths["/admin/webhooks/deliveries/{delivery_id}"] = .paths["/webhooks/deliveries/{delivery_id}"] |
  del(.paths["/webhooks/deliveries/{delivery_id}"])
' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add `x-admin-only: true` to new webhook endpoints (if not already present)**

Check if the webhook endpoints already have `x-admin-only`:

```bash
jq '.paths["/admin/webhooks/subscriptions"].get["x-admin-only"] // "missing"' api-schema/tmi-openapi.json
```

If missing, add it to all webhook operations:

```bash
jq '
  .paths | to_entries | map(select(.key | startswith("/admin/webhooks"))) | .[].key as $path |
  .paths[$path] | to_entries | map(select(.value | type == "object" and has("operationId"))) | .[].key as $method |
  .paths[$path][$method]["x-admin-only"] = true
' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

If the jq above is complex, add `x-admin-only` manually via targeted jq commands for each of the 7 webhook operations.

- [ ] **Step 3: Verify no old webhook paths remain**

```bash
jq -r '.paths | keys[]' api-schema/tmi-openapi.json | grep '^/webhooks'
```

Expected: No output.

```bash
jq -r '.paths | keys[]' api-schema/tmi-openapi.json | grep 'admin/webhooks'
```

Expected: 5 paths listed.

- [ ] **Step 4: Validate the spec**

```bash
make validate-openapi
```

Expected: Passes.

- [ ] **Step 5: Regenerate API code**

```bash
make generate-api
```

- [ ] **Step 6: Check generated method names**

```bash
grep -n 'Webhook' api/api.go | grep 'func\|interface' | head -20
```

Note the new generated method names. The path change from `/webhooks/` to `/admin/webhooks/` will likely change the generated method names (e.g., `ListWebhookSubscriptions` → `ListAdminWebhookSubscriptions` or similar). Record the exact names.

- [ ] **Step 7: Update webhook handler method names**

In `api/webhook_handlers.go`, rename all 8 handler methods to match the new generated `ServerInterface` method names. For example, if the generated interface now expects `ListAdminWebhookSubscriptions`:

```go
// Before:
func (s *Server) ListWebhookSubscriptions(c *gin.Context, params ListWebhookSubscriptionsParams) {

// After (method name matches generated interface):
func (s *Server) ListAdminWebhookSubscriptions(c *gin.Context, params ListAdminWebhookSubscriptionsParams) {
```

Update all 8 methods:
- `ListWebhookSubscriptions` (line 29)
- `CreateWebhookSubscription` (line 84)
- `GetWebhookSubscription` (line 222)
- `DeleteWebhookSubscription` (line 245)
- `TestWebhookSubscription` (line 290)
- `ListWebhookDeliveries` (line 369)
- `GetWebhookDelivery` (line 448)
- `addWebhookRateLimitHeaders` (line 17 — private helper, may not need renaming)

Also update the parameter type names to match the regenerated types.

- [ ] **Step 8: Remove redundant inline `RequireAdministrator()` calls**

The `adminRouteMiddleware()` in `cmd/server/main.go:937-945` already applies `AdministratorMiddleware` to all `/admin/*` paths. Now that webhooks are under `/admin/`, remove the 7 inline `RequireAdministrator()` checks in `api/webhook_handlers.go`.

Remove each block like this:

```go
// Before:
func (s *Server) ListAdminWebhookSubscriptions(c *gin.Context, params ListAdminWebhookSubscriptionsParams) {
	logger := slogging.Get().WithContext(c)

	if _, err := RequireAdministrator(c); err != nil {
		return // Error response already sent by RequireAdministrator
	}
	// ... rest of handler

// After:
func (s *Server) ListAdminWebhookSubscriptions(c *gin.Context, params ListAdminWebhookSubscriptionsParams) {
	logger := slogging.Get().WithContext(c)

	// ... rest of handler (admin check handled by adminRouteMiddleware)
```

Lines to update: 33-34, 88-89, 226-227, 249-250, 294-295, 373-374, 452-453.

- [ ] **Step 9: Update webhook handler tests**

In `api/webhook_handlers_test.go`, update all URL paths from `/webhooks/` to `/admin/webhooks/`:

Line 521: `r.GET("/webhooks/:webhook_id"` → `r.GET("/admin/webhooks/:webhook_id"`
Line 526: `r.DELETE("/webhooks/:webhook_id"` → `r.DELETE("/admin/webhooks/:webhook_id"`
Line 531: `r.POST("/webhooks/:webhook_id/test"` → `r.POST("/admin/webhooks/:webhook_id/test"`
Line 536: `r.GET("/webhooks/deliveries"` → `r.GET("/admin/webhooks/deliveries"`
Line 540: `r.GET("/webhooks/deliveries/:delivery_id"` → `r.GET("/admin/webhooks/deliveries/:delivery_id"`

And all request URLs:
Lines 961, 984, 1010: `/webhooks/` → `/admin/webhooks/`
Lines 1052, 1072, 1097: `/webhooks/` → `/admin/webhooks/`
Lines 1147, 1184, 1203, 1230: `/webhooks/` → `/admin/webhooks/`
Lines 1284, 1308, 1343: `/webhooks/deliveries/` → `/admin/webhooks/deliveries/`

Also update method name references (e.g., `server.ListWebhookSubscriptions` → `server.ListAdminWebhookSubscriptions`) to match the renamed handler methods from Step 7.

- [ ] **Step 10: Build and test**

```bash
make build-server
make test-unit
```

Expected: Both pass.

- [ ] **Step 11: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go api/webhook_handlers.go api/webhook_handlers_test.go
git commit -m "refactor(api): move webhook endpoints under /admin/ prefix

BREAKING CHANGE: All webhook endpoints moved from /webhooks/* to
/admin/webhooks/*. Webhook operations require administrator access,
which is now enforced by the path-level adminRouteMiddleware instead
of inline RequireAdministrator() calls in each handler.

Paths changed:
- /webhooks/subscriptions -> /admin/webhooks/subscriptions
- /webhooks/subscriptions/{id} -> /admin/webhooks/subscriptions/{id}
- /webhooks/subscriptions/{id}/test -> /admin/webhooks/subscriptions/{id}/test
- /webhooks/deliveries -> /admin/webhooks/deliveries
- /webhooks/deliveries/{id} -> /admin/webhooks/deliveries/{id}

Refs #131"
```

---

### Task 5: Remove redundant inline admin checks from other `/admin/` handlers

**Files:**
- Modify: `api/config_handlers.go` (remove 6 inline `IsUserAdministrator()` checks)
- Modify: `api/addon_handlers.go` (remove 2 inline `requireAdministrator()` calls)

The `adminRouteMiddleware()` at `cmd/server/main.go:937-945` already enforces admin access for all `/admin/*` paths. However, addon endpoints are NOT under `/admin/` — they're at `/addons/`. So addon handlers legitimately need their inline checks. Only `/admin/settings/*` handlers have redundant checks.

- [ ] **Step 1: Verify which config handler endpoints are under `/admin/`**

```bash
jq -r '.paths | keys[]' api-schema/tmi-openapi.json | grep admin/settings
```

Expected: `/admin/settings`, `/admin/settings/{key}`, `/admin/settings/migrate`, `/admin/settings/reencrypt`

- [ ] **Step 2: Remove inline admin checks from config_handlers.go**

In `api/config_handlers.go`, the `/admin/settings` handlers check `IsUserAdministrator()` inline. Since `adminRouteMiddleware` handles this, remove the checks.

Find each block like:

```go
isAdmin, err := IsUserAdministrator(c)
if err != nil || !isAdmin {
    c.JSON(http.StatusForbidden, Error{...})
    return
}
```

At lines: 359, 415, 500, 613, 691, 803.

Remove each admin check block. The handler logic after the check remains unchanged.

- [ ] **Step 3: Do NOT modify addon_handlers.go**

Addon endpoints (`/addons/*`) are NOT under `/admin/` and have a mixed access model. Their inline `requireAdministrator()` calls for Create and Delete are correct and necessary. Leave them as-is.

- [ ] **Step 4: Build and test**

```bash
make build-server
make test-unit
```

Expected: Both pass.

- [ ] **Step 5: Commit**

```bash
git add api/config_handlers.go
git commit -m "refactor(api): remove redundant admin checks from /admin/settings handlers

The adminRouteMiddleware already enforces administrator access for all
/admin/* paths. The inline IsUserAdministrator() checks in settings
handlers were redundant.

Refs #131"
```

---

### Task 6: Document URL pattern guidelines in CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add URL pattern guidelines after the existing API Design Guidelines section**

In `CLAUDE.md`, find the `### API Design Guidelines` section (ends before `### Logging Requirements`). Add the following subsection before `### Logging Requirements`:

```markdown
### URL Pattern Guidelines

TMI uses five URL pattern categories. When adding a new API resource, apply these criteria in order:

| Pattern | Authorization Model | Example | When to Use |
|---------|-------------------|---------|-------------|
| **Resource-hierarchical** | Ownership-based (readers/writers/owners) | `/threat_models/{id}/assets/{id}` | Access controlled by parent resource ownership |
| **Domain-segregated** | Workflow-stage-based | `/admin/surveys/{id}`, `/intake/surveys/{id}` | Same resource needs different capabilities per workflow stage |
| **User-scoped** | Current authenticated user | `/me/preferences` | Personal resources for the requesting user |
| **Cross-cutting** | Mixed (ownership + admin) | `/projects/{id}`, `/addons/{id}` | Top-level resources not nested under threat models |
| **Protocol** | Per RFC specification | `/oauth2/token`, `/.well-known/jwks.json` | Auth/identity endpoints defined by external standards |

**Selection criteria (apply in order):**
1. Auth/identity protocol endpoint? → **Protocol** (follow RFC for URL structure)
2. Personal resource scoped to current user? → **User-scoped** under `/me/`
3. Resource has a parent that controls access? → **Resource-hierarchical** (nest under parent)
4. Multi-stage workflow with different role capabilities? → **Domain-segregated** (default to `/admin/`, `/intake/`, `/triage/`; new prefix only when workflow doesn't fit)
5. Top-level resource with own access control? → **Cross-cutting** at root level

**Key question:** Does authorization flow from a parent entity (resource-hierarchical) or from the workflow context (domain-segregated)?

**Naming conventions:**
- TMI-defined path segments: `snake_case` (e.g., `threat_models`, `audit_trail`)
- RFC-mandated path segments: `kebab-case` (e.g., `openid-configuration`) — under `/.well-known/` only
- Schema names: `PascalCase`
- JSON properties: `snake_case`
- Operation IDs: `camelCase`
- Path/query parameters: `snake_case`
- Tag names: `Title Case`
```

- [ ] **Step 2: Lint**

```bash
make lint
```

Expected: Passes (CLAUDE.md changes are documentation-only).

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add URL pattern guidelines to CLAUDE.md

Documents the five URL pattern categories (resource-hierarchical,
domain-segregated, user-scoped, cross-cutting, protocol), selection
criteria for new resources, and naming conventions.

Refs #131"
```

---

### Task 7: Update GitHub wiki with pattern guidelines

**Files:**
- GitHub wiki (external)

- [ ] **Step 1: Check if an API design wiki page exists**

```bash
gh api repos/ericfitz/tmi/pages 2>/dev/null || echo "Check wiki manually"
```

If a wiki page for API design exists, update it. If not, the CLAUDE.md documentation from Task 6 is sufficient for now. The wiki can be updated as a follow-up.

- [ ] **Step 2: If wiki page exists, add the same content from Task 6 Step 1**

Use the GitHub web interface or `gh` CLI to update the wiki. The content is identical to what was added to CLAUDE.md in Task 6.

- [ ] **Step 3: No commit needed** (wiki is a separate repository)

---

### Task 8: File client bug for breaking changes

**Files:**
- New GitHub issue on `ericfitz/tmi-ux`

- [ ] **Step 1: Create the client issue**

```bash
gh issue create --repo ericfitz/tmi-ux \
  --title "fix(api): update client for TMI server API breaking changes (1.4.0)" \
  --assignee ericfitz \
  --milestone "1.4.0" \
  --label "bug" \
  --body "$(cat <<'EOF'
## Summary

TMI server 1.4.0 includes breaking API changes that require client updates. These changes improve API consistency (see ericfitz/tmi#131).

## Required Changes

### 1. Webhook endpoint URLs moved to `/admin/`

All webhook API calls must update their base path:

| Old Path | New Path |
|----------|----------|
| `GET /webhooks/subscriptions` | `GET /admin/webhooks/subscriptions` |
| `POST /webhooks/subscriptions` | `POST /admin/webhooks/subscriptions` |
| `GET /webhooks/subscriptions/{id}` | `GET /admin/webhooks/subscriptions/{id}` |
| `PATCH /webhooks/subscriptions/{id}` | `PATCH /admin/webhooks/subscriptions/{id}` |
| `DELETE /webhooks/subscriptions/{id}` | `DELETE /admin/webhooks/subscriptions/{id}` |
| `POST /webhooks/subscriptions/{id}/test` | `POST /admin/webhooks/subscriptions/{id}/test` |
| `GET /webhooks/deliveries` | `GET /admin/webhooks/deliveries` |
| `GET /webhooks/deliveries/{id}` | `GET /admin/webhooks/deliveries/{id}` |

### 2. WebSocket diagram model property renamed

In `MinimalNode` and `MinimalEdge` payloads (used in WebSocket diagram collaboration):

- **Old property:** `dataAssetIds`
- **New property:** `data_asset_ids`

Search client code for `dataAssetIds` and replace with `data_asset_ids`. This affects any code that:
- Sends or receives WebSocket diagram model messages
- Parses `MinimalNode` or `MinimalEdge` objects
- References the `dataAssetIds` field in TypeScript interfaces or type definitions

### 3. No action required for operationId changes

The 26 operationId casing changes (e.g., `CreateProject` → `createProject`) only affect code generators that consume the OpenAPI spec directly. If the client uses generated API clients, regenerate them from the updated spec. If the client uses hardcoded URLs, no change is needed.

## References

- Server issue: ericfitz/tmi#131
- Design spec: `docs/superpowers/specs/2026-03-26-url-pattern-consistency-design.md`
EOF
)"
```

- [ ] **Step 2: Add the issue to the TMI project with correct status**

```bash
# Get the issue number from the previous command output
ISSUE_URL=$(gh issue list --repo ericfitz/tmi-ux --state open --search "update client for TMI server API breaking changes" --json url --jq '.[0].url')
ISSUE_NUMBER=$(echo $ISSUE_URL | grep -o '[0-9]*$')

# Add to TMI project (project #2)
gh project item-add 2 --owner ericfitz --url "$ISSUE_URL"

# Set status to "In Progress" (get item ID first)
ITEM_ID=$(gh project item-list 2 --owner ericfitz --format json | jq -r ".items[] | select(.content.url == \"$ISSUE_URL\") | .id")
gh project item-edit --project-id PVT_kwHOACjZhM4BC0Z1 --id "$ITEM_ID" --field-id <STATUS_FIELD_ID> --single-select-option-id <IN_PROGRESS_OPTION_ID>
```

Note: The `--field-id` and `--single-select-option-id` values need to be discovered at runtime:

```bash
# Get field IDs
gh project field-list 2 --owner ericfitz --format json | jq '.fields[] | select(.name == "Status")'

# Get option IDs for the Status field
gh project field-list 2 --owner ericfitz --format json | jq '.fields[] | select(.name == "Status") | .options'
```

Use the discovered IDs to set the status to "In Progress".

- [ ] **Step 3: Verify the issue is correctly configured**

```bash
gh issue view $ISSUE_NUMBER --repo ericfitz/tmi-ux --json title,assignees,milestone,labels
```

Expected: Assigned to ericfitz, milestone 1.4.0, label "bug".

---

### Task 9: Close issue #131

- [ ] **Step 1: Run integration tests to verify everything works end-to-end**

```bash
make test-integration
```

Expected: All pass.

- [ ] **Step 2: Close the issue**

```bash
gh issue close 131 --repo ericfitz/tmi --reason completed --comment "Completed: URL pattern taxonomy documented, webhooks moved to /admin/, operationId casing fixed (26), dataAssetIds renamed to data_asset_ids, tag name fixed, redundant admin checks removed. Client issue filed: ericfitz/tmi-ux#<NUMBER>."
```

- [ ] **Step 3: Push all commits**

```bash
git pull --rebase
git push
git status
```

Expected: `Your branch is up to date with 'origin/dev/1.4.0'`.
