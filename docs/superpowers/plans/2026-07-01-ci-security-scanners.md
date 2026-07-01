# CI Security Scanners Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a blocking `govulncheck` job and an informational standalone `gosec` job (results uploaded to the GitHub Security tab) to CI, without changing CodeQL or the `gosec` already embedded in `golangci-lint`.

**Architecture:** Two new jobs appended to `.github/workflows/security.yml`. Both reuse the existing checkout + `setup-go` + tmi-clients replace-shim pattern already used by the `lint`/`test` jobs. `govulncheck` fails the build on findings; `gosec` runs with `-no-fail` and uploads SARIF via `github/codeql-action/upload-sarif`.

**Tech Stack:** GitHub Actions, `golang.org/x/vuln/cmd/govulncheck`, `github.com/securego/gosec/v2`, `github/codeql-action/upload-sarif`.

## Global Constraints

- Tool versions are **pinned** by repo convention (cf. `vacuum` `0.29.7`, `golangci-lint` `v2.12.2`). Do not use `@latest` in the committed workflow.
- Every Go-running job must apply the tmi-clients replace-shim, or `go` commands fail: checkout `ericfitz/tmi-clients` into `.tmi-clients`, then `sed -i 's|=> ../tmi-clients/|=> ./.tmi-clients/|' go.mod`.
- `setup-go` uses `go-version-file: go.mod` with `cache: true` (match existing jobs).
- Generated `api/api.go` carries a `Code generated ... DO NOT EDIT` header — exclude it from `gosec` via `-exclude-generated`.
- The `gosec` job must never block the build (`-no-fail` + non-fatal upload).
- Triggers match the workflow: `pull_request` and `push` to `main` (already set at the `on:` level).

---

### Task 1: Confirm the repo is currently govulncheck-clean (pre-flight)

**Files:** none (local check only).

**Interfaces:**
- Consumes: nothing.
- Produces: confirmation that making `govulncheck` a required gate won't immediately red-wall CI. If findings exist, they must be resolved (dependency bumps) BEFORE Task 2 — that is a separate dep-bump effort, not part of this plan.

- [ ] **Step 1: Run govulncheck locally against the whole module**

Run: `govulncheck ./...`
Expected: `No vulnerabilities found.` (or a summary with zero *called* vulnerabilities).

- [ ] **Step 2: If findings exist, stop and record them**

If `govulncheck` reports called vulnerabilities, do NOT proceed to make the job blocking. Record the module/CVE and hand off to a dependency bump (`deps:bump` skill / Dependabot). Re-run Step 1 until clean. Only continue to Task 2 once the module is clean.

---

### Task 2: Add the blocking `govulncheck` job

**Files:**
- Modify: `.github/workflows/security.yml` (add a `govulncheck` job under `jobs:`)

**Interfaces:**
- Consumes: the module being govulncheck-clean (Task 1).
- Produces: a required CI job named `govulncheck` that fails on findings.

- [ ] **Step 1: Add the govulncheck job**

Append this job to the `jobs:` map in `.github/workflows/security.yml` (sibling of `lint`, `build`, `test`):

```yaml
  govulncheck:
    name: Vulnerability Scan (govulncheck)
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7

      - name: Setup Go
        uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true

      - name: Provision generated client (tmi-clients)
        uses: actions/checkout@v7
        with:
          repository: ericfitz/tmi-clients
          path: .tmi-clients
      - name: Point go.mod replace at the checked-out client
        run: sed -i 's|=> ../tmi-clients/|=> ./.tmi-clients/|' go.mod

      - name: Install govulncheck
        # Pinned per repo convention. Verify this is the current release and
        # bump if a newer stable tag exists.
        run: go install golang.org/x/vuln/cmd/govulncheck@v1.1.4

      - name: Run govulncheck
        run: govulncheck ./...
```

- [ ] **Step 2: Validate the workflow YAML syntax**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/security.yml')); print('YAML OK')"`
Expected: `YAML OK`

- [ ] **Step 3: (If actionlint is available) lint the workflow**

