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
	ID     string `gorm:"column:id"`
	Status string `gorm:"column:status"`
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
