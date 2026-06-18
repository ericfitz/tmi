# Design: Automate the `deps:bump` skill on GitHub Actions

**Date:** 2026-06-18
**Repo (first target):** `ericfitz/tmi` (pattern generalizes to `tmi-ux` later)
**Status:** Awaiting user review → implementation plan

## Goal

Run the existing Claude Code `deps:bump` skill automatically, so dependency
updates land as reviewable PRs without a human kicking off each run.

### Requirements (from the user)

1. Run against **main (default), a specific branch, or all branches
   independently** — with **no cherry-pick and no merge** between branches.
   Each branch's bump operates only on that branch.
2. Run **automatically on a schedule** on GitHub, **and** be **triggered by
   Dependabot alerts** (approximated — see Constraints).

### Decisions (locked with the user)

| Topic | Decision |
|-------|----------|
| Output model | **Open a PR per branch** (commit to a working branch, PR targets the source branch). |
| "All branches" scope | **`main` + every `dev/*` release branch** (discovered dynamically). |
| Claude auth | **`CLAUDE_CODE_OAUTH_TOKEN`** (user's subscription via `claude setup-token`); no API billing. |
| Schedule | **Weekly full run** (Mon, after Dependabot's Monday run) + **daily alert poll**. |
| Alert trigger | **Daily poll** that runs the bump only when open Dependabot alerts exist. |

## What `deps:bump` does (and doesn't)

The skill (`efitz-skills` marketplace, `deps/skills/bump/SKILL.md`):
- Auto-detects ecosystems (Go / Python / Node).
- Loads exclusions from `## Bump Exclusions` in `CLAUDE.md`, `.bump-config.json`,
  and `// pinned:` / `# pinned:` comments.
- Applies **safe patch/minor** updates only (majors → manual-review plan).
- Runs build + test + lint; **bisects** to isolate a bad package on failure.
- **Commits** the safe subset and prints a prioritized manual-review plan.

It **does not push** and **does not open PRs** — automation supplies that step.
Its Phase 1 is **interactive** (asks about switching branches) — the CI prompt
pins it to the current branch and forbids prompting.

tmi validation commands (per tmi `CLAUDE.md`): `make build-server`,
`make test-unit` (fast, **no external deps** — CI-friendly), `make lint`
(`uv run scripts/lint.py` + golangci-lint + staticcheck checks).

## Architecture

Single workflow: **`.github/workflows/deps-bump.yml`**.

### Triggers (`on:`)
- `schedule: '0 16 * * 1'` — weekly full run (Mon 16:00 UTC, after the Monday
  Dependabot run).
- `schedule: '0 13 * * *'` — daily alert poll.
- `workflow_dispatch` — inputs: `branch` (specific name or `all`),
  `ecosystem` (optional: go/node/python).

Branch logic distinguishes the run type via `github.event.schedule` /
`github.event_name`.

### Job 1 — `discover` (matrix builder)
Outputs a JSON array of target branches:
- Weekly / dispatch `all` → `main` + `dev/*` (via `gh api repos/$REPO/branches`).
- Dispatch specific branch → just that branch.
- Daily poll → query open Dependabot alerts; map to affected branches; if none,
  output `[]` so the bump job is skipped.

### Job 2 — `bump` (matrix over `discover` output)
`strategy.fail-fast: false`; `concurrency` keyed per branch (no colliding runs).
Per branch:
1. `actions/checkout` the target branch (full history).
2. Set up toolchain: Go, uv, Node/pnpm; install `govulncheck`,
   `golangci-lint`, `staticcheck`, `gh`.
3. Make the `deps:bump` skill available to a headless Claude Code run
   (see Risk 1 for the mechanism).
4. `git checkout -b deps/auto-bump/<branch>/<run_id>`.
5. Run Claude headless with a pinned prompt: *"Run the deps bump skill on the
   current branch. Never switch branches, never prompt. Commit the safe
   updates and output the manual-review plan."*
6. If a commit was produced (working branch differs from base): push the
   working branch and `gh pr create --base <branch>` with the skill's
   manual-review **plan in the PR body**. If no changes: skip.

Existing PR checks (`security-deps-gate.yml`, `security.yml`, `codeql.yml`)
run on the PR before merge — this is the review gate.

### Auth & permissions
- Repo secret `CLAUDE_CODE_OAUTH_TOKEN` (from `claude setup-token`).
- Workflow `permissions: { contents: write, pull-requests: write }`.
- Dependabot-alert read may require a fine-grained **PAT** secret rather than
  the default `GITHUB_TOKEN` (see Risk 2).

## Constraints & Risks (resolve in implementation plan)

1. **Skill-in-CI (main risk).** The skill ships in the `efitz-skills`
   marketplace plugin; it must be available to a headless runner. Candidate
   approaches: (a) install the marketplace/plugin in CI, or (b) vendor the
   `bump` SKILL.md into the runner workspace and point Claude at it. Pick and
   validate one in the plan.
2. **Dependabot alerts API auth.** Reading `repos/$REPO/dependabot/alerts` may
   need a token with Dependabot-alerts: read, which the default `GITHUB_TOKEN`
   may not grant. Plan: confirm; add a fine-grained PAT secret only if needed.
3. **`dependabot_alert` is not a native Actions trigger** (webhook event only;
   open feature request). Hence the daily poll. Acknowledged tradeoff:
   up to ~24h latency from alert to bump run.
4. **PRs opened with `GITHUB_TOKEN` don't trigger downstream workflows** by
   default. Since merging is manual and the security gates run on `push`/PR via
   their own triggers, confirm the gates fire; if not, use a PAT for `pr create`.
5. **CI cost/usage.** Headless Claude runs consume subscription usage; weekly +
   daily-when-alerts keeps volume modest. `fail-fast: false` isolates per-branch
   failures.

## Out of scope (this iteration)
- Auto-merging PRs.
- Major-version upgrades (skill defers these to the manual plan by design).
- tmi-ux and other repos (replicate the pattern after tmi is proven).
- Cross-branch propagation (explicitly excluded: no cherry-pick / merge).

## Success criteria
- Manual `workflow_dispatch` on a chosen branch opens a PR (or cleanly reports
  "no safe updates") with passing toolchain setup.
- Weekly schedule fans out across `main` + `dev/*`, one independent PR per
  branch that has safe updates.
- Daily poll is a no-op when there are no open alerts and opens PRs when there
  are.
