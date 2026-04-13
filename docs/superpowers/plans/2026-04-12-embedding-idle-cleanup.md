# Embedding Idle Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically delete embeddings from threat models that haven't been accessed in a configurable period, reclaiming database storage.

**Architecture:** Middleware-level access tracking with in-memory debounce writes `last_accessed_at` on the threat model row. A background goroutine (EmbeddingCleaner) runs hourly, queries for idle threat models that have embeddings, and deletes them. The cleaner runs unconditionally (not gated on Timmy being enabled) so it cleans up after Timmy is disabled.

**Tech Stack:** Go, GORM, sync.Map, time.Ticker, SQLite (tests)

---

### Task 1: Add `LastAccessedAt` Field to ThreatModel

**Files:**
- Modify: `api/models/models.go:121-147` (ThreatModel struct)

- [ ] **Step 1: Add the field to the ThreatModel struct**

In `api/models/models.go`, add `LastAccessedAt` between `DeletedAt` (line 137) and the `// Relationships` comment (line 139):

```go
	DeletedAt                    *time.Time  `gorm:"index:idx_tm_deleted_at"`
	LastAccessedAt               *time.Time  `gorm:"index:idx_tm_last_accessed_at"`

	// Relationships
```

- [ ] **Step 2: Verify build succeeds**

Run: `make build-server`
Expected: Clean build, no errors.

- [ ] **Step 3: Commit**

```bash
git add api/models/models.go
git commit -m "feat(api): add LastAccessedAt field to ThreatModel

Adds a nullable, indexed last_accessed_at column to track when a
threat model was last accessed. GORM AutoMigrate will add the column
on next server start. Part of #250."
```

---

### Task 2: Add Configuration Fields

**Files:**
- Modify: `internal/config/timmy.go:4-55` (TimmyConfig struct + defaults)

- [ ] **Step 1: Add fields to TimmyConfig struct**

In `internal/config/timmy.go`, add these three fields after line 35 (`LLMTimeoutSeconds`), before the closing brace of the struct:

```go
	LLMTimeoutSeconds         int    `yaml:"llm_timeout_seconds" env:"TMI_TIMMY_LLM_TIMEOUT_SECONDS"`
	EmbeddingCleanupIntervalMinutes int `yaml:"embedding_cleanup_interval_minutes" env:"TMI_TIMMY_EMBEDDING_CLEANUP_INTERVAL_MINUTES"`
	EmbeddingIdleDaysActive         int `yaml:"embedding_idle_days_active" env:"TMI_TIMMY_EMBEDDING_IDLE_DAYS_ACTIVE"`
	EmbeddingIdleDaysClosed         int `yaml:"embedding_idle_days_closed" env:"TMI_TIMMY_EMBEDDING_IDLE_DAYS_CLOSED"`
}
```

- [ ] **Step 2: Add defaults in DefaultTimmyConfig()**

In `internal/config/timmy.go`, add defaults inside `DefaultTimmyConfig()` after `LLMTimeoutSeconds: 120,` (line 53):

```go
		LLMTimeoutSeconds:               120,
		EmbeddingCleanupIntervalMinutes: 60,
		EmbeddingIdleDaysActive:         30,
		EmbeddingIdleDaysClosed:         7,
	}
```

- [ ] **Step 3: Verify build succeeds**

Run: `make build-server`
Expected: Clean build, no errors.

- [ ] **Step 4: Run existing config tests**

Run: `make test-unit name=TestTimmyConfig`
Expected: All existing TimmyConfig tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/timmy.go
git commit -m "feat(config): add embedding cleanup configuration fields

Adds EmbeddingCleanupIntervalMinutes (default 60), EmbeddingIdleDaysActive
(default 30), and EmbeddingIdleDaysClosed (default 7) to TimmyConfig.
Setting interval to 0 disables the cleaner. Part of #250."
```

---

### Task 3: Create Access Tracker with Debounce

**Files:**
- Create: `api/access_tracker.go`
- Create: `api/access_tracker_test.go`

- [ ] **Step 1: Write the failing tests**

Create `api/access_tracker_test.go`:

```go
package api

