---
name: oracle-db-admin
description: Dispatch the oracle-db-admin subagent to review changes that can affect Oracle Database compatibility. Use whenever the working set includes migrations (auth/migrations/**), GORM models or struct tags affecting columns/indexes/constraints, repository/store code, raw SQL via db.Raw/db.Exec, transaction or locking patterns, JSON/CLOB handling, foreign-key or cascade design, retry/error-classification code (internal/dberrors/), or schema-affecting config. Invoke BEFORE reporting the change as complete; the subagent's verdict (APPROVED, APPROVED WITH NOTES, or BLOCKING ISSUES) must be addressed.
---

# Oracle DB Admin — Trigger Skill

This skill exists to make sure the `oracle-db-admin` subagent reviews any database-touching change before it is finalized. The subagent contains the deep Oracle expertise; this skill is the trigger logic.

## When to dispatch

Dispatch the subagent if the change includes ANY of:

| Category | Examples |
|---|---|
| Migrations | New or edited files under `auth/migrations/` |
| GORM models | Struct tag changes that affect column type, size, nullability, default, index, FK, or constraint |
| Repository / store code | New or edited files matching `*_repository.go`, `*_store_gorm.go`, anything in `auth/repository/` |
| Raw SQL | Any `db.Raw(...)`, `db.Exec(...)`, `gorm.Expr(...)`, or string-built queries |
| Transactions / locking | Use of `BEGIN`, `COMMIT`, `WithRetryableGormTransaction`, `Clauses(clause.Locking{...})`, isolation level changes |
| Cascades / FKs | Any FK addition/removal, `ON DELETE` clause, soft-vs-hard FK decision, new parent/child table relationship |
| JSON / CLOB | Columns storing JSON, large text, or BLOB data; JSON path queries |
| Retry & error handling | Changes to `internal/dberrors/`, `Classify()`, error sentinel definitions, retry policy |
| Connection pooling | Changes to pool config, `MaxOpenConns`, `MaxIdleConns`, idle timeout, ADB wallet setup |
| Schema config | `oapi-codegen` schema changes that drive DB columns, GORM `AutoMigrate` invocations |

If unsure whether a change qualifies, dispatch anyway. The subagent will return APPROVED quickly for non-issues; the cost of dispatching is low, the cost of skipping a real Oracle problem is high (we will have to fix it later).

## When NOT to dispatch

- Pure business logic, HTTP handler routing, frontend, or test fixtures that don't touch DB code.
- Pure test changes that exercise existing DB code without modifying it.
- Documentation-only changes.
- Generated code (`api/api.go`) — even when it lands as part of the diff, the subagent should review the *source* OpenAPI/migration changes that drove it, not the regenerated file.

## How to dispatch

Use the `Agent` tool with `subagent_type: "oracle-db-admin"`. The subagent starts with no conversation context — give it everything it needs in the prompt.

**Required prompt elements:**

1. **What changed.** List the specific files (with line ranges if possible) the subagent should review. If the change is staged or committed, mention the git ref so it can run `git diff` itself.
2. **Why.** One or two sentences on the goal of the change. (e.g., "Migrating threat-model sub-resource stores to the repository pattern per #272.")
3. **Specific concerns, if any.** If you already suspect an Oracle issue, name it — the subagent should still walk its full checklist, but knowing your concern lets it answer directly.
4. **What you've verified.** If you've already confirmed something is safe (e.g., "no new FKs, no raw SQL, identifier lengths checked"), say so. The subagent will spot-check rather than re-derive.

**Example dispatch prompt:**

```
Review this change for Oracle Database compatibility.

What changed:
- api/asset_repository.go (new file, ~250 lines)
- api/threat_repository.go (new file, ~280 lines)
- api/asset_sub_resource_handlers.go:140-220 (updated to use repository + errors.Is)
- internal/dberrors/sentinels.go (added ErrAssetNotFound, ErrThreatNotFound)

Why: Migrating threat-model sub-resource stores to the repository pattern per #272.
This is the first sub-issue under umbrella #271, establishing patterns for #273-#279.

Specific concerns: New typed error sentinels need to be classified correctly for both
Oracle and PG error codes. Verify the cascade chain from threat_models down through
assets/threats/documents/repositories/notes is unchanged.

What I verified: No new migrations, no raw SQL, no new FK definitions. Identifiers
all under 30 bytes. ON CONFLICT clauses unchanged from existing reference impls.
```

Dispatch in the **foreground** (not background) — you need the verdict before reporting the change as complete.

## Acting on the verdict

The subagent returns one of three verdicts:

- **`VERDICT: APPROVED`** — proceed. Note the verdict in your end-of-task summary.
- **`VERDICT: APPROVED WITH NOTES`** — proceed, but read the notes. If any are easy fixes worth doing now, do them. Otherwise, file follow-up issues so the notes don't get lost.
- **`VERDICT: BLOCKING ISSUES`** — do not finalize. Each blocking item must be fixed (or explicitly waived by the user with reasoning). Re-dispatch after fixes for a clean re-review only if the changes are non-trivial; for small fixes, just confirm the fix addresses the stated finding.

**Do not argue with the verdict in your own head.** The subagent is the deep Oracle expert; you are the orchestrator. If a finding seems wrong, ask the user to adjudicate — do not dismiss it silently. We will have to fix Oracle bugs eventually, so the cheap path is to listen now.

## Cost / latency notes

The subagent uses Opus and reads several reference files plus the diff. Expect ~30-90 seconds per review for a typical sub-issue-sized change. For very small changes (single-column tweak, error sentinel addition), it may finish in under 30 seconds. This is acceptable overhead for any DB change.
