# CATS false-positive rule migration: corpus equivalence report

**Task:** Task 10 (independent equivalence gate) of the CATS plugin extraction project.
**Author:** independent verification agent (did not write, read, import, or adapt any
harness produced by the porting agent).

## What was compared

- **Legacy:** `scripts/parse_cats_results.py`, `CATSResultsParser.detect_false_positive(data)`
  (lines 542–1449), instantiated as `CATSResultsParser(":memory:")` — the method touches no
  DB state, so no `connect()` call was needed.
- **New:** `catslib.parse.record_from_json(data)` to build the normalized record, then
  `catslib.rules.classify_record(rules, record, allow_5xx=...)` where
  `rules = catslib.rules.load_rules(test/cats/false-positives.yaml)` (62 rules, schema
  version 1).
- **Corpus:** every `Test*.json` file under `test/outputs/cats/report/`.

The harness (`/tmp/.../equiv_check.py`, deleted after this run per task instructions) was
written independently, without reading or adapting the porting agent's own verification
script. It iterates the corpus once, calling both implementations on each record and
comparing `(is_false_positive, rule_id)` exactly (boolean and rule id), with
`allow_5xx=True` for the equivalence comparison so the deliberate new 5xx guard is not
mistaken for a porting defect. A second `classify_record` call per record (`allow_5xx=False`)
measures the guard separately in the same pass.

## Result: exact agreement

```
total_files=121940 parsed=121940 unparsable=0
classified(error/warn)=54814
legacy_false_positive_true=54813
new_false_positive_true(allow_5xx=True)=54813
agreements=121940 disagreements=0
```

- **121,940** files in the corpus, all parsed successfully (0 unparsable/corrupt files).
- **54,814** records have `result` in `{error, warn}` — the only records either
  implementation classifies; both return "not a false positive" for `success` without
  even inspecting the record further.
- **54,813** of those are classified as false positives by the legacy code — leaving
  exactly **one** true positive in the corpus, matching the independently-confirmed
  figure from the legacy results database.
- The new engine reproduces **54,813** false positives too, and **on every single
  record** (not just in aggregate count) the boolean verdict and the specific matched
  rule id agree between legacy and new. Zero disagreements across all 121,940 files.

**GATE VERDICT: PASS.** This is a second, independent confirmation of the porting
agent's own zero-mismatch result — arrived at via a from-scratch harness with no
access to the porting agent's verification code.

## 5xx guard measurement (`allow_5xx=False`)

Re-running `classify_record` with `allow_5xx=False` produced:

```
5xx guard hits: 0
```

**Zero** records had `violation_rule_id` set. This is not because the guard is a no-op —
it is because the corpus itself contains **no classified (`error`/`warn`) record with a
5xx response code at all**:

```python
# independently verified by scanning response.responseCode across the corpus
classified records with 5xx code: 0
```

So the new 5xx guard exists as a safety net for future runs (per the Zero-500-Error
Policy) but had nothing to suppress in this particular corpus. This is a positive
finding about the current state of the API under test in this run, not a defect —
there is no evidence in this corpus of a false-positive rule that was silently
swallowing a real 500. No GitHub issues are warranted from this measurement; the guard
should stay enabled so it fires the moment a future run does produce a 5xx that a rule
would otherwise mask.

## Dead `response.responseBody`-reading sub-conditions

CATS's actual per-test JSON never emits a `responseBody` key under `response` — only
`jsonBody` (confirmed by sampling response object keys across thousands of corpus
files: `contentLengthInBytes, headers, httpMethod, jsonBody, numberOfLinesInResponse,
numberOfWordsInResponse, responseCode, responseContentType, responseTimeInMs`).
Consequently every legacy sub-condition that reads
`data.get('response', {}).get('responseBody')` always evaluates the empty string and
can never match. The port omitted these sub-conditions deliberately; per-instance
detail:

