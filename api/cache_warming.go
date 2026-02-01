package api

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// CacheWarmer handles proactive cache warming for frequently accessed data
type CacheWarmer struct {
	db                *sql.DB
	cache             *CacheService
	threatStore       ThreatStore
	documentStore     DocumentStore
	repositoryStore   RepositoryStore
	metadataStore     MetadataStore
	warmingEnabled    bool
	warmingInterval   time.Duration
	mutex             sync.RWMutex
	stopChannel       chan struct{}
	warmingInProgress bool
}

// WarmingStrategy defines different cache warming approaches
type WarmingStrategy int

const (
	// WarmOnAccess warms cache when data is first accessed
	WarmOnAccess WarmingStrategy = iota
	// WarmProactively warms cache on a schedule
	WarmProactively
	// WarmOnDemand warms cache only when explicitly requested
	WarmOnDemand
)

// WarmingPriority defines priority levels for cache warming
type WarmingPriority int

const (
	// PriorityHigh for critical data that must be cached
	PriorityHigh WarmingPriority = iota
	// PriorityMedium for important but not critical data
	PriorityMedium
	// PriorityLow for nice-to-have cached data
	PriorityLow
)

// WarmingRequest represents a request to warm specific cache data
type WarmingRequest struct {
	EntityType    string
	EntityID      string
	ThreatModelID string
	Priority      WarmingPriority
	Strategy      WarmingStrategy
	TTLOverride   *time.Duration
	ForceRefresh  bool
}

// WarmingStats tracks cache warming performance
type WarmingStats struct {
	TotalWarmed       int
	ThreatsWarmed     int
	DocumentsWarmed   int
	SourcesWarmed     int
	MetadataWarmed    int
	AuthDataWarmed    int
	WarmingDuration   time.Duration
	ErrorsEncountered int
	LastWarmingTime   time.Time
}

// NewCacheWarmer creates a new cache warmer instance
func NewCacheWarmer(
	db *sql.DB,
	cache *CacheService,
	threatStore ThreatStore,
	documentStore DocumentStore,
	repositoryStore RepositoryStore,
	metadataStore MetadataStore,
) *CacheWarmer {
	return &CacheWarmer{
		db:              db,
		cache:           cache,
		threatStore:     threatStore,
		documentStore:   documentStore,
		repositoryStore: repositoryStore,
		metadataStore:   metadataStore,
		warmingEnabled:  true,
		warmingInterval: 15 * time.Minute, // Default warming interval
		stopChannel:     make(chan struct{}),
	}
}

// StartProactiveWarming starts the proactive cache warming process
func (cw *CacheWarmer) StartProactiveWarming(ctx context.Context) error {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()

	if !cw.warmingEnabled {
		return fmt.Errorf("cache warming is disabled")
	}

	logger := slogging.Get()
	logger.Info("Starting proactive cache warming with interval %v", cw.warmingInterval)

	go cw.warmingLoop(ctx)
	return nil
}

// StopProactiveWarming stops the proactive cache warming process
func (cw *CacheWarmer) StopProactiveWarming() {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()

	logger := slogging.Get()
	logger.Info("Stopping proactive cache warming")

	close(cw.stopChannel)
	cw.warmingEnabled = false
}

// warmingLoop runs the continuous cache warming process
func (cw *CacheWarmer) warmingLoop(ctx context.Context) {
	logger := slogging.Get()
	ticker := time.NewTicker(cw.warmingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Cache warming stopped due to context cancellation")
			return
		case <-cw.stopChannel:
			logger.Info("Cache warming stopped due to stop signal")
			return
		case <-ticker.C:
			if !cw.isWarmingInProgress() {
				go func() {
					if err := cw.WarmFrequentlyAccessedData(ctx); err != nil {
						logger.Error("Error during proactive cache warming: %v", err)
					}
				}()
			}
		}
	}
}

