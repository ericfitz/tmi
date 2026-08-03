# Sub-Resource Tenancy Enforcement (#664) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Centrally verify that the child id in every `/threat_models/{threat_model_id}/<family>/{child_id}...` path actually belongs to that threat model, answering 404 on mismatch, closing the cross-tenant read/write hole in issue #664.

**Architecture:** One new gate in `AuthzMiddleware` (api/authz_middleware.go), invoked right after the parent-ACL ownership check passes. It parses the child family + child id from the path and runs a single indexed existence query (`id = ? AND threat_model_id = ?`, plus `deleted_at IS NULL` except on `/restore` routes) through the package-level `adminDB` GORM handle. The query is behind a package-var function seam so unit tests inject a fake; the real SQL is proven by integration tests. Because the check is keyed off the path, it uniformly covers CRUD, `/metadata/...`, `/audit_trail`, `/restore`, `/request_access`, `/collaborate`, and `/model` sub-routes for all six child families (threats, assets, documents, notes, repositories, diagrams) — including the child-metadata routes that today verify nothing at all.

**Tech Stack:** Go, Gin middleware, GORM (`adminDB` global at api/admin_checker.go:15), google/uuid, testify; integration harness `test/integration/framework`.

## Global Constraints

- **404 on mismatch, never 403** — 403 would confirm the id exists (existence oracle). Acceptance criterion in #664.
- **Checker DB error → 503 + `Retry-After: 30`** (matches the ThreatModelStore-nil arm at api/authz_middleware.go:299-307; 503 is documented on all 328 operations as of 1.7.0, and 404 is documented on all sub-resource operations).
- **`adminDB == nil` (in-memory/unit-test mode) → check passes.** `InitializeMockStores` never sets `adminDB`; unit tests would all 404 otherwise. Production and integration always run `InitializeGormStores`.
- **Never hardcode table names** — query via `db.Model(&models.X{})` so dialect-aware `TableName()` applies (Oracle).
- **MANDATORY make targets only**: `make lint`, `make build-server`, `make test-unit name=...`, `make test-integration`. Never `go test` directly. Never run two integration suites concurrently (shared server/database).
- Integration test functions must use the `_Integration` name suffix to be picked up by `make test-integration`.
- New/changed functions get `SEM@<sha>` one-line intent markers (match style in api/authz_middleware.go; run `/sem-annotate --update` at landing).
- Conventional commits; version bump to **1.7.1** (fix → PATCH; manual versioning per #627) in both `.version` and `api-schema/tmi-openapi.json` `info.version`.
- Do NOT remove the existing per-handler diagram/chat/audit/feedback parentage checks — the middleware gate is defense in depth on top of them.

---

### Task 1: Middleware child-parentage gate

**Files:**
- Modify: `api/authz_middleware.go` (new map, seam var, two functions; call site in `authzMiddlewareWithTable`)
- Test: `api/authz_middleware_subresource_test.go` (new test func; fix stale comment at line 197)

**Interfaces:**
- Consumes: `adminDB *gorm.DB` (api/admin_checker.go:15), `models.{Threat,Asset,Document,Note,Repository,Diagram}` (api/models/models.go), `Error` response struct, `slogging`.
- Produces: package var `checkChildParentage childParentageChecker` (signature `func(ctx context.Context, family, childID, threatModelID string, includeDeleted bool) (bool, error)`) — Task 2's integration tests exercise the default implementation end-to-end; unit tests here swap the var.

- [ ] **Step 1: Write the failing unit test**

Append to `api/authz_middleware_subresource_test.go`:

```go
// TestAuthzMiddleware_ChildParentage exercises the #664 gate: after the
// parent-ACL check passes, the middleware must verify the child id in the
// path belongs to the parent threat model, answering 404 (not 403) on
// mismatch so foreign ids are indistinguishable from nonexistent ones.
func TestAuthzMiddleware_ChildParentage(t *testing.T) {
	InitTestFixtures()
	tmID := TestFixtures.ThreatModelID
	const childID = "22222222-2222-2222-2222-222222222222"

	type call struct {
		family, childID, tmID string
		includeDeleted        bool
	}
	var calls []call
	restore := checkChildParentage
	t.Cleanup(func() { checkChildParentage = restore })

	setChecker := func(ok bool, err error) {
		calls = nil
		checkChildParentage = func(_ context.Context, family, cid, tid string, incDel bool) (bool, error) {
			calls = append(calls, call{family, cid, tid, incDel})
			return ok, err
		}
	}

	r := newSubResourceAuthzRouter()

	do := func(method, path string, u authzOwnershipUser) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		attachAuthzUser(req, u) // use the same request-identity helper the RoleMatrix test uses
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("mismatch answers 404 not 403", func(t *testing.T) {
		setChecker(false, nil)
		w := do(http.MethodGet, "/threat_models/"+tmID+"/threats/"+childID, authzUserReader)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("match passes through", func(t *testing.T) {
		setChecker(true, nil)
		w := do(http.MethodGet, "/threat_models/"+tmID+"/threats/"+childID, authzUserReader)
		assert.Equal(t, http.StatusOK, w.Code)
		if assert.Len(t, calls, 1) {
			assert.Equal(t, call{"threats", childID, tmID, false}, calls[0])
		}
	})

	t.Run("restore route includes soft-deleted rows", func(t *testing.T) {
		setChecker(true, nil)
		w := do(http.MethodPost, "/threat_models/"+tmID+"/threats/"+childID+"/restore", authzUserOwner)
		assert.Equal(t, http.StatusOK, w.Code)
		if assert.Len(t, calls, 1) {
			assert.True(t, calls[0].includeDeleted)
		}
	})

	t.Run("checker error answers 503", func(t *testing.T) {
		setChecker(false, errors.New("db down"))
		w := do(http.MethodGet, "/threat_models/"+tmID+"/assets/"+childID, authzUserReader)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Equal(t, "30", w.Header().Get("Retry-After"))
	})

	t.Run("child metadata routes are gated", func(t *testing.T) {
		setChecker(false, nil)
		w := do(http.MethodGet, "/threat_models/"+tmID+"/threats/"+childID+"/metadata/somekey", authzUserReader)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("literal bulk segment is not a child lookup", func(t *testing.T) {
		setChecker(false, nil)
		w := do(http.MethodDelete, "/threat_models/"+tmID+"/threats/bulk?threat_ids="+childID, authzUserOwner)
		assert.NotEqual(t, http.StatusNotFound, w.Code)
		assert.Empty(t, calls)
	})

	t.Run("non-family segments skip the check", func(t *testing.T) {
		setChecker(false, nil)
		w := do(http.MethodGet, "/threat_models/"+tmID+"/metadata/somekey", authzUserReader)
		assert.NotEqual(t, http.StatusNotFound, w.Code)
		assert.Empty(t, calls)
	})
}
```

Adapt the two seams to the file's actual helpers: `newSubResourceAuthzRouter` is at api/authz_middleware_subresource_test.go:148; copy whatever mechanism the `TestAuthzMiddleware_SubResources_RoleMatrix` cases use to attach the user identity to the request (there may be a helper or per-request header/context setup — mirror it exactly, and if it's not a standalone `attachAuthzUser` func, inline the same code). If a route needed above (e.g. `/threats/:threat_id/metadata/:key`, `/threats/bulk`, `/threat_models/:threat_model_id/metadata/:key`) is missing from the test router at lines 148-185, add it with the same `ok` stub handler. Add missing imports (`context`, `errors`, `net/http/httptest`).

