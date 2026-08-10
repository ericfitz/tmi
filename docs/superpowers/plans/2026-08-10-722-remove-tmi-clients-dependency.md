# Remove the tmi-clients Dependency (#722) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove TMI's build dependency on `ericfitz/tmi-clients` entirely, replacing `cmd/dbtool`'s five generated-SDK calls with the raw-HTTP helper already present in the same file, and deleting all staging plumbing that existed only to serve that dependency.

**Architecture:** `cmd/dbtool/data_api.go` gains a generic `findExistingByFieldHTTP` core (extracted from the existing `findExistingByNameHTTP`) and expresses all five former SDK lookups through it. With no importer left, `go.mod`, five Dockerfiles, a Makefile target, two Python scripts, five CI checkout steps, and the `.docker-deps` mechanism all become dead and are deleted.

**Tech Stack:** Go 1.26, Gin, `net/http/httptest` for tests, Docker/BuildKit, GitHub Actions, Python (uv + unittest) for the build scripts.

## Global Constraints

- Spec source of truth: `docs/superpowers/specs/2026-08-10-722-tmi-clients-dependency-removal-design.md`.
- Branch: `fix/722-remove-tmi-clients-dependency`. `main` is PR-only; the PR squash-merges to one commit.
- Never run `go test`, `go run`, `./bin/tmiserver`, `docker run`, or `docker exec` directly. Use Make targets. `go mod tidy` and `go build` via Make targets are fine.
- Logging: `internal/slogging` only. Never the stdlib `log` package, never `fmt.Println`.
- Array property names come from `api-schema/tmi-openapi.json` response schemas, verified in the spec — do not guess them.
- `findExistingByNameHTTP(path, itemsKey, name string) string` MUST keep its exact signature; nine existing call sites depend on it.
- SEM markers: any function whose body changes or is added needs its `SEM@<sha>` marker added/updated (`/sem-annotate --update <files>`).
- No `.docker-deps` references may remain anywhere after Task 4.

---

### Task 1: Replace dbtool's five SDK helpers with raw HTTP

**Files:**
- Modify: `cmd/dbtool/data_api.go` (imports 3-19; `apiClient` struct 25-33; `newAPIClient` 35-54; helpers 186-252 and 318-333; `findExistingByNameHTTP` 272-295)
- Test: `cmd/dbtool/data_api_find_test.go` (create)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func (c *apiClient) findExistingByFieldHTTP(path, itemsKey, matchKey, want, idKey string) string`. `findExistingByNameHTTP(path, itemsKey, name string) string` keeps its signature and becomes a wrapper. `findExistingTM`, `findExistingTeam`, `findExistingProject`, `findExistingWebhook`, `findExistingGroup` keep their existing signatures (`func (c *apiClient) findExistingX(name string) string`). Task 2 depends on `cmd/dbtool` no longer importing `tmiclient`.

- [ ] **Step 1: Write the failing test**

Create `cmd/dbtool/data_api_find_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestAPIClient returns an apiClient pointed at a stub server.
func newTestAPIClient(t *testing.T, handler http.HandlerFunc) *apiClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newAPIClient(srv.URL, "test-token")
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode stub response: %v", err)
	}
}

// The /admin/groups case: matches on group_name, returns internal_uuid.
func TestFindExistingByFieldHTTP_UsesMatchKeyAndIDKey(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"groups": []any{
			map[string]any{"group_name": "other", "internal_uuid": "uuid-other"},
			map[string]any{"group_name": "administrators", "internal_uuid": "uuid-admin"},
		}})
	})

	got := c.findExistingByFieldHTTP("/admin/groups", "groups", "group_name", "administrators", "internal_uuid")
	if got != "uuid-admin" {
		t.Fatalf("got %q, want %q", got, "uuid-admin")
	}
}

