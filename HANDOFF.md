# Handoff — CATS campaign 20260731T200650Z, 2026-08-01

## Where things stand

`main` is at **1.6.5**. The 1.6.4 work (#650 SAML auth fix + 23 completed response examples) merged
as `39d2d938` via PR #654, and the follow-up campaign it existed to prove has now run.

### The campaign: 542 → 162

Run `20260731T200650Z`, 107,608 requests, 38m. **Valid run** — both gates passed (transport errors
0.07% vs 1% limit; non-FP 401s 0.08% vs 5% limit) and all 6 seeded fixtures survived.

| category | 20260730 baseline | predicted | actual |
|---|---|---|---|
| Not matching response schema | 428 | ~4 | 72 |
| Unexpected 401 (`/saml/providers/{idp}/users`) | 19 | 0 | **0** |
| Unexpected 403 (`/me/groups/{id}/members`) | 12 | 0 | **0** |
| Unexpected 200 (`CheckDeletedResourcesNotAvailable`) | 2 | 0 | **0** |
| Unexpected 500 | 1 | 0 | **0** |
| everything else | ~79 | ~79 | ~99 |

Every 1.6.4 remediation landed. **Zero 500s in the entire run.** `/saml/providers/{idp}/users` went
from 69/69 × 401 to **58 × 200** — genuinely reachable for the first time, and it immediately
produced 18 new findings, all now resolved.

172 → **162** after the one false-positive rule added this session.

## Why the schema count was 72, not 4

The specific prediction held exactly: 4 findings on `GET .../diagrams/{id}/model`, the unfixable
`MinimalDiagramModel.metadata` residual. The other 68 were the *same* #637 mechanism on operations
the previous run never got a 2xx from.

The 1.6.4 fix built examples from **observed response bodies** — accurate, but a property that was
null or absent in the fixture never entered the example. Absent-in-fixture is not absent-in-schema.
`PATCH /teams/{team_id}` produced 54 findings purely because its example lacked `email_address` and
`uri`.

**This is now a build gate.** `make check-response-examples` (wired into `make validate-openapi`)
fails when a 2xx response example omits a property its schema declares. It is a ratchet in both
directions: a new gap fails, and a baselined gap that no longer reproduces must be retired.

## What's in 1.6.5

| Change | Why |
|---|---|
| `scripts/lib/openapi_examples.py` + `scripts/check-response-examples.py` | The gate. 13 unit tests in `scripts/lib/tests/`. |
| `api-schema/response-example-baseline.json` | 611 accepted gaps across 93 operations, none of which fires today (checked against all 107k responses). Tracked in **#659**. |
| `api-schema/tmi-openapi.json` — 24 example properties | The gaps the server demonstrably returns. Kills the 54 + others. |
| `api-schema/tmi-openapi.json` — real schema for `GET /saml/providers/{idp}/users` | Its response schema was literally `{"type":"object"}`. Kills 8. |
| `test/cats/false-positives.yaml` — `SAML_USERS_CROSS_PROVIDER_403` | Kills 10. |

### Verification

`make validate-openapi` (0 errors) · `make generate-api` (v2.7.1; embedded spec only, no type
changes) · `make build-server` · `make lint` (0 issues) · `make test-unit` (**2451 passed**) ·
`make test-dev-scripts` (**177 passed**) · `make test-integration` (**82 passed**) · security
review — no HIGH or MEDIUM findings.

## Triage conclusions worth not re-deriving

Everything in the 1.6.4 handoff's verified-correct list still holds. Added this session:

- **`GET .../threats` × 7 and `DELETE .../threats/bulk` × 3** — the CATS array-query-param defect,
  now **#658**. CATS comma-joins array params despite `explode: true`
  (`?severity=low%2Clow`), the server correctly 400s. Three of these are `HappyPath`, which reads
  exactly like a real regression until you look at the raw URL. **Do not re-triage these.**
- **`RandomResources` → 403 on `/saml/providers/{idp}/users` × 10** — 403 *is* documented; the
  fuzzer expects 404. Answering 404 for an unknown provider would make the endpoint a
  provider-existence oracle. Suppressed by rule, not fixed.
- **`RollbackResponse.restored_entity`** — `{"type":"object"}` holding whichever entity was rolled
  back; its example documents a threat model, the observed response was a diagram. Same
  unsatisfiable class as `MinimalDiagramModel.metadata`. 4 findings, left alone deliberately.

## DO NEXT

Nothing is blocking. In rough priority:

1. **#659** — decide `Project.team`. The schema declares a full `Team`; the server returns
   `{id, name}` and `team.created_at` appears in **0** of 107,608 responses. This is the single
   largest chunk of the baseline (~93 entries across the 4 project operations). A `TeamSummary`
   schema fixes it, but changes generated types, so it needs a decision.
2. **#658** — report the array-param defect upstream. It is not just noise: because every array-param
   request is malformed, the `severity` / `priority` / `threat_type` / `threat_ids` filters have
   **never been fuzzed with valid input**. Same class of hidden coverage loss as #651.
3. **#660** — Redis `i/o timeout` on the token-blacklist check returns 500. Found via a cascading
   `TestDiagramCRUD` failure; re-run passed 82/82. Fails closed, which is right, but 500 is the wrong
   code and there is no retry.
4. **#651** — anchor-path decoys still erase ~97% of the anchor's coverage. Still zero 2xx on
   `PATCH`/`PUT /threat_models/{id}`. Affects all 8 decoyed anchors.
5. Burn down the #659 baseline so the ratchet tightens.

### Open issues

**Filed this session**: #657 (upstream `example`-vs-`schema`, Endava/cats#206) · #658 (array query
params) · #659 (schema/server divergence + baseline) · #660 (Redis 500)

**Older, unchanged**: #653 `TestIdentityLink` flake · #652 `chat/sessions` SSE error envelope ·
#651 anchor decoys · #642 test-artifact disk retention · #633 restore `/me` coverage · #634 rewrite
`set-server-setting.py` · #631 k8s Secrets vs DB config · #608 CATS deletes its own fixtures ·
#596 CATS drops required nested array-item uuid fields · #627 automated versioning

### Still not filed

- **Spec-vs-middleware consistency lint.** #650's root cause was a coarse public-path prefix
  contradicting `x-public-endpoint`. `/webhook-deliveries/` and `/.well-known/` are still
  prefix-matched — believed correct, unverified.
- **Bulk-limit spec/code mismatch.** Handlers check `len > 50` while the spec caps `maxItems` at 20
  for threats/documents/repositories, so those branches are unreachable. Assets is consistent at 50.
  `threats/bulk` PATCH/DELETE and `teams|projects/{id}/metadata/bulk` PATCH declare no `maxItems`.
  Produces no finding; needs a decision on which number is intended.

## Reproducing the analysis

The gate itself replaces most of the previous session's ad-hoc tooling:

```
uv run scripts/check-response-examples.py                    # what is missing
uv run scripts/check-response-examples.py --update-baseline  # after fixing gaps
```

To re-derive which baseline entries actually fire, join `true_positives_view` against the response
bodies in a run database and compare recursive property-name sets — that is what established that
all 611 are currently dormant.