// WarmFrequentlyAccessedData warms cache with frequently accessed data
func (cw *CacheWarmer) WarmFrequentlyAccessedData(ctx context.Context) error {
	cw.setWarmingInProgress(true)
	defer cw.setWarmingInProgress(false)

	logger := slogging.Get()
	logger.Info("Starting cache warming for frequently accessed data")

	startTime := time.Now()
	stats := &WarmingStats{
		LastWarmingTime: startTime,
	}

	// Warm recently accessed threat models and their sub-resources
	if err := cw.warmRecentThreatModels(ctx, stats); err != nil {
		logger.Error("Failed to warm recent threat models: %v", err)
		stats.ErrorsEncountered++
	}

	// Warm popular authorization data
	if err := cw.warmPopularAuthData(ctx, stats); err != nil {
		logger.Error("Failed to warm popular auth data: %v", err)
		stats.ErrorsEncountered++
	}

	// Warm frequently accessed metadata
	if err := cw.warmPopularMetadata(ctx, stats); err != nil {
		logger.Error("Failed to warm popular metadata: %v", err)
		stats.ErrorsEncountered++
	}

	stats.WarmingDuration = time.Since(startTime)
	logger.Info("Cache warming completed in %v - warmed %d items with %d errors",
		stats.WarmingDuration, stats.TotalWarmed, stats.ErrorsEncountered)

	return nil
}

// warmRecentThreatModels warms cache with recently accessed threat models
func (cw *CacheWarmer) warmRecentThreatModels(ctx context.Context, stats *WarmingStats) error {
	logger := slogging.Get()

	// Query for recently accessed threat models (last 24 hours)
	query := `
		SELECT DISTINCT tm.id 
		FROM threat_models tm
		WHERE tm.modified_at >= NOW() - INTERVAL '24 hours'
		ORDER BY tm.modified_at DESC
		LIMIT 50
	`

	rows, err := cw.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query recent threat models: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var threatModelID string
		if err := rows.Scan(&threatModelID); err != nil {
			logger.Error("Failed to scan threat model ID: %v", err)
			stats.ErrorsEncountered++
			continue
		}

		// Warm all sub-resources for this threat model
		if err := cw.WarmThreatModelData(ctx, threatModelID); err != nil {
			logger.Error("Failed to warm threat model %s: %v", threatModelID, err)
			stats.ErrorsEncountered++
		} else {
			stats.TotalWarmed++
		}
	}

	return rows.Err()
}

// WarmThreatModelData warms cache with all data for a specific threat model
func (cw *CacheWarmer) WarmThreatModelData(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model %s", threatModelID)

	var wg sync.WaitGroup
	errorChan := make(chan error, 4)

	// Warm threats
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cw.warmThreatsForThreatModel(ctx, threatModelID); err != nil {
			errorChan <- fmt.Errorf("failed to warm threats: %w", err)
		}
	}()

	// Warm documents
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cw.warmDocumentsForThreatModel(ctx, threatModelID); err != nil {
			errorChan <- fmt.Errorf("failed to warm documents: %w", err)
		}
	}()

	// Warm sources
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cw.warmSourcesForThreatModel(ctx, threatModelID); err != nil {
			errorChan <- fmt.Errorf("failed to warm sources: %w", err)
		}
	}()

	// Warm authorization data
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cw.warmAuthDataForThreatModel(ctx, threatModelID); err != nil {
			errorChan <- fmt.Errorf("failed to warm auth data: %w", err)
		}
	}()

	wg.Wait()
	close(errorChan)

	// Collect any errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("warming errors: %v", errors)
	}

	return nil
}