func TestFindExistingByFieldHTTP_NoMatchReturnsEmpty(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"groups": []any{
			map[string]any{"group_name": "other", "internal_uuid": "uuid-other"},
		}})
	})

	if got := c.findExistingByFieldHTTP("/admin/groups", "groups", "group_name", "absent", "internal_uuid"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestFindExistingByFieldHTTP_ErrorStatusReturnsEmpty(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	if got := c.findExistingByFieldHTTP("/teams", "teams", "name", "anything", "id"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// Auth previously came from the SDK's configured DefaultHeader; it must still
// be sent now that these lookups go through apiRequest.
func TestFindExistingByFieldHTTP_SendsBearerToken(t *testing.T) {
	var gotAuth string
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(t, w, map[string]any{"teams": []any{}})
	})

	c.findExistingByFieldHTTP("/teams", "teams", "name", "x", "id")
	if gotAuth != "Bearer test-token" {
		t.Fatalf("got %q, want %q", gotAuth, "Bearer test-token")
	}
}

func TestFindExistingByNameHTTP_DefaultsToNameAndID(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"teams": []any{
			map[string]any{"name": "platform", "id": "team-1"},
		}})
	})

	if got := c.findExistingByNameHTTP("/teams", "teams", "platform"); got != "team-1" {
		t.Fatalf("got %q, want %q", got, "team-1")
	}
}