Run: `command -v actionlint >/dev/null && actionlint .github/workflows/security.yml || echo "actionlint not installed, skipping"`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/security.yml
git commit -m "ci(security): add blocking govulncheck job"
```

---

### Task 3: Add the informational `gosec` job (SARIF -> Security tab)

**Files:**
- Modify: `.github/workflows/security.yml` (add a `gosec` job under `jobs:`)

**Interfaces:**
- Consumes: nothing from prior tasks.
- Produces: a non-blocking CI job named `gosec` whose SARIF appears under Security -> Code scanning.

- [ ] **Step 1: Add the gosec job**

Append this job to the `jobs:` map in `.github/workflows/security.yml`:

```yaml
  gosec:
    name: Static Security Scan (gosec, informational)
    runs-on: ubuntu-latest
    permissions:
      contents: read
      security-events: write   # required to upload SARIF to the Security tab
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7

      - name: Setup Go
        uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true

      - name: Provision generated client (tmi-clients)
        uses: actions/checkout@v7
        with:
          repository: ericfitz/tmi-clients
          path: .tmi-clients
      - name: Point go.mod replace at the checked-out client
        run: sed -i 's|=> ../tmi-clients/|=> ./.tmi-clients/|' go.mod

      - name: Install gosec
        # Pinned per repo convention. Verify current release and bump if needed.
        run: go install github.com/securego/gosec/v2/cmd/gosec@v2.22.5

      - name: Run gosec (never fails the build)
        # -no-fail: exit 0 even on findings (informational job).
        # -exclude-generated: skip oapi-codegen's api/api.go and other generated code.
        # -exclude-dir: skip the checked-out client shim.
        run: gosec -no-fail -exclude-generated -exclude-dir=.tmi-clients -fmt sarif -out gosec.sarif ./...

      - name: Upload SARIF to the Security tab
        if: always()
        uses: github/codeql-action/upload-sarif@v4
        with:
          sarif_file: gosec.sarif
          category: gosec
```

- [ ] **Step 2: Validate the workflow YAML syntax**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/security.yml')); print('YAML OK')"`
Expected: `YAML OK`

- [ ] **Step 3: (Optional) smoke-test gosec locally**

Run: `go install github.com/securego/gosec/v2/cmd/gosec@v2.22.5 && gosec -no-fail -exclude-generated -exclude-dir=.tmi-clients -fmt sarif -out /tmp/gosec.sarif ./... && python3 -c "import json; d=json.load(open('/tmp/gosec.sarif')); print('runs:', len(d['runs']))"`
Expected: gosec completes with exit 0 and a valid SARIF file (`runs: 1`). Findings are fine — this job is informational.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/security.yml
git commit -m "ci(security): add informational gosec job with SARIF upload"
```

---

### Task 4: Verify on a branch and mark govulncheck required

**Files:** none (CI + GitHub settings).

**Interfaces:**
- Consumes: Tasks 2 and 3.
- Produces: confirmed CI behavior; `govulncheck` added to required checks.

- [ ] **Step 1: Push the branch and open a PR**

Push the working branch and open a PR against `main`. Both new jobs run.

- [ ] **Step 2: Confirm job behavior**

Expected:
- `Vulnerability Scan (govulncheck)` runs and PASSES (module is clean per Task 1).
- `Static Security Scan (gosec, informational)` runs, completes green regardless of findings, and its results appear under **Security -> Code scanning** (category `gosec`).

- [ ] **Step 3: Add govulncheck to required status checks**

In the repo's branch-protection rules for `main`, add `Vulnerability Scan (govulncheck)` to the required checks so it blocks merge. (This is a GitHub Settings action; do NOT add `gosec` — it is informational by design.)

---

## Notes / follow-ups (not tasks)

- The `gosec` linter already inside `golangci-lint` stays as-is (blocking, with `.golangci.yml` exclusions). The standalone job is broader-scope + Security-tab visibility; this duplication is intentional.
- If the informational `gosec` output proves high-signal after triage, a future change can tighten it to blocking (drop `-no-fail`).