// warmThreatsForThreatModel warms cache with threats for a threat model
func (cw *CacheWarmer) warmThreatsForThreatModel(ctx context.Context, threatModelID string) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}

	filter := ThreatFilter{Offset: 0, Limit: 100}
	threats, _, err := cw.threatStore.List(ctx, threatModelID, filter) // Warm first 100 threats
	if err != nil {
		return fmt.Errorf("failed to list threats: %w", err)
	}

	for _, threat := range threats {
		if threat.Id != nil {
			if err := cw.cache.CacheThreat(ctx, &threat); err != nil {
				return fmt.Errorf("failed to cache threat %s: %w", threat.Id.String(), err)
			}
		}
	}

	return nil
}

// warmDocumentsForThreatModel warms cache with documents for a threat model
func (cw *CacheWarmer) warmDocumentsForThreatModel(ctx context.Context, threatModelID string) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}

	documents, err := cw.documentStore.List(ctx, threatModelID, 0, 50) // Warm first 50 documents
	if err != nil {
		return fmt.Errorf("failed to list documents: %w", err)
	}

	for _, document := range documents {
		if document.Id != nil {
			if err := cw.cache.CacheDocument(ctx, &document); err != nil {
				return fmt.Errorf("failed to cache document %s: %w", document.Id.String(), err)
			}
		}
	}

	return nil
}

// warmSourcesForThreatModel warms cache with sources for a threat model
func (cw *CacheWarmer) warmSourcesForThreatModel(ctx context.Context, threatModelID string) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}

	repositories, err := cw.repositoryStore.List(ctx, threatModelID, 0, 50) // Warm first 50 repositories
	if err != nil {
		return fmt.Errorf("failed to list repositories: %w", err)
	}

	for _, repository := range repositories {
		if repository.Id != nil {
			if err := cw.cache.CacheRepository(ctx, &repository); err != nil {
				return fmt.Errorf("failed to cache repository %s: %w", repository.Id.String(), err)
			}
		}
	}

	return nil
}

// warmAuthDataForThreatModel warms cache with authorization data for a threat model
func (cw *CacheWarmer) warmAuthDataForThreatModel(ctx context.Context, threatModelID string) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}

	authData, err := GetInheritedAuthData(ctx, cw.db, threatModelID)
	if err != nil {
		return fmt.Errorf("failed to get auth data: %w", err)
	}

	if err := cw.cache.CacheAuthData(ctx, threatModelID, *authData); err != nil {
		return fmt.Errorf("failed to cache auth data: %w", err)
	}

	return nil
}

// warmPopularAuthData warms cache with frequently accessed authorization data
func (cw *CacheWarmer) warmPopularAuthData(ctx context.Context, stats *WarmingStats) error {
	// Query for threat models with recent access patterns
	query := `
		SELECT DISTINCT tm.id 
		FROM threat_models tm
		INNER JOIN threat_model_access tma ON tm.id = tma.threat_model_id
		WHERE tm.modified_at >= NOW() - INTERVAL '7 days'
		GROUP BY tm.id
		HAVING COUNT(tma.user_email) > 1
		ORDER BY COUNT(tma.user_email) DESC
		LIMIT 25
	`

	rows, err := cw.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query popular auth data: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var threatModelID string
		if err := rows.Scan(&threatModelID); err != nil {
			continue
		}

		if err := cw.warmAuthDataForThreatModel(ctx, threatModelID); err == nil {
			stats.AuthDataWarmed++
		} else {
			stats.ErrorsEncountered++
		}
	}

	return rows.Err()
}

// warmPopularMetadata warms cache with frequently accessed metadata
func (cw *CacheWarmer) warmPopularMetadata(ctx context.Context, stats *WarmingStats) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}
	// Query for frequently accessed metadata keys
	query := `
		SELECT entity_type, entity_id, key, value
		FROM metadata 
		WHERE modified_at >= NOW() - INTERVAL '7 days'
		ORDER BY modified_at DESC
		LIMIT 200
	`

	rows, err := cw.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query popular metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var entityType, entityID, key, value string
		if err := rows.Scan(&entityType, &entityID, &key, &value); err != nil {
			stats.ErrorsEncountered++
			continue
		}

		metadata := []Metadata{{Key: key, Value: value}}
		if err := cw.cache.CacheMetadata(ctx, entityType, entityID, metadata); err == nil {
			stats.MetadataWarmed++
		} else {
			stats.ErrorsEncountered++
		}
	}

	return rows.Err()
}

