# ADR: Bump versions after merge, with the portable version-bump-bot

- Status: Accepted
- Date: 2026-10-03
- Decision maker: Eric Fitzgerald (human decisions, 2026-10-02 and 2026-10-03)
- Supersedes: the in-PR bump mechanism of #627 and of `2026-09-28-adr-versioning-docs-skip-and-schema-decoupling.md` (its docs-skip and schema-decoupling rules stay). Replaces held PR #1024, which prototyped a tmi-only post-merge bump.

## Context

Under #627, a "Version Bump" job pushed a `chore(version)` commit onto every PR branch, and a required "Version Check" verified it. That had two recurring costs:

- **Failure alerts on every PR.** Version Check ran in parallel with Version Bump on the pre-bump commit, so its first run always failed before the bump commit landed and a rerun passed.
- **Conflicts on every concurrent PR.** Each PR edited the same lines of `.version` and `api/version.go`, plus the spec `info.version` and `api/api.go` when the schema changed. After any merge, every other open PR conflicted.

## Human-made decisions (Eric)

1. **2026-10-02: bump after merge on main.** PRs never touch version files. A push-to-main job folds every unbumped merged commit into one bump commit, still incrementing once per PR in merge order. A dedicated GitHub App is the only bypass actor on the main ruleset. (The GitHub Actions app, integration 15368, cannot be a bypass actor on a user-owned repo: the API returns 422.)
2. **2026-10-03: make it a portable bot in its own repo**, `ericfitz/version-bump-bot`, installable on any of Eric's repos, instead of tmi-only workflow code. A separate `ericfitz/deps-bump-bot` handles dependency bumps. tmi adopts version-bump-bot `v1` and closes #1024. Design: that repo's `docs/superpowers/specs/2026-10-03-version-bump-bot-design.md`.
3. The bump commit and its tags are pushed in one `git push --atomic`. tmi gets a `v<server version>` tag per bump, which it never had before.

## What tmi runs

- `.github/version-bump.toml` defines two streams: **server** (`.version` → `api/version.go`; `feat` → minor, other types → patch, `type!` → minor; tag `v{version}`) and **schema** (`api-schema/tmi-openapi.json` `info.version`; bumps only for commits that changed the spec with `info.version` excluded; `!` → major; `after = make generate-api`; `verify = scripts/check-embedded-spec.sh`). Commits touching only `docs/**`, `PROGRESS.md`, or `*.md` bump nothing.
- `.github/workflows/version.yml` calls `guard.yml@v1` on PRs (the required check **guard / Version Guard**, replacing "Version Check") and `bump.yml@v1` on push to main, with Go and oapi-codegen v2.7.1 for the hook.
- Repo settings (Eric applies them; agents are blocked): squash merges only with `PR_TITLE` commit titles, the `ericfitz-version-bump` App as the only bypass actor on ruleset 17850702, and `VERSION_BUMP_APP_ID` / `VERSION_BUMP_APP_PRIVATE_KEY` as repository Actions secrets.
- Removed: `.github/workflows/version-bump.yml`, `scripts/ci-version-bump.sh`, and `scripts/update-version.sh`. The guard rejects hand bumps, so the manual bumper had no remaining use. `scripts/hooks/post-commit` stays as an explanatory no-op.

## Consequences

- No version conflicts between PRs, and no transient version-check failures.
- Each PR lands as two commits on main, the squash merge then the bump, usually a minute or two apart. A build of the squash commit alone reports the previous version.
- A burst of merges may fold into one bump commit; every PR still counts.
- Pushes made by the App can trigger workflows. Two guards prevent a cascade: `bump.yml` skips a push by the App's bot user whose head subject starts with `chore(version)`, and the bump commit changes the version source, so a rerun folds nothing.
- If the App's bypass or secrets go missing, the push-to-main job fails loudly. `workflow_dispatch` re-runs the fold once that is fixed, and no bump is lost.
- PRs opened before the cutover lack `version.yml`, so the guard never reports for them until they are updated against main.
