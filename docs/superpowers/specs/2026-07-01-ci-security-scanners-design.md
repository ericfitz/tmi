# Design: CI security scanners (govulncheck + standalone gosec)

**Date:** 2026-07-01
**Status:** Approved (brainstorming), pending implementation plan
**Scope:** 2 of 3 — independent of the pre-commit-hook and k3s-dev-target specs.

## Summary

Add two jobs to CI: `govulncheck` (**blocking**) and a standalone `gosec`
(**informational**, results uploaded to the GitHub Security tab), alongside the existing
CodeQL workflow and the `gosec` already embedded in `golangci-lint`.

## Context

`gosec` **already runs today** as a blocking linter inside `golangci-lint` (enabled in
`.golangci.yml`, with a set of exclusions). The additions here are intentionally
**non-redundant**:

- `govulncheck` finds *known, published* vulnerabilities (CVEs) in dependencies and the
  standard library that actually reach the code — different from `gosec`'s heuristics.
- The standalone `gosec` runs with broader scope than the linter-embedded one and
  publishes results to the **GitHub Security tab** for visibility, without blocking.

CodeQL (`.github/workflows/codeql.yml`) is left unchanged.

## Design

New jobs added to `.github/workflows/security.yml`.

### `govulncheck` job (blocking)

- Standard checkout + `setup-go` + the tmi-clients replace-shim steps used by the other
  jobs in this workflow (checkout `ericfitz/tmi-clients` into `.tmi-clients`, `sed` the
  `go.mod` replace directive).
- Install `golang.org/x/vuln/cmd/govulncheck` at a **pinned** version (repo convention:
  tool versions are pinned — cf. `vacuum` at `0.29.7`, `golangci-lint` at `v2.12.2`).
- Run `govulncheck ./...`; a non-zero exit **fails the job** (blocks merge).
- Triggers: `pull_request` + `push` to `main`.

### `gosec` job (informational)

- `permissions: security-events: write`.
- Run `gosec -no-fail -fmt sarif -out gosec.sarif -exclude-dir=<generated> ./...` at a
  **pinned** gosec version. `-no-fail` guarantees the job never blocks the build.
- Upload with `github/codeql-action/upload-sarif` so findings appear under
  **Security → Code scanning**. Generated `api/api.go` is excluded to match the linter's
  noise policy.

## Verification

- On a PR branch: the `govulncheck` job appears and is required; the `gosec` job runs and
  its SARIF shows up under **Security → Code scanning**.
- Introduce a temporary known-vuln dependency → `govulncheck` fails the build; remove it
  → passes.
- Confirm a `gosec`-style finding does **not** block the build.

## Out of scope

- Changing CodeQL configuration.
- Removing or reconfiguring the `gosec` linter already inside `golangci-lint`.
- Making the standalone `gosec` blocking (can be tightened later once its output is
  triaged).
