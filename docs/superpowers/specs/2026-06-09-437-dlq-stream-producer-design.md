# Design: TMI_DLQ stream + dead-letter producer (#437)

**Issue:** #437 — wire a TMI_DLQ stream + producer so exhausted extraction jobs dead-letter to `failed` (#347 Plan 3 follow-up)
**Date:** 2026-06-09
**Branch:** dev/1.4.0
**Status:** Approved (brainstorming)

## Problem

The async extraction pipeline (#347 Plans 1–3) defines `worker.SubjectDLQ` (`jobs.dlq`) as a
constant, but nothing publishes to it and no JetStream stream binds it. JetStream's `MaxDeliver`
exhaustion does **not** auto-publish to a custom DLQ subject — that requires explicit wiring that
was never built.

### The gap, precisely

Terminal-bad jobs are **already** handled: a worker's `JobHandler` publishes a failed `Result`
envelope to `TMI_RESULTS` *before* returning a terminal `*JobError` (the `JobHandler` contract in
`internal/worker/consumer.go`). The monolith result-consumer then drives the `extraction_jobs` row
to `failed`.

The **only** unhandled path is a worker that **crashes / OOMs / segfaults mid-job**. It never
publishes a result; `AckWait` lapses; JetStream redelivers until `MaxDeliver` (3) is exhausted; and
the `extraction_jobs` row sits in `queued` indefinitely. The access-poller timeout can eventually
clean it up, but there is no clean `extraction_failed` terminal transition or webhook.

### Why in-worker republish cannot fix it

The stated failure mode is a dead worker, so any "republish to `jobs.dlq` on terminal failure"
code living in the worker never runs in exactly the case we care about. The DLQ producer **must**
be an always-on, separate process. The monolith is the natural home — it is already the stateful
coordinator, the result-consumer, and the job-state authority.

## Mechanism

JetStream emits a `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.<stream>.<consumer>` advisory when a
message exhausts redelivery. The advisory carries the source stream name and `stream_seq` but
**not** the original payload. So the producer recovers the payload by sequence.

## Components

### 1. `TMI_DLQ` stream
Created at runtime by the monolith (mirrors how Plan 2 workers create `TMI_RESULTS`; **no**
controller render, consistent with the existing `TMI_RESULTS` precedent in
`internal/worker/names.go`). Config: `WorkQueuePolicy` retention, `FileStorage`, subject
`jobs.dlq`. The monolith is both the sole producer and sole consumer.

### 2. Advisory-capture stream
A JetStream stream bound to `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>`. Capturing advisories in
a durable stream (rather than a plain core-NATS subscription) makes the producer **restart-safe**:
a core-NATS subscription would silently miss any advisory fired while the monolith is down (e.g.
during a deploy), leaving the row stuck. Name: `TMI_DLQ_ADVISORY` (or similar). `LimitsPolicy`
retention with a short `MaxAge` is acceptable since the durable consumer drains it promptly;
`FileStorage`.

### 3. `DLQProducer` (`api/dlq_producer.go`, new)
A durable consumer on the advisory-capture stream. Per advisory message:

1. **Skip self-referential advisories.** If the advisory's source stream is `TMI_RESULTS` or
   `TMI_DLQ`, ack and skip — this prevents dead-letter loops.
2. **Recover the original message.** `js.Stream(sourceStream).GetMsg(stream_seq)` returns the
   original bytes.
3. **Validate as a Job.** Decode as `jobenvelope.Job` and run `jobenvelope.Validate`. **Only valid
   jobs proceed.** This scopes the producer to job streams without hardcoding component names: a
   stray exhausted `Result` (e.g. a result message that exhausted the result-consumer's own
   `MaxDeliver`) will not validate as a `Job`, so it is skipped. (A stuck result message is a
   separate concern, out of scope; the access-poller backstop covers it.)
4. **Publish then delete.** Publish the recovered `Job` bytes to `jobs.dlq`; **then** `DeleteMsg`
   the source message. Publish-before-delete ensures nothing is lost if the publish fails. Deleting
   the source reclaims the per-component WorkQueue slot (an exhausted, un-acked message otherwise
   lingers in a `WorkQueuePolicy` stream forever).
5. **Idempotent redelivery.** If `GetMsg` returns not-found (the advisory was redelivered after we
   already deleted the source), ack the advisory — already processed.

The producer is panic-guarded (a bad advisory must never crash the monolith), matching the
result-consumer's callback discipline.

### 4. `ResultConsumer` extension (`api/result_consumer.go`)
Add a **second** subscription, bound to `TMI_DLQ`, alongside the existing `TMI_RESULTS` one. The
DLQ callback differs only in its decode and synthesis step:

- Decode the message as a `jobenvelope.Job` (not a `Result`).
- Synthesize `Result{JobID: job.JobID, Status: StatusFailed, ReasonCode: "extraction_dead_lettered",
  ReasonDetail: "worker exhausted redelivery (dead-letter)"}`.
- Run the **existing** `handleResult`, which marks the row `failed`, sets the document's
  `access_status` to `extraction_failed`, and emits the `document.extraction_failed` webhook.
- **Additionally** delete the crashed job's orphaned **input** blob (`job.Input.ObjectRef`) via
  `conn.DeletePayload`. `handleResult` only deletes `res.Output.ResultRef` (empty for a synthesized
  DLQ result), so without this the input payload blob in `TMI_PAYLOADS` would leak.

`TMI_DLQ` is bound skip-if-absent (like `TMI_RESULTS`): if the stream does not exist yet, log a
warning and continue.

### 5. New reason code (`api/access_diagnostics.go`)
Add `ReasonExtractionDeadLettered = "extraction_dead_lettered"` to the extraction reason-code block.
Add a `case ReasonExtractionDeadLettered:` to `BuildAccessDiagnostics` that appends a
`RemediationRetry` (a crashed worker is a transient condition; retry may succeed). Reason codes are
a stable API contract that tmi-ux localizes by code, so a **client-side localization follow-up**
issue must be filed against tmi-ux for the new code.

## Wiring (`wireExtractionNATS`, `cmd/server/main.go`)

Start order:
1. `DLQProducer` — ensure-creates `TMI_DLQ` and the advisory-capture stream, starts its durable
   advisory consumer.
2. `ResultConsumer.Start` — binds `TMI_RESULTS` and `TMI_DLQ` (the latter now exists).

Both fail-safe to a no-op when NATS is absent (the function already returns early when
`TMI_NATS_URL` is unset). Both are `Stop()`-ed on graceful shutdown (extend the existing
`StopResultConsumer` path with a `StopDLQProducer`).

## Error handling & idempotency

At-least-once throughout, consistent with the existing system contract. `MarkTerminal` is an
idempotent OnConflict upsert; a double DLQ delivery re-marks the same terminal row harmlessly.
Webhook emit-once under redelivery is tracked separately as #438. The producer and the DLQ callback
are both panic-guarded.

## Testing

### Integration test (gated by `TMI_RUN_NATS_TESTS`, live NATS)
Simulate a crashed worker deterministically: run a real `worker.RunConsumer` with `MaxDeliver=1`
(or 2) and a short `AckWait`, whose handler **always returns a transient error** (Naks every
delivery, never publishes a result) — the same end-state as an OOM/crash. Then assert:
- the original `Job` lands on `jobs.dlq`,
- the source message is deleted from the per-component stream,
- the DLQ path drives `handleResult` so the `extraction_jobs` row transitions to `failed` and the
  document `access_status` becomes `extraction_failed`.

This satisfies the #347 Plan 4 dead-letter acceptance criterion's production-code prerequisite.

### Unit tests
- `DLQProducer` decision logic: skip self-referential streams; skip messages that do not validate as
  a `Job`; the publish-then-delete and not-found-ack ordering (with fakes/mocks for the JetStream
  surface where feasible).
- `BuildAccessDiagnostics` emits a `RemediationRetry` for `extraction_dead_lettered`.

## Oracle / DB considerations

No schema change. The DLQ path reuses the existing `ExtractionJobStore.MarkTerminal` (already
reviewed and Oracle-portable per #347 Plan 3) and `UpdateAccessStatusWithDiagnostics`. The
`oracle-db-admin` subagent will still be dispatched because the change touches code that calls into
the repository layer; no new SQL is introduced.

## Out of scope

- Stuck **result** messages that exhaust the result-consumer's own `MaxDeliver` (separate concern;
  access-poller backstop covers it).
- Webhook emit-once under redelivery (#438).
- The kind+Calico cluster acceptance test for dead-lettering (#347 Plan 4) — this issue is its
  production-code prerequisite, not the test itself.
- Controller render of the DLQ / advisory streams (runtime creation by the monolith matches the
  `TMI_RESULTS` precedent).

## Follow-ups to file

- tmi-ux: localize the new `extraction_dead_lettered` reason code.
