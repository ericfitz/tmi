# ADR: SERIALIZABLE stays the transaction default; provably insert-only call sites opt down to READ COMMITTED

- **Status:** Accepted (human decision by Eric Fitzgerald, 2026-09-17)
- **Deciders:** Eric Fitzgerald
- **Context recorded by:** Claude (tmi session, issue #906)
- **Related:** #451 / #448 / #449 (SERIALIZABLE default and routing every transaction through the retry
  wrapper), #801, #900, #903 (per-site opt-downs), #906 (this decision), #783 (INITRANS)
- **Amends:** #451. It does not reverse it.

## Context

### What #451 decided

Before #451 every transaction ran at the database default, READ COMMITTED, which permits lost updates
and write skew. #451 made `auth/db.WithRetryableGormTransaction` / `WithRetryableTransaction` request
SERIALIZABLE unless the caller explicitly opts down, and #449 routed every transaction site through
those wrappers, for two reasons: under SERIALIZABLE any transaction can abort with a serialization
failure (PostgreSQL `40001`, Oracle `ORA-08177`) and must be retried, and PostgreSQL SSI only
guarantees serializability among transactions that are all serializable. #451 already recorded that
Oracle's SERIALIZABLE is snapshot isolation: it does not detect write skew, so cross-row invariants
on Oracle need `SELECT ... FOR UPDATE` or unique constraints regardless.

The implicit assumption was that a serialization failure signals real contention, so a retry cures it.

### What #903 measured

On Oracle Autonomous Database that assumption is false. With one session, no concurrent writer, and
INITRANS already at 10 (tables) / 20 (indexes), 120 back-to-back threat model creates under
SERIALIZABLE produced 20 false ORA-08177 and 2 creates exhausted all three attempts (HTTP 503).
Per-transaction `V$SYSSTAT` deltas tie the failures to `leaf node splits` and
`commit txn count during cleanout`: a recursive transaction (index leaf split, probably also ASSM/LOB
space allocation) commits past the snapshot SCN and trips Oracle's block-level serializable check, and
delayed block cleanout carries the failure into the next attempt. Raising INITRANS does not help. At
READ COMMITTED the same loop produced zero failures.

So on ADB, ORA-08177 is not a reliable contention signal, a three-attempt retry is not a sufficient
mitigation, and every transaction that inserts into indexed tables carries a latent 503. Three sites
had already been opted down one at a time, each only after failing (#801, #900, #903).

## Options considered

1. **Keep the #451 default and keep opting sites down as they fail.** No up-front work; every
   remaining insert-only site stays a latent production 503 until someone hits it.
2. **Invert the default** (READ COMMITTED default, SERIALIZABLE opt-in for read-then-write closures).
   One change instead of N, but a misclassified or future site fails *silently* as a data anomaly,
   and PostgreSQL SSI stops catching real bugs in development.
3. **Keep the #451 default and proactively opt down every call site that provably gains nothing from
   SERIALIZABLE.** A misclassified site still fails loudly (a spurious 503), never silently.

## Decision

**Option 3.** SERIALIZABLE remains the wrapper default. A call site may opt down to READ COMMITTED
only when all of the following hold, and the justification is written in a comment at the site:

1. Every write in the closure is an INSERT of rows whose identity is generated in the same request
   (a fresh UUID, or child rows keyed by such a fresh parent).
2. Reads in the closure only resolve identifiers or foreign keys for those inserts, with integrity
   enforced by FK or unique constraints. No decision that a write relies on (existence, uniqueness,
   quota, limit, count, max+1, ownership, authorization) is derived from a read.
3. The only permitted non-insert writes are a sequence draw, a row-locked
   (`SELECT ... FOR UPDATE`) read-then-increment, or a single-statement atomic expression
   update / upsert with duplicate-race recovery. These are correct at READ COMMITTED on both
   databases.

Anything else keeps the default: UPDATE or DELETE of existing rows, delete-then-insert replacement,
optimistic-lock version CAS, check-then-insert, state transitions, ACL writes, multi-row invariants.
When in doubt, keep SERIALIZABLE.

Every opt-down is a DB-touching change and goes through the `oracle-db-admin` review.

## Consequences

- Insert-only paths stop producing false ORA-08177 / 503 on ADB.
- The failure mode of a wrong classification stays a visible 503, not a silent anomaly, because the
  default did not move.
- New transaction sites still start SERIALIZABLE; opting down is a deliberate, reviewed act.
- Read-then-write sites keep SERIALIZABLE and can still see false ORA-08177 on ADB. They are retried;
  if one proves to exhaust retries in practice, address it at that site (lock-based design that is
  correct at READ COMMITTED, or a larger retry budget), not by moving the default.
- The inventory of call sites and their classification at the time of this decision is recorded in
  the pull request that implements #906.