// Each former SDK helper must hit the right path and read the right array key.
func TestFindExistingHelpers_UseCorrectPathAndItemsKey(t *testing.T) {
	cases := []struct {
		name     string
		wantPath string
		itemsKey string
		idKey    string
		nameKey  string
		call     func(c *apiClient) string
	}{
		{"threat model", "/threat_models", "threat_models", "id", "name",
			func(c *apiClient) string { return c.findExistingTM("target") }},
		{"team", "/teams", "teams", "id", "name",
			func(c *apiClient) string { return c.findExistingTeam("target") }},
		{"project", "/projects", "projects", "id", "name",
			func(c *apiClient) string { return c.findExistingProject("target") }},
		{"webhook", "/admin/webhooks/subscriptions", "subscriptions", "id", "name",
			func(c *apiClient) string { return c.findExistingWebhook("target") }},
		{"group", "/admin/groups", "groups", "internal_uuid", "group_name",
			func(c *apiClient) string { return c.findExistingGroup("target") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				writeJSON(t, w, map[string]any{tc.itemsKey: []any{
					map[string]any{tc.nameKey: "target", tc.idKey: "found-id"},
				}})
			})

			if got := tc.call(c); got != "found-id" {
				t.Fatalf("got %q, want %q", got, "found-id")
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-unit name=TestFindExistingByFieldHTTP_UsesMatchKeyAndIDKey`

Expected: FAIL — `c.findExistingByFieldHTTP undefined (type *apiClient has no field or method findExistingByFieldHTTP)`.

- [ ] **Step 3: Add the generic core and rewrite `findExistingByNameHTTP` as a wrapper**

In `cmd/dbtool/data_api.go`, replace the whole existing `findExistingByNameHTTP` function (its comment block plus body, currently lines 272-295) with:

```go
// findExistingByFieldHTTP searches a list endpoint for the item whose matchKey
// equals want, and returns the value of its idKey.
//
// Most TMI collections are name/id shaped, but /admin/groups matches on
// group_name and identifies rows by internal_uuid, so both the match field and
// the id field are parameters rather than assumptions.
// SEM: search a list endpoint by field and return the matching resource ID (pure)
func (c *apiClient) findExistingByFieldHTTP(path, itemsKey, matchKey, want, idKey string) string {
	result, status, err := c.apiRequest("GET", path+"?limit=100", nil)
	if err != nil || status >= 300 {
		return ""
	}
	items, ok := result[itemsKey].([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := m[matchKey].(string); got != want {
			continue
		}
		// extractID, not a string assertion: a few resources (TriageNote) use
		// an integer id, and this is the idempotency check -- getting it wrong
		// re-creates the resource on every seed rather than reusing it.
		if id, ok := extractID(m[idKey]); ok {
			return id
		}
	}
	return ""
}

// findExistingByNameHTTP finds a resource by its name property and returns its id.
// SEM: search a list endpoint by name and return the matching resource ID (pure)
func (c *apiClient) findExistingByNameHTTP(path, itemsKey, name string) string {
	return c.findExistingByFieldHTTP(path, itemsKey, "name", name, "id")
}
```

- [ ] **Step 4: Rewrite the five former SDK helpers**

Replace the block currently spanning `findExistingTM` through `findExistingWebhook` (lines 186-252, i.e. everything under the `// --- Idempotency helpers using SDK typed list responses ---` banner up to but NOT including `findExistingSurvey`) with:

```go
// --- Idempotency helpers ---

// SEM: fetch the ID of a threat model by name, returning empty if absent
func (c *apiClient) findExistingTM(name string) string {
	return c.findExistingByNameHTTP("/threat_models", "threat_models", name)
}

// SEM: fetch the ID of a team by name, returning empty if absent
func (c *apiClient) findExistingTeam(name string) string {
	return c.findExistingByNameHTTP("/teams", "teams", name)
}

// SEM: fetch the ID of a project by name, returning empty if absent
func (c *apiClient) findExistingProject(name string) string {
	return c.findExistingByNameHTTP("/projects", "projects", name)
}

// SEM: fetch the ID of a webhook subscription by name, returning empty if absent
func (c *apiClient) findExistingWebhook(name string) string {
	return c.findExistingByNameHTTP("/admin/webhooks/subscriptions", "subscriptions", name)
}
```

Then replace `findExistingGroup` (lines 318-333) with:

```go
// AdminGroup matches on group_name and identifies rows by internal_uuid rather
// than id, which is why the generic by-field helper exists.
// SEM: fetch the internal UUID of an admin group by group name, returning empty if absent
func (c *apiClient) findExistingGroup(groupName string) string {
	return c.findExistingByFieldHTTP("/admin/groups", "groups", "group_name", groupName, "internal_uuid")
}
```

Also update the `// --- Idempotency helpers using SDK typed list responses ---` banner already replaced above — confirm no `SDK` wording remains.

- [ ] **Step 5: Remove the SDK from the client struct and constructor**

In `cmd/dbtool/data_api.go`, change the import block to drop both `context` and the `tmiclient` import (nothing else in the file uses `context`; verified `c.ctx` has no users outside this file):

```go
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)
```

Replace the `apiClient` struct and `newAPIClient` with:

```go
// apiClient holds the auth context for API seeding.
// SEM: authenticated API client bundling bearer token and optional DB connection
type apiClient struct {
	serverURL string
	token     string
	db        *testdb.TestDB
}

// SEM: build an authenticated API client configured for a server URL and bearer token
func newAPIClient(serverURL, token string) *apiClient {
	return &apiClient{
		serverURL: serverURL,
		token:     token,
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `make test-unit name=TestFindExisting`

Expected: PASS for all `TestFindExisting*` tests, including the five subtests of `TestFindExistingHelpers_UseCorrectPathAndItemsKey`.

- [ ] **Step 7: Verify nothing in TMI still imports the client**

Run: `rg -n "tmiclient|tmi-clients/go-client-generated" --glob '!docs/**' --glob '!graphify-out/**' .`

Expected: matches only in `go.mod` (removed in Task 2) and the Dockerfiles/scripts (Tasks 2-4). No match in any `.go` file.

- [ ] **Step 8: Commit**

```bash
git add cmd/dbtool/data_api.go cmd/dbtool/data_api_find_test.go
git commit -m "refactor(dbtool): replace generated-SDK lookups with raw HTTP helpers"
```

---

### Task 2: Drop the go.mod dependency and the Dockerfile staging

**Files:**
- Modify: `go.mod` (replace line 7; require line 17)
- Modify: `Dockerfile.server:16-21`, `Dockerfile.server-oracle:85-90`, `Dockerfile.extractor:18-23`, `Dockerfile.chunkembed:19-24`
- Modify: `Dockerfile.controller:13-35`

**Interfaces:**
- Consumes: Task 1's removal of the `tmiclient` import (`go mod tidy` will not drop the require while an importer exists).
- Produces: a `go.mod` with no `tmi-clients` line, and Dockerfiles that build with no `.docker-deps` in the build context. Tasks 3 and 4 rely on this.

- [ ] **Step 1: Remove the go.mod require and replace**

Delete line 7 entirely:

```
replace github.com/ericfitz/tmi-clients/go-client-generated/v1_8_3 => ../tmi-clients/go-client-generated/v1_8_3
```

Delete the require line inside the main `require (` block:

```
	github.com/ericfitz/tmi-clients/go-client-generated/v1_8_3 v0.0.0-00010101000000-000000000000
```

- [ ] **Step 2: Tidy and confirm the module graph is clean**

Run: `go mod tidy && rg -n "tmi-clients" go.mod go.sum`

Expected: `go mod tidy` exits 0; the `rg` prints nothing (exit 1).

- [ ] **Step 3: Remove client staging from the four builder Dockerfiles**

In each of `Dockerfile.server`, `Dockerfile.server-oracle`, `Dockerfile.extractor`, `Dockerfile.chunkembed`, replace this five-line block:

```dockerfile
# Copy go mod files and staged tmi-client dependency
COPY go.mod go.sum ./
COPY .docker-deps/tmi-client/ /tmi-client/

# Rewrite go.mod replace directive to point at the in-container client path
RUN sed -i 's|=> ../tmi-clients/go-client-generated/[^ ]*|=> /tmi-client|' go.mod
```

with:

```dockerfile
# Copy go mod files
COPY go.mod go.sum ./
```

- [ ] **Step 4: Remove the drop-requirement dance from Dockerfile.controller**

Replace lines 13-25 (the comment block, `COPY go.mod go.sum ./`, and the first `RUN mod=$(sed ...)`) with:

```dockerfile
# Copy go mod files
COPY go.mod go.sum ./
```

Then delete the second occurrence, which currently follows `COPY . .`:

```dockerfile
# Copy source code. This overwrites go.mod with the original (still
# containing the tmi-clients require/replace), so drop it again before
# building.
COPY . .
RUN mod=$(sed -n 's|^replace \(github.com/ericfitz/tmi-clients/go-client-generated/v[0-9_]*\) =>.*|\1|p' go.mod) && \
    if [ -n "$mod" ]; then go mod edit -dropreplace="$mod" -droprequire="$mod"; fi
```

leaving just:

```dockerfile
# Copy source code
COPY . .
```

- [ ] **Step 5: Verify the Go build still works**

Run: `make build-dbtool && make build-server`

Expected: both print `[SUCCESS]`.

- [ ] **Step 6: Verify the container builds that previously needed staging**

Run: `make build-server-container`

Expected: build completes. This is the key check — it proves the removed `COPY .docker-deps/tmi-client/` was the only thing that path needed.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum Dockerfile.server Dockerfile.server-oracle Dockerfile.extractor Dockerfile.chunkembed Dockerfile.controller
git commit -m "build: drop the tmi-clients module and its Dockerfile staging"
```

---

### Task 3: Remove the Makefile target and the Python staging code

**Files:**
- Modify: `Makefile:897-899` (`.PHONY`), `Makefile:920-941` (target + comment), `Makefile:942-955` (recipe call sites)
- Modify: `scripts/build-app-containers.py:44-53, 56-221, 438-484`
- Modify: `scripts/lib/deploy.py:282-380, 396-437`
- Delete: `scripts/lib/tests/test_build_app_containers.py`

**Interfaces:**
- Consumes: Task 2's `go.mod` with no tmi-clients line.
- Produces: build scripts with no client-staging concept. No later task depends on these symbols.

- [ ] **Step 1: Delete the Makefile staging target and its comment**

Remove the comment block beginning `# Stage the tmi-client Go module into .docker-deps/tmi-client/` together with the entire `stage-worker-docker-deps:` target (through the `echo "Staged tmi-client dependency: ..."` line), and the following comment block beginning `# Each container build stages the tmi-client dependency itself`.

- [ ] **Step 2: Remove the target from `.PHONY` and from both recipes**

Change:

```make
.PHONY: build-extractor build-chunkembed build-workers test-workers \
        stage-worker-docker-deps build-extractor-container build-chunkembed-container \
        build-controller-container
```

to:

```make
.PHONY: build-extractor build-chunkembed build-workers test-workers \
        build-extractor-container build-chunkembed-container \
        build-controller-container
```

Change both container recipes, deleting the `$(MAKE) stage-worker-docker-deps` and `@rm -rf .docker-deps` lines:

```make
build-extractor-container:  ## Build the tmi-extractor container image
	docker build -f Dockerfile.extractor -t tmi-extractor:dev .

build-chunkembed-container:  ## Build the tmi-chunk-embed container image
	docker build -f Dockerfile.chunkembed -t tmi-chunk-embed:dev .
```

- [ ] **Step 3: Delete the client code from `scripts/build-app-containers.py`**

Delete these five functions in full: `_branch_to_client_version`, `_resolve_client_path`, `_go_mod_client_version`, `_resolve_client_version`, `stage_client_dependency`, and also `cleanup_staged_dependencies` (`.docker-deps` has no other user). Delete `_get_git_branch` only if nothing else calls it — check with `rg -n "_get_git_branch" scripts/`.

Delete the constants and their comment:

```python
# `controller` is NOT here: cmd/component-controller has zero tmi-clients
# imports and Dockerfile.controller drops the go.mod require/replace inside
# the image (go mod edit -droprequire) before `go mod download`, so it needs
# no staging (#550).
CLIENT_DEPENDENT_COMPONENTS = ("server", "extractor", "chunkembed")

# Staging directory for external dependencies copied into the Docker build context
DOCKER_DEPS_DIR = ".docker-deps"
STAGED_CLIENT_DIR = "tmi-client"
```

In `main()`, delete the staging block:

```python
    # Stage tmi-client dependency into build context if any client-dependent
    # component (server or a platform worker) is being built.
    staged_client = None
    if any(c in CLIENT_DEPENDENT_COMPONENTS for c in components):
        staged_client = stage_client_dependency(project_root)
```

and collapse the now-empty `finally`:

```python
    finally:
        if staged_client:
            cleanup_staged_dependencies(project_root)
```

Remove the `try:`/`finally:` wrapper entirely and de-indent its body one level, since nothing remains to clean up. Remove any imports (`shutil`, `re`, `os`) that become unused — verify each with `rg` before deleting.

- [ ] **Step 4: Delete the client code from `scripts/lib/deploy.py`**

Delete the section banner and all four functions: `_resolve_client_path`, `_resolve_client_version`, `stage_tmi_client`, `unstage_tmi_client`.

```python
# ---------------------------------------------------------------------------
# tmi-client staging
# ---------------------------------------------------------------------------
```

In the image-build function, delete `created = stage_tmi_client()` and the `finally: unstage_tmi_client(created)`, un-wrapping the `try:` and de-indenting its body. Update the docstring, which currently reads:

```
    All four Dockerfiles require the tmi-client staged in .docker-deps/.
    Stage once before the first build and clean up in a try/finally block
    so the staging dir is always removed even if a build fails — but only
    when this run created it (pre-existing dirs are left untouched).
```

Replace those four lines with nothing, keeping the rest of the docstring intact. Remove imports that become unused.

- [ ] **Step 5: Delete the obsolete test file**

The entire file tests client version resolution (`"""Unit tests for tmi-client version resolution in scripts/build-app-containers.py."""`) — all three classes and 16 tests target deleted functions.

```bash
git rm scripts/lib/tests/test_build_app_containers.py
```

- [ ] **Step 6: Run the Python helper tests**

Run: `make test-dev-scripts`

Expected: all remaining test modules pass; no import errors from the deleted symbols.

- [ ] **Step 7: Verify the worker and controller container builds**

Run: `make build-extractor-container && make build-chunkembed-container && make build-controller-container`

Expected: all three build. The first two exercise the deleted Makefile staging; the third exercises `build-app-containers.py`.

- [ ] **Step 8: Commit**

```bash
git add Makefile scripts/build-app-containers.py scripts/lib/deploy.py
git add -u scripts/lib/tests/
git commit -m "build: delete tmi-client staging from Makefile and build scripts"
```

---

### Task 4: Remove the CI provisioning steps and ignore entries

**Files:**
- Modify: `.github/workflows/security.yml` (4 step pairs, near lines 29-35, 83-89, and two more)
- Modify: `.github/workflows/codeql.yml` (1 step pair, near lines 49-58)
- Modify: `.github/codeql/codeql-config.yml:15-19`
- Modify: `.gitignore:164`, `.dockerignore:25-32`

**Interfaces:**
- Consumes: Task 2's `go.mod` (the `sed` these steps run has nothing left to rewrite).
- Produces: CI that checks out only TMI. Final task verifies.

- [ ] **Step 1: Delete all five checkout + sed step pairs**

In `.github/workflows/security.yml` (4 occurrences) and `.github/workflows/codeql.yml` (1 occurrence), delete this exact pair each time:

```yaml
      - name: Provision generated client (tmi-clients)
        uses: actions/checkout@v7
        with:
          repository: ericfitz/tmi-clients
          path: .tmi-clients
      - name: Point go.mod replace at the checked-out client
        run: sed -i 's|=> ../tmi-clients/|=> ./.tmi-clients/|' go.mod
```

Leave surrounding steps and blank-line spacing intact.

- [ ] **Step 2: Remove the CodeQL path exclusion**

In `.github/codeql/codeql-config.yml`, delete the comment and entry:

```yaml
  # CI-time checkout of the generated API client repo (ericfitz/tmi-clients).
  # OpenAPI-generator's stock template includes a Debug-gated httputil.DumpRequestOut
  # that logs request headers (incl. API key) when cfg.Debug=true — real pattern, but
  # third-party generated code we don't maintain here. Never enable Debug in production.
  - ".tmi-clients/**"
```

- [ ] **Step 3: Remove the ignore entries**

In `.gitignore`, delete the line `.docker-deps/`.

In `.dockerignore`, delete the whole NOTE block, since the directory it protects no longer exists:

```
# NOTE: Do NOT ignore .docker-deps/ here. The build scripts stage the
# tmi-client Go module into .docker-deps/tmi-client/ and every Dockerfile
# pulls it in via an explicit `COPY .docker-deps/tmi-client/ /tmi-client/`.
# .dockerignore filters the entire build context, so ignoring .docker-deps/
# would strip the staged module out and break that COPY. The directory only
# lands in the throwaway builder stage (the final image copies just the
# binary), so the slight duplication via `COPY . .` is harmless.
```

Also fix the stale cross-reference earlier in `.dockerignore` (line ~8), which reads `harmless like the .docker-deps note below; the final` — change that clause to `harmless; the final`.

- [ ] **Step 4: Verify the workflow YAML is still valid**

Run: `uv run --with pyyaml python -c "import yaml,sys; [yaml.safe_load(open(f)) for f in ['.github/workflows/security.yml','.github/workflows/codeql.yml','.github/codeql/codeql-config.yml']]; print('YAML OK')"`

Expected: `YAML OK`.

- [ ] **Step 5: Verify no reference to the dependency survives anywhere**

Run: `rg -n "tmi-clients|docker-deps|tmi-client" --glob '!docs/superpowers/**' --glob '!graphify-out/**' --glob '!HANDOFF.md' .`

Expected: exactly one match — the README line pointing at the clients repo, which the spec keeps deliberately.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/security.yml .github/workflows/codeql.yml .github/codeql/codeql-config.yml .gitignore .dockerignore
git commit -m "ci: stop provisioning the generated client"
```

---

### Task 5: Version bump, SEM refresh, and full verification

**Files:**
- Modify: `.version`, `api-schema/tmi-openapi.json` (`info.version`)
- Modify: `api/api.go` (regenerated)
- Modify: `cmd/dbtool/data_api.go` (SEM markers)

**Interfaces:**
- Consumes: Tasks 1-4 complete.
- Produces: the merge-ready branch.

- [ ] **Step 1: Bump both version strings**

Versioning is manual (see #627). This is not a `feat:`, so bump PATCH. Set `.version` to:

```json
{
  "major": 1,
  "minor": 8,
  "patch": 5,
  "prerelease": ""
}
```

Set `info.version` in `api-schema/tmi-openapi.json` to `1.8.5`:

```bash
jq '.info.version = "1.8.5"' api-schema/tmi-openapi.json > /tmp/spec.json && mv /tmp/spec.json api-schema/tmi-openapi.json
```

- [ ] **Step 2: Confirm the oapi-codegen version before regenerating**

Run: `oapi-codegen --version`

Expected: `v2.7.1`. **If it is not v2.7.1, stop** — a different version silently miscompiles `api/api.go` (the `UnderscoreMetadata` rename). Report to the user instead of proceeding.

- [ ] **Step 3: Validate and regenerate the API**

Run: `make validate-openapi && make generate-api`

Expected: validation passes; `api/api.go` regenerates.

- [ ] **Step 4: Confirm the regenerated diff is only the version string**

Run: `git diff --stat api/api.go && git diff api/api.go | rg '^[-+]' | rg -v '^[-+][-+]' | head -20`

Expected: a small diff containing only the embedded-spec version change. Anything structural means the wrong codegen version ran — stop and report.

- [ ] **Step 5: Refresh SEM markers on the changed file**

Run: `/sem-annotate --update cmd/dbtool/data_api.go`

Expected: markers on the new and modified functions get real `SEM@<sha>` anchors replacing the bare `SEM:` placeholders written in Task 1.

- [ ] **Step 6: Run the full local gate**

Run each and confirm before moving on:

```
make lint
make test-unit
make test-dev-scripts
```

Expected: lint `0 issues`; unit tests all pass with `cmd/dbtool` reported `ok`; Python helpers pass.

- [ ] **Step 7: Run integration tests against a clean database**

The test DB must be reset first — `TestGoogleWorkspaceDelegated_EndToEnd_Integration` leaves a NULL-`provider_user_id` user behind, and on a second run the #720 startup guard aborts the server. Redis and the OAuth stub must also be up or the workflow suite silently skips.

```
make clean-test-database
make start-test-redis
make start-oauth-stub
make test-integration
```

Expected: `[SUCCESS] All integration tests passed`, ~85 passed / 0 failed across 7 packages, including `test/integration/workflows`. A run reporting only ~23 tests in 4 packages means the workflow suite was skipped — that is NOT a pass.

- [ ] **Step 8: Prove seed idempotency end to end**

This is the behavioral check for Task 1: the five rewritten helpers exist only to make seeding idempotent.

```
make cats-seed
make cats-seed
```

Expected: the second run logs `already exists (skipping)` for threat models, teams, projects, webhooks, and groups. Any line creating a duplicate of an already-seeded resource means a rewritten helper is reading the wrong array key or match field.

- [ ] **Step 9: Commit**

```bash
git add .version api-schema/tmi-openapi.json api/api.go cmd/dbtool/data_api.go
git commit -m "chore: bump to 1.8.5 and refresh SEM markers"
```

- [ ] **Step 10: Push and open the PR**

```bash
git push -u origin fix/722-remove-tmi-clients-dependency
gh pr create --title "fix: remove the tmi-clients build dependency (#722)" --body "..."
```

The PR body must state that #722 is resolved by *removing* the dependency rather than integrating the new client, and must include `Closes #722`. Because the repo squash-merges, the PR title is the conventional-commit subject that lands on `main`.

---

## Self-Review

**Spec coverage.** Section 1 (dbtool rewrite) → Task 1. Section 2 (plumbing excision) → Tasks 2, 3, 4, covering every row of the spec's table: go.mod and 5 Dockerfiles in Task 2; Makefile, both Python scripts, and their tests in Task 3; CI workflows, CodeQL config, and both ignore files in Task 4. Section 3 (verification) → Task 5 steps 6-8, including the container builds distributed into Tasks 2 and 3 where they verify their own change. Section 4 (out of scope) → honored: the README line is untouched and appears as the single expected match in Task 4 step 5.

**Placeholder scan.** No TBD/TODO. Every code step carries literal code. The one deliberate conditional is Task 3 step 3's `_get_git_branch` check, which names the exact `rg` command that decides it.

**Type consistency.** `findExistingByFieldHTTP(path, itemsKey, matchKey, want, idKey string) string` is used with that argument order in Task 1 steps 1, 3, and 4. `findExistingByNameHTTP(path, itemsKey, name string) string` keeps its original signature throughout. The five helpers keep `func (c *apiClient) findExistingX(name string) string`, matching every existing call site in `seedThreatModel`, `seedTeam`, `seedProject`, `seedGroup`, and `seedWebhook`.

**Gap found and fixed during review.** Task 1 originally left the `// --- Idempotency helpers using SDK typed list responses ---` banner referring to an SDK that no longer exists; step 4 now replaces it. Task 4 originally missed the stale `.docker-deps` cross-reference at the top of `.dockerignore`; step 3 now fixes it.
