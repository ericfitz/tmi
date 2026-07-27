# CATS Fuzz Tooling → Portable Claude Code Plugin

**Date:** 2026-07-26
**Status:** Approved design, ready for planning
**Repos:** `~/Projects/skills` (new `cats` plugin), `/Users/efitz/Projects/tmi` (migration)

## Problem

TMI's CATS API fuzzing tooling works well but is welded to TMI. Three scripts totalling
~2,450 lines carry the whole pipeline, and the most valuable artifact in it — 43
hand-derived false-positive rules — exists as ~900 lines of hardcoded Python inside
`detect_false_positive()`. None of it can be used in another repo, and the rules cannot be
edited, reviewed, or reasoned about as data.

Current inventory:

| File | Lines | Role |
| --- | --- | --- |
| `scripts/run-cats-fuzz.py` | 480 | preflight, Redis rate-limit clearing, dbtool seeding, OAuth-stub token acquisition, CATS invocation, auto-parse |
| `scripts/parse_cats_results.py` | 1798 | SQLite schema (6 tables, 5 views, 18 indexes) + `detect_false_positive()` (43 rules) |
| `scripts/query-cats-results.py` | 178 | canned summary queries |

Plus Makefile targets `cats-seed`, `cats-fuzz`, `cats-fuzz-oci`, `query-cats-results`,
`analyze-cats-results`, and CATS documentation in `.claude/skills/test/SKILL.md`.

## Goal

Extract the tooling into a repo-agnostic Claude Code plugin, with per-repo configuration
and false-positive rules living in the consuming repo — losing no TMI functionality.

## Non-goals

- Multi-identity fuzzing (cross-user authz scenarios). The config schema is shaped for it;
  the runner stays single-identity.
- Replacing CATS, or adding non-CATS probe phases.
- Migrating TMI's GitHub Wiki CATS pages (follow-up issue).

## Architecture

### Plugin layout (`~/Projects/skills`)

```
cats/
  .claude-plugin/plugin.json
  skills/
    init/SKILL.md
    run/SKILL.md
    report/SKILL.md
    analyze/SKILL.md
    fp/SKILL.md
  scripts/cats_tool.py          # single entrypoint, uv inline deps (pyyaml)
  scripts/catslib/
    config.py                   # discovery + schema validation
    runner.py                   # hooks + CATS invocation
    parse.py                    # report JSON -> SQLite
    classify.py                 # YAML rules -> is_false_positive / fp_rule
    rules.py                    # matcher: field vocabulary + operators
    report.py                   # self-contained HTML generation
    schema.sql
  agents/cats-run.md            # subagent for the long fuzz run
```

Registered in `.claude-plugin/marketplace.json` as plugin `cats`, category `testing`.

Subcommands: `init | run | parse | classify | query | report | doctor`.

### Repo contract

Machine-specific configuration is gitignored under `.local/`; the false-positive rules are
committed, because they are shared repo knowledge that must survive a clone and be
reviewable in PRs.

```yaml
# .local/cats/config.yaml   (gitignored)
version: 1
spec: api-schema/tmi-openapi.json
server: http://localhost:8080
health_url: http://localhost:8080/          # TMI has no /health endpoint
results_dir: test/results/cats              # prompted at init; offered to .gitignore
false_positives: test/cats/false-positives.yaml   # committed path
retain_raw_report: false
allow_suppressing_5xx: false

identities:
  admin: { token_cmd: "uv run scripts/cats-token.py --user charlie" }
default_identity: admin
auth: { header: Authorization, template: "Bearer {token}" }

hooks:
  seed:     "make cats-seed"
  pre_run:  "uv run scripts/cats-prep.py"
  post_run: ""

cats:
  http_methods: [POST, PUT, GET, DELETE, PATCH]
  max_requests_per_minute: 3000
  ref_data: test/results/cats/cats-test-data.yml
  skip_field_format: [uuid]
  skip_field: [offset]
  skip_fuzzers: [...]                       # today's 11
  skip_fuzzers_for_extension:               # today's 4
    - { extension: x-public-endpoint, value: "true", fuzzers: [BypassAuthentication] }
  extra_args: []
```

Every CATS flag TMI passes today is expressed as config. Nothing repo-specific is
hardcoded in the plugin.

Config is discovered by walking up from cwd to find `.local/cats/config.yaml`. `/cats:init`
writes this file directly; the `provision-repo-config.py` sole-writer rule applies to
`repos.json` / `gh-projects.json`, not to all of `.local/`.

### Results layout

One database per run; the filename carries the history. Old runs are deleted by hand.

```
test/results/cats/
  cats-results-20260726T220200Z.db
  report-20260726T220200Z/          # raw CATS output, pruned by default after parse
  latest.db -> cats-results-20260726T220200Z.db
```

`retain_raw_report: false` prunes the raw report after a successful parse. Justification:
TMI's June 30 report directory is 11 GB (121,940 `Test*.json` plus a 116 MB `index.html`),
and once error/warn bodies are in the DB the raw files are redundant.

## Run pipeline

`/cats:run` dispatches a background subagent that executes `cats_tool.py run`:

1. **Discover config** — walk up from cwd.
2. **Preflight** — spec exists; `cats` on PATH (version recorded); `health_url` responds;
   rules file parses. Fail fast with actionable messages.
3. **`hooks.seed`** — skippable via `--skip-seed`.
4. **`hooks.pre_run`** — TMI: Redis rate-limit clearing.
5. **Resolve token** — run `identities.<id>.token_cmd`; stdout, trimmed; empty ⇒ abort.
6. **Run CATS** — argv built from the `cats:` config block; progress streamed.
7. **Parse** — report JSON → new timestamped DB, raw fields only.
8. **Classify** — apply YAML rules → `is_false_positive` / `fp_rule`.
9. **`hooks.post_run`** — receives `CATS_DB`, `CATS_RUN_ID`, `CATS_EXIT_CODE`.
10. **Prune raw report** — if `retain_raw_report: false`.
11. **Summary** — counts by result, FP count, top paths; this is what the subagent returns.

### Hook contract

Hooks are opaque shell commands run via `bash -c` with cwd = repo root, so a repo using any
toolchain can plug in. Non-zero exit from `seed` or `pre_run` aborts the run; a `post_run`
failure is a warning, since the DB is already written.

Environment provided to every hook: `CATS_SERVER`, `CATS_SPEC`, `CATS_RESULTS_DIR`,
`CATS_REPORT_DIR`, `CATS_RUN_ID`, `CATS_IDENTITY`. `post_run` additionally gets `CATS_DB`
and `CATS_EXIT_CODE`. Tokens are never placed in hook environments.

### Token handling

Today the token is passed as `-H "Authorization=Bearer <token>"` on the CATS command line,
where it is visible in `ps`. The runner instead writes a `umask 077` temporary headers file
(`all: {Authorization: Bearer …}`), passes `--headers=<file>`, and deletes it in a `finally`.
Tokens remain redacted from all logging. This also lays the groundwork for per-path
identities.

### CLI surface

Preserved from today: `--identity`, `--path`, `--rate`, `--blackbox`, `--skip-seed`,
`--skip-parse`.

## False-positive rule engine

### Field vocabulary

Exactly the fields the 43 existing rules touch, nothing speculative:

`result` · `response_code` · `fuzzer` · `path` · `contract_path` · `method` · `url` ·
`scenario` · `result_reason` · `result_details` · `any_text` (virtual: `result_reason` +
`result_details`, today's `text_to_check`) · `response_body` · `response_content_type` ·
`request_body` · `json_body.<dotted.path>` · `request_header.<name>`

### Operators

A bare scalar means `equals`. Also: `in`, `not_equals`, `contains`, `contains_any`,
`contains_all`, `starts_with`, `starts_with_any`, `ends_with`, `matches` (regex), `exists`.

Case semantics mirror today's Python: `equals` and `in` are case-sensitive (matching exact
fuzzer-name list membership); substring and prefix operators are case-insensitive (today
lowercases text before checking).

### Rule file

```yaml
# test/cats/false-positives.yaml   (committed)
version: 1
rules:
  - id: RATE_LIMIT_429
    why: Rate limiting is infrastructure protection, not API behavior.
    when: { response_code: 429 }

  - id: OAUTH_AUTH_401_403
    why: Expected auth failures during fuzzing.
    when:
      response_code: { in: [401, 403] }
      any_text: { contains_any: [unauthorized, forbidden, invalid_token,
                                 invalid_grant, access_denied] }

  - id: SURVEY_METADATA_CONFLICT_409
    why: Metadata key already exists from seeding; 409 is correct.
    when:
      response_code: 409
      method: POST
      path: { contains_all: ["/admin/surveys/", "/metadata"] }
```

### Semantics

- Rules evaluate in file order; **first match wins**; the matching `id` is written to
  `fp_rule`. This reproduces today's if-chain, where order is load-bearing because earlier
  rules shadow later ones.
- Only `error` and `warn` rows are classified, matching today's early return.
- Keys within `when:` are ANDed. A rule may carry `any_of: [<when-block>, …]` for OR.
- Rules support `enabled: false` and `tags:`.
- A rule that matches a **5xx** response is refused by default
  (`allow_suppressing_5xx: false`), reported as a config error naming the rule and test.
  This makes it structurally impossible to silence a 500 by widening an FP rule, per the
  zero-500 policy. Overridable per repo.

### Classification as a separate pass

Parsing stores raw results; classification is a distinct, re-runnable pass over the DB. A
rule change reclassifies in seconds without re-fuzzing. Each pass records per-rule match
counts, enabling stale-rule detection (0 matches) and over-broad-rule detection.

## Database schema

Today's 6 tables, 5 views and 18 indexes are preserved verbatim, so existing SQL keeps
working. Additions:

**`run_meta`** — one row: `run_id`, `started_at`, `finished_at`, `identity`, `spec_path`,
`spec_sha256`, `rules_sha256`, `git_sha`, `cats_version`, `cats_args` (token-redacted),
`server`, `tool_version`.

**Bodies for `error`/`warn` rows** — `responses.response_body` and `requests.request_body`.
Required because classification runs from the DB, and 14 of the 43 rules match on response
body or `json_body.*`. Success rows skip bodies, containing the size increase.

