# CATS bug-fix session — handoff

Paste this into a new session to continue.

---

We were fixing CATS testing/migration bugs in `/Users/efitz/Projects/tmi`.
**That work is landed.** Read this whole note before doing anything, and verify
rather than inherit — three handoffs running, each carried a claim that failed
checking.

## Where things stand

**Everything is merged to `main` and pushed.** `main` is at `920421e6`,
version 1.5.0, working tree clean apart from this untracked file.

- **PR #617** — `dev/1.6.0/cats-fixes` → `main`, 57 commits, all 12 CI checks
  green. Merged.
- **PR #612** (deps bump against the branch) merged into it first, so it rode in.
- **PRs #618, #619** — the real Dependabot bumps, which only appeared *after*
  main updated. Both merged.
- **PR #613** closed, not merged: its base was already fully contained in main
  and its `go.mod` had zero versions main lacked.
- **PR #589** closed as superseded by `de0fcc79`, a strict superset.
- **Dependabot alerts: 0.** Both were `kin-openapi <= 0.143.0` — a *critical*
  `ValidationHandler.Load()` fail-open auth bypass and a medium nil-pointer
  panic — filed against `test/integration/go.mod`, the nested module Dependabot
  wasn't covering. #607 fixed the coverage gap and #617 carried v0.145.0 to main.

The skills repo is on `main` and contains the full cats work
(`dev/cats-fixes-tmi-578-581-587` is 0 commits ahead — safe to delete). The
wiki is pushed at `13bcc63`.