Also fix the now-stale comment at line 197:

```go
const subID = "11111111-1111-1111-1111-111111111111" // nonexistent child: the #664 parentage gate passes in unit tests (adminDB == nil)
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-unit name=TestAuthzMiddleware_ChildParentage`
Expected: FAIL — `undefined: checkChildParentage` (compile error).

- [ ] **Step 3: Implement the gate in `api/authz_middleware.go`**

Add imports `context` and `github.com/google/uuid`. Then:

```go
// subResourceChildFamilies maps the path segment of every child family
// nested under /threat_models/{id}/ to its display name and GORM model.
// Families whose handlers already verify parentage (diagrams, and the
// non-family children chat sessions / audit entries / feedback) keep those
// checks; diagrams are ALSO listed here because their /metadata and
// /audit_trail sub-routes have no handler-level check (#664).
var subResourceChildFamilies = map[string]struct {
	singular string
	model    func() interface{}
}{
	"threats":      {"Threat", func() interface{} { return &models.Threat{} }},
	"assets":       {"Asset", func() interface{} { return &models.Asset{} }},
	"documents":    {"Document", func() interface{} { return &models.Document{} }},
	"notes":        {"Note", func() interface{} { return &models.Note{} }},
	"repositories": {"Repository", func() interface{} { return &models.Repository{} }},
	"diagrams":     {"Diagram", func() interface{} { return &models.Diagram{} }},
}

// childParentageChecker reports whether the child row exists under the given
// threat model. includeDeleted widens the probe to soft-deleted rows (restore
// routes). Package-var seam so unit tests can inject a fake.
type childParentageChecker func(ctx context.Context, family, childID, threatModelID string, includeDeleted bool) (bool, error)

var checkChildParentage childParentageChecker = gormChildParentageCheck

// SEM marker: verify a sub-resource child row belongs to a threat model (reads DB)
func gormChildParentageCheck(ctx context.Context, family, childID, threatModelID string, includeDeleted bool) (bool, error) {
	if adminDB == nil {
		// In-memory mode (unit tests): no relational store to consult.
		return true, nil
	}
	fam, ok := subResourceChildFamilies[family]
	if !ok {
		return true, nil
	}
	q := adminDB.WithContext(ctx).Model(fam.model()).
		Where("id = ? AND threat_model_id = ?", childID, threatModelID)
	if !includeDeleted {
		q = q.Where("deleted_at IS NULL")
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// SEM marker: enforce that the child id in a sub-resource path belongs to the parent threat model, answering 404 on mismatch
func enforceChildParentage(c *gin.Context) bool {
	logger := slogging.Get().WithContext(c)
	path := c.Request.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "threat_models" {
		return true
	}
	family, childID := parts[2], parts[3]
	fam, known := subResourceChildFamilies[family]
	if !known {
		return true
	}
	if _, err := uuid.Parse(childID); err != nil {
		// Literal segments like "bulk" and malformed ids are not child
		// lookups; downstream validation answers those.
		return true
	}
	tmID := parts[1]
	includeDeleted := c.Request.Method == http.MethodPost &&
		strings.HasSuffix(strings.TrimRight(path, "/"), "/restore")
	ok, err := checkChildParentage(c.Request.Context(), family, childID, tmID, includeDeleted)
	if err != nil {
		logger.Error("AuthzMiddleware: parentage check failed for %s %s: %v",
			c.Request.Method, path, err)
		c.Header("Retry-After", "30")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, Error{
			Error:            "service_unavailable",
			ErrorDescription: "Storage service temporarily unavailable - please retry",
		})
		return false
	}
	if !ok {
		logger.Warn("AuthzMiddleware: %s %s not in threat model %s (404 for %s %s)",
			family, childID, tmID, c.Request.Method, path)
		c.AbortWithStatusJSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: fam.singular + " not found",
		})
		return false
	}
	return true
}
```