**`fp_rules`** — `rule_id`, `why`, `order_index`, `enabled`, `match_count`. The DB carries
its own rationale, so a report explains why a finding was suppressed without reading the
repo file.

**`true_positives_view`** — `result IN ('error','warn') AND is_false_positive = 0`.

## Skills

### `/cats:init`

Probes for the OpenAPI spec (`api-schema/*.json`, `openapi.{json,yaml}`, `api/openapi*`,
`docs/`); prompts for the results sub-path (default `test/results/cats/`) and offers to add
it to `.gitignore`; prompts for server and health URLs; checks for the `cats` binary and
records its version; writes `.local/cats/config.yaml` with commented hook scaffolding; and
creates the committed rules file. Starter rules are limited to stack-agnostic ones (429
rate limiting, CATS connection-error 999) under a format-documenting header, so no TMI
assumptions leak into a fresh repo. `cats_tool.py doctor` re-validates a config at any time.

### `/cats:run`

Dispatches the run subagent in the background; returns run id, DB path, counts by result,
FP count, and top true-positive paths. Detects the unconfigured case and points at
`/cats:init`.

### `/cats:report`

`cats_tool.py report --db latest --out report.html [--open]` produces a self-contained HTML
file (no external assets): result mix, FP breakdown by rule, true positives grouped by
path/fuzzer/status, per-finding request/response detail, rule-coverage table.

The SKILL.md documents the full SQLite schema — tables, columns, views, worked query
examples — so loading the skill lets an agent one-shot any query rather than exploring the
DB. The frontmatter description advertises this.

### `/cats:analyze`

Triage over `true_positives_view`: cluster findings, then classify each cluster as (a) a
real bug, (b) a spec gap (CATS "undocumented response code" — the documented-status-code
policy), or (c) an FP-rule candidate; emit a remediation plan. Category (c) hands drafted
YAML to `/cats:fp`. 500s are always reported and never auto-suppressed.

### `/cats:fp`

- **add** — from a `test_id` or finding cluster, draft rule YAML and dry-run it: show
  exactly which currently-true-positive tests it would suppress before writing, hard-stop
  on 5xx, warn on outsized suppression share. No equivalent exists today.
- **review** — rule coverage: stale rules (0 matches), over-broad rules, disabled rules.
- **reclassify** — re-apply rules to an existing DB and show the delta.

## Rule migration and validation

The 43 rules port to YAML mechanically, preserving order. Correctness is verified against
the complete golden corpus already on disk from the June 30 run:
**121,940 `Test*.json` files, 54,813 currently flagged as false positives.**

1. A throwaway harness runs legacy `detect_false_positive()` and the new engine over the
   same corpus.
2. It diffs `(is_false_positive, fp_rule)` per test.
3. **Merge gate: exact agreement on all 121,940 tests**, or every divergence explained and
   signed off before the old parser is deleted.

## TMI migration

1. Build the plugin on a branch in `~/Projects/skills`; port rules; validate against the
   corpus; register in `marketplace.json`.
2. Extract TMI hook scripts: `scripts/cats-token.py` (the OAuth-stub `/flows/start` flow,
   lifted from `run-cats-fuzz.py`) and `scripts/cats-prep.py` (Redis rate-limit clearing).
   The seed hook is the existing `make cats-seed`.
3. Land rules at `test/cats/false-positives.yaml` (committed).
4. Rewrite `cats-fuzz`, `cats-fuzz-oci`, `query-cats-results`, `analyze-cats-results` as
   thin Makefile wrappers over `cats_tool.py`, preserving the make-target convention.
5. Delete `run-cats-fuzz.py`, `parse_cats_results.py`, `query-cats-results.py` — only after
   the corpus diff is clean.
6. Update references: `CLAUDE.md` (CATS section), `.claude/skills/test/SKILL.md`,
   `.claude/skills/test-oci/SKILL.md`, `scripts/test-framework.mk:127`
   (`rm -rf test/outputs/cats/*`), `scripts/help.py`, `scripts/README.md`, `README.md`,
   `test/TESTING_STRATEGY.md`.
7. File a follow-up issue for GitHub Wiki CATS pages.

## Testing

Plugin tests follow the skills repo's pytest layout:

- `tests/test_cats_config.py` — discovery, schema validation, error messages.
- `tests/test_cats_rules.py` — every operator, case semantics, first-match-wins ordering,
  `any_of`, 5xx refusal.
- `tests/test_cats_parse.py` — report JSON → schema, body storage only for error/warn.
- `tests/test_cats_classify.py` — reclassification, match counts, delta reporting.

Plus the one-off corpus-diff harness described above, which is deleted after migration.

## Acceptance criteria

1. The ported rule set reproduces legacy classification on all 121,940 corpus tests.
2. A full `make cats-fuzz` on TMI produces the same true-positive set as the June 30
   baseline, modulo real changes since.
3. `/cats:init` bootstraps a non-TMI repo with no TMI-specific defaults.
4. No TMI-specific string remains in the plugin.
5. TMI's three CATS scripts are deleted and its make targets still work.
