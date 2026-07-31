# Handoff — CATS true-positive triage, 2026-07-31

## Where things stand

Branch **`fix/cats-triage-1.6.4`**, version bumped **1.6.3 → 1.6.4** (`.version` +
`api-schema/tmi-openapi.json` `info.version`, both in step).

The 2026-07-30 CATS baseline (`20260730T220551Z`, 542 true positives) is now fully triaged and the
remediable findings are fixed. Expected effect on the next campaign:

| category | was | expected |
|---|---|---|
| Not matching response schema | 428 | **~4** |
| Unexpected 401 (`/saml/providers/{idp}/users`) | 19 | **0** |
| Unexpected 403 (`/me/groups/{id}/members`) | 12 | **0** |
| Unexpected 200 (`CheckDeletedResourcesNotAvailable` on `/collaborate`) | 2 | **0** |
| Unexpected 500 (bulk create) | 1 | **0** (fixed in 1.6.3) |
| Not found (`PATCH /threat_models/{id}`) | 1 | 1 (decoy artifact, see #651) |
| everything else | ~79 | ~79 (verified correct server behavior / CATS artifacts) |

Roughly **542 → ~85**, and the residue is understood rather than unexplained.

## The big finding: #637 is a CATS bug, now root-caused

CATS validates a 2xx response against the operation's declared **`example`**, not its **`schema`**.
When an example is present, the response's property names (recursively) must be a *subset* of the
example's. The schema is never consulted — replacing it with `{"type":"object"}` or even `true`
still fails.

- Reproduced standalone: 1 path, 1 schema, a 20-line Python stub, one `--fuzzer HappyPath` run.
  Deleting the `example` alone flips fail → pass.
- Validated across the whole run: **428 flagged / 0 with a covering example.** Zero counterexamples.
- Explains the `Note`-vs-`TeamNote` discriminator that stalled two prior sessions: `TriageNote`,
  `Survey`, `ContentFeedback` have **no** example (check never runs); `TeamNote`, `DfdDiagram`,
  `Repository` have **exhaustive** ones; `Note`, `Asset`, `ThreatModel`, `Document` have **partial**
  ones. Two structurally identical siblings landed on opposite sides because one example listed two
  more optional fields.
- Filed upstream: **[Endava/cats#206](https://github.com/Endava/cats/issues/206)**.

Chosen remediation was **not** suppression: the 23 incomplete response examples were completed, so
the findings go away legitimately and future genuine schema regressions still surface. No
false-positive rule was added. Example *structure* was taken from real response bodies (so
`POST /projects` documents `team` as the `{id, name}` summary it actually is, not a fully-populated
Team) and *values* from schema examples, so no fixture strings leaked into published docs.

One site is unfixable from the spec side: `MinimalDiagramModel.metadata` is
`additionalProperties: {type: string}`, a free-form map whose keys are unbounded, so no example can
enumerate them. That is the expected ~4 residual findings, and the unsatisfiability is documented
upstream.

## What's in this branch

| Change | Why |
|---|---|
| `cmd/server/main.go` — replace blanket `/saml/` public prefix with 3 exact paths + `isPublicSAMLPath` | **#650**, real bug: `GET /saml/providers/{idp}/users` returned 401 on **69/69** requests and had never worked |
| `cmd/server/public_paths_test.go` (new) | Guards #650 — 17 path cases incl. traversal, empty provider, case, trailing slash |
| `api-schema/tmi-openapi.json` — 23 response examples completed | #637 remediation |
| `api-schema/tmi-openapi.json` — `RepositoryBulkUpdateItem.required` += `name` | Server enforced it (`repository_sub_resource_handlers.go:592`) but no schema declared it |
| `api-schema/tmi-openapi.json` — `x-skip-deleted-resource-check` on `GET /collaborate` | Sessions aren't CRUD: DELETE ends one, POST starts another, so 200 after DELETE is correct |
| `test/seeds/cats-seed-data.json` — add `charlie` to `CATS Test Group` | `/me/groups/{id}/members` answers from the caller's own membership; both routes are read-only so the campaign can't re-break it |
| `.version`, `api/api.go` | Version bump; regenerated (embedded spec + one struct tag: `Name` lost `omitempty`, stays `*string`, handler nil-check unaffected) |

### Verification done

- `make validate-openapi` — 0 errors, all 328 operations annotated
- `make generate-api` (oapi-codegen v2.7.1), `make build-server`, `make lint` — 0 issues
- `make test-unit` — **2451 passed, 0 failed**
- `make test-integration` — **82 passed, 0 failed, 9 skipped**
- Security review — **no HIGH or MEDIUM findings**; the auth change strictly *narrows* the public
  path set, and `isPublicSAMLPath` fails closed on traversal, empty provider, wrong case and
  trailing slash. Confirmed no fixture/PII strings entered the spec.
- Deployed 1.6.4 to k3s and verified live: `/saml/providers/tmi/users` → **200 with 39 users**
  (was 401); anonymous → 401; `/saml/providers/okta/users` → **403**, meaning the handler's
  cross-provider check is reachable for the first time; all 6 public SAML routes still work
  without a token; `/me` and `/threat_models` unaffected.

## DO NOW

1. **Fix the SEM marker.** `cmd/server/main.go:167` carries a placeholder
   `SEM@0000000` — the real sha can only be known once the commit exists. Run
   `/sem-annotate --update cmd/server/main.go` and add it as a second commit on the branch.
2. **Review and merge the PR.** `main` is PR-only. The PR references `Fixes #650`.
3. **Close #650** manually after merge — with squash-merge from a feature branch, `Fixes` does not
   auto-close.
4. **Redeploy from `main` after merge.** k3s currently runs 1.6.4 built from this working tree, so
   the cluster is ahead of `main` until then.

*(The `TestIdentityLink` failure seen mid-session was chased down and is **not** a regression — it is
a pre-existing flake, now **#653**. Both the clean base and the restored branch pass 82/82; the
failing run had drawn an already-bound suffix.)*

## DO NEXT

### Fresh CATS campaign — the actual proof
Re-seed and run a full campaign to confirm 542 → ~85. **Do not skip re-seeding**: the group
membership fix lives in the seed file, so the 12 403s only clear on a freshly seeded DB.

```
kubectl config current-context          # MUST be k3s-rp; set-oauth-secret.sh flips it to EKS
make cats-fuzz                          # ~36 min; port-forwards are automatic since #643
make analyze-cats-results
```

Fuzz the NodePort `http://rp2:30080` directly, never a port-forward (#463/#578). Remember a
completed run is not automatically a valid one — the transport-error and non-FP-401 gates decide,
and the plugin enforces both.

Two predictions worth checking explicitly, since both were verified only against the *old* DB:
- schema findings drop to ~4, all on `GET /threat_models/{id}/diagrams/{id}/model`
- `/saml/providers/{idp}/users` now produces 2xx coverage, and possibly **new** findings — it has
  never actually been exercised, so treat anything it reports as genuinely new surface

### Issues filed this session
- **#650** — SAML 401 bug (fixed here, close after merge)
- **#651** — anchor-path decoys erase ~97% of the anchor's own coverage. Zero 2xx on `PATCH` or
  `PUT` for `/threat_models/{id}` across the entire run, so no fuzzer ever exercised a successful
  threat-model update. Also the source of the lone `Not found` finding. Four options written up;
  option 3 (fuzz the anchor scoped, without DELETE) is cheapest, option 1 (per-method refData, if
  CATS supports it) is better. **Affects all 8 decoyed anchors, not just threat models.**
- **#652** — `chat/sessions` returns HTTP 200 with an SSE `event: error` when session creation is
  refused *before* streaming ("maximum of 50 active sessions"). Validate before writing SSE headers
  and return 429/409. Secondary: the campaign exhausts the 50-session cap itself, so later tests
  only exercise the rejection path.
- **#653** — `TestIdentityLink` flake: user suffix is `time.Now().UnixNano() % 10000`, only 10,000
  values against a DB that accumulates `il-alt-NNNN` identity bindings forever. Fix by widening the
  suffix (`login_hint` allows 11 more chars) and/or deleting the identity in teardown.

### Follow-ups identified but not filed
- **Spec-vs-middleware consistency lint.** #650's root cause was a coarse public-path prefix
  contradicting `x-public-endpoint`. A check that every operation lacking `x-public-endpoint` is
  actually behind auth would make this class of drift fail the build. `/webhook-deliveries/` and
  `/.well-known/` are still prefix-matched — believed correct, unverified.
- **Bulk-limit spec/code mismatch.** Handlers check `len > 50` while the spec caps `maxItems` at
  **20** for threats, documents and repositories bulk ops, so those branches are unreachable. Assets
  is consistent at 50. Also `threats/bulk` PATCH/DELETE and `teams|projects/{id}/metadata/bulk`
  PATCH declare **no** `maxItems` at all. Deliberately left alone — reconciling needs a decision on
  which number is intended, and it produced no finding.
- **Two more CATS defects** noted in the upstream issue as possibly sharing a code path:
  `ExamplesFields` posts a schema-level *object* example as the whole body where the request schema
  is an *array* (400 "value must be an array" on `assets/bulk`, `documents/bulk`); and array query
  params are comma-joined regardless of `explode: true` (`?severity=high%2Chigh` instead of
  `severity=high&severity=high`).

### Older open issues, unchanged
#642 test-artifact disk retention (~6.6 GB, 2.4 GB held by runs that failed validity gates and can
never be a baseline) · #633 restore `/me` coverage · #634 rewrite `set-server-setting.py` on the
generated client · #631 k8s Secrets vs DB config for OAuth providers · #608 CATS deletes its own
fixtures · #596 CATS drops required nested array-item uuid fields

## Triage conclusions worth not re-deriving

Everything below was checked against code or spec and is **correct server behavior** — do not
"fix" these:

- **11× 400 on `POST /threat_models`** (incl. `HappyPath`) — CATS generates two **byte-identical**
  `authorization` array items; the server correctly rejects duplicate subjects. The same array-item
  duplication is what exposed the real bulk-create 500 fixed in 1.6.3.
- **`VeryLargeStringsInFields` → 2xx on `content`** — CATS's default 40,000-char payload is well
  under the declared `maxLength: 262144`. Not a validation bypass.
- **`StringFormatAlmostValidValues` on `uri` → 201** — `format` is annotation-only in OAS.
- **`HomoglyphEnumFields` → 201** — CATS sent plain ASCII `DFD-1.0.0`; a no-op mutation.
- **MaxLength/MinLength on `type` → 400** — single-value enum wins over the length constraint.
- **`/collaborate` accepts `[{}]` with 201** — the operation declares **no** `requestBody`, so the
  body is correctly ignored.
- **SSE endpoints** (`chat/sessions`, `collaborate`) — CATS wraps event streams as
  `{"notAJson": "event: ..."}` and cannot parse them; also the source of the lone missing-security-
  headers finding.
- **8× 501 on `PATCH /threat_models/{id}`** — `Transfer-Encoding: cats`; correct RFC 9112 §7
  behavior from Go's HTTP server, below the application layer. CATS did not flag these.

## Reusable tooling

Session scratchpad (session-scoped, will be cleaned up — copy anything you want to keep):
`/private/tmp/claude-501/-Users-efitz-Projects-tmi/4289b005-.../scratchpad/`

- `repro/` — the standalone upstream reproduction (`openapi.json` + `server.py`)
- `probe.py`, `sweep1..5.py` — spec-mutation bisect harness against the real server
- `synth.py`, `synth2.py`, `synth3.py` — standalone characterization against the stub
- `predict3.py` — the whole-DB confusion matrix; re-run it after any spec example change to see
  how many findings remain predicted
- `fill_examples2.py` — the example completer (structure from responses, values from schemas)
- `cutspec.py` — extract a minimal spec for given paths with the transitive `$ref` closure