| Line(s) | Rule (`FP_RULE_*`) | What the dead sub-condition checked | Live sibling condition that preserves the rule |
|---|---|---|---|
| 733–748 | `NOT_FOUND_404` | `response_body` containing any of `legitimate_not_found_messages` (`"not found"`, `"add-on not found"`, `"invocation not found"`, `"user not found"`, `"group not found"`, `"webhook not found"`, `"threat model not found"`, `"diagram not found"`, `"document not found"`, `"threat not found"`, `"not defined in the api specification"`) | Same message list checked against `result_details` (line 749–750), which is populated from `resultDetails` and does appear in the corpus |
| 942–944 | `TRANSFER_ENCODING_501` | `response_body` containing `"unsupported transfer encoding"` | `fuzzer == 'DummyTransferEncodingHeaders'` (939) and `"transfer encoding" in result_reason` (945) |
| 1006–1009 | `ADMIN_SETTINGS_RESERVED` | `response_body` containing `"reserved"` on `/admin/settings/*` | Same check against `result_details` (1011–1012) |
| 1026–1028 | `PATH_PARAM_VALIDATION` | `response_body` containing `"parameter"` and `"doesn't match the regular expression"` | `error_description` from `jsonBody` checked for the OpenAPI3Filter regex-mismatch message (1020–1024) |
| 1042–1046 | `EMPTY_BODY_REQUIRED_FIELDS` | `response_body` containing (`"property"` and `"is missing"`) or `"is required"` | `error_description` from `jsonBody` checked for the same two phrasings (1035–1040) |
| 1096–1098 | `NO_BODY_ENDPOINT` | `response_body_text` containing `"does not accept a request body"` | `error_description` from `jsonBody` checked for the same phrase (1093) |
| 1221 | `ADDON_PARAMETER_VALIDATION_400` | Not even part of a condition — `response_body` is assigned and then never referenced again; the match logic only reads `error_description` from `jsonBody` | N/A — this was simply an unused local variable in the legacy code, not a reachable branch |

In most cases the rule's actual behavior is fully preserved by a live sibling condition
that reads `jsonBody`/`result_details`/`result_reason` instead, and the 121,940-file,
zero-disagreement run above is direct empirical confirmation that dropping the dead
sub-condition changed no observed classification for that rule.

**Two exceptions, stated plainly:** for `NOT_FOUND_404` and `ADMIN_SETTINGS_RESERVED`,
the claim above is **unverified, not confirmed**. Their live sibling conditions —
`NOT_FOUND_404`'s `result_details` message-list check, and `ADMIN_SETTINGS_RESERVED`'s
`result_details` containing `"reserved"` — have **zero matches anywhere in this corpus**
(independently re-checked: 0 and 0). The zero-disagreement equivalence run proves the
YAML doesn't do anything *different* from the legacy code on the records this corpus
happens to contain, but it cannot prove the sibling condition is equivalent to the
dropped `responseBody` branch, because neither branch ever fired here. If a future
corpus produces a 404 with a "not found"-style message in `resultDetails`, or a 400 on
`/admin/settings/*` with "reserved" in `resultDetails`, that will be the first real test
of whether the live sibling actually covers what the dead branch was meant to. Until
then, treat those two rules' behavior on responseBody-shaped input as unverified.

`TRANSFER_ENCODING_501` is a related but distinct case, not a third instance of the same
gap: per `test/cats/rule-baseline.json` this corpus contains **zero** classified records
with `response_code == 501` at all, so the entire rule — `when` clause and both `any_of`
branches, dead and live alike — is unexercised here, not just the dropped sub-condition.
Nothing in this run says anything about whether the live sibling (`fuzzer ==
'DummyTransferEncodingHeaders'` or `"transfer encoding" in result_reason`) preserves the
rule; there is simply no 501 to test it against.

For the remaining four rules in the table (`PATH_PARAM_VALIDATION`,
`EMPTY_BODY_REQUIRED_FIELDS`, `NO_BODY_ENDPOINT`, `ADDON_PARAMETER_VALIDATION_400`), per
`test/cats/rule-baseline.json` the live sibling condition does have corpus support (each
rule fires at least once), so "fully preserves" is accurate for those.

**No restoration is recommended** for any of the seven — they read a field CATS has
never emitted in any file in this corpus (or, per the code comments and history, in any
real CATS output), so restoring them would add dead code back, not behavior.

## Rule-firing distribution

`test/cats/rule-baseline.json` (added after this report was first written) captures,
for each of the 62 rules, how many classified records it **matches** in isolation and
how many it actually **fires** on under first-match-wins, against this same corpus. The
headline numbers from that file:

- **21 of 62 rules never fire against this corpus** (`matched == 0` for all of them):
  `IDOR_ADMIN`, `IDOR_LIST`, `IDOR_OPTIONAL`, `RESPONSE_CONTRACT`, `CONFLICT_409`,
  `LEADING_ZEROS_400`, `TRANSFER_ENCODING_501`, `ADMIN_SETTINGS_RESERVED`,
  `EMPTY_PATH_PARAM_405`, `METADATA_BULK_VALIDATION_400` (matches 30 but is shadowed —
  see below), `METADATA_LIST_RANDOM_200`, `SAML_ACS_NO_IDP`,
  `SURVEY_RESPONSE_SCHEMA_ALLOF`, `REVOKE_HTTP_METHODS_RFC7009`,
  `SAML_ACS_HTTP_METHODS`, `OAUTH_TOKEN_INVALID_CLIENT_401` (matches 1 but is shadowed
  — see below), `SSRF_CALLBACK_REDIRECT`, `CONTENT_TOKENS_LIST_EMPTY_200`,
  `CONTENT_TOKENS_DELETE_IDEMPOTENT_204`, `CONTENT_OAUTH_HAPPY_PATH_400`,
  `CONTENT_OAUTH_AUTHORIZE_422`. Most of these have zero matches outright; a small
  number match but never fire because an earlier rule always claims the record first
  (see next point). Either way, none of them has been exercised as the *deciding* rule
  in this corpus.