import (
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupAccessTrackerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ThreatModel{}))
	return db
}

func createTestThreatModel(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	tm := models.ThreatModel{
		ID:                    id,
		OwnerInternalUUID:     "owner-uuid",
		Name:                  "Test TM " + id,
		CreatedByInternalUUID: "creator-uuid",
	}
	require.NoError(t, db.Create(&tm).Error)
}

func TestAccessTracker_FirstAccessUpdatesDB(t *testing.T) {
	db := setupAccessTrackerTestDB(t)
	tracker := NewAccessTracker(db)
	defer tracker.Reset()

	tmID := "tm-first-access-001"
	createTestThreatModel(t, db, tmID)

	tracker.RecordAccess(tmID)
	// Give the async goroutine time to complete
	time.Sleep(100 * time.Millisecond)

	var tm models.ThreatModel
	require.NoError(t, db.First(&tm, "id = ?", tmID).Error)
	assert.NotNil(t, tm.LastAccessedAt, "LastAccessedAt should be set after first access")
}

func TestAccessTracker_RapidAccessDebounces(t *testing.T) {
	db := setupAccessTrackerTestDB(t)
	tracker := NewAccessTracker(db)
	defer tracker.Reset()

	tmID := "tm-debounce-001"
	createTestThreatModel(t, db, tmID)

	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	// Record the first write time
	var tm1 models.ThreatModel
	require.NoError(t, db.First(&tm1, "id = ?", tmID).Error)
	require.NotNil(t, tm1.LastAccessedAt)
	firstWrite := *tm1.LastAccessedAt

	// Second access within debounce window should not update
	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	var tm2 models.ThreatModel
	require.NoError(t, db.First(&tm2, "id = ?", tmID).Error)
	require.NotNil(t, tm2.LastAccessedAt)
	assert.Equal(t, firstWrite, *tm2.LastAccessedAt, "LastAccessedAt should not change within debounce window")
}

