# Removing the tmi-clients dependency (#722)

Date: 2026-08-10
Issue: #722
Status: approved

## Problem

`tmi-clients` deleted the generated Go client `go-client-generated/v1_6_0` while
TMI's `go.mod` still required and replaced it. Nothing warned us. The result:

- `cmd/dbtool` stopped compiling locally, which surfaced as
  `cmd/dbtool [setup failed]` in `make test-unit` and silently removed the
  end-to-end workflow suite from `make test-integration`.
- CI broke, and was worked around by pinning five `actions/checkout` steps
  (`security.yml` x4, `codeql.yml` x1) to SHA `eeda4f6`, the last commit that
  still contained `v1_6_0`.

The brief was to integrate the newly generated client, unpin CI, and design a
guard against recurrence. Investigating the guard produced a better answer than
any guard.

## What the dependency actually bought us

The generated client module is 18MB across 710 files: 334 `.go` files (6.1MB),
`docs/` (2.7MB), `test/` (208KB), and `api/openapi.yaml` (6.8MB, a second copy
of TMI's own specification).

`cmd/dbtool/data_api.go` is the only file in TMI that imports it, and it uses it
in exactly five places. Every one has the same shape: list a collection, find
the item whose name matches, return its id.

```go
result, resp, err := c.sdk.ThreatModelsAPI.ListThreatModels(c.ctx).Execute()
// ...scan result.GetThreatModels() for GetName() == name, return GetId()
```

The same file already contains `findExistingByNameHTTP(path, itemsKey, name)`,
which does that job over raw HTTP and is what surveys, addons, client
credentials, and every nested resource already use.

So TMI carried an 18MB cross-repo build dependency, a `../tmi-clients`
sibling-checkout assumption, and roughly 250 lines of staging plumbing, to
serve five list calls that duplicate a helper already present in the same file.

## Decision

Remove the dependency rather than vendor, submodule, or re-pin it.

This is the strongest available recurrence guard, because it is structural
rather than procedural: with no dependency, no sibling repository can break
TMI's build by deleting or regenerating anything. It also retires the
`../tmi-clients` sibling-checkout assumption that made local builds depend on
the state of an adjacent working copy.

Options considered and rejected:

- **Vendor or submodule the client.** Solves reproducibility but imports 6.1MB
  of generated Go into TMI (or adds submodule friction) to serve five calls.
- **Re-pin CI to a new tag.** Cheap, but only defers the same break, and
  requires a manual tag bump on every client migration.
- **Deletion check / policy in tmi-clients.** Prevents recurrence at the source,
  but only while the dependency exists; deleting the dependency subsumes it.

## Design

### 1. `cmd/dbtool/data_api.go`

Extract the body of `findExistingByNameHTTP` into a generic core, and express
all five former SDK helpers through it:

```go
func (c *apiClient) findExistingByFieldHTTP(path, itemsKey, matchKey, want, idKey string) string
func (c *apiClient) findExistingByNameHTTP(path, itemsKey, name string) string // → (..., "name", name, "id")
```

`findExistingByNameHTTP` keeps its exact signature, so its nine existing call
sites are untouched.

| Helper | Replacement |
|---|---|
| `findExistingTM` | `findExistingByNameHTTP("/threat_models", "threat_models", name)` |
| `findExistingTeam` | `findExistingByNameHTTP("/teams", "teams", name)` |
| `findExistingProject` | `findExistingByNameHTTP("/projects", "projects", name)` |
| `findExistingWebhook` | `findExistingByNameHTTP("/admin/webhooks/subscriptions", "subscriptions", name)` |
| `findExistingGroup` | `findExistingByFieldHTTP("/admin/groups", "groups", "group_name", groupName, "internal_uuid")` |

Array property names come from the response schemas in
`api-schema/tmi-openapi.json`, not from guesswork. `AdminGroup` is why the
generic core is needed: it matches on `group_name` and carries `internal_uuid`
rather than `id`.

Also removed: the `sdk` and `ctx` fields on `apiClient`, the `tmiclient` and
`context` imports, and the client configuration in `newAPIClient`. `c.ctx` is
referenced nowhere else in `cmd/dbtool`.

**Behavioral effect.** Unchanged in kind. Authentication is the same bearer
token, since `apiRequest` sets it. Failure semantics are identical: any error or
non-2xx status yields `""`, meaning "not found", so the seeder creates the
resource. Coverage slightly improves — the SDK calls passed no `limit` and so
scanned the server default of 50 (`LimitQueryParam.default`), while these scan
100.

### 2. Plumbing excision

All of the following exists solely to serve the dependency, and is deleted:

| Location | Removed |
|---|---|
| `go.mod` | require + replace, then `go mod tidy` |
| `Dockerfile.server`, `.server-oracle`, `.extractor`, `.chunkembed` | `COPY .docker-deps/tmi-client/` and `RUN sed -i` rewrite |
| `Dockerfile.controller` | the "drop the requirement from go.mod" blocks and comment |
| `Makefile` | `stage-worker-docker-deps` target and its 3 call sites |
| `scripts/build-app-containers.py` | 5 client resolution/staging functions, `cleanup_staged_dependencies` |
| `scripts/lib/deploy.py` | `_resolve_client_path`, `_resolve_client_version`, `stage_tmi_client`, `unstage_tmi_client` and call sites |
| `scripts/lib/tests/test_build_app_containers.py` | tests covering the deleted functions |
| `.github/workflows/security.yml` (x4), `codeql.yml` (x1) | tmi-clients checkout step + `sed` step pairs |
| `.github/codeql/codeql-config.yml` | `.tmi-clients/**` path exclusion |
| `.gitignore`, `.dockerignore` | `.docker-deps` entries |

The Dockerfile changes are mandatory, not optional. `stage_client_dependency`
returns `None` as soon as `go.mod` stops naming tmi-clients, so nothing stages
`.docker-deps/tmi-client`, and a Docker `COPY` of a missing path is a hard
error. `.docker-deps` has no other user, so the whole mechanism goes.

### 3. Verification

- `make lint`
- `make test-unit`
- `make test-dev-scripts` (the Python helpers whose tests are edited)
- `make test-integration` against a freshly reset test database

Because the container build path carries the real risk in this change, it is
built rather than assumed: `make build-dbtool`, `make build-server-container`,
`make build-controller-container`, and the two worker containers.

The decisive check is behavioral. These five helpers exist only for seed
idempotency, so `make cats-seed` is run twice against the dev cluster; the
second pass must log `already exists (skipping)` for threat models, teams,
projects, webhooks, and groups rather than creating duplicates.

### 4. Out of scope

The README pointer to the tmi-clients repository stays — it remains accurate,
and merely stops being a build dependency. No policy or deletion-check work in
tmi-clients: with the dependency gone there is nothing left to protect. The
`ci-pin-go-v1_6_0` tag in that repository becomes unused and may be deleted, but
that is a separate repository and a separate decision.

## Known adjacent issue (not addressed here)

`TestGoogleWorkspaceDelegated_EndToEnd_Integration` creates a user with a NULL
`provider_user_id` and never cleans it up. On a second run against the same test
database there are two such rows, and the #720 startup guard correctly refuses
to create `idx_users_sparse_email` and aborts the server. Integration tests
therefore only pass against a freshly reset test container. This is not fixed
here; it is tracked as a separate issue.
