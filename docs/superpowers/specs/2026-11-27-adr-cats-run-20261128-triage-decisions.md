# ADR: CATS run 20261128 triage decisions

- Status: accepted (human decisions, Eric, 2026-11-27)
- Context: triage of CATS run 20261128T005435Z (301 true positives, zero 5xx). Findings are filed as #1255–#1259, plus evidence on #1250 and #1252.

## Decisions

1. **The operator-pinned webhook subscription shows its real URL** (#1257). `GET /admin/webhooks/subscriptions` and the GET-by-id equivalent return the configured `url` instead of the `"(operator-pinned)"` placeholder (`api/webhook_handlers.go:715`). Admins already control the server, so hiding the address from them adds no protection, and the placeholder broke the `WebhookSubscription.url` response schema. There is no spec change. This reverses the earlier choice to redact the pinned address in subscription responses. The redaction of the address in delivery error messages (`api/webhook_delivery_handlers.go:461-469`) is not covered by this decision; keep it until it is decided separately.
2. **`threat_type` is optional** (#1258). Drop it from `ThreatBase.required` and keep `default: []` and `minItems: 0`. This matches current server behavior, which accepts and defaults a missing value on POST, bulk POST and PUT. It is a spec-only change with no handler change.
3. **The fix for cross-parent note access shipped without a public issue** (#1260). GET, PATCH and DELETE on project and team notes did not check the note's parent. It was fixed on a branch and described only in the PR and commit, because the repository is public and the fix landed before disclosure.
4. **False-positive rule changes over the 5% threshold are accepted** (#1261). The three rule changes suppress 162 of 301 true positives (54%). Eric confirmed this, since 156 of them extend the 2026-11-24 generated-JSON-Pointer decision to the header and security fuzzers that reuse the same body.
