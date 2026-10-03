# ADR: Bump versions after merge on main, not inside each PR

- Status: Accepted
- Date: 2026-10-02
- Decision maker: Eric Fitzgerald (human decision)
- Supersedes: the in-PR bump mechanism of #627 and `2026-09-28-adr-versioning-docs-skip-and-schema-decoupling.md` (its docs-skip and schema-decoupling rules stay)

## Context

Under #627, a "Version Bump" job pushed a `chore(version)` commit onto every PR branch, and a required "Version Check" verified it. That had two recurring costs:

- **Failure alerts on every PR.** Version Check ran in parallel with Version Bump on the pre-bump commit, so its first run always failed before the bump commit landed and triggered a passing rerun.
- **Conflicts on every concurrent PR.** Each PR edited the same lines of `.version` and `api/version.go`, and also the spec `info.version` and `api/api.go` when the schema changed. After any merge, every other open PR conflicted and needed a manual or scripted merge.

Eric asked for both to go away, with versions still incremented once per PR, by a mechanism that does not depend on anyone remembering to do it.

## Options considered

- **A: bump after merge on main.** PRs never touch version files. A push-to-main workflow folds every unbumped merged commit and pushes one bump commit.
- **B: keep the in-PR bump, and make Version Check wait for Version Bump and the merge tooling auto-resolve conflicts.** This silences the alerts but keeps the conflicts and depends on tooling.

## Decision

**A.** `.github/workflows/version-bump.yml`:

- **Version Check** (required, every PR): fails only if the PR edits `.version`, `api/version.go`, or the spec `info.version` (`ci-version-bump.sh check-pr-untouched`), or if `api/api.go`'s embedded spec disagrees with the spec file.
- **Version Bump** (push to main, serialized): `ci-version-bump.sh plan-pending` takes the first-parent commits after the last commit that touched `.version`. Oldest first, it skips docs-only commits and bumps the server version per each commit subject (the squash subject is the PR title). It bumps the schema version only for commits that changed the spec. It then pushes one `chore(version)` commit with `GITHUB_TOKEN`.
- The GitHub Actions app (integration 15368) is the main ruleset's only bypass actor, so the job can push.

**No cascade.** Three guards, any one of them sufficient: pushes made with `GITHUB_TOKEN` start no workflows; the job skips `chore(version)` head commits; and the bump commit touches `.version`, so a rerun computes `pending=0`.

## Consequences

- No version conflicts between PRs, and no transient Version Check failures.
- Each PR lands as two commits on main: the squash merge, then the bump, usually within a minute or two. A build of the squash commit alone reports the previous version.
- A burst of merges may fold into one bump commit. Each PR still increments the version, in merge order.
- CodeQL and security workflows do not run on the bump commit itself (a `GITHUB_TOKEN` push); it changes only version files and, for schema bumps, regenerated `api/api.go`.
- If the bypass is ever removed, the push fails loudly. `workflow_dispatch` re-runs the fold after it is fixed, and no bump is lost, because the fold covers every pending commit.
- Rebase-merges bump once per landed commit. Squash merge remains the convention.
