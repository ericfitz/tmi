# ADR: Threat severity "none" means informational; "none" and "unknown" are retired

- **Status:** Accepted (human decision by Eric Fitzgerald, 2026-09-19)
- **Deciders:** Eric Fitzgerald
- **Context recorded by:** Claude (tmi session, open question in issue #926)
- **Related:** #925 (legacy value migration), #912/#910 (severity ranks), #926 (retire the migration),
  tmi-ux #946

## Context

The server mapped a stored severity of `none` to `informational` (pre-#925 behavior, kept by #925),
while tmi-ux displayed `None` as `unknown` and #912 ranked `none` with `unknown`. The two sides
disagreed about what the value meant. Separately, `unknown` was a first-class severity (rank 0 and a
value of the `severity` list filter enum) even though it records the absence of an assessment, which
the nullable `severity` field already expresses.

## Decision

1. `none` means `informational`. It is a legacy input only: it is canonicalized on write, migrated in
   storage, and ranks as `informational` until migrated.
2. `unknown` is retired as a severity. An unassessed threat has no severity (NULL). Stored `unknown`
   and the legacy numeric key `5` are cleared to NULL by the one-time migration
   (`MigrateLegacyThreatValues`, the existing idempotent boot migration), and a write of `unknown`
   is stored as unset. `unknown` is removed from the `severity` filter enum in the OpenAPI spec.
3. The canonical severities are `informational`, `low`, `medium`, `high`, `critical`. The column stays
   free-form; unset and free-form values sort below every rank.

Eric chose NULL over collapsing `unknown` into `informational` so that unassessed threats stay
distinguishable from threats deliberately rated informational.

## Consequences

- Clients must stop sending `severity=unknown` as a list filter (now a 400 from request validation)
  and should render a missing severity as unassessed. tmi-ux needs the matching change.
- The migration is not reversible: after it runs, a former `unknown` is indistinguishable from a
  severity that was never set. That is the intended meaning.
- #926 still tracks removing the boot migration and the legacy rank entries once every environment
  has run it.
