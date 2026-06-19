package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// threatModelStatusClosed is the status value for closed threat models.
const threatModelStatusClosed = "closed"

// threatModelStatusActive is the display label for non-closed threat models.
const threatModelStatusActive = "active"

// EmbeddingCleaner periodically deletes embeddings from idle threat models.
// SEM@78c94ddab0f9e346067370bad3e9d3d88bd6a99b: background service that periodically deletes embeddings from idle threat models (mutates shared state)
type EmbeddingCleaner struct {
	embeddingStore TimmyEmbeddingStore
	db             *gorm.DB
	interval       time.Duration
	activeIdleDays int
	closedIdleDays int
	stopCh         chan struct{}
}

// idleThreatModel holds the result of the idle TM query.
// SEM@5981ac53dd2229e2bb211a96f0b495fe72df5f32: query result holding a threat model ID and status for idle-embedding cleanup (pure)
type idleThreatModel struct {
	ID     string `gorm:"column:id"`
	Status string `gorm:"column:status"`
}

// NewEmbeddingCleaner creates a new embedding cleaner.
// SEM@78c94ddab0f9e346067370bad3e9d3d88bd6a99b: build an EmbeddingCleaner with configurable interval and idle-day thresholds (pure)
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
// SEM@78c94ddab0f9e346067370bad3e9d3d88bd6a99b: start the background embedding-cleanup goroutine (mutates shared state)
func (ec *EmbeddingCleaner) Start() {
	go ec.run()
}

// Stop signals the cleaner to stop.
// SEM@78c94ddab0f9e346067370bad3e9d3d88bd6a99b: signal the embedding-cleanup loop to stop (mutates shared state)
func (ec *EmbeddingCleaner) Stop() {
	close(ec.stopCh)
}

// SEM@78c94ddab0f9e346067370bad3e9d3d88bd6a99b: run the periodic embedding-cleanup ticker loop until stopped (mutates shared state)
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
// SEM@5981ac53dd2229e2bb211a96f0b495fe72df5f32: delete embeddings for all currently idle threat models and return the total count deleted (reads DB)
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
			status := threatModelStatusActive
			if tm.Status == threatModelStatusClosed {
				status = threatModelStatusClosed
			}
			logger.Info("EmbeddingCleaner: deleted %d embeddings for idle %s threat model %s", deleted, status, tm.ID)
			totalDeleted += deleted
		}
	}

	return totalDeleted
}

// findIdleThreatModels queries for threat models that have embeddings and are idle.
// Uses COALESCE(last_accessed_at, modified_at) to determine the effective last activity time.
// SEM@f8417a5cf7ccccd973f67a4a09364e8065dddf5f: query threat models that have embeddings and exceed the active or closed idle-day threshold (reads DB)
func (ec *EmbeddingCleaner) findIdleThreatModels() ([]idleThreatModel, error) {
	var results []idleThreatModel

	activeThreshold := time.Now().Add(-time.Duration(ec.activeIdleDays) * 24 * time.Hour)
	closedThreshold := time.Now().Add(-time.Duration(ec.closedIdleDays) * 24 * time.Hour)

	// Resolve table names through the helper so Oracle uppercase-mode
	// (UseUppercaseTableNames=true) works end-to-end. The raw SQL fragments
	// below reference these names directly via fmt.Sprintf so the query
	// stays portable across PG and Oracle.
	tmTable := models.ThreatModel{}.TableName()
	embTable := models.TimmyEmbedding{}.TableName()

	// Query: find threat models that have at least one embedding AND are idle.
	// Two conditions ORed together:
	//   1. Closed TMs idle longer than closedIdleDays
	//   2. Non-closed TMs idle longer than activeIdleDays
	err := ec.db.Table(tmTable).
		Select(fmt.Sprintf("%s.id, %s.status", tmTable, tmTable)).
		Where(fmt.Sprintf("%s.deleted_at IS NULL", tmTable)).
		Where(fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s.threat_model_id = %s.id)", embTable, embTable, tmTable)).
		Where(
			ec.db.Where(
				// Closed + idle
				fmt.Sprintf("%s.status = ? AND COALESCE(%s.last_accessed_at, %s.modified_at) < ?", tmTable, tmTable, tmTable),
				threatModelStatusClosed, closedThreshold,
			).Or(
				// Active (non-closed) + idle
				fmt.Sprintf("(%s.status IS NULL OR %s.status != ?) AND COALESCE(%s.last_accessed_at, %s.modified_at) < ?", tmTable, tmTable, tmTable, tmTable),
				threatModelStatusClosed, activeThreshold,
			),
		).
		Find(&results).Error

	return results, err
}
