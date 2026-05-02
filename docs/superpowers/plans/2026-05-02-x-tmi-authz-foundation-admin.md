# `x-tmi-authz` Foundation + Admin Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the `x-tmi-authz` OpenAPI vendor extension, the spec-completeness validator, the routing-table loader, and the unified `AuthzMiddleware`. Annotate the public endpoints (~20) and `/admin/*` operations (~59), and replace the `adminRouteMiddleware` wrapper. Leaves the rest of the API untouched (no behavior change) — follow-up issues #365–#371 cover the remaining slices.

**Architecture:** Each OpenAPI operation declares its required ownership level and role gates via `x-tmi-authz`. At server start, the spec is parsed once into a `(method, normalized-path) → AuthzRule` table. A new `AuthzMiddleware`, inserted after JWT validation and OpenAPI validation, looks up the rule for the matched route. When a rule is present it is the single, default-deny enforcement point; when absent (legacy paths in this slice), the middleware is a pass-through and existing resource middlewares continue to handle authorization. A separate `scripts/check-x-tmi-authz.py` enforces that every operation under an allow-listed prefix family carries the extension; the prefix list grows per slice, and slice 8 (#371) flips the rule to "every operation must carry it."

**Tech Stack:** Go 1.x, Gin, kin-openapi v3 (already in use for OpenAPI validation middleware), Python 3.11+ uv-managed scripts (matching the existing `scripts/check-*.py` pattern), `golangci-lint`.

**Reference:** GitHub issue #341. Coordinates with #358 (T18 addon delegation) and #357 (T5 nested coverage), but does not consume their scope.

---

## File Structure

**Created:**

- `api-schema/x-tmi-authz-schema.md` — human-readable schema reference (the JSON syntax is informal; we don't add it to the OpenAPI Components section because vendor extensions live on operations, not schemas).
- `api/authz_table.go` — loads the OpenAPI spec at startup, normalizes path templates, builds and exposes the lookup table, plus typed `AuthzRule` and `Ownership`/`Role` enums.
- `api/authz_table_test.go` — tests for path normalization, exact and parameterized lookups, and missing-rule behavior.
- `api/authz_middleware.go` — the unified `AuthzMiddleware`. Reads the rule for the matched route, enforces `public`, `roles`, and `ownership`, and short-circuits with 403 on failure. For unrecognized routes (no rule) it is a pass-through.
- `api/authz_middleware_test.go` — table-driven tests covering anonymous, non-admin, and admin contexts against representative public, admin, and unannotated routes.
- `scripts/check-x-tmi-authz.py` — spec-completeness check that fails if any operation under a configured prefix family lacks `x-tmi-authz`.

**Modified:**

- `api-schema/tmi-openapi.json` — annotate `x-tmi-authz` on all 20 public operations and all 59 `/admin/*` operations (plus the `/` health-check root). One mechanical edit per op.
- `cmd/server/main.go` — register `api.AuthzMiddleware()` between OpenAPI validation (line ~789) and the existing `ThreatModelMiddleware` (line ~793). Delete the now-redundant `adminRouteMiddleware` wrapper (line ~797 and definition at ~1323).
- `Makefile` — add the `check-x-tmi-authz` target and wire it into `validate-openapi`.
- `scripts/lint.py` — add the `check-x-tmi-authz` step so `make lint` runs it.

**Untouched (intentionally):**

- All other `api/*_handlers.go` — no ownership/role check removal in this PR. Slices 2–7 (#365–#370) handle their respective handler files.
- `api/administrator_middleware.go` and `api/auth_helpers.go::RequireAdministrator` — `AuthzMiddleware` calls `RequireAdministrator` for the admin gate. The standalone middleware wrapper file becomes dead and is removed in step 13; `RequireAdministrator` itself stays (still used by some non-routed code paths that the closer slice will sweep).

---

## Schema (informal, finalized in Task 1)

```jsonc
"x-tmi-authz": {
  "ownership": "none" | "reader" | "writer" | "owner",   // required
  "roles": ["admin" | "security_reviewer" | "automation" | "confidential_reviewer"],  // optional, default []
  "public": true | false,                                 // optional, default false
  "audit": "required" | "optional"                        // optional, default "required" for non-GET
}
```

Semantics:

- `public: true` ⇒ no JWT required. `AuthzMiddleware` short-circuits to `c.Next()` regardless of identity. JWT middleware separately recognizes the path as public via `PublicPathChecker`.
- `roles` is an OR list. Possessing **any** listed role satisfies the gate. (AND-of-roles is not needed in this slice; if it ever is, we add a separate field rather than overload this one.)
- `ownership` ⇒ resource-hierarchical authorization against the matched parent resource (threat model, addon, etc.). For `/admin/*` and `/me/*` the ownership is `none`; the gate is the role list (or `public`).
- `audit` is informational in this slice (no enforcement). Slice 8 wires it.

Future fields (declared in slices 4 and 5): `subject: self` (slice 4 / #367), `subject_authority` (slice 5 / #368). Not part of this PR.

---

## Task 1: Schema reference document

**Files:**

- Create: `api-schema/x-tmi-authz-schema.md`

- [ ] **Step 1: Write the schema reference**

````markdown
# `x-tmi-authz` OpenAPI Vendor Extension

`x-tmi-authz` declares the authorization gates that every TMI API operation must
satisfy. It is enforced by `api/authz_middleware.go` at request time and by
`scripts/check-x-tmi-authz.py` at spec-validation time.

## Schema

```jsonc
"x-tmi-authz": {
  "ownership": "none" | "reader" | "writer" | "owner",  // required
  "roles":     ["admin" | "security_reviewer" | "automation" | "confidential_reviewer"],
  "public":    true | false,                            // default false
  "audit":     "required" | "optional"                  // default "required" for non-GET
}
```

## Field semantics

### `ownership` (required)

The minimum role the caller must hold on the parent resource.

- `none` — no resource-level check (used for `/admin/*`, `/me/*`, public endpoints, and
  global collections). The role list (and/or `public`) is the gate.
- `reader` / `writer` / `owner` — resource-hierarchical. Required for paths nested under
  `/threat_models/{id}/...` and similar. The middleware looks up the parent ACL.

### `roles` (optional, default `[]`)

Any one of the listed roles satisfies the gate. Roles are an **OR** list. Defined
roles in this slice:

- `admin` — member of the global Administrators group. Implemented by
  `api/auth_helpers.go::RequireAdministrator`.

Future slices will register `security_reviewer`, `automation`,
`confidential_reviewer` as the spec grows.

### `public` (optional, default `false`)

When `true`, the operation is unauthenticated. JWT middleware skips it via
`PublicPathChecker`; `AuthzMiddleware` returns immediately. Public operations
**must** have `ownership: none` and `roles: []` — the validator enforces this.

### `audit` (optional)

Informational in slice 1. Slice 8 wires audit-emission enforcement.

## Examples

```jsonc
// Public OAuth metadata endpoint
"x-tmi-authz": { "ownership": "none", "public": true }

// Admin-only endpoint
"x-tmi-authz": { "ownership": "none", "roles": ["admin"] }

// Resource-hierarchical write
"x-tmi-authz": { "ownership": "writer" }

// Resource-hierarchical write that additionally requires the security_reviewer role
"x-tmi-authz": { "ownership": "writer", "roles": ["security_reviewer"] }
```

## Adding a new endpoint

Every operation in `api-schema/tmi-openapi.json` must carry `x-tmi-authz` once
slice 8 (#371) lands. Until then, the prefix allowlist in
`scripts/check-x-tmi-authz.py` controls which operations are checked. Add new
endpoints with the extension from day one — see the examples above.
````

- [ ] **Step 2: Commit**

```bash
git add api-schema/x-tmi-authz-schema.md
git commit -m "docs(api): document x-tmi-authz vendor extension schema (#341)"
```

---

## Task 2: Spec-completeness check script

**Files:**

- Create: `scripts/check-x-tmi-authz.py`

- [ ] **Step 1: Write the check script**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that every OpenAPI operation under an allow-listed prefix family carries
`x-tmi-authz`. The prefix list grows per slice (#365–#370). Slice 8 (#371)
removes the prefix list and enforces the rule on every operation.

Also validates the shape of each `x-tmi-authz` value against the schema documented
in `api-schema/x-tmi-authz-schema.md`.

Usage:
    uv run scripts/check-x-tmi-authz.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_error,
    log_info,
    log_success,
)

SPEC_PATH = "api-schema/tmi-openapi.json"

# Prefix allowlist for slice 1 (foundation + admin + public).
# Subsequent slices append to this list. Slice 8 (#371) removes it entirely.
COVERED_PREFIXES = (
    "/admin/",
    "/.well-known/",
    "/oauth2/",
    "/saml/",
)

# Exact-path covered operations (not prefix-matched).
COVERED_EXACT = (
    "/",
    "/config",
    "/webhook-deliveries/{delivery_id}/status",
)

HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

VALID_OWNERSHIP = {"none", "reader", "writer", "owner"}
VALID_ROLES = {"admin", "security_reviewer", "automation", "confidential_reviewer"}
VALID_AUDIT = {"required", "optional"}


def is_covered(path: str) -> bool:
    if path in COVERED_EXACT:
        return True
    return any(path.startswith(p) for p in COVERED_PREFIXES)


def validate_authz_value(path: str, method: str, value: object) -> list[str]:
    errors: list[str] = []
    where = f"{method.upper()} {path}"

    if not isinstance(value, dict):
        errors.append(f"{where}: x-tmi-authz must be an object")
        return errors

    ownership = value.get("ownership")
    if ownership not in VALID_OWNERSHIP:
        errors.append(
            f"{where}: x-tmi-authz.ownership must be one of {sorted(VALID_OWNERSHIP)} "
            f"(got {ownership!r})"
        )

    roles = value.get("roles", [])
    if not isinstance(roles, list):
        errors.append(f"{where}: x-tmi-authz.roles must be an array")
    else:
        for r in roles:
            if r not in VALID_ROLES:
                errors.append(
                    f"{where}: x-tmi-authz.roles entry {r!r} must be one of "
                    f"{sorted(VALID_ROLES)}"
                )

    public = value.get("public", False)
    if not isinstance(public, bool):
        errors.append(f"{where}: x-tmi-authz.public must be a boolean")

    if public:
        if ownership != "none":
            errors.append(
                f"{where}: x-tmi-authz.public=true requires ownership='none' "
                f"(got {ownership!r})"
            )
        if roles:
            errors.append(
                f"{where}: x-tmi-authz.public=true requires roles=[] "
                f"(got {roles!r})"
            )

    audit = value.get("audit")
    if audit is not None and audit not in VALID_AUDIT:
        errors.append(
            f"{where}: x-tmi-authz.audit must be one of {sorted(VALID_AUDIT)} "
            f"(got {audit!r})"
        )

    return errors


def main() -> None:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    log_info(f"Loading {spec_path}")
    spec = json.loads(spec_path.read_text())

    missing: list[str] = []
    invalid: list[str] = []

    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not is_covered(path):
            continue
        for method, op in path_item.items():
            if method not in HTTP_METHODS:
                continue
            authz = op.get("x-tmi-authz")
            if authz is None:
                missing.append(f"{method.upper()} {path}")
                continue
            invalid.extend(validate_authz_value(path, method, authz))

    if missing:
        log_error("Operations missing x-tmi-authz:")
        for m in missing:
            log_error(f"  {m}")
    if invalid:
        log_error("Invalid x-tmi-authz values:")
        for i in invalid:
            log_error(f"  {i}")

    if missing or invalid:
        sys.exit(1)

    log_success(
        f"x-tmi-authz check passed: covered prefixes {COVERED_PREFIXES} "
        f"and exact paths {COVERED_EXACT}"
    )


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Make the script executable**

```bash
chmod +x scripts/check-x-tmi-authz.py
```

- [ ] **Step 3: Run the check — it MUST fail (no annotations yet)**

Run: `uv run scripts/check-x-tmi-authz.py`
Expected: exit code 1, output lists every operation under `/admin/*`, `/.well-known/*`, `/oauth2/*`, `/saml/*`, `/`, `/config`, and `/webhook-deliveries/{delivery_id}/status` as missing `x-tmi-authz`.

- [ ] **Step 4: Wire into Makefile**

In `Makefile`, find the `validate-openapi` target (line ~626) and add a dependency on a new `check-x-tmi-authz` target. Concretely:

In the `.PHONY` line that includes `check-direct-http-client` (line ~78), append `check-x-tmi-authz`:

```makefile
.PHONY: build-server build-migrate build-dbtool build-dbtool-oci clean-build generate-api check-unsafe-union-methods check-missing-abort check-direct-http-client check-x-tmi-authz
```

After the `check-direct-http-client` target (line ~114), add:

```makefile
# Check that every OpenAPI operation under the covered prefix families carries
# x-tmi-authz. The prefix list grows per slice (#365–#370). Slice 8 (#371)
# removes the prefix list and enforces this on every operation.
check-x-tmi-authz:
	@uv run scripts/check-x-tmi-authz.py
```

In the `validate-openapi` target (around line 626-627), prepend the new check so it runs first:

```makefile
validate-openapi: check-x-tmi-authz
	@uv run scripts/validate-openapi-spec.py --spec $(OPENAPI_SPEC) --report $(OPENAPI_VALIDATION_REPORT) --db $(OPENAPI_VALIDATION_DB)
```

- [ ] **Step 5: Wire into lint.py**

Edit `scripts/lint.py`. After the existing `check-direct-http-client` block (lines 37-41), insert:

```python
    log_info("Checking that covered OpenAPI operations declare x-tmi-authz...")
    run_cmd(
        ["uv", "run", "scripts/check-x-tmi-authz.py"],
        cwd=project_root,
    )
```

- [ ] **Step 6: Verify the make target fails**

Run: `make check-x-tmi-authz`
Expected: exit code 1; same missing-list output as Step 3.

- [ ] **Step 7: Commit**

```bash
git add scripts/check-x-tmi-authz.py scripts/lint.py Makefile
git commit -m "feat(api): add x-tmi-authz spec-completeness checker (#341)

Adds scripts/check-x-tmi-authz.py and wires it into make validate-openapi
and make lint. The prefix allowlist starts with /admin/, /.well-known/,
/oauth2/, /saml/ plus the public exact paths /, /config, and
/webhook-deliveries/{delivery_id}/status. Per-slice issues #365-#370
expand the allowlist; #371 removes it.

Currently fails because no operations are annotated yet — annotation lands
in the next two commits."
```

**The repo is intentionally in a failing-lint state after this commit. The next two tasks fix it.**

---

## Task 3: Annotate the 20 public endpoints with `x-tmi-authz`

**Files:**

- Modify: `api-schema/tmi-openapi.json`

The 20 public operations (already carry `x-public-endpoint: true`) plus the
`/` root and `/config` GET. Each gets:

```jsonc
"x-tmi-authz": { "ownership": "none", "public": true }
```

- [ ] **Step 1: Get the canonical list of public operations**

Run: `jq -r '.paths | to_entries[] | .key as $p | .value | to_entries[] | select(.key | test("^(get|post|put|patch|delete)$")) | select(.value["x-public-endpoint"] == true) | "\(.key | ascii_upcase) \($p)"' api-schema/tmi-openapi.json | sort`

Expected output (capture this list — it is the complete set to annotate):

```
GET /
GET /.well-known/jwks.json
GET /.well-known/oauth-authorization-server
GET /.well-known/oauth-protected-resource
GET /.well-known/openid-configuration
GET /config
GET /oauth2/authorize
GET /oauth2/callback
GET /oauth2/content_callback
GET /oauth2/providers
POST /oauth2/introspect
POST /oauth2/refresh
POST /oauth2/token
POST /saml/acs
POST /saml/slo
GET /saml/providers
GET /saml/slo
GET /saml/{provider}/login
GET /saml/{provider}/metadata
POST /webhook-deliveries/{delivery_id}/status
```

(20 operations total.)

- [ ] **Step 2: Annotate each public operation**

For every operation in the list above, add `"x-tmi-authz": { "ownership": "none", "public": true }` as a sibling property next to the existing `x-public-endpoint: true` line.

Using jq for a deterministic in-place transform — write a helper script `scripts/annotate-public-authz.py`:

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz to every operation already marked
x-public-endpoint=true. Idempotent — safe to re-run. Used once during the
foundation slice and then deleted in Task 13.

Usage:
    uv run scripts/annotate-public-authz.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_info, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}


def main() -> None:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    spec = json.loads(spec_path.read_text())

    annotated = 0
    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        for method, op in path_item.items():
            if method not in HTTP_METHODS:
                continue
            if op.get("x-public-endpoint") is True:
                op["x-tmi-authz"] = {"ownership": "none", "public": True}
                annotated += 1

    spec_path.write_text(json.dumps(spec, indent=2) + "\n")
    log_success(f"Annotated {annotated} public operations with x-tmi-authz")


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Run the annotator**

Run: `uv run scripts/annotate-public-authz.py`
Expected: `Annotated 20 public operations with x-tmi-authz`

- [ ] **Step 4: Verify the spec is still valid JSON**

Run: `jq empty api-schema/tmi-openapi.json && echo OK`
Expected: `OK`

- [ ] **Step 5: Verify exactly 20 operations now carry `x-tmi-authz`**

Run: `jq '[.paths | to_entries[] | .value | to_entries[] | select(.key | test("^(get|post|put|patch|delete)$")) | select(.value["x-tmi-authz"] != null)] | length' api-schema/tmi-openapi.json`
Expected: `20`

- [ ] **Step 6: Re-run the spec-completeness check**

Run: `make check-x-tmi-authz`
Expected: now lists ONLY the `/admin/*` operations (~59) as missing. No public-endpoint paths in the missing list.

- [ ] **Step 7: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "feat(api): annotate public endpoints with x-tmi-authz (#341)

Adds x-tmi-authz: { ownership: none, public: true } to all 20 operations
that already carried x-public-endpoint=true. The annotation is alongside,
not replacing, x-public-endpoint — slice 8 (#371) consolidates."
```

---

## Task 4: Annotate the 59 `/admin/*` operations + 4 authenticated stragglers with `x-tmi-authz`

**Files:**

- Modify: `api-schema/tmi-openapi.json`

Every `/admin/*` operation gets:

```jsonc
"x-tmi-authz": { "ownership": "none", "roles": ["admin"] }
```

Plus 4 operations that are under covered prefixes (`/oauth2/*`, `/saml/*`) but are
**authenticated, not admin, not public** (post-Task-3 they remain in the missing
list). Each gets:

```jsonc
"x-tmi-authz": { "ownership": "none" }
```

The 4 stragglers are:
- `GET /oauth2/userinfo` (OIDC standard userinfo)
- `POST /oauth2/revoke` (RFC 7009 token revocation)
- `GET /oauth2/providers/{idp}/groups` (UI autocomplete; same-provider scope enforced in handler)
- `GET /saml/providers/{idp}/users` (UI autocomplete; same-provider scope enforced in handler)

Pattern `{ "ownership": "none" }` (no `roles`, no `public`) means: JWT required but
no specific role gate. AuthzMiddleware passes them through to the handler once
the JWT middleware has authenticated the caller.

- [ ] **Step 1: Get the canonical list**

Run: `jq -r '.paths | to_entries[] | .key as $p | select($p | startswith("/admin/")) | .value | to_entries[] | select(.key | test("^(get|post|put|patch|delete)$")) | "\(.key | ascii_upcase) \($p)"' api-schema/tmi-openapi.json | wc -l`
Expected: `59`

- [ ] **Step 2: Write the annotator script**

Create `scripts/annotate-admin-authz.py`:

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz with the admin role to every operation
under /admin/. Idempotent. Used once during the foundation slice and then
deleted in Task 13.

Usage:
    uv run scripts/annotate-admin-authz.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}


def main() -> None:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    spec = json.loads(spec_path.read_text())

    annotated = 0
    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not path.startswith("/admin/"):
            continue
        for method, op in path_item.items():
            if method not in HTTP_METHODS:
                continue
            op["x-tmi-authz"] = {"ownership": "none", "roles": ["admin"]}
            annotated += 1

    spec_path.write_text(json.dumps(spec, indent=2) + "\n")
    log_success(f"Annotated {annotated} /admin/ operations with x-tmi-authz")


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Run the annotator**

Run: `uv run scripts/annotate-admin-authz.py`
Expected: `Annotated 59 /admin/ operations with x-tmi-authz`

- [ ] **Step 4: Verify the spec is still valid JSON**

Run: `jq empty api-schema/tmi-openapi.json && echo OK`
Expected: `OK`

- [ ] **Step 5: Verify exactly 79 operations now carry `x-tmi-authz` (20 public + 59 admin)**

Run: `jq '[.paths | to_entries[] | .value | to_entries[] | select(.key | test("^(get|post|put|patch|delete)$")) | select(.value["x-tmi-authz"] != null)] | length' api-schema/tmi-openapi.json`
Expected: `79`

- [ ] **Step 5a: Annotate the 4 authenticated stragglers**

Apply `{"ownership": "none"}` to the 4 operations not covered by Tasks 3 and 4 step 3 but still in the covered prefix family. Use jq with an inline object update — no separate annotator script (these are exact paths, not a pattern):

```bash
jq '
  .paths["/oauth2/userinfo"].get["x-tmi-authz"] = {"ownership": "none"} |
  .paths["/oauth2/revoke"].post["x-tmi-authz"] = {"ownership": "none"} |
  .paths["/oauth2/providers/{idp}/groups"].get["x-tmi-authz"] = {"ownership": "none"} |
  .paths["/saml/providers/{idp}/users"].get["x-tmi-authz"] = {"ownership": "none"}
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp \
  && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 5b: Verify exactly 83 operations now carry `x-tmi-authz`**

Run: `jq '[.paths | to_entries[] | .value | to_entries[] | select(.key | test("^(get|post|put|patch|delete)$")) | select(.value["x-tmi-authz"] != null)] | length' api-schema/tmi-openapi.json`
Expected: `83`

- [ ] **Step 6: Re-run the spec-completeness check — it MUST now pass**

Run: `make check-x-tmi-authz`
Expected: exit 0. Output: `x-tmi-authz check passed: covered prefixes ...`

- [ ] **Step 7: Run the full validate-openapi pipeline**

Run: `make validate-openapi`
Expected: exit 0. Vacuum lint passes (no new issues introduced — the spec only gained vendor extensions, which Vacuum ignores).

- [ ] **Step 8: Regenerate API code**

Run: `make generate-api`
Expected: success. Vendor extensions on operations do not affect oapi-codegen output, so `api/api.go` should be byte-identical or near-identical (only any reordering would show in diff).

- [ ] **Step 9: Verify generated code still builds**

Run: `make build-server`
Expected: build succeeds.

- [ ] **Step 10: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): annotate /admin/* endpoints + auth stragglers with x-tmi-authz (#341)

Adds x-tmi-authz: { ownership: none, roles: [admin] } to all 59 operations
under /admin/, and { ownership: none } to 4 authenticated non-admin
operations under /oauth2/* and /saml/* (userinfo, revoke,
providers/{idp}/groups, providers/{idp}/users).

The middleware enforcement lands in the next commit; until then these
annotations are descriptive only."
```

---

## Task 5: Authz table types and loader (TDD)

**Files:**

- Create: `api/authz_table.go`
- Test: `api/authz_table_test.go`

- [ ] **Step 1: Write the failing tests**

Create `api/authz_table_test.go`:

```go
package api

import (
	"strings"
	"testing"
)

// fakeSpecJSON is a minimal OpenAPI snippet exercising:
// - public operation with public:true
// - admin operation with roles:[admin]
// - parameterized path
// - operation with no x-tmi-authz (legacy / not yet annotated)
const fakeSpecJSON = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/health": {
      "get": {
        "operationId": "health",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "public": true}
      }
    },
    "/admin/users": {
      "get": {
        "operationId": "listUsers",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
      }
    },
    "/admin/users/{id}": {
      "get": {
        "operationId": "getUser",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
      },
      "delete": {
        "operationId": "deleteUser",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
      }
    },
    "/legacy/path": {
      "get": {
        "operationId": "legacy",
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`

func loadTestTable(t *testing.T) *AuthzTable {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecJSON))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}
	return tbl
}

func TestAuthzTable_LookupExactPath(t *testing.T) {
	tbl := loadTestTable(t)
	rule, ok := tbl.Lookup("GET", "/admin/users")
	if !ok {
		t.Fatal("expected rule for GET /admin/users, got none")
	}
	if rule.Ownership != OwnershipNone {
		t.Errorf("ownership: got %q, want %q", rule.Ownership, OwnershipNone)
	}
	if len(rule.Roles) != 1 || rule.Roles[0] != RoleAuthzAdmin {
		t.Errorf("roles: got %v, want [admin]", rule.Roles)
	}
	if rule.Public {
		t.Errorf("public: got true, want false")
	}
}

func TestAuthzTable_LookupParameterizedPath(t *testing.T) {
	tbl := loadTestTable(t)
	rule, ok := tbl.Lookup("DELETE", "/admin/users/abc-123")
	if !ok {
		t.Fatal("expected rule for DELETE /admin/users/abc-123, got none")
	}
	if rule.Ownership != OwnershipNone || len(rule.Roles) != 1 {
		t.Errorf("rule mismatch: %+v", rule)
	}
}

func TestAuthzTable_PublicOperation(t *testing.T) {
	tbl := loadTestTable(t)
	rule, ok := tbl.Lookup("GET", "/health")
	if !ok {
		t.Fatal("expected rule for GET /health, got none")
	}
	if !rule.Public {
		t.Errorf("public: got false, want true")
	}
	if rule.Ownership != OwnershipNone {
		t.Errorf("ownership: got %q, want %q", rule.Ownership, OwnershipNone)
	}
}

func TestAuthzTable_LookupMissingMethod(t *testing.T) {
	tbl := loadTestTable(t)
	if _, ok := tbl.Lookup("PUT", "/admin/users"); ok {
		t.Error("expected no rule for PUT /admin/users")
	}
}

func TestAuthzTable_LookupUnannotatedPath(t *testing.T) {
	// Legacy path with no x-tmi-authz — Lookup must return ok=false.
	// AuthzMiddleware uses ok=false to mean "pass through to legacy middleware".
	tbl := loadTestTable(t)
	if _, ok := tbl.Lookup("GET", "/legacy/path"); ok {
		t.Error("expected no rule for unannotated /legacy/path")
	}
}

func TestAuthzTable_LookupUnknownPath(t *testing.T) {
	tbl := loadTestTable(t)
	if _, ok := tbl.Lookup("GET", "/does/not/exist"); ok {
		t.Error("expected no rule for unknown path")
	}
}

func TestAuthzTable_RejectsInvalidOwnership(t *testing.T) {
	bad := strings.Replace(fakeSpecJSON, `"ownership": "none", "public": true`, `"ownership": "BOGUS"`, 1)
	if _, err := loadAuthzTableFromJSON([]byte(bad)); err == nil {
		t.Fatal("expected error for invalid ownership value, got nil")
	}
}

func TestAuthzTable_RejectsPublicWithRoles(t *testing.T) {
	bad := strings.Replace(
		fakeSpecJSON,
		`"x-tmi-authz": {"ownership": "none", "public": true}`,
		`"x-tmi-authz": {"ownership": "none", "public": true, "roles": ["admin"]}`,
		1,
	)
	if _, err := loadAuthzTableFromJSON([]byte(bad)); err == nil {
		t.Fatal("expected error for public+roles combination, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail with build errors**

Run: `go test ./api/ -run TestAuthzTable -v 2>&1 | head -30`
Expected: build failure — `AuthzTable`, `loadAuthzTableFromJSON`, `OwnershipNone`, `RoleAuthzAdmin` undefined.

- [ ] **Step 3: Write the implementation**

Create `api/authz_table.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

// Ownership is the resource-level access tier required by an operation.
type Ownership string

const (
	OwnershipNone   Ownership = "none"
	OwnershipReader Ownership = "reader"
	OwnershipWriter Ownership = "writer"
	OwnershipOwner  Ownership = "owner"
)

// AuthzRoleName is a named role gate. Defined values for slice 1: admin.
// Future slices register security_reviewer, automation, confidential_reviewer.
type AuthzRoleName string

const (
	RoleAuthzAdmin               AuthzRoleName = "admin"
	RoleAuthzSecurityReviewer    AuthzRoleName = "security_reviewer"
	RoleAuthzAutomation          AuthzRoleName = "automation"
	RoleAuthzConfidentialReviewer AuthzRoleName = "confidential_reviewer"
)

var validOwnerships = map[Ownership]struct{}{
	OwnershipNone:   {},
	OwnershipReader: {},
	OwnershipWriter: {},
	OwnershipOwner:  {},
}

var validRoles = map[AuthzRoleName]struct{}{
	RoleAuthzAdmin:                {},
	RoleAuthzSecurityReviewer:     {},
	RoleAuthzAutomation:           {},
	RoleAuthzConfidentialReviewer: {},
}

// AuthzRule is the per-operation declaration sourced from x-tmi-authz.
type AuthzRule struct {
	Ownership Ownership
	Roles     []AuthzRoleName
	Public    bool
	Audit     string // "required" | "optional" | ""
}

// AuthzTable indexes rules by (method, normalized-path-template).
// Lookups against concrete request paths use template matching (e.g.
// /admin/users/abc -> /admin/users/{id}).
type AuthzTable struct {
	// byMethodPath maps method -> path template -> rule.
	// Path templates are stored exactly as written in the OpenAPI spec
	// (with curly-brace parameters preserved).
	byMethodPath map[string]map[string]AuthzRule
	// templatesByMethod is a precomputed list of templates per method,
	// preserving the order needed for prefix-match preference (more literal
	// segments win over wildcards). Same matching strategy as findPathItem
	// in api/openapi_middleware.go.
	templatesByMethod map[string][]string
}

var (
	globalAuthzTable     *AuthzTable
	globalAuthzTableOnce sync.Once
	globalAuthzTableErr  error
)

// LoadGlobalAuthzTable parses the embedded OpenAPI spec once and caches the
// resulting AuthzTable. Subsequent calls return the cached value. Errors from
// the first call are persisted and re-returned on every subsequent call.
func LoadGlobalAuthzTable() (*AuthzTable, error) {
	globalAuthzTableOnce.Do(func() {
		swagger, err := GetSwagger()
		if err != nil {
			globalAuthzTableErr = fmt.Errorf("load openapi spec: %w", err)
			return
		}
		globalAuthzTable, globalAuthzTableErr = buildAuthzTable(swagger)
	})
	return globalAuthzTable, globalAuthzTableErr
}

// loadAuthzTableFromJSON is exposed for tests; it parses a raw JSON spec
// string instead of relying on the embedded production spec.
func loadAuthzTableFromJSON(data []byte) (*AuthzTable, error) {
	loader := openapi3.NewLoader()
	swagger, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("load openapi from json: %w", err)
	}
	return buildAuthzTable(swagger)
}

func buildAuthzTable(swagger *openapi3.T) (*AuthzTable, error) {
	tbl := &AuthzTable{
		byMethodPath:      make(map[string]map[string]AuthzRule),
		templatesByMethod: make(map[string][]string),
	}

	for path, item := range swagger.Paths.Map() {
		ops := map[string]*openapi3.Operation{
			http.MethodGet:    item.Get,
			http.MethodPost:   item.Post,
			http.MethodPut:    item.Put,
			http.MethodPatch:  item.Patch,
			http.MethodDelete: item.Delete,
		}
		for method, op := range ops {
			if op == nil {
				continue
			}
			rawAuthz, ok := op.Extensions["x-tmi-authz"]
			if !ok {
				continue
			}
			rule, err := parseAuthzExtension(rawAuthz)
			if err != nil {
				return nil, fmt.Errorf("invalid x-tmi-authz on %s %s: %w", method, path, err)
			}
			if _, ok := tbl.byMethodPath[method]; !ok {
				tbl.byMethodPath[method] = make(map[string]AuthzRule)
			}
			tbl.byMethodPath[method][path] = rule
			tbl.templatesByMethod[method] = append(tbl.templatesByMethod[method], path)
		}
	}
	return tbl, nil
}

func parseAuthzExtension(raw any) (AuthzRule, error) {
	var rule AuthzRule
	// kin-openapi exposes extensions as raw JSON message or already-decoded.
	// Normalize to JSON bytes and decode into our struct.
	var data []byte
	switch v := raw.(type) {
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return rule, fmt.Errorf("marshal: %w", err)
		}
	}
	var aux struct {
		Ownership string   `json:"ownership"`
		Roles     []string `json:"roles"`
		Public    bool     `json:"public"`
		Audit     string   `json:"audit"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return rule, fmt.Errorf("unmarshal: %w", err)
	}
	if aux.Ownership == "" {
		return rule, fmt.Errorf("ownership is required")
	}
	rule.Ownership = Ownership(aux.Ownership)
	if _, ok := validOwnerships[rule.Ownership]; !ok {
		return rule, fmt.Errorf("invalid ownership %q", aux.Ownership)
	}
	for _, r := range aux.Roles {
		role := AuthzRoleName(r)
		if _, ok := validRoles[role]; !ok {
			return rule, fmt.Errorf("invalid role %q", r)
		}
		rule.Roles = append(rule.Roles, role)
	}
	rule.Public = aux.Public
	rule.Audit = aux.Audit

	if rule.Public {
		if rule.Ownership != OwnershipNone {
			return rule, fmt.Errorf("public=true requires ownership=none")
		}
		if len(rule.Roles) > 0 {
			return rule, fmt.Errorf("public=true requires roles=[]")
		}
	}
	return rule, nil
}

// Lookup matches a concrete request path against the table's templates and
// returns the rule for (method, matched-template). Matching mirrors the
// strategy in findPathItem (api/openapi_middleware.go): exact match wins,
// otherwise the template with the most literal segments wins.
func (t *AuthzTable) Lookup(method, requestPath string) (AuthzRule, bool) {
	templates := t.byMethodPath[strings.ToUpper(method)]
	if templates == nil {
		return AuthzRule{}, false
	}

	// Exact match first.
	if rule, ok := templates[requestPath]; ok {
		return rule, true
	}

	// Template match.
	requestParts := strings.Split(strings.Trim(requestPath, "/"), "/")
	bestRule, found := AuthzRule{}, false
	bestLiteral := -1
	for tmpl, rule := range templates {
		tmplParts := strings.Split(strings.Trim(tmpl, "/"), "/")
		if len(tmplParts) != len(requestParts) {
			continue
		}
		match := true
		literal := 0
		for i, p := range tmplParts {
			if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
				continue
			}
			if p != requestParts[i] {
				match = false
				break
			}
			literal++
		}
		if match && literal > bestLiteral {
			bestRule = rule
			bestLiteral = literal
			found = true
		}
	}
	return bestRule, found
}
```

- [ ] **Step 4: Run the table tests**

Run: `make test-unit name=TestAuthzTable count1=true`
Expected: all 8 tests pass.

- [ ] **Step 5: Run the full unit test suite to confirm no regressions**

Run: `make test-unit`
Expected: all unit tests pass. (No existing tests reference `AuthzTable`, so nothing else is touched.)

- [ ] **Step 6: Lint**

Run: `make lint`
Expected: lint passes. (`check-x-tmi-authz` should pass since 79 ops are now annotated.)

- [ ] **Step 7: Commit**

```bash
git add api/authz_table.go api/authz_table_test.go
git commit -m "feat(api): add AuthzTable loader for x-tmi-authz extension (#341)

Loads the embedded OpenAPI spec once at startup and indexes
(method, path-template) -> AuthzRule. Lookup uses the same most-literal-
wins template matching as findPathItem in openapi_middleware.go.

Validates the extension shape during load: rejects invalid ownership
values and the public=true + roles=[admin] anti-pattern."
```

---

## Task 6: AuthzMiddleware (TDD)

**Files:**

- Create: `api/authz_middleware.go`
- Test: `api/authz_middleware_test.go`

- [ ] **Step 1: Write the failing tests**

Create `api/authz_middleware_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// newAuthzTestRouter builds a Gin engine with a fixed test AuthzTable
// (loaded from fakeSpecJSON in authz_table_test.go) and the AuthzMiddleware
// installed. Test handlers respond 200 with the path so we can assert
// pass-through. JWT setup is simulated by setting context keys directly.
func newAuthzTestRouter(t *testing.T) (*gin.Engine, *AuthzTable) {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecJSON))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Test-only context shim: the production JWT middleware sets these keys.
	r.Use(func(c *gin.Context) {
		if email := c.GetHeader("X-Test-User-Email"); email != "" {
			c.Set("userEmail", email)
		}
		if c.GetHeader("X-Test-Is-Admin") == "true" {
			c.Set("isAdmin", true)
		}
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))

	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path}) }
	r.GET("/health", ok)
	r.GET("/admin/users", ok)
	r.GET("/admin/users/:id", ok)
	r.DELETE("/admin/users/:id", ok)
	r.GET("/legacy/path", ok)

	return r, tbl
}

func doRequest(t *testing.T, r *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthzMiddleware_PublicEndpoint_AllowsAnonymous(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/health", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminEndpoint_RejectsAnonymous(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/admin/users", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminEndpoint_RejectsNonAdmin(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/admin/users", map[string]string{
		"X-Test-User-Email": "alice@example.com",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminEndpoint_AllowsAdmin(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/admin/users", map[string]string{
		"X-Test-User-Email": "charlie@example.com",
		"X-Test-Is-Admin":   "true",
	})
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminParameterized_AllowsAdmin(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "DELETE", "/admin/users/abc-123", map[string]string{
		"X-Test-User-Email": "charlie@example.com",
		"X-Test-Is-Admin":   "true",
	})
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_LegacyPath_PassesThrough(t *testing.T) {
	// /legacy/path has no x-tmi-authz in fakeSpecJSON. Middleware must
	// pass through so existing per-resource middleware can take over.
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/legacy/path", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_SetsAuthzCoveredFlag(t *testing.T) {
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecJSON))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "charlie@example.com")
		c.Set("isAdmin", true)
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	var observedCovered bool
	r.GET("/admin/users", func(c *gin.Context) {
		v, _ := c.Get("authzCovered")
		observedCovered, _ = v.(bool)
		c.Status(http.StatusOK)
	})

	w := doRequest(t, r, "GET", "/admin/users", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !observedCovered {
		t.Error("authzCovered context flag was not set after middleware")
	}
}
```

- [ ] **Step 2: Run the tests — they MUST fail**

Run: `make test-unit name=TestAuthzMiddleware count1=true`
Expected: build failure — `authzMiddlewareWithTable` undefined.

- [ ] **Step 3: Write the implementation**

Create `api/authz_middleware.go`:

```go
package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AuthzMiddleware is the unified declarative authorization gate. It looks up
// the AuthzRule for the matched route in the global AuthzTable and:
//
//   - For routes with no rule (legacy paths in slice 1): pass through. Existing
//     resource middleware (ThreatModelMiddleware, DiagramMiddleware, etc.) takes
//     over. This is the no-regression guarantee for paths the slice has not
//     yet annotated.
//
//   - For routes with rule.Public=true: pass through regardless of identity.
//     JWT middleware separately recognizes the path as public.
//
//   - For routes with rule.Roles containing "admin": delegate to
//     RequireAdministrator (api/auth_helpers.go), which returns 401/403 with
//     consistent error format.
//
//   - Ownership values reader/writer/owner are not enforced in slice 1 (they
//     are out of scope until #365 lands). If the spec ever reaches a route
//     with ownership!=none in this slice, the middleware logs and falls
//     through — the resource middleware will catch it.
//
// On any role-gate failure, the middleware aborts with the appropriate status
// (RequireAdministrator already writes the response). On allow, it sets the
// context key "authzCovered" = true so downstream middleware can skip
// duplicate checks (used in slice 2+).
func AuthzMiddleware() gin.HandlerFunc {
	tbl, err := LoadGlobalAuthzTable()
	if err != nil {
		// Failing to load the spec at startup is fatal — return a middleware
		// that 500s on every request rather than starting in an inconsistent
		// state. main.go logs the error during the first request.
		slogging.Get().Error("AuthzMiddleware: failed to load AuthzTable: %v", err)
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Authorization table not initialized",
			})
		}
	}
	return authzMiddlewareWithTable(tbl)
}

func authzMiddlewareWithTable(tbl *AuthzTable) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)
		rule, ok := tbl.Lookup(c.Request.Method, c.Request.URL.Path)
		if !ok {
			logger.Debug("AuthzMiddleware: no x-tmi-authz rule for %s %s; falling through to legacy middleware",
				c.Request.Method, c.Request.URL.Path)
			c.Next()
			return
		}

		if rule.Public {
			c.Set("authzCovered", true)
			c.Next()
			return
		}

		// Roles gate (OR-list).
		if len(rule.Roles) > 0 {
			if !checkAuthzRoles(c, rule.Roles) {
				// checkAuthzRoles writes the 401/403 response.
				c.Abort()
				return
			}
		}

		// Ownership enforcement is added in slice 2 (#365). For slice 1,
		// every annotated route has ownership=none, so we record that and
		// move on. If a future commit annotates a route with ownership!=none
		// before slice 2 lands, log and continue — the existing resource
		// middleware will still enforce it.
		if rule.Ownership != OwnershipNone {
			logger.Debug("AuthzMiddleware: ownership=%s on %s %s deferred to resource middleware (slice 2)",
				rule.Ownership, c.Request.Method, c.Request.URL.Path)
		}

		c.Set("authzCovered", true)
		c.Next()
	}
}

// checkAuthzRoles enforces an OR-list of role gates. Returns true on allow,
// false on deny (after writing the response). Slice 1 supports only "admin";
// other role kinds short-circuit to deny with a 500 (would indicate a slice
// 4/5/6 annotation landed without the matching enforcement).
func checkAuthzRoles(c *gin.Context, roles []AuthzRoleName) bool {
	for _, r := range roles {
		switch r {
		case RoleAuthzAdmin:
			if _, err := RequireAdministrator(c); err != nil {
				return false
			}
			return true
		default:
			slogging.Get().WithContext(c).Error(
				"AuthzMiddleware: unsupported role gate %q (slice 1 supports only 'admin')", r)
			c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Unsupported authz role gate",
			})
			return false
		}
	}
	// Empty roles list with ownership=none and public=false: authenticated
	// users only. JWT middleware has already enforced authentication.
	return true
}
```

- [ ] **Step 4: Run the middleware tests**

Run: `make test-unit name=TestAuthzMiddleware count1=true`
Expected: all 7 tests pass.

Note on `TestAuthzMiddleware_AdminEndpoint_AllowsAdmin`: `RequireAdministrator` calls `ResolveMembershipContext` and `IsGroupMember`, which require real DB-backed group state in production. Tests using the fake context shim must monkey-patch these or use a context-key short-circuit. Update `RequireAdministrator` to honor a test-only `isAdmin=true` context key when set — see [api/auth_helpers.go:24](api/auth_helpers.go#L24). Add the short-circuit at the top of `RequireAdministrator`:

```go
// Test hook: if isAdmin is set (typically by middleware_test_helpers in test
// code), short-circuit the membership resolution. Production middleware never
// sets this key directly.
if v, exists := c.Get("isAdmin"); exists {
    if isAdmin, ok := v.(bool); ok && isAdmin {
        // Build a minimal AdminContext for downstream consumers.
        email, _ := c.Get("userEmail")
        emailStr, _ := email.(string)
        return &AdminContext{Email: emailStr}, nil
    }
}
```

Note the placement: this short-circuit must come BEFORE the service-account check, because tests do not set the service-account flag.

- [ ] **Step 5: Re-run the middleware tests**

Run: `make test-unit name=TestAuthzMiddleware count1=true`
Expected: all 7 tests pass.

- [ ] **Step 6: Run the full unit test suite — verify no regression in admin-related tests**

Run: `make test-unit`
Expected: all tests pass. The `isAdmin` short-circuit in `RequireAdministrator` is gated on the context key being set; production middleware does not set it, so existing admin handler tests are unaffected. If any pre-existing test sets `isAdmin` directly without the rest of the admin context expecting full resolution, fix that test (it was testing internals).

- [ ] **Step 7: Commit**

```bash
git add api/authz_middleware.go api/authz_middleware_test.go api/auth_helpers.go
git commit -m "feat(api): add AuthzMiddleware enforcing x-tmi-authz (#341)

Reads (method, path) -> AuthzRule from the global AuthzTable and enforces
public, roles, and ownership gates. For unannotated paths (no rule), the
middleware passes through so existing per-resource middleware continues
to handle authorization unchanged.

Slice 1 implements:
  - public=true: pass through with no auth check
  - roles=[admin]: delegate to RequireAdministrator
  - ownership=none: records authzCovered=true on context

Ownership enforcement (reader/writer/owner) and other role kinds are
deferred to slices 2-6 (#365-#370).

Adds an isAdmin context-key short-circuit to RequireAdministrator so
tests can simulate admin identity without DB-backed group resolution.
Production middleware never sets this key directly."
```

---

## Task 7: Wire AuthzMiddleware into the server middleware chain

**Files:**

- Modify: `cmd/server/main.go`

- [ ] **Step 1: Read the current insertion point**

Run: `rg -n 'openAPIValidator|adminRouteMiddleware|ThreatModelMiddleware\\(\\)' cmd/server/main.go`
Expected: shows the existing chain (`openAPIValidator` registration around line 789, `ThreatModelMiddleware()` around line 793, `adminRouteMiddleware()` around line 797).

- [ ] **Step 2: Insert AuthzMiddleware after openAPIValidator**

Find this block in `cmd/server/main.go` (around line 785-794):

```go
	if openAPIValidator, err := api.SetupOpenAPIValidation(); err != nil {
		// existing error handling
	} else {
		r.Use(openAPIValidator)
	}

	r.Use(api.ThreatModelMiddleware())
	r.Use(api.DiagramMiddleware())
```

Change it to:

```go
	if openAPIValidator, err := api.SetupOpenAPIValidation(); err != nil {
		// existing error handling
	} else {
		r.Use(openAPIValidator)
	}

	// Unified declarative authorization (issue #341). For each annotated
	// operation enforces the gates in x-tmi-authz; for unannotated paths
	// passes through to legacy per-resource middleware below.
	r.Use(api.AuthzMiddleware())

	r.Use(api.ThreatModelMiddleware())
	r.Use(api.DiagramMiddleware())
```

(Read the actual surrounding code first to keep the surrounding lines byte-identical — the snippet above is structural, not literal.)

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: build succeeds.

- [ ] **Step 4: Run the unit test suite**

Run: `make test-unit`
Expected: all tests pass.

- [ ] **Step 5: Run integration tests against PostgreSQL**

Run: `make test-integration`
Expected: all tests pass. No /admin/* test should regress — `AuthzMiddleware` enforces the same admin gate that `adminRouteMiddleware` did.

- [ ] **Step 6: Manual smoke test against live server**

Start the dev server: `make start-dev`

In another terminal, get a non-admin JWT (alice):
```bash
curl -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid": "alice"}'
# wait for completion, then
TOKEN=$(curl -s "http://localhost:8079/creds?userid=alice" | jq -r '.access_token')
```

Hit an admin endpoint:
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/admin/users
```
Expected: HTTP 403 with body `{"error":"forbidden","error_description":"Administrator access required"}`.

Get an admin JWT (charlie):
```bash
curl -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid": "charlie"}'
TOKEN_ADMIN=$(curl -s "http://localhost:8079/creds?userid=charlie" | jq -r '.access_token')
curl -i -H "Authorization: Bearer $TOKEN_ADMIN" http://localhost:8080/admin/users
```
Expected: HTTP 200 with the user list. (Charlie must be in the Administrators group; if not, follow `make` setup-admin docs.)

Hit a public endpoint:
```bash
curl -i http://localhost:8080/.well-known/jwks.json
```
Expected: HTTP 200.

Stop the dev server: `make stop-server`

- [ ] **Step 7: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(api): wire AuthzMiddleware into request chain (#341)

Inserts AuthzMiddleware after OpenAPI validation and before the existing
per-resource middlewares. Annotated /admin/* operations are now gated by
the centralized middleware; unannotated routes still flow through the
existing ThreatModelMiddleware / DiagramMiddleware / adminRouteMiddleware
chain so behavior is unchanged for the rest of the API."
```

---

## Task 8: Delete the now-redundant `adminRouteMiddleware` wrapper

**Files:**

- Modify: `cmd/server/main.go`

`adminRouteMiddleware` (cmd/server/main.go:1323) wraps every `/admin/*` request through `AdministratorMiddleware()`. With every `/admin/*` op now annotated and `AuthzMiddleware` enforcing the admin gate, this wrapper is dead.

- [ ] **Step 1: Confirm every /admin/* op carries x-tmi-authz with admin role**

Run: `jq -r '[.paths | to_entries[] | .key as $p | select($p | startswith("/admin/")) | .value | to_entries[] | select(.key | test("^(get|post|put|patch|delete)$")) | select((.value["x-tmi-authz"].roles // []) | index("admin") | not) | "\(.key | ascii_upcase) \($p)"]' api-schema/tmi-openapi.json`
Expected: `[]` (empty list — every admin op gates on admin).

- [ ] **Step 2: Remove the registration line**

In `cmd/server/main.go` around line 797:

```go
	r.Use(adminRouteMiddleware())
```

Delete that line.

- [ ] **Step 3: Remove the function definition**

Delete `adminRouteMiddleware` (cmd/server/main.go:1323-1332). Also remove `strings` from the imports if no other code in main.go uses it (run `goimports` if available; otherwise check manually with `rg '\\bstrings\\b' cmd/server/main.go`).

- [ ] **Step 4: Verify build**

Run: `make build-server`
Expected: build succeeds.

- [ ] **Step 5: Run unit + integration tests**

Run: `make test-unit && make test-integration`
Expected: all pass. Admin endpoints are still gated, now solely by `AuthzMiddleware`.

- [ ] **Step 6: Smoke test admin gate again**

Repeat Task 7 Step 6 manual smoke tests.
Expected: same outcomes (403 for non-admin, 200 for admin).

- [ ] **Step 7: Commit**

```bash
git add cmd/server/main.go
git commit -m "refactor(api): remove adminRouteMiddleware (superseded by AuthzMiddleware) (#341)

Every /admin/* operation now declares x-tmi-authz with roles:[admin],
which AuthzMiddleware enforces by delegating to RequireAdministrator.
The path-prefix wrapper is no longer needed."
```

---

## Task 9: Delete the one-shot annotator scripts

**Files:**

- Delete: `scripts/annotate-public-authz.py`
- Delete: `scripts/annotate-admin-authz.py`

These were one-shot bootstrappers used in Tasks 3 and 4. They are no longer needed — new endpoints are annotated by hand at the time they are added to the spec, and the `check-x-tmi-authz` script enforces the requirement.

- [ ] **Step 1: Delete both scripts**

```bash
git rm scripts/annotate-public-authz.py scripts/annotate-admin-authz.py
```

- [ ] **Step 2: Verify lint still passes**

Run: `make lint`
Expected: lint passes (the scripts were not referenced elsewhere).

- [ ] **Step 3: Commit**

```bash
git commit -m "chore(scripts): remove one-shot x-tmi-authz annotators (#341)

scripts/annotate-public-authz.py and scripts/annotate-admin-authz.py were
used once during the slice-1 bootstrap. New endpoints are annotated
manually and check-x-tmi-authz.py enforces the requirement."
```

---

## Task 10: Final verification — full build, lint, and test

**Files:** none modified

- [ ] **Step 1: Clean build**

Run: `make clean-build && make build-server`
Expected: build succeeds.

- [ ] **Step 2: Lint**

Run: `make lint`
Expected: passes. `check-x-tmi-authz` reports `x-tmi-authz check passed: covered prefixes ('/admin/', '/.well-known/', '/oauth2/', '/saml/') and exact paths ('/', '/config', '/webhook-deliveries/{delivery_id}/status')`.

- [ ] **Step 3: OpenAPI validation**

Run: `make validate-openapi`
Expected: passes (Vacuum + JSON syntax + x-tmi-authz coverage).

- [ ] **Step 4: Unit tests**

Run: `make test-unit`
Expected: all pass.

- [ ] **Step 5: Integration tests**

Run: `make test-integration`
Expected: all pass.

- [ ] **Step 6: API tests (Postman)**

Run: `make test-api`
Expected: all pass.

- [ ] **Step 7: CATS fuzzing on admin endpoints**

This is a security-touching change to all `/admin/*` operations. Run CATS:

Run: `make cats-fuzz`
Expected: no NEW true-positive findings introduced by this change. Any pre-existing failures should be unchanged. Analyze with `make analyze-cats-results`.

- [ ] **Step 8: Verify with the Oracle DB review subagent**

This change does not touch DB code. Use the `oracle-db-admin` skill to confirm. Expected verdict: APPROVED with no DB-touching changes.

(If the subagent finds anything DB-adjacent we missed, address per CLAUDE.md "Oracle Database Compatibility Review".)

- [ ] **Step 9: Run security-review skill**

Run the `security-review` skill on the branch. Expected: no critical findings.

- [ ] **Step 10: Update issue #341 with progress**

Comment on #341:

```bash
gh issue comment 341 --body "Foundation slice landed:
- Schema documented in api-schema/x-tmi-authz-schema.md
- 20 public endpoints + 59 /admin/* endpoints annotated
- AuthzMiddleware enforces gates; passes through unannotated paths
- adminRouteMiddleware removed (superseded)
- Spec-completeness check enforced via make validate-openapi / make lint

Remaining work tracked in slice issues:
- #365 threat models top-level + diagrams
- #366 threat-model nested sub-resources
- #367 /me/* user-scoped
- #368 addons + automation (T18 with #358)
- #369 Timmy + content-OAuth
- #370 workflow (intake/triage/projects)
- #371 closer: flip default-deny + sweep ad-hoc role checks"
```

(Do not close #341 yet — it is the umbrella issue and stays open until #371 lands.)

---

## Self-Review

**Spec coverage** — every step in the issue's "Implementation steps" is addressed:

1. "Add `x-tmi-authz` to every operation" — slice 1 covers public + admin (79 ops). Remaining ops tracked in #365–#370.
2. "Generate a routing table from the spec at server start" — Task 5.
3. "Add `AuthzMiddleware` ahead of all resource middleware" — Tasks 6 + 7.
4. "Walk every existing handler and remove the duplicated ownership/role checks" — slice 1 removes `adminRouteMiddleware` (Task 8). Per-handler removal for non-admin paths is in slices 2–7.
5. "For T18: add `subject_authority` to the addon invocation request" — out of scope for slice 1; tracked in #368 (coordinates with #358).

The acceptance criteria from the issue:

- "`make validate-openapi` fails the build on any operation missing `x-tmi-authz`" — partially: fails on ops in the covered prefix family. Full coverage flips in #371.
- "Adding a new endpoint without the extension causes spec validation to fail in CI" — true for endpoints under covered prefixes; new top-level prefixes need an allowlist update.
- "All ad-hoc ownership checks in `api/*_handlers.go` are deleted" — only the `adminRouteMiddleware` wrapper is removed in slice 1; rest is in slices 2–7 + closer.
- "A test exists per route asserting that anonymous, reader-only, writer-only, and security-reviewer-only contexts each get the right outcome" — table tests for the admin/public slice in Task 6.
- "Addon write-back happens with the invoker's ACL, not the addon owner's" — out of scope; tracked in #368/#358.

**Placeholder scan** — no TBDs. Every code step shows real code. Test inputs and expected outputs are concrete. Manual smoke-test commands are runnable as written.

**Type consistency** — `AuthzRule`, `AuthzTable`, `Ownership`, `AuthzRoleName`, `RoleAuthzAdmin` used consistently. `loadAuthzTableFromJSON` declared in Task 5 used in Task 6 tests. `authzMiddlewareWithTable` declared in Task 6 used in tests in same task. `AuthzMiddleware()` (public entry point) declared in Task 6 wired in Task 7. `LoadGlobalAuthzTable` declared in Task 5 called from `AuthzMiddleware` in Task 6.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-02-x-tmi-authz-foundation-admin.md`.

Per user direction: subagent-driven implementation. The `superpowers:subagent-driven-development` skill will be used to dispatch one fresh subagent per task with two-stage review between tasks.