Wire it into `authzMiddlewareWithTable` (currently lines 95-100) so it runs only after the ownership gate allows:

```go
		if rule.Ownership != OwnershipNone {
			if !enforceOwnership(c, rule.Ownership) {
				c.Abort()
				return
			}
			if !enforceChildParentage(c) {
				c.Abort()
				return
			}
		}
```

Write real `SEM@<current-short-sha>` markers in the file's existing style (see line 35) in place of the `SEM marker:` placeholders above.

- [ ] **Step 4: Run the new test and the existing suite**

Run: `make test-unit name=TestAuthzMiddleware_ChildParentage` — Expected: PASS.
Run: `make test-unit name=TestAuthzMiddleware_SubResources_RoleMatrix` — Expected: PASS (unit mode skips the DB probe; the RoleMatrix table is unchanged).

- [ ] **Step 5: Lint and build**

Run: `make lint` (0 issues) and `make build-server` (builds).

- [ ] **Step 6: Commit**

```bash
git add api/authz_middleware.go api/authz_middleware_subresource_test.go
git commit -m "fix(api): enforce sub-resource child parentage in AuthzMiddleware

A caller with any role on threat model A could read, update, patch, or
delete children of threat model B by naming B's child id under A's path:
no single-item sub-resource handler verified the child belongs to the
{threat_model_id} that authorization was granted against. Child metadata
routes verified nothing at all, and restore was equally unscoped.

One centralized gate now runs after the parent-ACL check for all six
child families (threats, assets, documents, notes, repositories,
diagrams) and every sub-route under a child id. Mismatches answer 404,
not 403, so the endpoint cannot be used as an existence oracle."
```

