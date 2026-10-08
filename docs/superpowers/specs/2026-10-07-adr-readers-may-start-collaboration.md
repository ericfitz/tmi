# ADR: Readers may start collaboration sessions

Status: accepted. **Human decision (Eric, 2026-10-07).**

## Context

`POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate` returns 201 for a user with only `reader` access to the threat model. The OpenAPI spec documents no role restriction for this operation, and the handler (`api/threat_model_diagram_handlers.go`, `CreateDiagramCollaborate`) checks only read access. The permission-matrix Postman collection expected 403 for a reader, so the test and the server disagreed. This came up while aligning the Postman collections with the documented API on PR #1049 (#956).

## Decision

Readers may start a collaboration session. The current server behavior is intended, and tests expect success (201, or 200 when a session already exists) for a reader.

## Consequences

- No server or spec change. The Postman permission-matrix expectation was changed to the documented success.
- Edit rights inside a session are still governed by the WebSocket authorization (readers cannot modify cells). Starting a session does not grant write access.
- If the policy changes later, it needs a role check in the handler, a documented 403 on the operation, regenerated code, and updated Postman expectations.