// WarmOnDemandRequest handles on-demand cache warming requests
func (cw *CacheWarmer) WarmOnDemandRequest(ctx context.Context, request WarmingRequest) error {
	logger := slogging.Get()
	logger.Debug("Processing on-demand warming request for %s:%s", request.EntityType, request.EntityID)

	switch request.EntityType {
	case "threat_model":
		return cw.WarmThreatModelData(ctx, request.EntityID)
	case "threat":
		return cw.warmSpecificThreat(ctx, request.EntityID)
	case "document":
		return cw.warmSpecificDocument(ctx, request.EntityID)
	case "repository":
		return cw.warmSpecificRepository(ctx, request.EntityID)
	case "auth":
		return cw.warmAuthDataForThreatModel(ctx, request.ThreatModelID)
	default:
		return fmt.Errorf("unsupported entity type for warming: %s", request.EntityType)
	}
}

// warmSpecificThreat warms cache with a specific threat
func (cw *CacheWarmer) warmSpecificThreat(ctx context.Context, threatID string) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}

	threat, err := cw.threatStore.Get(ctx, threatID)
	if err != nil {
		return fmt.Errorf("failed to get threat %s: %w", threatID, err)
	}

	return cw.cache.CacheThreat(ctx, threat)
}

// warmSpecificDocument warms cache with a specific document
func (cw *CacheWarmer) warmSpecificDocument(ctx context.Context, documentID string) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}

	document, err := cw.documentStore.Get(ctx, documentID)
	if err != nil {
		return fmt.Errorf("failed to get document %s: %w", documentID, err)
	}

	return cw.cache.CacheDocument(ctx, document)
}

// warmSpecificRepository warms cache with a specific repository
func (cw *CacheWarmer) warmSpecificRepository(ctx context.Context, repositoryID string) error {
	// Check if cache service is available
	if cw.cache == nil {
		return nil // Skip warming if cache is not available
	}

	repository, err := cw.repositoryStore.Get(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("failed to get repository %s: %w", repositoryID, err)
	}

	return cw.cache.CacheRepository(ctx, repository)
}

// SetWarmingInterval configures the proactive warming interval
func (cw *CacheWarmer) SetWarmingInterval(interval time.Duration) {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()
	cw.warmingInterval = interval
}

// EnableWarming enables cache warming
func (cw *CacheWarmer) EnableWarming() {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()
	cw.warmingEnabled = true
}

// DisableWarming disables cache warming
func (cw *CacheWarmer) DisableWarming() {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()
	cw.warmingEnabled = false
}

// IsWarmingEnabled returns whether cache warming is enabled
func (cw *CacheWarmer) IsWarmingEnabled() bool {
	cw.mutex.RLock()
	defer cw.mutex.RUnlock()
	return cw.warmingEnabled
}

// isWarmingInProgress returns whether warming is currently in progress
func (cw *CacheWarmer) isWarmingInProgress() bool {
	cw.mutex.RLock()
	defer cw.mutex.RUnlock()
	return cw.warmingInProgress
}

// setWarmingInProgress sets the warming in progress flag
func (cw *CacheWarmer) setWarmingInProgress(inProgress bool) {
	cw.mutex.Lock()
	defer cw.mutex.Unlock()
	cw.warmingInProgress = inProgress
}

// GetWarmingStats returns current warming statistics
func (cw *CacheWarmer) GetWarmingStats() WarmingStats {
	cw.mutex.RLock()
	defer cw.mutex.RUnlock()

	// Return a copy of current stats
	// In a real implementation, this would return actual statistics
	return WarmingStats{
		LastWarmingTime: time.Now(),
	}
}
