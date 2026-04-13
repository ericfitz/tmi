# Automatic Embedding Cleanup for Idle Threat Models

**Issue:** [#250](https://github.com/ericfitz/tmi/issues/250)
**Date:** 2026-04-12
**Status:** Design approved

## Problem

Embeddings stored in the `timmy_embeddings` table consume significant database storage. Threat models that are no longer actively used still retain their embeddings indefinitely. We need to automatically reclaim this storage by deleting embeddings for idle threat models.

## Idle Thresholds

- **Active/open threat models**: Embeddings deleted after **30 days** of no access (configurable)
- **Closed threat models**: Embeddings deleted after **7 days** of no access (configurable)

"Idle" means no authenticated user has accessed the threat model or any of its sub-resources.

## Approach

In-memory debounced access tracking in the request middleware, combined with a periodic background cleanup job. Embeddings are not automatically regenerated when a cleaned-up threat model is accessed again; re-indexing happens through the normal Timmy ingestion flow.

## Design

### 1. Database Schema Change

Add a `LastAccessedAt` column to the `threat_models` table.

**Model change** in `api/models/models.go` on the `ThreatModel` struct:

- Field: `LastAccessedAt *time.Time`
- GORM tag: `gorm:"index:idx_tm_last_accessed_at"`
- Nullable; `NULL` for existing rows

No new tables. No changes to the OpenAPI spec or API responses. GORM AutoMigrate handles the column addition.

For rows where `last_accessed_at` is `NULL`, the cleanup job falls back to `modified_at` to determine idle duration.

### 2. Last Access Time Tracking

**Interception point**: `ThreatModelMiddleware` in `api/middleware.go`, after authorization succeeds (around line 415), before `c.Next()`. At this point the threat model ID is resolved and access is confirmed.

**Debounce mechanism** in a new file `api/access_tracker.go`:

- Package-level `sync.Map` keyed by threat model ID, storing `time.Time` of last DB write
- On each authorized request:
  1. Check the map; if entry exists and is less than 1 minute old, skip
  2. Otherwise, fire a goroutine: `UPDATE threat_models SET last_accessed_at = NOW() WHERE id = ?`
  3. Update the map entry to `time.Now()`

**Characteristics:**

- Fire-and-forget goroutine: zero added latency on the request path
- Errors logged at `Error` level but do not affect the response
- `sync.Map` has no eviction; ~40 bytes per entry (UUID string + time.Time), so 100K threat models = ~4MB
- On server restart, map resets; worst case is one extra DB write per TM on first post-restart access
- Multi-instance: each instance debounces independently; a TM accessed on N instances within the same minute gets at most N DB writes, which is harmless

### 3. Embedding Cleanup Job

New file `api/embedding_cleaner.go` with an `EmbeddingCleaner` struct following the `AccessPoller` pattern from `api/access_poller.go`.

**Struct fields:**

```
EmbeddingCleaner {
    embeddingStore   TimmyEmbeddingStore
    db               *gorm.DB
    interval         time.Duration        // default: 1 hour
    activeIdleDays   int                  // default: 30
    closedIdleDays   int                  // default: 7
    stopCh           chan struct{}
}
```

**Cleanup logic** (runs every `interval`):

1. Single SQL query joins `threat_models` with a subquery checking for existence in `timmy_embeddings`, returning only threat models that:
   - Have at least one embedding, AND
   - Meet the idle criteria:
     - **Closed + idle**: `status = 'closed' AND COALESCE(last_accessed_at, modified_at) < NOW() - closedIdleDays`
     - **Active + idle**: `(status IS NULL OR status != 'closed') AND COALESCE(last_accessed_at, modified_at) < NOW() - activeIdleDays`
2. For each candidate, call `embeddingStore.DeleteByThreatModel(threatModelID)`
3. Log each deletion at `Info` level: threat model ID, count deleted, idle duration, status

**Logging behavior:**

- No embeddings deleted in a cycle: **no log entry** (silent)
- Embeddings deleted: one `Info` log per threat model cleaned
- Errors: `Error` level
- Startup/shutdown: `Debug` level only

**Lifecycle:**

- `NewEmbeddingCleaner(...)` constructor
- `Start()` spawns goroutine with `time.NewTicker`
- `Stop()` closes `stopCh`
- Initialized in main server startup (**unconditionally**, not gated on Timmy being enabled), so that embeddings are cleaned up even if Timmy is later disabled
- Added to the graceful shutdown sequence alongside other workers

**Disable behavior:** Setting cleanup interval to 0 prevents the cleaner from starting.

### 4. Configuration

Three new fields added to `TimmyConfig` in `internal/config/timmy.go`:

| Field | YAML key | Env var | Default | Description |
|-------|----------|---------|---------|-------------|
| `EmbeddingCleanupIntervalMinutes` | `embedding_cleanup_interval_minutes` | `TMI_TIMMY_EMBEDDING_CLEANUP_INTERVAL_MINUTES` | `60` | How often the cleanup job runs (0 = disabled) |
| `EmbeddingIdleDaysActive` | `embedding_idle_days_active` | `TMI_TIMMY_EMBEDDING_IDLE_DAYS_ACTIVE` | `30` | Days of inactivity before active TM embeddings are deleted |
| `EmbeddingIdleDaysClosed` | `embedding_idle_days_closed` | `TMI_TIMMY_EMBEDDING_IDLE_DAYS_CLOSED` | `7` | Days of inactivity before closed TM embeddings are deleted |

Defaults set in `DefaultTimmyConfig()`. These live in `TimmyConfig` because they govern embedding behavior.

### 5. Re-activation Behavior

When a previously cleaned threat model is accessed again, embeddings are **not** automatically regenerated. The user must re-index through the normal Timmy ingestion flow. This keeps the cleanup feature simple and decoupled from the ingestion pipeline.

## Files Changed

| File | Change |
|------|--------|
| `api/models/models.go` | Add `LastAccessedAt *time.Time` field to `ThreatModel` struct |
| `api/middleware.go` | Add access tracking call after authorization succeeds |
| `api/access_tracker.go` | **New** -- `sync.Map` debounce + async DB update |
| `api/embedding_cleaner.go` | **New** -- `EmbeddingCleaner` with Start/Stop/run/cleanOnce |
| `api/embedding_cleaner_test.go` | **New** -- Cleanup logic unit tests |
| `api/access_tracker_test.go` | **New** -- Debounce behavior unit tests |
| `internal/config/timmy.go` | Add 3 config fields + defaults |
| `cmd/server/main.go` | Initialize `EmbeddingCleaner` unconditionally, add to graceful shutdown |

**Not changed**: OpenAPI spec, API responses, Redis/cache layer, embedding store interface.

## Testing

### Cleanup logic tests (`api/embedding_cleaner_test.go`)

1. Idle active TM gets cleaned -- no access for 31 days, status nil, has embeddings -> deleted
2. Idle closed TM gets cleaned sooner -- status "closed", no access for 8 days -> deleted
3. Recently accessed TM preserved -- accessed 1 day ago -> embeddings kept
4. TM with no embeddings skipped -- no errors, no log output
5. Fallback to modified_at (old) -- `last_accessed_at` NULL, `modified_at` 35 days ago -> deleted
6. Fallback to modified_at (recent) -- `last_accessed_at` NULL, `modified_at` 2 days ago -> preserved

### Debounce tests (`api/access_tracker_test.go`)

1. First access updates DB -- access a TM -> `last_accessed_at` is set
2. Rapid access debounces -- two accesses within 1 minute -> one DB write
3. Access after debounce window -- access, wait >1 min, access again -> two DB writes
