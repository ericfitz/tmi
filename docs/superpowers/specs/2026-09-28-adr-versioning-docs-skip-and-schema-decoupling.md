# ADR: Skip version bump for docs-only PRs; decouple the OpenAPI schema version

Date: 2026-09-28. Status: accepted; the bump MECHANISM (in-PR bump commit) is superseded by `2026-10-02-adr-post-merge-version-bump.md`. The docs-skip and schema-decoupling rules below still apply, now evaluated per merged commit.

## Human-made architectural decisions (Eric, 2026-09-28)

1. **Skip the version bump for docs-only PRs.** If every file changed by a PR (vs the
   merge-base with `main`) matches `docs/**`, `PROGRESS.md`, or `*.md` in any directory,
   "Version Bump" makes no commit and "Version Check" passes without requiring a bump — the
   branch's `.version` must simply still equal `main`'s.
2. **Decouple the OpenAPI schema version from the server version.** The server version
   (`.version` + `api/version.go`) keeps bumping per the existing PR-title rule for non-docs
   PRs. The schema's `info.version` bumps only when the PR actually changes the schema
   (`api-schema/tmi-openapi.json` diffed with `.info.version` removed from both sides). If it
   changed: bump the schema's own version with the same title rule, plus a `!` breaking marker
   (`^[a-z]+(\(.+\))?!:`) now bumps MAJOR (reset MINOR/PATCH) — a rule the server version does
   not have. If it didn't change: `info.version` and the embedded spec in `api/api.go` are left
   untouched. The schema version continues from whatever value it currently holds (equal to
   `.version` as of this decision) and diverges from there.
3. **Version Check enforces:**
   - non-docs-only PRs: `.version` / `api/version.go` equal the computed next server version;
     docs-only PRs: equal `main`'s current value.
   - the spec's `info.version` always equals the version embedded in `api/api.go`.
   - if the schema changed (excluding `info.version`): `info.version` equals the computed next
     schema version; if not: it equals `main`'s current schema version.
   - Fork PRs keep working: "Version Check" only reads (`git fetch origin main`, no push),
     unaffected by this change.
4. `scripts/update-version.sh` (the manual one-off bumper) already only wrote `.version` and
   `api/version.go` — it never touched the OpenAPI file. No behavior change; its header now
   says so explicitly and points at `ci-version-bump.sh`'s schema subcommands for the schema
   side.

## Mechanics

- "Changed by the PR" (both the docs-only check and the schema-changed check) is computed
  against `git merge-base origin/main HEAD`, not `main`'s current tip — squash-merge applies
  only the PR's own diff, so comparing against a tip that advanced past the fork point would
  treat untouched files as "changed."
- The *expected version values* (what the branch's files should equal) still derive from
  `main`'s current tip, same as before decoupling. This preserves the already-documented
  residual race: two PRs open concurrently can compute the same next version, and the second to
  merge looks stale until pushed again (rebase retriggers recomputation). A hand-edited
  `info.version` with no accompanying schema diff is treated as drift and gets corrected back to
  `main`'s current schema value by "Version Bump" (or fails "Version Check" on fork PRs) — same
  self-healing behavior as `.version` drift today.
- `scripts/ci-version-bump.sh` gained `is-docs-only`, `compute-schema-version`, and
  `apply-schema-version`; `apply-version` no longer touches the OpenAPI file.
- `make generate-api` only reruns (and `oapi-codegen` only installs) in "Version Bump" when the
  schema version is actually moving.

## What was checked and left unchanged

The root endpoint's `api.version` JSON field is sourced from `GetSwagger().Info.Version` — the
embedded **spec/schema** version — not from `api/version.go`'s `APIVersion` constant (which is
fixed at `"v1"` and only reaches a CLI `--version` print in `cmd/server/main.go`, never the HTTP
response). The server's own version is already reported separately, in `service.build`. This
matches the OpenAPI schema's own description of the field ("API version") and is unaffected by
this change: once the two versions diverge, `/` will correctly report the schema version in
`api.version` and the server version in `service.build`, as two already-distinct fields. No code
change was made to `api/version.go`'s handler.

## Consequences

- A schema-changing PR without `feat`/`!` in its title still only patch-bumps the schema, same
  granularity as the server version today.
- Docs-only PRs (wiki-adjacent housekeeping, `PROGRESS.md` updates, ADRs under
  `docs/superpowers/`) no longer produce a bump commit or a version churn in their diff.