func TestAccessTracker_AccessAfterDebounceWindowWritesAgain(t *testing.T) {
	db := setupAccessTrackerTestDB(t)
	// Use a short debounce for testing
	tracker := NewAccessTrackerWithDebounce(db, 50*time.Millisecond)
	defer tracker.Reset()

	tmID := "tm-after-debounce-001"
	createTestThreatModel(t, db, tmID)

	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	var tm1 models.ThreatModel
	require.NoError(t, db.First(&tm1, "id = ?", tmID).Error)
	require.NotNil(t, tm1.LastAccessedAt)
	firstWrite := *tm1.LastAccessedAt

	// Wait for debounce window to expire
	time.Sleep(100 * time.Millisecond)

	// Second access after debounce window should update
	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	var tm2 models.ThreatModel
	require.NoError(t, db.First(&tm2, "id = ?", tmID).Error)
	require.NotNil(t, tm2.LastAccessedAt)
	assert.True(t, tm2.LastAccessedAt.After(firstWrite), "LastAccessedAt should be updated after debounce window expires")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestAccessTracker`
Expected: FAIL — `NewAccessTracker` and related functions not defined.

- [ ] **Step 3: Write the implementation**

Create `api/access_tracker.go`:

```go
package api

import (
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

const defaultDebounceDuration = 1 * time.Minute

// AccessTracker records threat model access times with in-memory debouncing.
// It uses a sync.Map to avoid writing to the database more than once per
// debounce window per threat model.
type AccessTracker struct {
	db               *gorm.DB
	debounceDuration time.Duration
	lastWriteTimes   sync.Map // map[string]time.Time
}

// NewAccessTracker creates an AccessTracker with the default 1-minute debounce.
func NewAccessTracker(db *gorm.DB) *AccessTracker {
	return &AccessTracker{
		db:               db,
		debounceDuration: defaultDebounceDuration,
	}
}

// NewAccessTrackerWithDebounce creates an AccessTracker with a custom debounce duration (for testing).
func NewAccessTrackerWithDebounce(db *gorm.DB, debounce time.Duration) *AccessTracker {
	return &AccessTracker{
		db:               db,
		debounceDuration: debounce,
	}
}

// RecordAccess updates last_accessed_at for a threat model, debouncing writes.
// The DB update runs in a fire-and-forget goroutine to avoid adding latency.
func (at *AccessTracker) RecordAccess(threatModelID string) {
	now := time.Now()

	// Check debounce: skip if we wrote recently
	if lastWrite, ok := at.lastWriteTimes.Load(threatModelID); ok {
		if now.Sub(lastWrite.(time.Time)) < at.debounceDuration {
			return
		}
	}

	// Update debounce map immediately (before goroutine) to prevent duplicates
	at.lastWriteTimes.Store(threatModelID, now)

	go func() {
		logger := slogging.Get()
		result := at.db.Table("threat_models").
			Where("id = ?", threatModelID).
			Update("last_accessed_at", now)
		if result.Error != nil {
			logger.Error("AccessTracker: failed to update last_accessed_at for %s: %v", threatModelID, result.Error)
		}
	}()
}

// Reset clears the debounce map. Used in tests.
func (at *AccessTracker) Reset() {
	at.lastWriteTimes = sync.Map{}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestAccessTracker`
Expected: All 3 tests pass.

- [ ] **Step 5: Run lint**

Run: `make lint`
Expected: No new lint issues.

- [ ] **Step 6: Commit**

```bash
git add api/access_tracker.go api/access_tracker_test.go
git commit -m "feat(api): add AccessTracker with in-memory debounce

Records last_accessed_at on threat models via async DB writes,
debouncing to at most once per minute per threat model.
Includes unit tests for first access, debounce, and post-window
writes. Part of #250."
```

---

### Task 4: Wire Access Tracker into Middleware

**Files:**
- Modify: `api/middleware.go:415` (ThreatModelMiddleware)

- [ ] **Step 1: Add a package-level AccessTracker variable**

In `api/access_tracker.go`, add a global variable at the top of the file (after the imports, before the struct definition):

```go
// GlobalAccessTracker is initialized at server startup and used by ThreatModelMiddleware.
var GlobalAccessTracker *AccessTracker
```

- [ ] **Step 2: Add access tracking call in ThreatModelMiddleware**

In `api/middleware.go`, after line 415 (`logger.Debug("Access granted for user %s with role %s", userEmail, userRole)`) and before line 417 (`c.Next()`), add:

```go
		logger.Debug("Access granted for user %s with role %s", userEmail, userRole)

		// Record access for embedding idle cleanup (#250)
		if GlobalAccessTracker != nil {
			GlobalAccessTracker.RecordAccess(id)
		}

		c.Next()
```

- [ ] **Step 3: Verify build succeeds**

Run: `make build-server`
Expected: Clean build.

- [ ] **Step 4: Run existing middleware tests**

Run: `make test-unit name=TestThreatModel`
Expected: All existing middleware tests still pass. (GlobalAccessTracker is nil, so the nil check skips it safely.)

- [ ] **Step 5: Commit**

```bash
git add api/access_tracker.go api/middleware.go
git commit -m "feat(api): wire AccessTracker into ThreatModelMiddleware

Records last_accessed_at for every authorized threat model request.
Guarded by nil check so it's safe when the tracker isn't initialized.
Part of #250."
```

---

### Task 5: Create Embedding Cleaner

**Files:**
- Create: `api/embedding_cleaner.go`
- Create: `api/embedding_cleaner_test.go`

- [ ] **Step 1: Write the failing tests**

Create `api/embedding_cleaner_test.go`:

```go
package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupCleanerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.ThreatModel{},
		&models.TimmyEmbedding{},
	))
	return db
}

func createTestTM(t *testing.T, db *gorm.DB, id string, status *string, lastAccessed *time.Time, modifiedAt time.Time) {
	t.Helper()
	tm := models.ThreatModel{
		ID:                    id,
		OwnerInternalUUID:     "owner-uuid",
		Name:                  "Test TM " + id,
		CreatedByInternalUUID: "creator-uuid",
		Status:                status,
		LastAccessedAt:        lastAccessed,
	}
	require.NoError(t, db.Create(&tm).Error)
	// Override ModifiedAt (GORM autoUpdateTime sets it on create)
	require.NoError(t, db.Model(&models.ThreatModel{}).Where("id = ?", id).
		Update("modified_at", modifiedAt).Error)
}