---

### Task 2: Cross-tenant integration tests

**Files:**
- Create: `test/integration/workflows/subresource_tenancy_test.go`

**Interfaces:**
- Consumes: `test/integration/framework` — `EnsureOAuthStubRunning`, `UniqueUserID`, `AuthenticateUser`, `NewClient`, `Request`, `AssertNoError`, `AssertStatusCreated`, `AssertStatusNotFound`, `ExtractID`, `NewThreatModelFixture`, plus the Task 1 middleware behavior (running server enforces it for real via `gormChildParentageCheck`).
- Produces: nothing downstream; this is the end-to-end proof.

- [ ] **Step 1: Write the test**

Model the file skeleton (env gating, oauth stub, client setup, `createThreatModel` helper) directly on `test/integration/workflows/threat_bulk_delete_test.go:31-62`. Two separate users — attacker and victim — each authenticated via `framework.AuthenticateUser(framework.UniqueUserID())` with their own client. The victim creates `tmVictim` with one child per family; the attacker creates `tmAttacker` (which the attacker fully owns) and then names the victim's child ids under `tmAttacker`'s path.

```go
//go:build integration
// (match the build-tag header used by threat_bulk_delete_test.go exactly; copy it)

package workflows

// TestSubResourceTenancy_Integration proves the #664 fix: a caller who
// owns threat model A cannot reach a child of threat model B through A's
// path. Every cross-parent request must answer 404 — never 403 (existence
// oracle), never 200, and DELETE/PATCH attempts must leave the victim's
// child untouched.
func TestSubResourceTenancy_Integration(t *testing.T) {
	// env gating + oauth stub + two clients (attacker, victim), per the
	// threat_bulk_delete_test.go pattern.

	// families: for each, a create-payload for the victim's child and a
	// minimal valid PATCH body.
	families := []struct {
		name        string // path segment
		createBody  map[string]interface{}
		patchBody   []map[string]interface{}
	}{
		{"threats", map[string]interface{}{
			"name": "victim threat", "description": "t",
			"threat_type": []string{"Spoofing"}, "severity": "low", "status": "Open",
		}, []map[string]interface{}{{"op": "replace", "path": "/name", "value": "pwned"}}},
		{"assets", framework.NewAssetFixture().(map...)  /* see note below */, ...},
		// documents, notes, repositories, diagrams likewise
	}
	// ... per-family subtests
}
```

Concretely, for each family run these subtests (attacker's client, victim's ids):

