# test/cats/

This directory holds the CATS false-positive rule set (`false-positives.yaml`)
and its supporting records. Everything else under `test/cats/` is generated
output and is gitignored; see `.gitignore` for the explicit negations that
keep `false-positives.yaml`, `rule-baseline.json`, and this `README.md`
tracked.

## Rule set and order

`false-positives.yaml` is evaluated by the CATS plugin at
`/Users/efitz/Projects/skills/cats/scripts/` (`catslib.rules`). Rules are
matched in **file order, first match wins** — this mirrors the sequential
if-chain with early returns in the legacy Python function
(`scripts/parse_cats_results.py`, `detect_false_positive()`) that the YAML
was ported from. **Order is load-bearing**: inserting a rule in the wrong
place, or reordering existing rules, can silently change which rule (or
whether any rule) suppresses a given fuzzer finding. Each rule's leading
comment records its actual file position as `rule N of 62`; if you add,
remove, or reorder a rule, renumber every comment (and the total) to match.

## `rule-baseline.json`

This is a golden snapshot of what the rule set actually does against a real
fuzzing corpus: for each of the 62 rules, how many classified (`error`/
`warn`) records it **matches** in isolation, and how many it actually
**fires** on once first-match-wins is applied (a record a later rule
"matches" may already have been claimed by an earlier one).

It exists because the equivalence gate that originally proved the YAML
behaves identically to the legacy Python (three independent runs, zero
disagreements across 121,940 corpus records) depends on `detect_false_positive()`
still existing in `scripts/parse_cats_results.py`. Once that function is
removed, the gate can never be re-run — this file is the only remaining
record of what the rule set did, and the reference point for judging whether
a future rule change altered behavior.

### Regenerating after a deliberate rule change

If you intentionally change a rule's conditions (not just comments), update
`rule-baseline.json` so it keeps reflecting reality:

1. Get (or refresh) a CATS fuzzing corpus under `test/outputs/cats/report/`
   (`Test*.json` files) — run `make cats-fuzz` or reuse an existing report
   directory.
2. Write a short script that imports `catslib.rules` and `catslib.parse`
   from `/Users/efitz/Projects/skills/cats/scripts/`, loads
   `false-positives.yaml` via `load_rules`, and for each classified record
   calls `match_rule` per rule (for `matched`) and `classify_record` /
   first-match iteration (for `fired`). Run it with
   `uv run --with pyyaml python3 <script>`.
3. Write the corpus totals (`total_records`, `classified_records`,
   `suppressed_records`) and the per-rule `{id, position, matched, fired}`
   list to `rule-baseline.json`, along with the corpus path and date in the
   header fields.
4. Delete any scratch script you wrote — it isn't part of the repo.

Do not hand-edit the counts in `rule-baseline.json`; they should always come
from an actual run against a real corpus.