func createTestEmbedding(t *testing.T, db *gorm.DB, tmID string, entityID string) {
	t.Helper()
	emb := models.TimmyEmbedding{
		ID:             "emb-" + tmID + "-" + entityID,
		ThreatModelID:  tmID,
		EntityType:     "document",
		EntityID:       entityID,
		ChunkIndex:     0,
		IndexType:      "text",
		ContentHash:    "hash-" + entityID,
		EmbeddingModel: "test-model",
		EmbeddingDim:   384,
		ChunkText:      "test chunk text",
	}
	require.NoError(t, db.Create(&emb).Error)
}

func countEmbeddings(t *testing.T, db *gorm.DB, tmID string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&models.TimmyEmbedding{}).
		Where("threat_model_id = ?", tmID).Count(&count).Error)
	return count
}

func TestEmbeddingCleaner_IdleActiveTMGetsCleaned(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	thirtyOneDaysAgo := time.Now().Add(-31 * 24 * time.Hour)
	createTestTM(t, db, "tm-idle-active", nil, &thirtyOneDaysAgo, thirtyOneDaysAgo)
	createTestEmbedding(t, db, "tm-idle-active", "doc-1")
	createTestEmbedding(t, db, "tm-idle-active", "doc-2")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(2), deleted)
	assert.Equal(t, int64(0), countEmbeddings(t, db, "tm-idle-active"))
}

func TestEmbeddingCleaner_IdleClosedTMGetsCleanedSooner(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	closed := "closed"
	eightDaysAgo := time.Now().Add(-8 * 24 * time.Hour)
	createTestTM(t, db, "tm-idle-closed", &closed, &eightDaysAgo, eightDaysAgo)
	createTestEmbedding(t, db, "tm-idle-closed", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(1), deleted)
	assert.Equal(t, int64(0), countEmbeddings(t, db, "tm-idle-closed"))
}