- **2 rules are fully shadowed** — they match real records but an earlier rule always
  wins first: `METADATA_BULK_VALIDATION_400` (matches 30, fires 0) and
  `OAUTH_TOKEN_INVALID_CLIENT_401` (matches 1, fires 0).
- **A single rule, `OAUTH_AUTH_401_403`, accounts for 96.3% of all suppressions**
  (52,804 of 54,813 fired classifications). No other rule comes close — the next
  largest is `NOT_FOUND_404` at 547.

See `test/cats/rule-baseline.json` for the full per-rule table (id, file position,
matched count, fired count) and `test/cats/README.md` for how to regenerate it after a
deliberate rule change.

## Docstring-only legacy rule ids: correctly not ported

The legacy `detect_false_positive()` docstring lists two rule ids that have no
corresponding `FP_RULE_*` constant and no implementation anywhere in
`scripts/parse_cats_results.py`: `ONEOF_VALIDATION_400` ("Incomplete oneOf bodies
correctly rejected") and `REMOVE_FIELDS_ONEOF` ("RemoveFields on oneOf endpoints
correctly returns 400"). Both were checked against the legacy source (grep for the
literal strings, and for any `FP_RULE_ONEOF*`/`FP_RULE_REMOVE_FIELDS*` constant) and
confirmed to be docstring-only — stale documentation for rules that were apparently
removed or never implemented, not rules the port missed. They were correctly excluded
from `test/cats/false-positives.yaml`. This is noted here so a future reader diffing
the legacy docstring against the YAML's 62 rules doesn't mistake these two names for
omissions in the port.

## On the equivalence gate's reproducibility

This gate's evidence is real but not indefinitely reproducible, and that should be
stated plainly rather than left implicit:

- The comparison harnesses (this report's and the porting agent's) were throwaway
  scripts under `/tmp`, written independently and deleted after each run per task
  instructions. None of them is preserved in the repository.
- The equivalence check passed **three times**, with independently written harnesses,
  across the full 121,940-record corpus, agreeing on every record's verdict and rule id
  each time. That is meaningful triangulation, not a single unverified run.
- Once Task 13 removes `detect_false_positive()` from
  `scripts/parse_cats_results.py`, there is no "legacy" side left to compare against —
  **this gate can never be re-run** after that deletion. It is a one-time, historical
  confirmation, not a regression test that continues to protect the YAML.
- This is precisely why `test/cats/rule-baseline.json` now exists: it is the only
  artifact that survives Task 13 and gives a future editor something concrete to check
  a rule change against (via `matched`/`fired` counts), even though it cannot re-derive
  the legacy comparison itself.

## Reconciled disagreements

None. The harness produced zero disagreements on the first run; no reconciliation with
the YAML was required.

## Recommendation

- **Gate: PASS.** Proceed to Task 13 (deletion of the legacy hardcoded false-positive
  detection). The 62-rule YAML port is behaviorally equivalent to
  `detect_false_positive()` on this corpus, verified independently on both the boolean
  verdict and the specific matched rule id for all 121,940 records. This confirmation
  is not re-runnable once Task 13 lands (see "On the equivalence gate's
  reproducibility" above); `test/cats/rule-baseline.json` is the durable record of what
  the rule set did.
- **5xx guard:** keep `allow_5xx=False` as the plugin's default runtime behavior (per
  the porting design) — it is a genuine improvement over the legacy code with no cost
  observed in this corpus, and should stay on so any future 5xx among classified
  results is surfaced rather than silently absorbed by a false-positive rule.
- **Dead `responseBody` sub-conditions:** no action; do not restore. They target a JSON
  key CATS does not produce. Note the two exceptions under "Dead
  `response.responseBody`-reading sub-conditions" above (`NOT_FOUND_404`,
  `ADMIN_SETTINGS_RESERVED`) where the live sibling's equivalence is unverified rather
  than confirmed, and the related `TRANSFER_ENCODING_501` case where the whole rule is
  unexercised by this corpus.
- **Over-broad / low-quality rules** (e.g. rules matching far more than their `why`
  implies) and the **inert conditions** now annotated in
  `test/cats/false-positives.yaml` (`EMPTY_PATH_PARAM_405`,
  `STRING_BOUNDARY_EMPTY_PATH`, and the `IDOR_LIST` dead sub-condition) are deliberately
  out of scope for this change and are tracked as separate follow-up work.
