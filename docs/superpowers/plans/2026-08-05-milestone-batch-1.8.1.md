# 1.8.1 Backlog Batch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 11 backlog issues (#667, #655, #572, #550, #687, #682+#685, #630+#629, #652, #691) as one commit per issue (or issue pair) on a single branch, ending in one PR and a 1.8.1 version bump.

**Architecture:** Nine independent work items on branch `dev/1.8.1/backlog-batch`. Each task is self-contained: its own files, tests, and conventional commit. Two tasks write to the separate `tmi.wiki` repo (pushed directly to its master, outside the PR). A final task bumps the version to 1.8.1 and assembles the PR.

**Tech Stack:** Go (Gin, GORM, testify), Python build scripts (uv/PEP 723), Terraform, YAML, grype/syft, Docker.

## Global Constraints

- Branch: `dev/1.8.1/backlog-batch` off `main`. Never commit to `main`.
- MANDATORY: use Make targets, never raw `go test`/`go run`/`docker run`. Unit tests: `make test-unit` (scoped: `make test-unit name=<regex> count1=true`). Lint: `make lint`. Build: `make build-server`.
- Use `rg`, never `grep`, for searching.
- Every commit message: conventional format, body contains `Fixes #NNN` (and the pair's second `Fixes #MMM` where applicable), and ends with:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9
  ```
- SEM markers: after changing any Go function's behavior, run `/sem-annotate --update <changed files>` (orchestrator does this at review time if the subagent cannot invoke skills).
- Task 5 (Oracle pair) MUST get an `oracle-db-admin` subagent review before its commit is considered done; address every BLOCKING finding.
- Wiki edits go to `/Users/efitz/Projects/tmi.wiki` (its own git repo, branch `master`); NEVER add project docs under `docs/` in the tmi repo.
- Stage files explicitly (`git add <paths>`), never `git add -A`.
- Version stays 1.8.0 until Task 10, which bumps `.version` AND `api-schema/tmi-openapi.json` `info.version` to 1.8.1 together.

---

### Task 1: #667 — CATS false-positive rule numbering cleanup + lint check

Current reality (verified 2026-08-05, differs from issue text): `test/cats/false-positives.yaml` has **89** rules delimited by `  - id:` under top-level `rules:`; a **complete, correct** `# rule N of 89:` series already exists (1..89, no gaps). The remaining defects: 48 stale `# rule N of 48: ...` lines on rules 1–48, one stale `# rule 49 of 49: ...` line at ~line 886, the file's header block (lines ~31–38) still instructs "rule N of 48", header line ~20 says "62 distinct FP_RULE_* ids ... ports all 62", and `CLAUDE.md` line ~201 says "(48 rules," while later saying "(89 as of the 1.7.1 CATS pass)". `test/cats/README.md` is already correct — do not edit it.

**Files:**
- Modify: `test/cats/false-positives.yaml`
- Modify: `CLAUDE.md` (line ~201 only)
- Create: `scripts/check-cats-fp-numbering.py`
- Modify: `scripts/lint.py` (append one check block)

**Interfaces:**
- Produces: `scripts/check-cats-fp-numbering.py` — exits 0 when every rule has exactly one `# rule N of M:` line in its leading comment block with N == actual 1-indexed position and M == total rule count, and no other `rule N of M` line exists anywhere in the file; exits 1 with per-line errors otherwise.

- [ ] **Step 1: Write the checker (it doubles as the failing test)**

Create `scripts/check-cats-fp-numbering.py`:

```python
#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# ///
"""Verify test/cats/false-positives.yaml rule-position comments.

Rule order is load-bearing (first match wins). Each rule's leading comment
must contain exactly one line `# rule N of M:` where N is the rule's actual
1-indexed file position and M is the total rule count. Any other
`rule N of M` string in the file is a stale leftover and fails the check.
"""

import re
import sys
from pathlib import Path

RULE_RE = re.compile(r"^  - id:")
NUM_RE = re.compile(r"#\s*rule (\d+) of (\d+)")


def main() -> int:
    path = Path(__file__).resolve().parent.parent / "test/cats/false-positives.yaml"
    lines = path.read_text().splitlines()
    rule_lines = [i for i, l in enumerate(lines) if RULE_RE.match(l)]
    total = len(rule_lines)
    errors: list[str] = []

    numbered_lines: set[int] = set()
    for pos, start in enumerate(rule_lines, 1):
        # leading comment block: contiguous comment/blank lines above the rule
        j = start - 1
        block: list[int] = []
        while j >= 0 and (lines[j].strip().startswith("#") or not lines[j].strip()):
            block.append(j)
            j -= 1
        found = []
        for k in block:
            m = NUM_RE.search(lines[k])
            if m:
                found.append((k, int(m.group(1)), int(m.group(2))))
                numbered_lines.add(k)
        if len(found) != 1:
            errors.append(
                f"line {start + 1}: rule {pos} has {len(found)} 'rule N of M' "
                f"comment lines (want exactly 1)"
            )
            continue
        k, n, m_ = found[0]
        if n != pos or m_ != total:
            errors.append(
                f"line {k + 1}: says 'rule {n} of {m_}', actual position is "
                f"rule {pos} of {total}"
            )

    # stray numbering lines outside any rule's leading block (e.g. header text)
    for i, l in enumerate(lines):
        if NUM_RE.search(l) and i not in numbered_lines:
            errors.append(f"line {i + 1}: stray 'rule N of M' text: {l.strip()}")

    if errors:
        print(f"check-cats-fp-numbering: {len(errors)} error(s) in {path.name}:")
        for e in errors:
            print(f"  {e}")
        return 1
    print(f"check-cats-fp-numbering: OK ({total} rules, numbering consistent)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

NOTE: the header block (lines ~31–38) contains the literal text `"rule N of 48"` — after Step 3 rewrites it to avoid embedding a count (see below), it must not match `NUM_RE` (the placeholder `rule N of <total>` does not match `\d+`).

- [ ] **Step 2: Run the checker to verify it fails on current state**

Run: `uv run scripts/check-cats-fp-numbering.py`
Expected: exit 1, ~49+ errors (each of rules 1–48 has 2 numbering lines; line ~886 stale `49 of 49`; header stray lines).

- [ ] **Step 3: Fix the YAML**

In `test/cats/false-positives.yaml`:
1. Delete all 48 stale `# rule N of 48: ...` lines and the one `# rule 49 of 49: ...` line (~line 886). Keep every `# rule N of 89:` line and all descriptive comment lines. (The stale lines carry section-title text like `Rate Limit False Positives (429)` — where that title text does not already appear in the remaining comments for that rule, move the title text onto the surviving `# rule N of 89:` line, e.g. `# rule 2 of 89: Rate Limit False Positives (429)`, rather than losing it.)
2. Rewrite the header note (lines ~31–38) to:
   ```
   # NOTE on rule numbering: each rule's leading comment reads "rule N of <total>",
   # where N is the rule's actual position in this file (1-indexed, matching
   # evaluation order — first match wins). If you add, remove, or reorder a rule,
   # renumber every comment (and the total) to match.
   # `scripts/check-cats-fp-numbering.py` (run by `make lint`) fails the build
   # when any comment disagrees with a rule's real position.
   ```
3. Fix header line ~20: change "62 distinct FP_RULE_* ids ... This file ports all 62" to past tense with context, e.g. "originally ported 62 FP_RULE_* ids from the legacy detect_false_positive(); rules added since are appended in file order."

- [ ] **Step 4: Fix CLAUDE.md**

Line ~201: change `(48 rules, evaluated in file order, first match wins — see \`test/cats/README.md\`)` to `(evaluated in file order, first match wins — see \`test/cats/README.md\`)`. Leave the trailing "The rule count grows... (89 as of the 1.7.1 CATS pass)" sentence as is.

- [ ] **Step 5: Wire into lint**

Append to `scripts/lint.py`, following the existing pattern (e.g. the `check-unsafe-union-methods.py` block):

```python
    log_info("Checking CATS false-positive rule numbering...")
    run_cmd(
        ["uv", "run", "scripts/check-cats-fp-numbering.py"],
        cwd=project_root,
    )
```

- [ ] **Step 6: Verify**

Run: `uv run scripts/check-cats-fp-numbering.py` → exit 0, "OK (89 rules...)".
Run: `make lint` → passes, includes the new check.
Sanity: `rg -c '^  - id:' test/cats/false-positives.yaml` → 89 (unchanged; only comments were touched). `uv run yq '.rules | length' test/cats/false-positives.yaml` if yq is available, else skip.

- [ ] **Step 7: Commit**

```bash
git add test/cats/false-positives.yaml CLAUDE.md scripts/check-cats-fp-numbering.py scripts/lint.py
git commit -m "docs(cats): fix false-positive rule numbering drift and enforce it in lint

Delete the stale 'rule N of 48'/'of 49' comment series (the current
'rule N of 89' series was already complete and correct), rewrite the
file header so it no longer hardcodes a count, fix the self-contradictory
count in CLAUDE.md, and add scripts/check-cats-fp-numbering.py to make
lint so position comments can no longer drift silently.

Fixes #667

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 2: #550 — Dockerfile.controller drops the tmi-client staging

CRITICAL correction to the issue text: simply deleting the COPY breaks the build. `go.mod` line 7 has `replace github.com/ericfitz/tmi-clients/go-client-generated/v1_6_0 => ../tmi-clients/go-client-generated/v1_6_0` and line 17 requires that module, and `go mod download` (no args) resolves every required module regardless of build target — verified: with the replace pointing at a missing path it fails `reading /nonexistent/path/go.mod: no such file or directory`. The fix must drop the require+replace inside the image before `go mod download`.

**Files:**
- Modify: `Dockerfile.controller` (lines ~13–22 plus wherever `COPY . .` re-introduces go.mod)
- Modify: `scripts/build-app-containers.py` (lines ~44–51, `CLIENT_DEPENDENT_COMPONENTS`)
- Modify: `Makefile` (add `build-controller-container` target next to `build-extractor-container` ~line 926)

**Interfaces:**
- Consumes: `uv run scripts/build-app-containers.py --target local --component controller` (supported today, `VALID_COMPONENTS` includes controller).
- Produces: controller image builds with no `.docker-deps/tmi-client` staging; `CLIENT_DEPENDENT_COMPONENTS = ("server", "extractor", "chunkembed")`.

- [ ] **Step 1: Read Dockerfile.controller end-to-end**

Note every point where go.mod enters the image (initial `COPY go.mod go.sum ./` AND any later `COPY . .` that overwrites it) — the module edit must hold at build time, not just download time.

- [ ] **Step 2: Rewrite the staging block**

Replace lines ~13–22:

```dockerfile
# Copy go mod files and staged tmi-client dependency
COPY go.mod go.sum ./
COPY .docker-deps/tmi-client/ /tmi-client/

# Rewrite go.mod replace directive to point at the in-container client path
RUN sed -i 's|=> ../tmi-clients/go-client-generated/[^ ]*|=> /tmi-client|' go.mod

# Download dependencies with security verification
RUN go mod download && \
    go mod verify
```

with:

```dockerfile
# Copy go mod files. The controller imports nothing from tmi-clients
# (verified: go list -deps ./cmd/component-controller has no tmi-client
# entry), but go.mod requires + replaces that module for the server and
# workers, and a bare `go mod download` resolves every requirement. Drop
# the requirement inside the image instead of staging the module (#550).
COPY go.mod go.sum ./
RUN go mod edit \
      -dropreplace=github.com/ericfitz/tmi-clients/go-client-generated/v1_6_0 \
      -droprequire=github.com/ericfitz/tmi-clients/go-client-generated/v1_6_0

# Download dependencies with security verification
RUN go mod download && \
    go mod verify
```

If a later `COPY . .` overwrites go.mod, repeat the same `RUN go mod edit -dropreplace=... -droprequire=...` immediately after it (before any `go build`). `go mod verify` and `go build` must not see the tmi-clients requirement. If the build step uses `-mod=readonly` (the default) and complains about go.sum entries, add `RUN go mod tidy` is NOT allowed (network); instead keep go.sum as copied — dropping a require does not invalidate remaining go.sum entries, extra entries are harmless.

- [ ] **Step 3: Update CLIENT_DEPENDENT_COMPONENTS**

In `scripts/build-app-containers.py` lines ~44–51, change to:

```python
# Worker components depend on the staged tmi-client module, same as the server.
# `controller` is NOT here: cmd/component-controller has zero tmi-clients
# imports and Dockerfile.controller drops the go.mod require/replace inside
# the image (go mod edit -droprequire) before `go mod download`, so it needs
# no staging (#550).
CLIENT_DEPENDENT_COMPONENTS = ("server", "extractor", "chunkembed")
```

- [ ] **Step 4: Add the Makefile wrapper**

Next to `build-extractor-container` (~line 926), following its exact style:

```makefile
build-controller-container:  ## Build TMI component-controller container image
	@uv run scripts/build-app-containers.py --target local --component controller
```

Add `build-controller-container` to the relevant `.PHONY` line.

- [ ] **Step 5: Verify by building**

Run: `make build-controller-container`
Expected: image builds successfully with no `.docker-deps/tmi-client` staging step in the log. If it fails at `go build` due to the replace reappearing, apply the Step 2 fallback (repeat go mod edit after `COPY . .`) and rebuild.

Also confirm nothing else stages for controller-only builds: `rg -n "CLIENT_DEPENDENT" scripts/` shows only the updated tuple and its single use.

- [ ] **Step 6: Lint and commit**

Run `make lint` (Python file changed; also catches Makefile tab issues via build).

```bash
git add Dockerfile.controller scripts/build-app-containers.py Makefile
git commit -m "chore(build): stop staging tmi-client into the controller image

cmd/component-controller imports nothing from tmi-clients; drop the
go.mod require/replace inside Dockerfile.controller (go mod edit
-droprequire/-dropreplace before go mod download) instead of staging the
module, remove controller from CLIENT_DEPENDENT_COMPONENTS, and add a
build-controller-container make target for isolated builds.

Fixes #550

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 3: #572 — align terraform aws-private with aws-public

Three drifts, all in `terraform/environments/aws-private/`. Research correction: `terraform init -upgrade` would land aws-private on **6.58.0** (registry moved on), re-creating drift in the other direction. Instead, pin aws-private's lock to aws-public's exact 6.56.0 stanza. No infrastructure is touched (aws-private is not deployed; no apply anywhere).

**Files:**
- Modify: `terraform/environments/aws-private/main.tf` (helm block ~lines 31–34; `module "kubernetes"` block ~line 173)
- Modify: `terraform/environments/aws-private/variables.tf` (insert between `vpc_cidr` ~line 15 and `db_name` ~line 21)
- Modify: `terraform/environments/aws-private/.terraform.lock.hcl` (aws provider stanza, lines ~4–6)

- [ ] **Step 1: Bound the helm constraint**

In `aws-private/main.tf` replace:

```hcl
    helm = {
      source  = "hashicorp/helm"
      version = ">= 2.12.0"
    }
```

with (comment deliberately reworded from aws-public's, whose last sentence was PR-specific):

```hcl
    helm = {
      source = "hashicorp/helm"
      # Pinned below 3.0: the helm provider v3 line replaced the
      # `kubernetes { ... }` nested block used by provider "helm" below with
      # a different schema, breaking `terraform validate`/`init` on an
      # unbounded ">= 2.12.0" constraint. Mirrors aws-public and the
      # kubernetes module's own constraint.
      version = ">= 2.12.0, < 3.0.0"
    }
```

- [ ] **Step 2: Thread kubernetes_version**

In `aws-private/variables.tf`, insert between `vpc_cidr` and `db_name` (copy verbatim from aws-public/variables.tf lines 21–33):

```hcl
# EKS only accepts ONE minor version bump per update, for the control plane and
# for the node group alike. Upgrading an existing cluster across several minors
# is therefore a sequence of applies, not one: set this to each intermediate
# version in turn (1.33, then 1.34, ...) and apply each time. Terraform already
# orders the work correctly within a hop — control plane, then node group, then
# the core addons — because of the dependencies between those resources.
# Leaving this unset takes the module default, which is the version this
# deployment is expected to converge on.
variable "kubernetes_version" {
  description = "Kubernetes version for the EKS cluster and node group. Bump one minor at a time when upgrading an existing cluster."
  type        = string
  default     = null
}
```

In `aws-private/main.tf` `module "kubernetes"` (starts ~line 173), add after `name_prefix`:

```hcl
  # null falls through to the module's own default (Terraform substitutes a
  # module variable's default when the caller passes null), so the pin lives in
  # one place unless an operator is deliberately stepping through an upgrade.
  kubernetes_version     = var.kubernetes_version
```

Match the block's aligned-`=` formatting (aws-private aligns; run `terraform fmt` in Step 4 to settle it).

- [ ] **Step 3: Pin the aws provider lock to 6.56.0**

In `aws-private/.terraform.lock.hcl`, replace the entire `provider "registry.terraform.io/hashicorp/aws"` stanza with the corresponding stanza copied verbatim from `aws-public/.terraform.lock.hcl` (version 6.56.0 + its hashes). Both locks are darwin_arm64-single-platform, so the hash set is directly transplantable. Leave all other stanzas (helm, kubernetes, random, tls, null) untouched.

- [ ] **Step 4: Verify — init without backend, validate, fmt**

```bash
cd terraform/environments/aws-private
terraform init -backend=false   # no backend.hcl needed; installs providers per lock, verifies hashes
terraform validate
terraform fmt -check . || terraform fmt .
cd ../aws-public && terraform fmt -check .
```

Expected: init succeeds installing aws 6.56.0 (proves the transplanted hashes are valid), validate passes, fmt clean. No plan/apply anywhere.

- [ ] **Step 5: Commit**

```bash
git add terraform/environments/aws-private/main.tf terraform/environments/aws-private/variables.tf terraform/environments/aws-private/.terraform.lock.hcl
git commit -m "chore(terraform): align aws-private with aws-public

Bound the helm provider constraint below 3.0 (matching aws-public and the
kubernetes module), thread kubernetes_version through to module.kubernetes
so EKS upgrades can be stepped one minor at a time, and pin the aws
provider lock to aws-public's 6.56.0 (init -upgrade would have jumped to
6.58.0, recreating the drift in the other direction).

Fixes #572

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 4: #687 — one-way EmailVerified guard on token mint

Bug: `auth/service.go:286` (`GenerateTokensWithAuthTime`) does `user.EmailVerified = userInfo.EmailVerified` unconditionally, then persists via `s.UpdateUser` (~:309) and mints the claim from it (~:328). SAML's synthesized-email path (`applyEmailFallback`) and OAuth IdPs that carry the flag only in ID-token claims both pass `EmailVerified=false` here, downgrading a stored `true`. The OAuth path's own guard (`auth/handlers_oauth_user.go:254-258`) is the idiom: one-way false→true. A single guard at the mint site fixes both SAML (`auth/saml_manager.go:261`) and OAuth (`auth/handlers_token.go:263`) callers — centralized enforcement, per project preference.

**Files:**
- Modify: `auth/service.go` (~line 286)
- Test: `auth/service_email_verified_test.go` (new)

**Interfaces:**
- Consumes: `setupTestServiceWithRepos(t, userRepo, credRepo)` (auth/client_credentials_grant_test.go:78), `stubUserRepo` (same file :20, its `Update` is a no-op — needs a recording wrapper).
- Produces: no API changes; `GenerateTokensWithAuthTime` behavior: EmailVerified transitions false→true only.

- [ ] **Step 1: Write the failing test**

Create `auth/service_email_verified_test.go`. Define a recording repo that embeds the existing stub and captures `Update` calls, then two subtests: downgrade blocked, upgrade applied. Shape (adapt constructor/field names to the actual `stubUserRepo` and `setupTestServiceWithRepos` signatures — read `auth/client_credentials_grant_test.go` first):

```go
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingUserRepo captures the last user passed to Update so tests can
// assert what would be persisted.
type recordingUserRepo struct {
	stubUserRepo
	updated []User
}

func (r *recordingUserRepo) Update(ctx context.Context, u User) error {
	r.updated = append(r.updated, u)
	return nil
}

func TestGenerateTokensWithUserInfo_EmailVerifiedOneWay(t *testing.T) {
	t.Run("synthesized_saml_email_cannot_downgrade", func(t *testing.T) {
		repo := &recordingUserRepo{}
		svc := setupTestServiceWithRepos(t, repo, nil)

		user := User{ID: "u1", Email: "alice@example.com", EmailVerified: true}
		info := &UserInfo{IdP: "saml-test", EmailVerified: false} // synthesized email path

		pair, err := svc.GenerateTokensWithAuthTime(context.Background(), user, info, time.Now())
		require.NoError(t, err)
		require.NotEmpty(t, pair.AccessToken)

		require.NotEmpty(t, repo.updated, "token mint should persist provider data")
		assert.True(t, repo.updated[len(repo.updated)-1].EmailVerified,
			"stored EmailVerified=true must never be downgraded by an unverified provider payload")
	})

	t.Run("verified_provider_payload_upgrades", func(t *testing.T) {
		repo := &recordingUserRepo{}
		svc := setupTestServiceWithRepos(t, repo, nil)

		user := User{ID: "u2", Email: "bob@example.com", EmailVerified: false}
		info := &UserInfo{IdP: "test", EmailVerified: true}

		_, err := svc.GenerateTokensWithAuthTime(context.Background(), user, info, time.Now())
		require.NoError(t, err)
		require.NotEmpty(t, repo.updated)
		assert.True(t, repo.updated[len(repo.updated)-1].EmailVerified,
			"EmailVerified must transition false→true")
	})
}
```

Also assert the claim if cheap: parse `pair.AccessToken` with the service's key manager helper if one is exposed in tests (see `jwt_key_manager_test.go:305` for the claim-assertion pattern); if not trivially available, the persisted-row assertions suffice.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestGenerateTokensWithUserInfo_EmailVerifiedOneWay count1=true`
Expected: first subtest FAILS (EmailVerified persisted as false); second passes.

- [ ] **Step 3: Implement the guard**

In `auth/service.go`, replace line ~286:

```go
		user.EmailVerified = userInfo.EmailVerified
```

with:

```go
		// EmailVerified transitions one-way false -> true, matching
		// updateUserOnLogin (#648, #687): a provider payload that lacks a
		// verifiable email (e.g. a synthesized SAML fallback address, or an
		// IdP that only carries the flag in ID-token claims) must never
		// downgrade a stored verified flag.
		if userInfo.EmailVerified && !user.EmailVerified {
			user.EmailVerified = true
		}
```

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestGenerateTokensWithUserInfo_EmailVerifiedOneWay count1=true` → PASS.
Run: `make test-unit` (full) → PASS (guards against regressions in `TestGenerateTokensWithAuthTime_SetsClaim`, `TestUpdateSAMLUserOnLogin_GuardsAndTiers`, etc.).
Run: `make build-server` and `make lint`.

- [ ] **Step 5: Update SEM marker**

`auth/service.go:282` `GenerateTokensWithAuthTime` carries `SEM@18f87a01...` — behavior changed; refresh via `/sem-annotate --update auth/service.go` (orchestrator does this if the subagent cannot invoke skills; if neither can, update the marker's description by hand to mention the one-way guard is NOT needed — intent unchanged — but the sha must be refreshed by the skill, so leave a note for the orchestrator).

- [ ] **Step 6: Commit**

```bash
git add auth/service.go auth/service_email_verified_test.go
git commit -m "fix(auth): token mint can no longer downgrade EmailVerified

GenerateTokensWithAuthTime assigned userInfo.EmailVerified unconditionally
and persisted it, so a SAML login with a synthesized fallback email (or an
OAuth IdP carrying the flag only in ID-token claims) downgraded a stored
verified flag on every mint. Enforce the same one-way false->true rule the
OAuth login path uses, at the single choke point both paths share.

Fixes #687

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 5: #685 + #682 — Oracle error sentinels + ADB quota-default repair (pair, one commit)

Part A (#685): add three codes to `oracleCodeSentinels` (`internal/dberrors/classify_oracle_codes.go`, map at lines 30–114; sole consumer `classifyOracleCode` :127). Classifications:
- `1000: ErrTransient` (ORA-01000 max open cursors — resource exhaustion, joins the "Additional ADB transient conditions" block alongside 18/20/12520; maps to 503+Retry-After via `StoreErrorToRequestError`, api/request_utils.go:583)
- `4031: ErrTransient` (ORA-04031 shared pool — same rationale)
- `932: ErrConstraint` (ORA-00932 inconsistent datatypes → 400. Rationale to state in the code comment: reachable from CLOB-backed columns (`StringArray`, `CVSSArray`, `NullableDBText`) landing in ORDER BY/GROUP BY/DISTINCT, typically driven by request-supplied sort/filter parameters, so 400 beats 500; PG analogue is class 42804 which PG never hits for this because text columns sort fine there. ErrConstraint is the only existing sentinel yielding a non-500 without inventing a new category — flag this decision explicitly to the oracle-db-admin reviewer.)

Part B (#682): the additive Oracle migrator (`auth/db/gorm_oracle.go`) no-ops `MigrateColumn`, so #649's GORM `default:` tag fixes never reached already-provisioned ADB columns. Ship a small tagged Go fixer, run it against dev ADB, and document the gotcha in the wiki.

**Files:**
- Modify: `internal/dberrors/classify_oracle_codes.go`
- Modify: `internal/dberrors/classify_oracle_codes_test.go`
- Create: `scripts/oracle-quota-default-fix/main.go` (build tag `oracle`)
- Modify: `Makefile` (target `oracle-fix-quota-defaults`, mirroring the clob-probe target at ~line 343)
- Wiki (separate repo `/Users/efitz/Projects/tmi.wiki`): modify `Database-Operations.md`

**Interfaces:**
- Consumes: sentinels `ErrTransient`, `ErrConstraint` (internal/dberrors/errors.go:13–42); test style of `classify_oracle_codes_test.go` (per-code `TestClassifyOracleCode_<Case>` funcs, testify, negative assertions).
- Produces: `classifyOracleCode` returns wrapped `ErrTransient` for 1000/4031, `ErrConstraint` for 932.

- [ ] **Step 1: Write failing tests**

Append to `internal/dberrors/classify_oracle_codes_test.go`, following house style (positive + negative assertions):

```go
func TestClassifyOracleCode_MaxOpenCursors(t *testing.T) {
	src := fmt.Errorf("ORA-01000: maximum open cursors exceeded")
	err := classifyOracleCode(src, 1000)
	assert.True(t, errors.Is(err, ErrTransient), "ORA-01000 is resource exhaustion; retryable")
	assert.False(t, errors.Is(err, ErrConstraint))
}

func TestClassifyOracleCode_SharedPoolExhausted(t *testing.T) {
	src := fmt.Errorf("ORA-04031: unable to allocate bytes of shared memory")
	err := classifyOracleCode(src, 4031)
	assert.True(t, errors.Is(err, ErrTransient), "ORA-04031 is resource exhaustion; retryable")
	assert.False(t, errors.Is(err, ErrConstraint))
}

func TestClassifyOracleCode_InconsistentDatatypes(t *testing.T) {
	src := fmt.Errorf("ORA-00932: inconsistent datatypes: expected - got CLOB")
	err := classifyOracleCode(src, 932)
	assert.True(t, errors.Is(err, ErrConstraint), "ORA-00932 from request-driven sort/filter on CLOB columns should 400, not 500")
	assert.False(t, errors.Is(err, ErrTransient))
	assert.False(t, errors.Is(err, ErrDuplicate))
}
```

Run: `make test-unit name='TestClassifyOracleCode_(MaxOpenCursors|SharedPoolExhausted|InconsistentDatatypes)' count1=true` → FAIL (unclassified returns nil).

- [ ] **Step 2: Add the sentinels**

In `classify_oracle_codes.go`: add `1000` and `4031` entries to the "Additional ADB transient conditions" block; add `932` near the other constraint entries with a multi-line comment in the style of the ORA-01407 block (state reachability via CLOB columns in ORDER BY/GROUP BY/DISTINCT and why 400). Re-run the scoped tests → PASS. Run `make test-unit name=TestClassifyOracleCode count1=true` (all 25) → PASS.

- [ ] **Step 3: Write the quota-default fixer**

Create `scripts/oracle-quota-default-fix/main.go`, build-tagged `oracle`. Copy the connection bootstrap from `scripts/oracle-clob-like-probe/` (read it first — same `TMI_DATABASE_URL` sourcing via `scripts/oci-env.sh`). Program logic:

1. Query current state:
   ```sql
   SELECT table_name, column_name, data_default FROM user_tab_columns
   WHERE (table_name = 'USER_API_QUOTAS' AND column_name = 'MAX_REQUESTS_PER_MINUTE')
      OR (table_name = 'ADDON_INVOCATION_QUOTAS' AND column_name = 'MAX_ACTIVE_INVOCATIONS')
   ```
   Print each. (`data_default` is a LONG — scan into string via the driver; if the driver balks, `TO_LOB` is not valid on user_tab_columns, so fall back to printing "unreadable, proceeding".)
2. Execute (idempotent, metadata-only):
   ```sql
   ALTER TABLE user_api_quotas MODIFY (max_requests_per_minute DEFAULT 1000)
   ALTER TABLE addon_invocation_quotas MODIFY (max_active_invocations DEFAULT 3)
   ```
3. Re-query step 1 and print the after state.
4. Assess stale rows (print counts only, no UPDATE — issue says data-fix optional and current write paths always pass explicit values):
   ```sql
   SELECT COUNT(*) FROM user_api_quotas WHERE max_requests_per_minute = 100
   SELECT COUNT(*) FROM addon_invocation_quotas WHERE max_active_invocations = 1
   ```

Makefile target (mirror the clob-probe pattern at ~line 343):

```makefile
oracle-fix-quota-defaults:  ## One-off: repair quota column DEFAULTs on Oracle ADB (#682; requires scripts/oci-env.sh)
	@bash -c "source scripts/oci-env.sh && go run -tags oracle ./scripts/oracle-quota-default-fix/..."
```

- [ ] **Step 4: Run against dev ADB**

Run: `make oracle-fix-quota-defaults`
Expected: before-state shows `1` / `100` defaults, ALTERs succeed sub-second, after-state shows `3` / `1000`, row-assessment counts printed (record them in the task report; nonzero counts get noted on issue #682 when closing, not UPDATEd). If OCI is unreachable (creds/network), STOP and surface to the orchestrator — do not fake the run.

- [ ] **Step 5: Wiki note**

In `/Users/efitz/Projects/tmi.wiki/Database-Operations.md`, add a section "Changing a column DEFAULT on Oracle" stating: TMI's `additiveOracleMigrator` (auth/db/gorm_oracle.go) makes GORM's `MigrateColumn` a no-op on Oracle, so editing a `default:` struct tag never alters an existing ADB column (PostgreSQL self-heals on every AutoMigrate); every future default-tag change needs a hand-written `ALTER TABLE ... MODIFY (... DEFAULT ...)` companion, and the schema fingerprint will mask the drift after one boot; reference `make oracle-fix-quota-defaults` (#682) as the worked example. Commit in the wiki repo:

```bash
cd /Users/efitz/Projects/tmi.wiki
git add Database-Operations.md
git commit -m "Document Oracle MODIFY DEFAULT companion-step requirement (tmi#682)"
git push
```

If push fails on SSH key touch policy, leave committed and surface to orchestrator.

- [ ] **Step 6: Full gates + oracle-db-admin review**

Run `make lint`, `make build-server`, `make test-unit`. Then dispatch the `oracle-db-admin` subagent (this task changes `internal/dberrors/` — mandatory) with the diff of `classify_oracle_codes.go`, the test additions, and the fixer script; flag the ORA-00932→ErrConstraint decision explicitly. Address every BLOCKING finding before committing; fold notes in or file follow-ups.

- [ ] **Step 7: Commit (tmi repo)**

```bash
git add internal/dberrors/classify_oracle_codes.go internal/dberrors/classify_oracle_codes_test.go scripts/oracle-quota-default-fix/ Makefile
git commit -m "fix(db): classify ORA-00932/01000/04031; repair ADB quota column defaults

Add three Oracle codes to oracleCodeSentinels: ORA-01000 (max open
cursors) and ORA-04031 (shared pool) as ErrTransient (503 + Retry-After),
ORA-00932 (inconsistent datatypes, reachable from request-driven sorts on
CLOB-backed columns) as ErrConstraint (400). Previously all three fell
through to HTTP 500 on Oracle only.

Ship scripts/oracle-quota-default-fix (make oracle-fix-quota-defaults) and
run it against dev ADB: the additive Oracle migrator no-ops MigrateColumn,
so #649's default: tag fixes (100->1000, 1->3) never reached provisioned
columns. Companion wiki note added to Database-Operations.

Oracle-db-admin verdict: <fill in from Step 6>

Fixes #685
Fixes #682

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 6: #630 + #629 — container scans: platform selection + DB provenance (pair, one commit)

Part A (#630): `scan_image()` (`scripts/container_build_helpers.py:639-771`) calls grype/syft without `--platform`; cloud images are linux/amd64, hosts are arm64 → scan cannot resolve the image. `TargetConfig.platform` (field :142, values from `_resolve_arch` :160–173; heroku hardcodes at :353) is in scope at all three call sites. Trap: `--arch both` yields `"linux/amd64,linux/arm64"`, not a valid `--platform` value — split and scan per-arch.

Part B (#629): record grype DB provenance (already present in scan JSON at `descriptor.db.status`: `built`, `schemaVersion`, `from`, `valid`) into `security-summary.md` (`generate_security_summary` :774–817); run one `grype db update` per script run; collapse the three grype invocations into one (verified working: `grype <img> -o json=<path> -o sarif=<path> -o table` writes both files and leaves table on stdout).

Also fold in: report-name disambiguation (`report_name` currently collapses `iad.ocir.io/.../tmi-server:latest` and local `tmi/tmi-server:latest` to the same file) — suffix the arch when `platform` is passed.

**Files:**
- Modify: `scripts/container_build_helpers.py` (`scan_image`, `generate_security_summary`, new module-level `ensure_grype_db_current()`)
- Modify: `scripts/build-app-containers.py` (`scan_component` :368–380 passes `config.platform`)
- Modify: `scripts/build-db-containers.py` (call sites :140 and :178 pass `config.platform`)

**Interfaces:**
- Produces: `scan_image(image_name: str, reports_dir: Path, platform: str | None = None) -> bool`. `platform=None` preserves today's host-arch behavior exactly (local `make scan-containers` unaffected). New artifacts per image: `{report_name}-scan.json` (authoritative JSON, kept), plus existing `-scan.sarif`, `-scan.txt`, SBOMs. `generate_security_summary` unchanged signature; output gains a `**Vulnerability DB:**` line per scanned image (built timestamp + schema + valid flag) read from the `-scan.json` files.

- [ ] **Step 1: Restructure scan_image**

Rewrite `scan_image` with these changes (preserve the three explanatory comment blocks and all existing failure semantics — nonzero grype exit, unparseable JSON, missing `source` all return False):

1. Signature: `def scan_image(image_name: str, reports_dir: Path, platform: str | None = None) -> bool:`
2. At top: `platforms = platform.split(",") if platform else [None]`; loop the whole scan body per entry; overall result is AND of all entries.
3. Per-platform report base: `report_name = image_name.split("/")[-1].replace(":", "-")` plus, when the platform entry is set, `-{p.split('/')[-1]}` suffix (e.g. `tmi-server-latest-amd64`).
4. Single grype invocation replaces the three:
   ```python
   json_path = reports_dir / f"{report_name}-scan.json"
   sarif_path = reports_dir / f"{report_name}-scan.sarif"
   cmd = ["grype", image_name, "-o", f"json={json_path}", "-o", f"sarif={sarif_path}", "-o", "table"]
   if p:
       cmd += ["--platform", p]
   result = run(cmd, capture=True, check=False)
   ```
   On `returncode != 0` keep the existing two-branch error message (image present locally vs not). Parse JSON from `json_path`; write table (stdout) to `{report_name}-scan.txt` and `print()` it, as today.
5. DB provenance: after parsing, extract `db_status = data.get("descriptor", {}).get("db", {}).get("status", {})`; `log_info` the `built`/`schemaVersion`; if `db_status.get("valid") is False`, `log_error` and return False.
6. Syft: append `["--platform", p]` when set; SBOM filenames get the same arch suffix via `report_name`.
7. CVE counting, thresholds, return semantics unchanged.

8. Module-level, called once per process before the first scan (guard with a global `_grype_db_checked` flag):
   ```python
   def ensure_grype_db_current() -> None:
       """Best-effort grype DB refresh, once per run (#629). A failed update
       is a warning — grype's own validate-age gate (120h) still applies and
       a stale-beyond-limit DB fails the scan itself."""
   ```
   Body: `run(["grype", "db", "update"], check=False)`; log warn on nonzero. Call it at the top of `scan_image` under the flag.

- [ ] **Step 2: Thread platform at the call sites**

- `build-app-containers.py` `scan_component` :380: `return helpers.scan_image(f"{image_name}:latest", reports_dir, platform=config.platform)`
- `build-db-containers.py` :140 and :178: add `platform=config.platform`.

- [ ] **Step 3: Surface provenance in the summary**

In `generate_security_summary`, alongside the existing `*-scan.txt` glob, read each sibling `*-scan.json` (if present) and append to the header block after `**Scanner:** Grype (Anchore)`:

```
**Vulnerability DB:** schema vN, built <built-timestamp> (valid: true)
```

(one line per distinct DB build seen — normally all images share one). While in there, switch the per-image Critical/High counts to come from the JSON `matches` when the JSON exists, falling back to the existing substring count for legacy `.txt`-only rows.

- [ ] **Step 4: Verify locally**

1. `make scan-containers` (host-arch local images) → passes; inspect `security-reports/`: new `-scan.json` present, sarif + txt still produced from the single invocation, `security-summary.md` has the DB line, counts sane.
2. Platform flag smoke test (matching arch is harmless — verified in research): `uv run scripts/build-app-containers.py --target local --component server --scan-only` after temporarily... skip mutation: instead run grype manually once to confirm flag syntax: `grype tmi/tmi-server:latest --platform linux/arm64 -o table | head -5` (on this arm64 host) → resolves. The amd64 cloud path gets exercised on the next `make build-app-aws`; note that in the commit body.
3. `rg -n "grype|syft" scripts/container_build_helpers.py` → no remaining bare invocations missing platform threading.

- [ ] **Step 5: Commit**

```bash
git add scripts/container_build_helpers.py scripts/build-app-containers.py scripts/build-db-containers.py
git commit -m "fix(security): scan cloud images for their actual platform; record grype DB provenance

Thread TargetConfig.platform into scan_image() and pass --platform to
grype and syft (splitting the 'both' multi-arch value), so cloud-target
scans stop failing to resolve linux/amd64 images from arm64 hosts — every
AWS deploy before #628 reported a clean scan without reading any image.
Arch-suffixed report names keep cloud artifacts from overwriting local
ones.

Collapse the three grype invocations into one (json+sarif+table from a
single scan), refresh the DB once per run, fail the scan when grype marks
its DB invalid, and record the DB schema/build timestamp in
security-summary.md so 'was this scan current?' is answerable after the
fact.

First post-fix cloud scan should be treated as a baseline, not assumed
clean (four of five images have never been read).

Fixes #630
Fixes #629

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 7: #652 — pre-stream refusals return real JSON status codes on Timmy SSE endpoints

Bug: `CreateTimmyChatSession` (`api/timmy_handlers.go:26`) creates the SSE writer (`NewSSEWriter(c)` :59) before `CreateSession`, and its error branch (:93–99) funnels every failure — including the pre-stream typed 429 `RequestError{Status:429, Code:"session_limit_exceeded"}` from `timmy_session_manager.go:163-170` — into `sse.SendError`, committing 200. Same shape in `CreateTimmyChatMessage` (:217, error branch :360–366) where `HandleMessage` returns pre-stream `RequestError`s: 404 `session_not_found`, 409 `session_not_active`, 429 `message_rate_limit`, 503 `llm_busy`.

Fix (centralized, both handlers): on error, if nothing has been written yet (`!c.Writer.Written()`), strip the SSE headers and route through `HandleRequestError` (which honors arbitrary `RequestError.Status`, sets Retry-After on 429/503); only fall back to `sse.SendError` when bytes are already on the wire (progress events from `CreateSession`'s snapshot phase, mid-stream tokens). The first progress event commits the stream, so `Written()` is exactly the right discriminator.

Spec: `createTimmyChatSession` already declares 429 (`$ref TooManyRequests`) — no spec change for sessions. For `createTimmyChatMessage`, CHECK the declared responses in `api-schema/tmi-openapi.json`; any of 404/409/429 not declared must be added as `$ref`s to the existing components (`#/components/responses/Conflict` exists unreferenced), then `make validate-openapi` + `make generate-api` (Documented-Status-Code Policy).

**Files:**
- Modify: `api/timmy_handlers.go` (both handlers' error branches; add a small shared helper)
- Modify: `api/timmy_session_manager.go` (wrap the `CountActiveByThreatModel` error with `StoreErrorToRequestError` so a transient DB error 503s instead of 500s)
- Possibly modify: `api-schema/tmi-openapi.json` + regenerate `api/api.go`
- Test: `api/timmy_handlers_test.go` (extend)

**Interfaces:**
- Consumes: `HandleRequestError(c, err)` / `RequestError` (api/request_utils.go:397/:375), `StoreErrorToRequestError` (api/request_utils.go:583), test harness `setupTimmyHandlerTest` (api/timmy_handlers_test.go:37 — NOTE: its server has no session manager, `getTimmyRuntime` returns nil → handler 503s; the new test must wire a `TimmySessionManager` with a low cap, see Step 1).
- Produces: helper `func sendTimmyError(c *gin.Context, sse *SSEWriter, code string, err error)` in timmy_handlers.go — JSON via HandleRequestError when `!c.Writer.Written()` (after deleting `Content-Type`, `X-Accel-Buffering` headers), else `sse.SendError(code, err.Error())`.

- [ ] **Step 1: Write the failing test**

In `api/timmy_handlers_test.go`, add a test that drives the real handler over the cap. Read `api/server.go:468` (`getTimmyRuntime`) and `setupTimmyHandlerTest` first to wire a runtime: construct a `TimmySessionManager` with `MaxSessionsPerThreatModel: 1` config against the test `GlobalTimmySessionStore`, attach it to the test server the way production wiring does (field on `Server`; follow how `NewServerForTests` exposes it — if no seam exists, add one: a `SetTimmyRuntimeForTests(...)` method is acceptable). Then:

```go
func TestTimmyCreateSession_CapExceededReturns429JSON(t *testing.T) {
	s, tmID := setupTimmyHandlerTestWithSessionManager(t, 1) // helper you add: cap=1
	createTestTimmySession(t, tmID /* one active session, fills the cap */)

	w := httptest.NewRecorder()
	c := ginTestContext(w, "POST",
		fmt.Sprintf("/threat_models/%s/chat/sessions", tmID), nil) // follow file's existing request-builder idiom
	s.CreateTimmyChatSession(c, uuid.MustParse(tmID))

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.NotContains(t, w.Body.String(), "event:", "no SSE framing on a refused request")
	assert.Contains(t, w.Body.String(), "session_limit_exceeded")
}
```

(Adapt request-construction to the file's existing idiom — `TestTimmyCreateSession_Unauthenticated` at :503 shows it. Match `createTestTimmySession`'s real signature at :62.)

Run: `make test-unit name=TestTimmyCreateSession_CapExceededReturns429JSON count1=true` → FAIL (today: 200 + `event: error`, or 503 if wiring incomplete — iterate wiring until the failure is the 200-vs-429 one).

- [ ] **Step 2: Implement**

1. Add the helper in `api/timmy_handlers.go`:

```go
// sendTimmyError reports a handler failure on an SSE endpoint. Before any
// byte is written the HTTP status is still ours to set (#652): strip the
// SSE headers and return a normal JSON error. Once the stream is committed
// (progress/token events), fall back to an SSE error event.
func sendTimmyError(c *gin.Context, sse *SSEWriter, code string, err error) {
	if !c.Writer.Written() {
		c.Writer.Header().Del("X-Accel-Buffering")
		c.Writer.Header().Set("Content-Type", "application/json")
		HandleRequestError(c, err)
		return
	}
	if sendErr := sse.SendError(code, err.Error()); sendErr != nil {
		slogging.Get().WithContext(c).Warn("Timmy: could not deliver %s: %v", code, sendErr)
	}
}
```

(If `HandleRequestError` fed a non-RequestError produces 500 `server_error` — that's the intended fallback for genuinely unexpected pre-stream errors.)

2. In `CreateTimmyChatSession` replace the :93–99 branch with `sendTimmyError(c, sse, "session_creation_failed", createErr); return` (keep the existing `logger.Error`).
3. In `CreateTimmyChatMessage` replace the :360–366 branch likewise with `sendTimmyError(c, sse, "message_failed", handleErr)`.
4. In `timmy_session_manager.go:159-162`, wrap the count error: `return nil, nil, StoreErrorToRequestError(err)` — adjust to the actual return signature — so a transient Oracle/PG blip 503s. Keep the 429 construction (:163–170) exactly as is (defense in depth; `TestTimmySessionManager_CreateSession_RateLimitEnforcement` at timmy_session_manager_test.go:156 keeps passing).

- [ ] **Step 3: Spec check for createTimmyChatMessage**

`jq '.paths["/threat_models/{threat_model_id}/chat/sessions/{session_id}/messages"].post.responses | keys' api-schema/tmi-openapi.json` (verify the exact path key first with `jq '.paths | keys'` filtered on `chat`). If 404/409/429 are all declared → no spec change. For each missing one, add the standard `$ref` (`NotFound` / `Conflict` / `TooManyRequests` components), then:

```bash
make validate-openapi
make generate-api
```

- [ ] **Step 4: Tests + gates**

Run: `make test-unit name=TestTimmyCreateSession count1=true` → new test PASSES, `_NotConfigured` (503) and `_Unauthenticated` (401) still pass.
Run: `make test-unit` (full), `make build-server`, `make lint`.
If spec changed: re-run `make test-unit` after `generate-api`.
Integration: `make test-integration` (TestTimmyCRUD exercises the happy SSE path at timmy_crud_test.go:197/:234 — confirms streams still work).

- [ ] **Step 5: SEM markers**

Behavior changed: `CreateTimmyChatSession`, `CreateTimmyChatMessage` (api/timmy_handlers.go:25/:216), `CreateSession` (timmy_session_manager.go:150), new `sendTimmyError` needs a fresh marker. `/sem-annotate --update api/timmy_handlers.go api/timmy_session_manager.go`.

- [ ] **Step 6: Commit**

```bash
git add api/timmy_handlers.go api/timmy_session_manager.go api/timmy_handlers_test.go
# plus api-schema/tmi-openapi.json api/api.go if the spec changed
git commit -m "fix(api): Timmy SSE endpoints return real status codes for pre-stream refusals

CreateTimmyChatSession and CreateTimmyChatMessage committed HTTP 200 and
delivered refusals (session cap 429, session_not_found 404, rate limits)
as SSE error events even when nothing had been streamed. Route errors
through a shared helper: before the first written byte, strip the SSE
headers and return the typed JSON error (HandleRequestError honors the
RequestError status and sets Retry-After); after the stream is committed,
keep the event:error framing. Transient store errors during the cap count
now 503 instead of 500.

Fixes #652

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 8: #691 — pin webhook delivery assertions to the subtest's own delivery

Root cause (research-corrected): draining the worker would NOT fix this. The three failure-style subtests (`DeliveryFailure_ServerError` :696, `DeliveryRetry_PermanentFailure` :844, `DeliveryFailure_4xx` :894 in `test/integration/workflows/webhook_delivery_test.go`) scan ALL of `sharedSubID`'s deliveries and take the first with `event_type == "threat_model.created" && attempts >= 1`. `ListBySubscription` sorts CreatedAt ASC, so the OLDEST failed record wins — typically ServerError's leftover pending record, whose ~60s retry (backoff [1,5,15,30] min, worker tick 2s) lands while the receiver is back on 200 (each subtest's `defer sharedReceiver.SetStatusCode(200)`) and flips it to `delivered`, poisoning a later subtest's read. There is no worker quiescence hook and no delivery DELETE endpoint, so ID-pinning is the fix.

**Files:**
- Modify: `test/integration/workflows/webhook_delivery_test.go` (three subtests + one small helper)

**Interfaces:**
- Consumes: `getSubscriptionDeliveries(t, client, subID)` (:254), `framework.PollUntil` (framework/webhook_receiver.go:349).
- Produces: helper `snapshotDeliveryIDs(t *testing.T, client *..., subID string) map[string]bool` in webhook_delivery_test.go (match the file's existing client type).

- [ ] **Step 1: Add the helper**

Near `getSubscriptionDeliveries` (:254):

```go
// snapshotDeliveryIDs records the IDs already present on a subscription so a
// subtest can pin its assertions to the delivery IT triggered. The shared
// subscription accumulates records across subtests, ListBySubscription sorts
// oldest-first, and a stale failed record can be retried into 'delivered'
// mid-subtest (#691) — matching on event_type+attempts alone selects it.
func snapshotDeliveryIDs(t *testing.T, client *TestClient, subID string) map[string]bool {
	t.Helper()
	seen := make(map[string]bool)
	for _, d := range getSubscriptionDeliveries(t, client, subID) {
		if id, ok := d["id"].(string); ok {
			seen[id] = true
		}
	}
	return seen
}
```

(`*TestClient` — substitute the file's actual client type from `getSubscriptionDeliveries`'s signature.)

- [ ] **Step 2: Pin the three subtests**

In each of `DeliveryFailure_ServerError` (:705–718), `DeliveryRetry_PermanentFailure` (:852–864), `DeliveryFailure_4xx` (:903–914): capture `preexisting := snapshotDeliveryIDs(t, client, sharedSubID)` BEFORE the event-triggering `createThreatModel` call, then inside the `PollUntil` match loop add, before the event-type check:

```go
			if id, _ := d["id"].(string); preexisting[id] {
				continue
			}
```

Read each subtest fully before editing: any later re-fetch of `deliveryRecord` by ID (e.g. `getDeliveryRecord`) is already safe once the initial selection is pinned. Also scan the remaining shared-receiver subtests (`EventDelivery`, `DeliveryHMAC`, `DeliveryTracking`, `AsyncCallback*`, `DeliveryStatus_PublicEndpoint_Auth`) for the same `attempts >= 1`-style history scan; apply the snapshot pattern anywhere the match isn't already pinned to a fresh ID (success-path scans matching `attempts == 0` records or receiver-side waits are unaffected — successful first attempts keep `attempts == 0`).

- [ ] **Step 3: Run the two flaky subtests in isolation (regression check)**

```bash
TMI_TEST_WORKFLOW_RUN='TestWebhookDelivery/(DeliveryRetry_PermanentFailure|DeliveryFailure_4xx)' make test-integration-pg
```
Expected: PASS (they always passed isolated; this confirms no new breakage — note they still depend on the parent's shared-subscription setup, which the runner executes).

- [ ] **Step 4: Run the full webhook test (the actual race window)**

```bash
TMI_TEST_WORKFLOW_RUN='TestWebhookDelivery' make test-integration-pg
```
Expected: PASS including the previously-flaky pair, with `DeliveryRetry_EventualSuccess` running before them. Run it twice (`count` is not threaded; just invoke twice) — the race was intermittent.

- [ ] **Step 5: Lint and commit**

`make lint`. Integration-test-only change: no build/unit gates beyond compilation, which `make lint`'s typecheck covers; the `_Integration` suffix rule doesn't apply (existing top-level test unchanged).

```bash
git add test/integration/workflows/webhook_delivery_test.go
git commit -m "fix(test): pin webhook delivery assertions to the triggering subtest's record

The failure-style subtests scanned the shared subscription's whole
delivery history for the first record with attempts >= 1; oldest-first
ordering handed back a prior subtest's failed record, which the retry
worker could flip to 'delivered' mid-assertion once the shared receiver
was restored to 200. Snapshot the pre-existing delivery IDs before
triggering the event and skip them when matching.

Fixes #691

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

---

### Task 9: #655 — wiki Testing page: correct the tmi-ux Playwright description

Wiki repo only (`/Users/efitz/Projects/tmi.wiki`, branch master, pushed directly — not part of the PR).

**Files:**
- Modify: `/Users/efitz/Projects/tmi.wiki/Testing.md`

- [ ] **Step 1: Locate and rewrite the tmi-ux e2e section**

Find the passage claiming Chromium/Firefox/WebKit projects and an auto-started dev server. Replace so the page states:
1. Playwright runs **Chrome-only** (`devices['Desktop Chrome']`) across five projects: `workflows`, `field-coverage`, `visual-regression`, `admin`, and the opt-in `google-drive-live`.
2. There is **no `webServer` block**: `e2e/setup/global-setup.ts` fails the run unless both the frontend and the backend API are already reachable — start both services before `pnpm test:e2e`. `E2E_APP_URL` / `E2E_API_URL` point the suite at the running services.
3. Cross-browser coverage is **deliberately out of scope** (per tmi-ux `docs/superpowers/specs/2026-04-10-e2e-comprehensive-test-plan-design.md`), so the Chrome-only setup is intentional, not an omission.

Verify claims against the actual `/Users/efitz/Projects/tmi-ux/playwright.config.ts` before writing (project names may have moved since the issue was filed).

While in the page, if the "Vitest integration tests currently skipped pending conversion" claim about `src/app/pages/dfd/integration/` is present, annotate it with a pointer to ericfitz/tmi-ux#817 rather than rewriting it (that issue is tracked separately).

- [ ] **Step 2: Commit and push (wiki repo)**

```bash
cd /Users/efitz/Projects/tmi.wiki
git add Testing.md
git commit -m "Correct tmi-ux Playwright description: Chrome-only projects, externally-started services (tmi#655)"
git push
```

If push fails on SSH key touch policy, leave committed and surface to orchestrator. Issue #655 is closed manually in Task 10 (wiki commits can't auto-close).

---

### Task 10: Version bump to 1.8.1, PR assembly, closure

Runs LAST, after Tasks 1–9 are committed.

**Files:**
- Modify: `.version` (patch 0 → 1)
- Modify: `api-schema/tmi-openapi.json` (`info.version` "1.8.0" → "1.8.1")
- Regenerate: `api/api.go` (embedded spec)

- [ ] **Step 1: Bump versions**

`.version`: `{"major": 1, "minor": 8, "patch": 1, "prerelease": ""}`.
`jq '.info.version = "1.8.1"' api-schema/tmi-openapi.json` (use jq with a backup per repo convention; file is >100KB).

- [ ] **Step 2: Validate, regenerate, verify**

```bash
make validate-openapi
make generate-api
make build-server   # must print 1.8.1 as the embedded version
make lint
make test-unit
```

- [ ] **Step 3: Commit**

```bash
git add .version api-schema/tmi-openapi.json api/api.go
git commit -m "chore(release): bump version to 1.8.1

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9"
```

- [ ] **Step 4: Security review**

Run the `security-review` skill against the branch. If findings: STOP, report to the user, await direction.

- [ ] **Step 5: Push and open the PR**

```bash
git pull --rebase origin main
git push -u origin dev/1.8.1/backlog-batch
gh pr create --title "1.8.1: backlog batch (#667 #550 #572 #687 #682 #685 #630 #629 #652 #691)" --body "$(cat <<'EOF'
Nine backlog fixes, one commit each (Oracle and container-scan issues paired), plus the 1.8.1 version bump.

- docs(cats): FP rule numbering cleanup + lint enforcement — Fixes #667
- chore(build): controller image drops tmi-client staging — Fixes #550
- chore(terraform): aws-private drift (helm bound, kubernetes_version, aws 6.56.0 lock) — Fixes #572
- fix(auth): one-way EmailVerified guard on token mint — Fixes #687
- fix(db): ORA-00932/01000/04031 sentinels + ADB quota default repair — Fixes #685, Fixes #682
- fix(security): cloud scans use --platform; grype DB provenance recorded — Fixes #630, Fixes #629
- fix(api): Timmy SSE pre-stream refusals return real status codes — Fixes #652
- fix(test): webhook delivery assertions pinned to triggering subtest — Fixes #691

Wiki (pushed directly to tmi.wiki): Testing page Playwright corrections (#655), Oracle MODIFY DEFAULT companion-step note (#682).

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01BQTURdP6A26WuGHUDMrYp9
EOF
)"
```

- [ ] **Step 6: Close wiki-only issue and update the project**

```bash
gh issue comment 655 --body "Resolved in tmi.wiki commit <sha> (Testing page rewritten per acceptance criteria)."
gh issue close 655
```
The other ten issues auto-close when the PR merges to main (`Fixes #N` in the PR body). Do not close them manually.

- [ ] **Step 7: Hand off**

Report to the user: PR URL, per-task outcomes, the #682 row-assessment counts, the oracle-db-admin verdict, security-review result, and any deviations.

---

## Self-Review Notes

- Spec coverage: all 11 issues map to Tasks 1–9; version bump + PR = Task 10. #655's second AC (env vars) and third AC (out-of-scope note) are explicit in Task 9. #682's three ACs: ALTERs (Task 5 Step 4), row assessment (Step 4 item 4), wiki note (Step 5). #667's four ACs: renumber (already correct; stale lines deleted), counts updated (CLAUDE.md; README needs none), decision = keep comments + automated check, check added.
- Deliberate scope choices: #629's freshness policy = grype's default 120h gate + `valid:false` hard-fail + provenance recording (tighter policy deferred — provenance makes it decidable later). #652 includes the messages endpoint (same defect, same helper). #682 performs no data UPDATE (issue marks it optional; counts reported instead).
- Type-consistency: helper names used exactly once each (`sendTimmyError`, `snapshotDeliveryIDs`, `ensure_grype_db_current`, `check-cats-fp-numbering.py`); no cross-task type references.
