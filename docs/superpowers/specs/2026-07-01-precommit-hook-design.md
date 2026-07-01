# Design: Pre-commit hook for lightweight static analysis

**Date:** 2026-07-01
**Status:** Approved (brainstorming), pending implementation plan
**Scope:** 1 of 3 — independent of the CI-scanners and k3s-dev-target specs.

## Summary

Add a tracked, installable Git **pre-commit** hook that runs `gofmt`, `go vet`, and
`golangci-lint run --fast-only` on staged Go files and **blocks** the commit on failure.

## Problem

There is no tracked hooks directory and no installer. `core.hooksPath` is at its default
(`.git/hooks`), and the existing version-bumping `post-commit` hook lives **untracked**
in `.git/hooks/post-commit`. Any shared pre-commit hook therefore needs both a tracked
home and an install mechanism.

## Design

### Layout and install

- Create a **tracked** `scripts/hooks/` directory containing:
  - `pre-commit` (new).
  - `post-commit` (relocated from `.git/hooks/post-commit`, byte-for-byte behavior
    preserved).
- Add a `make install-hooks` target that runs `git config core.hooksPath scripts/hooks`.
  A single switch points Git at the tracked directory and versions both hooks.

**Why the relocation is mandatory:** setting `core.hooksPath` makes Git ignore
`.git/hooks/*` entirely. If the versioning `post-commit` were left there it would
silently stop running. Moving it into `scripts/hooks/` keeps it active and brings it
under version control.

### `pre-commit` behavior

Operates on **staged Go files only**:

```
git diff --cached --diff-filter=ACM --name-only -- '*.go'
```

1. **gofmt** — `gofmt -l` on the staged files; fail if any are unformatted (report the
   offending files and the `gofmt -w` fix).
2. **go vet** — scoped to the unique package directories of the staged files (not the
   whole module, for speed).
3. **golangci-lint** — `golangci-lint run --fast-only` on those same package
   directories. This automatically honors the existing `.golangci.yml` (including the
   generated-code exclusions for `api/api.go`).

### Control flow / edge cases

- No staged Go files → exit 0 immediately (hook is a no-op for docs/config commits).
- `git commit --no-verify` bypasses the hook (standard Git behavior; documented).
- The versioning `post-commit` amends with `git commit --amend --no-verify`, which does
  not re-trigger `pre-commit` — no loop.
- If `golangci-lint` is not installed, fail with a clear install hint. It is a
  deliberate gate; silently skipping would defeat the purpose. (`gofmt` and `go vet`
  ship with the Go toolchain and are always present.)

## Verification

- `make install-hooks`, then confirm `git config core.hooksPath` == `scripts/hooks`.
- Stage an unformatted file → commit blocked; `gofmt -w` → commit succeeds.
- Stage a `go vet` / lint violation → blocked; fix → succeeds.
- `--no-verify` bypasses.
- A commit that changes production Go still triggers the relocated `post-commit`
  version bump on `main`.

## Out of scope

- Running the full (non-`--fast-only`) linter or tests in the hook — CI covers that.
- Any change to CI workflows (separate spec).
