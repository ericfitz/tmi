# Pre-commit Hook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tracked, installable Git pre-commit hook that runs `gofmt`, `go vet`, and `golangci-lint run --fast-only` on staged Go files and blocks the commit on failure.

**Architecture:** Move Git to a tracked hooks directory (`scripts/hooks/`) via `core.hooksPath`, relocate the existing untracked `post-commit` version-bump hook there so it keeps running, add a new `pre-commit` hook script, and provide `make install-hooks` to wire it up.

**Tech Stack:** Bash, Git hooks, Go toolchain (`gofmt`, `go vet`), `golangci-lint` v2.

## Global Constraints

- Never use the standard `log` package / print logging in Go — N/A here (shell only).
- Use Make targets by convention; add the hook installer as a Make target.
- `golangci-lint` is v2.x; the fast-subset flag is `--fast-only` (NOT `--fast`).
- The existing `post-commit` hook (version bump on `main`) must keep working byte-for-byte.
- `gofmt` and `go vet` ship with the Go toolchain; `golangci-lint` is a separate install.
- Bypass mechanism is the standard `git commit --no-verify`.

---

### Task 1: Create the tracked hooks directory (pre-commit + relocated post-commit)

**Files:**
- Create: `scripts/hooks/pre-commit`
- Create: `scripts/hooks/post-commit` (copied verbatim from `.git/hooks/post-commit`)

**Interfaces:**
- Consumes: nothing.
- Produces: two executable hook scripts under `scripts/hooks/`. Task 2 installs them via `core.hooksPath`.

- [ ] **Step 1: Copy the existing post-commit hook into the tracked directory**

The current version-bump hook lives untracked at `.git/hooks/post-commit`. Copy it verbatim so it survives the switch to `core.hooksPath` (which makes Git ignore `.git/hooks/*`).

```bash
mkdir -p scripts/hooks
cp .git/hooks/post-commit scripts/hooks/post-commit
chmod +x scripts/hooks/post-commit
```

- [ ] **Step 2: Verify the copy is byte-for-byte identical**

Run: `diff .git/hooks/post-commit scripts/hooks/post-commit && echo IDENTICAL`
Expected: `IDENTICAL` (no diff output).

- [ ] **Step 3: Write the pre-commit hook**

Create `scripts/hooks/pre-commit`:

```bash
#!/bin/bash
# pre-commit hook - lightweight static analysis on staged Go files.
#
# Runs gofmt, go vet, and golangci-lint --fast-only on the packages of the
# staged Go files, and blocks the commit on any failure.
#
# Bypass with: git commit --no-verify
set -uo pipefail

# Staged Go files (Added/Copied/Modified), excluding deletions. Git runs hooks
# with the working directory at the repository root, so paths are repo-relative.
staged_go=$(git diff --cached --diff-filter=ACM --name-only -- '*.go')

if [ -z "$staged_go" ]; then
    # Nothing Go-related staged (docs/config commit) -> no-op.
    exit 0
fi

fail=0

# 1. gofmt: list any staged file that is not gofmt-clean.
unformatted=$(gofmt -l $staged_go)
if [ -n "$unformatted" ]; then
    echo "pre-commit: the following staged files are not gofmt-formatted:" >&2
    echo "$unformatted" | sed 's/^/  /' >&2
    echo "  fix with: gofmt -w <files>" >&2
    fail=1
fi

# Unique package directories of the staged files, as ./dir patterns.
pkg_dirs=$(echo "$staged_go" | xargs -n1 dirname | sort -u | sed 's,^,./,')

# 2. go vet on just the affected packages (fast; whole-module vet is slow).
if ! go vet $pkg_dirs; then
    echo "pre-commit: go vet reported problems (see above)." >&2
    fail=1
fi

# 3. golangci-lint --fast-only on the affected packages (honors .golangci.yml).
if ! command -v golangci-lint >/dev/null 2>&1; then
    echo "pre-commit: golangci-lint is not installed." >&2
    echo "  install: https://golangci-lint.run/welcome/install/ (or: brew install golangci-lint)" >&2
    fail=1
elif ! golangci-lint run --fast-only $pkg_dirs; then
    echo "pre-commit: golangci-lint reported problems (see above)." >&2
    fail=1
fi

if [ "$fail" -ne 0 ]; then
    echo "pre-commit: checks failed; commit aborted (bypass with 'git commit --no-verify')." >&2
    exit 1
fi

exit 0
```