1. `GET /threat_models/{tmAttacker}/{family}/{victimChild}` → `AssertStatusNotFound` and assert the status is not 403.
2. `PATCH` same path with the family's minimal JSON-Patch body → 404, then victim's client `GET /threat_models/{tmVictim}/{family}/{victimChild}` → 200 and the field is unchanged.
3. `DELETE` same path → 404, then victim's `GET` → still 200.
4. `GET /threat_models/{tmAttacker}/{family}/{victimChild}/metadata` → 404 (the child-metadata hole).
5. `POST /threat_models/{tmAttacker}/{family}/{victimChild}/restore` → 404 (victim first soft-deletes their child via their own client for this case only, then restores it at the end so later subtests aren't affected — or order this subtest last per family).
6. Happy path control: victim's client `GET /threat_models/{tmVictim}/{family}/{victimChild}` → 200 throughout.

Child creation payloads: use `framework.New*Fixture()` builders where they exist (`NewThreatFixture`, `NewAssetFixture`, `NewDocumentFixture`, `NewRepositoryFixture`, `NewDiagramFixture` — see test/integration/framework/fixtures.go). There is no note fixture; build the note body from the `Note` schema in `api-schema/tmi-openapi.json` (look up its required fields with jq before guessing). If any create returns 400, read the response body and fix the payload against the spec — do not weaken the test.

Also add one non-family control subtest: `GET /threat_models/{tmAttacker}/metadata/{key}` with any key → must NOT 404 through the parentage gate (expect whatever the metadata handler answers for a missing key — likely 404 from the handler itself; the point is it must not 503 or panic; keep the assertion to "status is 404 or 200" with a comment).

- [ ] **Step 2: Run the integration suite**

Run: `make test-integration`
Expected: `TestSubResourceTenancy_Integration` PASSES along with the whole suite (83+ tests). Never run a second suite concurrently.

If the new test FAILS with cross-parent 200s, the middleware isn't wired on those routes — debug via `x-tmi-authz` coverage (api/authz_table.go), not by adding per-handler checks.

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/subresource_tenancy_test.go
git commit -m "test(integration): prove sub-resource tenancy isolation per family and verb"
```

---

### Task 3: Version bump + full gates

**Files:**
- Modify: `.version` (JSON: bump to 1.7.1)
- Modify: `api-schema/tmi-openapi.json` (`info.version` → 1.7.1, via jq with a backup per house rules)

- [ ] **Step 1: Bump both version stamps to 1.7.1**

```bash
cd /Users/efitz/Projects/tmi
cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.bak
jq '.info.version = "1.7.1"' api-schema/tmi-openapi.json.bak > api-schema/tmi-openapi.json
# .version is small JSON — edit its version field to 1.7.1 with jq the same way
jq empty api-schema/tmi-openapi.json && rm api-schema/tmi-openapi.json.bak
```

- [ ] **Step 2: Validate, regenerate, verify**

Run: `make validate-openapi` (0 errors) → `make generate-api` (oapi-codegen must be v2.7.1) → `make build-server` (prints embedded version 1.7.1) → `make lint` → `make test-unit` (all pass).

- [ ] **Step 3: Commit**

```bash
git add .version api-schema/tmi-openapi.json api/api.go
git commit -m "chore: bump version to 1.7.1"
```

---

### Task 4: Reviews (Oracle + security) and SEM markers

- [ ] **Step 1: Dispatch the `oracle-db-admin` subagent** on the diff (new GORM query in api/authz_middleware.go: `Model(&models.X{}).Where("id = ? AND threat_model_id = ?", ...)` + optional `deleted_at IS NULL`, `Count`). Address every BLOCKING finding; fold easy notes in; file follow-ups for the rest.
- [ ] **Step 2: Run the security-review skill.** Stop and report to the user if it finds anything.
- [ ] **Step 3: Refresh SEM markers**: `/sem-annotate --update api/authz_middleware.go` (plus any other touched Go files). Run `graphify update .` if graphify-out/graph.json exists.
- [ ] **Step 4: Commit any resulting fixes** with an appropriate conventional message.

---

### Task 5: Land

- [ ] **Step 1: Push** `git push -u origin dev/1.7.1/subresource-tenancy` (SSH key needs a physical touch; if it fails because the user is away, stop and report — do not work around).
- [ ] **Step 2: PR** to main titled `fix(api): enforce sub-resource child parentage in AuthzMiddleware` with `Closes #664` in the body (squash-merge reads the PR body).
- [ ] **Step 3: Merge when CI is green**; verify #664 auto-closed.

---

## Self-Review Notes

- Spec coverage vs #664 acceptance criteria: (1) every single-item op across the five families — covered by path-keyed gate, Task 1; (2) centralized — one middleware call site; (3) 404 not 403 — Task 1 Step 1 asserts it, Task 2 re-asserts over HTTP; (4) cross-parent tests per family and verb — Task 2; (5) child metadata gap — confirmed real (recon: `api/server.go:211-216` passes nil verifiers) and covered by the same gate + tested in both tasks.
- Known accepted trade-offs: +1 indexed PK probe per child request (no caching; YAGNI until measured); diagrams keep their redundant handler checks; chat sessions/audit entries/feedback rely on their existing handler checks (recon: api/timmy_handlers.go:691,706, api/audit_handlers.go:128,165, api/content_feedback_handlers.go:108).
- The unit test swaps a package var; tests in package `api` don't run those cases in parallel, and `t.Cleanup` restores it.
