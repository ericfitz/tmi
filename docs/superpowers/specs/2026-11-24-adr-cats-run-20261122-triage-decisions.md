# ADR: CATS run 20261122 triage decisions

- Status: accepted (human decisions, Eric, 2026-11-24)
- Context: triage of CATS run 20261122T014607Z (2,395 true positives, zero 5xx). The full plan is machine-local at `test/results/cats/triage-20261122T014607Z.md`.

## Decisions

1. **Malformed Accept headers get a strict 406.** `AcceptHeaderValidation` parses each media range and q-value properly; any malformed entry (for example `*/*; q=invalid`) gets 406. This replaces substring matching.
2. **Chat session `source_snapshot` has no item cap.** Drop `maxItems: 100` from the spec. The snapshot reflects the threat model's size, and the server is unchanged.
3. **GET `/me/sessions` returns an array.** Fix the spec to `type: array` of CollaborationSession, which matches the server and tmi-ux. No wire change.
4. **JSON Patch `copy`/`move` without `from` is a CATS false positive.** The server's 400 is correct per RFC 6902. Add a false-positive rule rather than modeling per-op `oneOf` in `JsonPatchDocument`.
5. **The webhook `*` wildcard is a supported event value** (human decision, Eric, 2026-11-25, while fixing #1230). `*` subscribes to all events. It is added to the `WebhookEventType` enum, so subscription create and PATCH accept it, and stored `*` subscriptions conform to the response schema. Every other event name outside the enum gets 400 on create and PATCH. The test-delivery request enum (`WebhookTestRequest.event_type`) stays concrete and excludes `*`.