- [ ] **Step 4: Make the pre-commit hook executable**

Run: `chmod +x scripts/hooks/pre-commit`
Expected: no output; `test -x scripts/hooks/pre-commit && echo OK` prints `OK`.

- [ ] **Step 5: Lint the shell scripts (if shellcheck is available)**

Run: `command -v shellcheck >/dev/null && shellcheck scripts/hooks/pre-commit scripts/hooks/post-commit || echo "shellcheck not installed, skipping"`
Expected: no errors (warnings about intentional word-splitting of `$pkg_dirs`/`$staged_go` are acceptable — that splitting is deliberate).

- [ ] **Step 6: Commit**

```bash
git add scripts/hooks/pre-commit scripts/hooks/post-commit
git commit -m "build(hooks): add tracked pre-commit hook and relocate post-commit"
```

---

### Task 2: Add `make install-hooks` and verify end-to-end

**Files:**
- Modify: `Makefile` (add `install-hooks` target + `.PHONY` entry)

**Interfaces:**
- Consumes: `scripts/hooks/pre-commit`, `scripts/hooks/post-commit` from Task 1.
- Produces: `make install-hooks`, which sets `git config core.hooksPath scripts/hooks`.

- [ ] **Step 1: Add the install-hooks target to the Makefile**

Add near the other infrastructure/atomic-component targets (the section that begins with `start-database:`). Match the existing `##`-help-comment style:

```makefile
.PHONY: install-hooks

install-hooks:  ## Install Git hooks (points core.hooksPath at scripts/hooks)
	@git config core.hooksPath scripts/hooks
	@chmod +x scripts/hooks/pre-commit scripts/hooks/post-commit
	@echo "Git hooks installed (core.hooksPath -> scripts/hooks)"
```

- [ ] **Step 2: Install the hooks**

Run: `make install-hooks`
Expected: prints `Git hooks installed (core.hooksPath -> scripts/hooks)`.

- [ ] **Step 3: Verify core.hooksPath is set**

Run: `git config core.hooksPath`
Expected: `scripts/hooks`

- [ ] **Step 4: Verify the no-op path (no staged Go files)**

```bash
echo "note" >> README.md
git add README.md
git commit -m "chore: no-op hook check" --dry-run 2>/dev/null; \
  git commit -m "test: docs-only commit runs no Go checks"
```
Expected: commit succeeds immediately with no gofmt/vet/lint output. Then undo: `git reset --soft HEAD~1 && git restore --staged README.md && git checkout -- README.md`.

- [ ] **Step 5: Verify the gofmt block**

Create a deliberately misformatted Go file and try to commit it:

```bash
printf 'package tmp\nfunc  Bad( ){\n}\n' > internal/hooktmp.go
git add internal/hooktmp.go
git commit -m "should be blocked"
```
Expected: commit is ABORTED; output lists `internal/hooktmp.go` as not gofmt-formatted and prints the `gofmt -w` hint and the abort message.

- [ ] **Step 6: Verify the bypass and then clean up**

```bash
git commit -m "test: bypass" --no-verify   # should succeed despite bad formatting
git reset --hard HEAD~1                     # drop the bypass commit
rm -f internal/hooktmp.go
git restore --staged internal/hooktmp.go 2>/dev/null || true
```
Expected: the `--no-verify` commit succeeds (proves bypass works), then the reset/removal leaves the tree clean.

- [ ] **Step 7: Verify the relocated post-commit still bumps the version on main**

Confirm the relocated hook is active. On `main`, a commit touching production Go triggers the version bump (`scripts/update-version.sh`) and amends the commit. Do a safe check that the hook file is the one Git will run:

Run: `git rev-parse --git-path hooks/post-commit; echo "active hooksPath: $(git config core.hooksPath)"`
Expected: `core.hooksPath` is `scripts/hooks`, so `scripts/hooks/post-commit` is the active hook. (Full behavior is exercised by any later real production-Go commit on `main`; no separate destructive test needed.)

- [ ] **Step 8: Commit**

```bash
git add Makefile
git commit -m "build(hooks): add make install-hooks target"
```

---

## Notes / follow-ups (not tasks)

- Onboarding documentation for `make install-hooks` belongs in the GitHub Wiki (per project docs policy), not `docs/`. File a wiki update separately.
- `core.hooksPath` is per-clone local config; every developer runs `make install-hooks` once. Consider mentioning it in the repo's setup script if one exists.
