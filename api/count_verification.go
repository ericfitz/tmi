package api

import (
	"context"
	"fmt"
	"time"
)

// CountVerificationService provides periodic verification of count field accuracy
type CountVerificationService struct {
	store    ThreatModelStoreInterface
	interval time.Duration
	stopCh   chan struct{}
}

// NewCountVerificationService creates a new count verification service
func NewCountVerificationService(store ThreatModelStoreInterface, interval time.Duration) *CountVerificationService {
	return &CountVerificationService{
		store:    store,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the periodic count verification process
func (cvs *CountVerificationService) Start(ctx context.Context) {
	ticker := time.NewTicker(cvs.interval)
	defer ticker.Stop()

	fmt.Printf("[INFO] Starting count verification service with interval %v\n", cvs.interval)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[INFO] Count verification service stopped due to context cancellation")
			return
		case <-cvs.stopCh:
			fmt.Println("[INFO] Count verification service stopped")
			return
		case <-ticker.C:
			cvs.verifyAllCounts(ctx)
		}
	}
}

// Stop stops the count verification service
func (cvs *CountVerificationService) Stop() {
	close(cvs.stopCh)
}

// verifyAllCounts checks count accuracy for all threat models
func (cvs *CountVerificationService) verifyAllCounts(ctx context.Context) {
	fmt.Println("[DEBUG] Starting count verification cycle")

	// Only perform verification for database stores
	dbStore, ok := cvs.store.(*ThreatModelDatabaseStore)
	if !ok {
		// In-memory stores don't need count verification
		return
	}

	// Get all threat model IDs
	threatModelIDs, err := cvs.getAllThreatModelIDs(dbStore)
	if err != nil {
		fmt.Printf("[ERROR] Failed to get threat model IDs for verification: %v\n", err)
		return
	}

	verifiedCount := 0
	correctedCount := 0

	for _, id := range threatModelIDs {
		select {
		case <-ctx.Done():
			return
		default:
			if corrected, err := cvs.verifyThreatModelCounts(dbStore, id); err != nil {
				fmt.Printf("[ERROR] Failed to verify counts for threat model %s: %v\n", id, err)
			} else {
				verifiedCount++
				if corrected {
					correctedCount++
				}
			}
		}
	}

	if correctedCount > 0 {
		fmt.Printf("[INFO] Count verification completed: %d verified, %d corrected\n", verifiedCount, correctedCount)
	} else {
		fmt.Printf("[DEBUG] Count verification completed: %d verified, no corrections needed\n", verifiedCount)
	}
}

// verifyThreatModelCounts verifies and corrects counts for a single threat model
func (cvs *CountVerificationService) verifyThreatModelCounts(dbStore *ThreatModelDatabaseStore, threatModelID string) (bool, error) {
	// Get stored counts from threat_models table
	storedCounts, err := cvs.getStoredCounts(dbStore, threatModelID)
	if err != nil {
		return false, fmt.Errorf("failed to get stored counts: %w", err)
	}

	// Compute actual counts from related tables
	actualCounts, err := cvs.computeActualCounts(dbStore, threatModelID)
	if err != nil {
		return false, fmt.Errorf("failed to compute actual counts: %w", err)
	}

	// Check for mismatches
	if !cvs.countsEqual(storedCounts, actualCounts) {
		fmt.Printf("[WARNING] Count mismatch detected for threat model %s:\n", threatModelID)
		fmt.Printf("  Stored:  doc=%d, src=%d, diag=%d, threat=%d\n",
			storedCounts.DocumentCount, storedCounts.SourceCount, storedCounts.DiagramCount, storedCounts.ThreatCount)
		fmt.Printf("  Actual:  doc=%d, src=%d, diag=%d, threat=%d\n",
			actualCounts.DocumentCount, actualCounts.SourceCount, actualCounts.DiagramCount, actualCounts.ThreatCount)

		// Correct the counts
		if err := dbStore.updateCountFields(threatModelID,
			actualCounts.DocumentCount, actualCounts.SourceCount,
			actualCounts.DiagramCount, actualCounts.ThreatCount); err != nil {
			return false, fmt.Errorf("failed to correct counts: %w", err)
		}

		fmt.Printf("[INFO] Corrected counts for threat model %s\n", threatModelID)
		return true, nil
	}

	return false, nil
}

// CountValues represents count field values
type CountValues struct {
	DocumentCount int
	SourceCount   int
	DiagramCount  int
	ThreatCount   int
}

// getAllThreatModelIDs retrieves all threat model IDs
func (cvs *CountVerificationService) getAllThreatModelIDs(dbStore *ThreatModelDatabaseStore) ([]string, error) {
	query := `SELECT id FROM threat_models`

	rows, err := dbStore.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// getStoredCounts retrieves the stored count values from threat_models table
func (cvs *CountVerificationService) getStoredCounts(dbStore *ThreatModelDatabaseStore, threatModelID string) (CountValues, error) {
	var counts CountValues

	query := `SELECT document_count, source_count, diagram_count, threat_count FROM threat_models WHERE id = $1`

	err := dbStore.db.QueryRow(query, threatModelID).Scan(
		&counts.DocumentCount, &counts.SourceCount, &counts.DiagramCount, &counts.ThreatCount,
	)

	return counts, err
}

// computeActualCounts computes the actual counts from related tables
func (cvs *CountVerificationService) computeActualCounts(dbStore *ThreatModelDatabaseStore, threatModelID string) (CountValues, error) {
	var counts CountValues
	var err error

	counts.DocumentCount, err = dbStore.countDocuments(threatModelID)
	if err != nil {
		return counts, fmt.Errorf("failed to count documents: %w", err)
	}

	counts.SourceCount, err = dbStore.countSources(threatModelID)
	if err != nil {
		return counts, fmt.Errorf("failed to count sources: %w", err)
	}

	counts.DiagramCount, err = dbStore.countDiagrams(threatModelID)
	if err != nil {
		return counts, fmt.Errorf("failed to count diagrams: %w", err)
	}

	counts.ThreatCount, err = dbStore.countThreats(threatModelID)
	if err != nil {
		return counts, fmt.Errorf("failed to count threats: %w", err)
	}

	return counts, nil
}

// countsEqual compares two CountValues for equality
func (cvs *CountVerificationService) countsEqual(a, b CountValues) bool {
	return a.DocumentCount == b.DocumentCount &&
		a.SourceCount == b.SourceCount &&
		a.DiagramCount == b.DiagramCount &&
		a.ThreatCount == b.ThreatCount
}
