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

In every case the rule's actual behavior is fully preserved by a live sibling condition
that reads `jsonBody`/`result_details`/`result_reason` instead. The 121,940-file, zero-
disagreement run above is direct empirical confirmation that dropping these dead
sub-conditions changed no observed classification. **No restoration is recommended** —
they read a field CATS has never emitted in any file in this corpus (or, per the code
comments and history, in any real CATS output), so restoring them would add dead code
back, not behavior.

## Reconciled disagreements

None. The harness produced zero disagreements on the first run; no reconciliation with
the YAML was required.

## Recommendation

- **Gate: PASS.** Proceed to Task 13 (deletion of the legacy hardcoded false-positive
  detection). The 62-rule YAML port is behaviorally equivalent to
  `detect_false_positive()` on this corpus, verified independently on both the boolean
  verdict and the specific matched rule id for all 121,940 records.
- **5xx guard:** keep `allow_5xx=False` as the plugin's default runtime behavior (per
  the porting design) — it is a genuine improvement over the legacy code with no cost
  observed in this corpus, and should stay on so any future 5xx among classified
  results is surfaced rather than silently absorbed by a false-positive rule.
- **Dead `responseBody` sub-conditions:** no action; do not restore. They target a JSON
  key CATS does not produce.