**Branches: tmi is down to `main` and `release/1.3.5`.** Deleted `dev/1.6.0/cats-fixes`,
`dev/1.6.0/cats-plugin-extraction`, `deps/auto-bump/dev/1.6.0/cats-fixes/30373036047`
and `feature/pin-portforward-context` (the last was unmerged but superseded by
`de0fcc79`; its commit `a45ae679` is retained on closed PR #589).

**`release/x.y.z` branches are permanent — never delete one.** They are maintenance
snapshots of released/deployed versions, kept so a reported problem against that
version can be patched. Their commits are mostly dependency bumps plus a bugfix or
two, already replicated onto `main` directly or by cherry-pick, so a large
`git log main..release/x.y.z` count is expected and is not a reason to touch them.

Gates on the current tree, run after the Dependabot merges: **lint 0, 2419 unit,
153 dev-script**.

## Still open, deliberately

Four issues, each with a comment on it explaining why. Do not sweep them.

- **#594** — `ApplyOptimisticLock` runs its CAS outside the transaction it
  guards, at all 16 call sites. `4f25eb11` only corrected the doc comment that
  claimed otherwise. The fix threads a transaction handle through every caller,
  changes boundaries on a concurrency primitive, and has a race for a failure
  mode the integration suite cannot exercise. Needs the oracle-db-admin gate.
- **#593** — falls out of #594.
- **#596** — CATS never *generates* the nested array-item uuid, so
  `related_project_id`/`related_team_id` are never sent. The bisect it asks for
  (delete one keyword at a time from the **real** `RelatedProject` schema — the
  mini-reproduction shortcut is how #582 reached a wrong conclusion) is not done.
- **#608** — the destructive-DELETE half is fixed. The residual,
  `InvalidReferencesFields` injecting `?` into a path parameter, cannot be fixed
  server-side. **It auto-closed on merge** (a `Fixes #608` trailer, inert on a
  dev branch but live on `main`) and was reopened. If you merge another branch
  carrying that trailer, expect to reopen it again.

Plus the three Oracle follow-ups, untouched: **#614, #615, #616**. Do not re-file.

## The finding worth carrying forward

**The June 2026 legacy corpus was itself contaminated by #591** — and it was the
baseline everything else had been compared against, which is why nobody noticed.

Its `OAUTH_AUTH_401_403` count is 52,804, the exact figure `rule-baseline.json`
attributes to "a run that lost its bearer token 21% of the way in".
`git show 84bd1430^:test/cats/rule-baseline.json` confirms the identification:
`corpus_run: "test/outputs/cats/report/"`, 121,940 records. 401s by test-number
decile: 0.4%, 1.4%, then 36%, 52%, 55%, 68%, 64%, 53%, 52%, 50%.

So #585's "confirmed dead on both corpora" rested on two contaminated corpora —
June by #591, July by #578. **The removals in `84bd1430` still stand**, because
they were also confirmed against `20260728T042755Z`. But there is exactly **one**
clean corpus, and the five rules #585 deliberately left alone must be judged
against it alone. Recorded as a comment on #585.

This surfaced only because the corpus was inspected on the way to deleting it.

## What's next

1. **#596** — the schema bisect, on the real spec. Largest remaining CATS item,
   and it also unblocks the deferred `cats_remove_field` entries for
   `responsible_parties`/`members` in `cmd/dbtool/reference.go`.
2. **A clean re-run.** Three zero-match FP rules still want a campaign where
   every seeded fixture survives, and #585's remaining five need clean evidence.
   Feeds #596 too.
3. **Oracle #614/#615/#616.** #615 is cheapest — extending
   `scripts/check-oracle-unsafe-map-keys.py` to `Updates(map)` keys makes the
   `modified_at`/ORA-00957 invariant machine-enforced instead of comment-enforced.
4. **#594/#593** — the optimistic-lock transaction fix, when there's a way to
   exercise the race.
5. **Skills branch cleanup** (tmi's is done). Skills'
   `dev/cats-fixes-tmi-578-581-587` is merged into its `main` and 0 commits
   ahead — safe to delete, deliberately left alone this session.

## Techniques worth carrying forward

- **Measure before theorising.** Every handoff in this chain had a stale claim.
  Check `git status -sb`, `gh issue list` and `gh pr list` rather than reading a
  note — including this one.
- **Inspect before an irreversible delete.** 1.4 GB went away; the inspection on
  the way out is what found the contaminated reference corpus.
- **A test you haven't seen fail isn't a test.** `CATS_TOOL_GUARD` was verified
  by running it against a nonexistent path and an empty one, not by reading it.
- **Fix the class, not the instance.** Two broken make targets became a repo
  sweep (`2efc4c7c`), which stopped at the repo boundary; the wiki sweep
  (`13bcc63`) then found 18 more. When a sweep has a boundary, name it.
- **`Fixes #N` is inert on a dev branch and live on `main`.** Merging a long dev
  branch fires every trailer at once — check afterwards for issues you meant to
  keep open.
- **The offline CATS probe** against a dead server (`--server=http://127.0.0.1:1`)
  is still the cheapest way to bisect spec-vs-fuzzer behaviour: CATS records the
  generated payload before the connection fails.
- **`pgrep -f "cats_tool.py run"` matches its own waiter shell.** Use another pattern.

## Environment notes

- **The cats plugin is installed** (`cats@efitz-skills` v0.1.0). `/cats:run`,
  `/cats:report`, `/cats:analyze`, `/cats:fp`, `/cats:init` and the
  `cats:cats-run` subagent are live. The Makefile's `CATS_TOOL` resolves the
  installed plugin first and falls back to `~/Projects/skills/cats`, so
  `make cats-fuzz` and `/cats:run` cannot diverge. Override with
  `make ... CATS_TOOL=/path/to/cats_tool.py`. Never hardcode either path again.
- Seeding must run over loopback (port-forward), not the NodePort: `tmi-dbtool`
  is an unsigned Go binary and macOS local-network restrictions block it from
  dialing the cluster node at all, while `curl`/`nc`/Homebrew `cats` reach it
  fine. The campaign itself still fuzzes `http://rp2:30080` directly.
- Rule count is 48. `test/results/cats/` holds five campaigns; `keep_runs` is 5.
  `latest.db` → `cats-results-20260728T095140Z.db`, and **that is fine to use**.
  `20260728T042755Z` was the *first* run to pass both validity gates, not the
  only one; every later campaign passes too, and measured 2026-07-28 the newest
  is cleanest (transport % / unauth-401 %): 042755Z 0.064/0.166, 080417Z
  0.069/0.155, 095140Z 0.069/0.139. Since the gates landed, a contaminated run
  exits 3 without repointing the symlink, so `latest.db` can only ever point at
  a run that passed.
- `test/outputs/cats/` is gone. Its summary is at
  `test/cats/legacy-corpus-2026-06-summary.json` (8.6 KB) — **untracked and
  gitignored by choice**, so it will not survive a clone. Three things cite the
  deleted corpus: commit `988e32b8`, issue #585, and
  `docs/threat-model/2026-05/THREAT_MODEL.md`'s inputs list.
- `git fetch`/`git pull` need a physical key touch and will fail without one.
  That is expected; do not work around it.