func TestEmbeddingCleaner_RecentlyAccessedTMPreserved(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	oneDayAgo := time.Now().Add(-1 * 24 * time.Hour)
	createTestTM(t, db, "tm-recent", nil, &oneDayAgo, oneDayAgo)
	createTestEmbedding(t, db, "tm-recent", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
	assert.Equal(t, int64(1), countEmbeddings(t, db, "tm-recent"))
}

func TestEmbeddingCleaner_TMWithNoEmbeddingsSkipped(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	thirtyOneDaysAgo := time.Now().Add(-31 * 24 * time.Hour)
	createTestTM(t, db, "tm-no-embeddings", nil, &thirtyOneDaysAgo, thirtyOneDaysAgo)

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
}

func TestEmbeddingCleaner_FallbackToModifiedAt_Old(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	thirtyFiveDaysAgo := time.Now().Add(-35 * 24 * time.Hour)
	// last_accessed_at is nil, should fall back to modified_at
	createTestTM(t, db, "tm-fallback-old", nil, nil, thirtyFiveDaysAgo)
	createTestEmbedding(t, db, "tm-fallback-old", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(1), deleted)
	assert.Equal(t, int64(0), countEmbeddings(t, db, "tm-fallback-old"))
}

func TestEmbeddingCleaner_FallbackToModifiedAt_Recent(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	twoDaysAgo := time.Now().Add(-2 * 24 * time.Hour)
	// last_accessed_at is nil, modified_at is recent
	createTestTM(t, db, "tm-fallback-recent", nil, nil, twoDaysAgo)
	createTestEmbedding(t, db, "tm-fallback-recent", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
	assert.Equal(t, int64(1), countEmbeddings(t, db, "tm-fallback-recent"))
}

func TestEmbeddingCleaner_ClosedTMNotIdleLongEnough(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	closed := "closed"
	threeDaysAgo := time.Now().Add(-3 * 24 * time.Hour)
	createTestTM(t, db, "tm-closed-recent", &closed, &threeDaysAgo, threeDaysAgo)
	createTestEmbedding(t, db, "tm-closed-recent", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
	assert.Equal(t, int64(1), countEmbeddings(t, db, "tm-closed-recent"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestEmbeddingCleaner`
Expected: FAIL — `NewEmbeddingCleaner` and `CleanOnce` not defined.

- [ ] **Step 3: Write the implementation**

Create `api/embedding_cleaner.go`:

```go
package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// EmbeddingCleaner periodically deletes embeddings from idle threat models.
type EmbeddingCleaner struct {
	embeddingStore TimmyEmbeddingStore
	db             *gorm.DB
	interval       time.Duration
	activeIdleDays int
	closedIdleDays int
	stopCh         chan struct{}
}

// idleThreatModel holds the result of the idle TM query.
type idleThreatModel struct {
	ID     string  `gorm:"column:id"`
	Status *string `gorm:"column:status"`
}

// NewEmbeddingCleaner creates a new embedding cleaner.
func NewEmbeddingCleaner(
	embeddingStore TimmyEmbeddingStore,
	db *gorm.DB,
	interval time.Duration,
	activeIdleDays int,
	closedIdleDays int,
) *EmbeddingCleaner {
	return &EmbeddingCleaner{
		embeddingStore: embeddingStore,
		db:             db,
		interval:       interval,
		activeIdleDays: activeIdleDays,
		closedIdleDays: closedIdleDays,
		stopCh:         make(chan struct{}),
	}
}

// Start begins the background cleanup loop.
func (ec *EmbeddingCleaner) Start() {
	go ec.run()
}

// Stop signals the cleaner to stop.
func (ec *EmbeddingCleaner) Stop() {
	close(ec.stopCh)
}

func (ec *EmbeddingCleaner) run() {
	logger := slogging.Get()
	logger.Debug("EmbeddingCleaner: started (interval=%s, activeIdleDays=%d, closedIdleDays=%d)",
		ec.interval, ec.activeIdleDays, ec.closedIdleDays)

	ticker := time.NewTicker(ec.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ec.stopCh:
			logger.Debug("EmbeddingCleaner: stopped")
			return
		case <-ticker.C:
			ec.CleanOnce()
		}
	}
}

// CleanOnce runs a single cleanup cycle. Returns total embeddings deleted.
func (ec *EmbeddingCleaner) CleanOnce() int64 {
	logger := slogging.Get()
	ctx := context.Background()

	candidates, err := ec.findIdleThreatModels()
	if err != nil {
		logger.Error("EmbeddingCleaner: failed to query idle threat models: %v", err)
		return 0
	}

	var totalDeleted int64
	for _, tm := range candidates {
		deleted, delErr := ec.embeddingStore.DeleteByThreatModel(ctx, tm.ID)
		if delErr != nil {
			logger.Error("EmbeddingCleaner: failed to delete embeddings for threat model %s: %v", tm.ID, delErr)
			continue
		}
		if deleted > 0 {
			status := "active"
			if tm.Status != nil && *tm.Status == "closed" {
				status = "closed"
			}
			logger.Info("EmbeddingCleaner: deleted %d embeddings for idle %s threat model %s", deleted, status, tm.ID)
			totalDeleted += deleted
		}
	}

	return totalDeleted
}

// findIdleThreatModels queries for threat models that have embeddings and are idle.
// Uses COALESCE(last_accessed_at, modified_at) to determine the effective last activity time.
func (ec *EmbeddingCleaner) findIdleThreatModels() ([]idleThreatModel, error) {
	var results []idleThreatModel

	activeThreshold := time.Now().Add(-time.Duration(ec.activeIdleDays) * 24 * time.Hour)
	closedThreshold := time.Now().Add(-time.Duration(ec.closedIdleDays) * 24 * time.Hour)

	// Query: find threat models that have at least one embedding AND are idle.
	// Two conditions ORed together:
	//   1. Closed TMs idle longer than closedIdleDays
	//   2. Non-closed TMs idle longer than activeIdleDays
	err := ec.db.Table("threat_models").
		Select("threat_models.id, threat_models.status").
		Where("threat_models.deleted_at IS NULL").
		Where("EXISTS (SELECT 1 FROM timmy_embeddings WHERE timmy_embeddings.threat_model_id = threat_models.id)").
		Where(
			ec.db.Where(
				// Closed + idle
				"threat_models.status = ? AND COALESCE(threat_models.last_accessed_at, threat_models.modified_at) < ?",
				"closed", closedThreshold,
			).Or(
				// Active (non-closed) + idle
				"(threat_models.status IS NULL OR threat_models.status != ?) AND COALESCE(threat_models.last_accessed_at, threat_models.modified_at) < ?",
				"closed", activeThreshold,
			),
		).
		Find(&results).Error

	return results, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestEmbeddingCleaner`
Expected: All 7 tests pass.

- [ ] **Step 5: Run lint**

Run: `make lint`
Expected: No new lint issues.

- [ ] **Step 6: Commit**

```bash
git add api/embedding_cleaner.go api/embedding_cleaner_test.go
git commit -m "feat(api): add EmbeddingCleaner for idle threat model cleanup

Background job that deletes embeddings from threat models idle for
30 days (active) or 7 days (closed). Uses COALESCE(last_accessed_at,
modified_at) for fallback. Follows the AccessPoller pattern with
Start/Stop lifecycle. Includes 7 unit tests. Part of #250."
```

---

### Task 6: Wire Into Server Startup and Shutdown

**Files:**
- Modify: `cmd/server/main.go:886` (after initializeTimmySubsystem call)
- Modify: `cmd/server/main.go:1579-1585` (shutdown sequence)

- [ ] **Step 1: Initialize EmbeddingCleaner and AccessTracker after stores are ready**

In `cmd/server/main.go`, after line 886 (`initializeTimmySubsystem(config, apiServer)`), add the embedding cleaner and access tracker initialization:

```go
	initializeTimmySubsystem(config, apiServer)

	// Start embedding idle cleanup (runs unconditionally, even if Timmy is disabled,
	// to clean up embeddings if Timmy was previously enabled)
	var embeddingCleaner *api.EmbeddingCleaner
	if config.Timmy.EmbeddingCleanupIntervalMinutes > 0 {
		cleanupInterval := time.Duration(config.Timmy.EmbeddingCleanupIntervalMinutes) * time.Minute
		embeddingCleaner = api.NewEmbeddingCleaner(
			api.GlobalTimmyEmbeddingStore,
			gormDB.DB(),
			cleanupInterval,
			config.Timmy.EmbeddingIdleDaysActive,
			config.Timmy.EmbeddingIdleDaysClosed,
		)
		embeddingCleaner.Start()
	}

	// Initialize access tracker for last_accessed_at updates
	api.GlobalAccessTracker = api.NewAccessTracker(gormDB.DB())
```

- [ ] **Step 2: Add graceful shutdown**

In `cmd/server/main.go`, in the shutdown sequence, after the audit pruner/debouncer block (after line 1585 `api.GlobalAuditDebouncer.FlushAll()`) and before the auth shutdown (line 1587), add:

```go
	// Stop embedding cleaner
	if embeddingCleaner != nil {
		logger.Info("Stopping embedding cleaner...")
		embeddingCleaner.Stop()
	}
```

- [ ] **Step 3: Verify build succeeds**

Run: `make build-server`
Expected: Clean build, no errors.

- [ ] **Step 4: Run unit tests**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go api/access_tracker.go
git commit -m "feat(server): wire EmbeddingCleaner and AccessTracker into startup/shutdown

EmbeddingCleaner starts unconditionally after store initialization
(disabled when interval is 0). AccessTracker initialized as global
for ThreatModelMiddleware. Cleaner added to graceful shutdown
sequence. Closes #250."
```

---

### Task 7: Final Verification

- [ ] **Step 1: Full lint check**

Run: `make lint`
Expected: No new warnings (existing api/api.go warnings are acceptable per CLAUDE.md).

- [ ] **Step 2: Full unit test suite**

Run: `make test-unit`
Expected: All tests pass, including the new access_tracker and embedding_cleaner tests.

- [ ] **Step 3: Build server**

Run: `make build-server`
Expected: Clean build.
