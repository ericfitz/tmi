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
(`detect_false_positive()`, formerly in `scripts/parse_cats_results.py`,
deleted once the port was verified equivalent) that the YAML was ported
from. **Order is load-bearing**: inserting a rule in the wrong
place, or reordering existing rules, can silently change which rule (or
whether any rule) suppresses a given fuzzer finding. Each rule's leading
comment records its actual file position as `rule N of 89`; if you add,
remove, or reorder a rule, renumber every comment (and the total) to match.

## `rule-baseline.json`

This is a golden snapshot of what the rule set actually does against a real
fuzzing corpus: for each of the 89 rules, how many classified (`error`/
`warn`) records it **matches** in isolation, and how many it actually
**fires** on once first-match-wins is applied (a record a later rule
"matches" may already have been claimed by an earlier one).

It exists because the equivalence gate that originally proved the YAML
behaves identically to the legacy Python (three independent runs, zero
disagreements across 121,940 corpus records) depended on `detect_false_positive()`
in `scripts/parse_cats_results.py`, which has since been deleted along with
the rest of the legacy pipeline. That gate can never be re-run — this file
is the only remaining record of what the rule set did, and the reference
point for judging whether a future rule change altered behavior.

### Regenerating after a deliberate rule change

If you intentionally change a rule's conditions (not just comments), update
`rule-baseline.json` so it keeps reflecting reality:

1. Get (or refresh) a CATS fuzzing corpus (`Test*.json` files) — run
   `make cats-fuzz` with `retain_raw_report: true` set in
   `.local/cats/config.yaml`, which leaves the raw report at
   `test/results/cats/report-<run_id>/` instead of deleting it after parsing,
   or reuse an existing retained report directory.
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

## Spec vendor extensions that shape a campaign

The campaign config (`.local/cats/config.yaml`, gitignored and machine-local)
maps operation-level `x-` extensions in `api-schema/tmi-openapi.json` to fuzzers
that should not run against that operation. Because the extension sits on an
**operation**, not a path, this is the only mechanism available for scoping
campaign behaviour to a single HTTP method — CATS `refData` is per-path and has
no method qualifier (the format's only special keys are `all`, `#` json-path
separators, `$$` environment variables, `cats_remove_field` and
`additionalProperties`).

| extension | skips | why |
|---|---|---|
| `x-public-endpoint` | `BypassAuthentication` | Endpoint is intentionally unauthenticated per RFC. |
| `x-cacheable-endpoint` | `CheckSecurityHeaders` | Cache headers are intentional. |
| `x-skip-deleted-resource-check` | `CheckDeletedResourcesNotAvailable` | Resource is legitimately readable after deletion. |
| `x-skip-idor-check` | `InsecureDirectObjectReferences` | Identifier is not an object reference. |
| `x-preserve-fixture` | `AcceptLanguageHeaders`, `ExtraHeaders`, `CheckSecurityHeaders`, `NewFields`, `HappyPath` | Keeps a seeded anchor alive for the rest of its path's tests (#651). |

### `x-preserve-fixture` (#651)

`refData` binds one id per path, so the decoy seeded for an anchor path is also
the id every `GET`/`PATCH`/`PUT` on that path uses. A `DELETE` that *succeeds*
therefore removes the resource the rest of the path's tests depend on. In run
`20260730T220551Z` the decoy for `/threat_models/{threat_model_id}` was consumed
7 requests into a ~2,300-request block, so roughly **97%** of that path's tests
ran against a 404 and neither `PATCH` nor `PUT` ever recorded a single 2xx.

The extension is set on the **`delete` operation only** of each anchor that has
a decoy, and withholds just the fuzzers that issue an otherwise-valid `DELETE`.
Over run `20260731T200650Z` those five fuzzers accounted for every one of the 41
successful anchor deletes, and represent 72 of 424 anchor `DELETE` tests — 17%
of `DELETE` coverage traded for the other ~97% of each path's coverage. Every
other `DELETE` fuzzer still runs and still gets its 4xx.

Anchors currently carrying it:

    /teams/{team_id}                             /admin/groups/{group_id}
    /projects/{project_id}                       /admin/users/{user_id}
    /threat_models/{threat_model_id}             /intake/survey_responses/{survey_response_id}
    /teams/{team_id}/notes/{team_note_id}        /admin/webhooks/subscriptions/{webhook_id}

Destructive `DELETE` coverage of these paths is not lost permanently — run it
scoped, where consuming the fixture at the end costs nothing:

    cats run --path /threat_models/{threat_model_id}

To confirm the fix held after a campaign, check that the anchor recorded 2xx on
the methods that previously could not:

```sql
SELECT m.method, r.response_code, COUNT(*)
FROM tests t
JOIN paths p        ON p.id = t.path_id
JOIN responses r    ON r.test_id = t.id
JOIN http_methods m ON m.id = r.http_method_id
WHERE p.path = '/threat_models/{threat_model_id}'
GROUP BY m.method, r.response_code
ORDER BY m.method, r.response_code;
```
