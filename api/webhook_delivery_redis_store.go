package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// WebhookDeliveryRecord is the unified delivery record used for both resource-change
// events and addon invocations. It replaces the separate DBWebhookDelivery (Postgres)
// and AddonInvocation (Redis) types with a single Redis-backed record.
type WebhookDeliveryRecord struct {
	ID             uuid.UUID  `json:"id"`
	SubscriptionID uuid.UUID  `json:"subscription_id"`
	EventType      string     `json:"event_type"`
	Payload        string     `json:"payload"`
	Status         string     `json:"status"`         // pending, in_progress, delivered, failed
	StatusPercent  int        `json:"status_percent"` // 0-100 progress indicator
	StatusMessage  string     `json:"status_message"` // human-readable status description
	Attempts       int        `json:"attempts"`       // number of delivery attempts so far
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	LastActivityAt time.Time  `json:"last_activity_at"`

	// Addon-specific fields (only populated for addon.invoked events)
	AddonID        *uuid.UUID `json:"addon_id,omitempty"`
	InvokedByUUID  *uuid.UUID `json:"invoked_by_uuid,omitempty"`
	InvokedByEmail string     `json:"invoked_by_email,omitempty"`
	InvokedByName  string     `json:"invoked_by_name,omitempty"`
}

// Delivery status constants
const (
	DeliveryStatusPending    = "pending"
	DeliveryStatusInProgress = "in_progress"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusFailed     = "failed"
)

// Delivery TTL and timeout constants
const (
	DeliveryTTLActive    = 4 * time.Hour      // TTL for pending/in_progress records
	DeliveryTTLTerminal  = 7 * 24 * time.Hour // TTL for failed/delivered records
	DeliveryStaleTimeout = 15 * time.Minute   // inactivity timeout for in_progress records
)

// webhookDeliveryKeyPrefix is the Redis key prefix for delivery records
const webhookDeliveryKeyPrefix = "webhook:delivery:"

// webhookDeliveryKeyPattern is the Redis SCAN pattern for all delivery records
const webhookDeliveryKeyPattern = "webhook:delivery:*"

// WebhookDeliveryRedisStoreInterface defines all operations for the unified webhook delivery store
type WebhookDeliveryRedisStoreInterface interface {
	// Create creates a new delivery record
	Create(ctx context.Context, record *WebhookDeliveryRecord) error
	// Get retrieves a delivery record by ID
	Get(ctx context.Context, id uuid.UUID) (*WebhookDeliveryRecord, error)
	// Update updates an existing delivery record
	Update(ctx context.Context, record *WebhookDeliveryRecord) error
	// UpdateStatus updates only the status and optional delivered-at timestamp
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, deliveredAt *time.Time) error
	// UpdateRetry updates retry-related fields after a failed attempt
	UpdateRetry(ctx context.Context, id uuid.UUID, attempts int, nextRetryAt *time.Time, lastError string) error
	// Delete removes a delivery record
	Delete(ctx context.Context, id uuid.UUID) error
	// ListPending returns delivery records that are pending and ready for delivery
	ListPending(ctx context.Context, limit int) ([]WebhookDeliveryRecord, error)
	// ListReadyForRetry returns delivery records that have been retried and are due for another attempt
	ListReadyForRetry(ctx context.Context) ([]WebhookDeliveryRecord, error)
	// ListStale returns in_progress records with no activity within the given timeout
	ListStale(ctx context.Context, timeout time.Duration) ([]WebhookDeliveryRecord, error)
	// ListBySubscription returns delivery records for a specific subscription with pagination
	ListBySubscription(ctx context.Context, subscriptionID uuid.UUID, limit, offset int) ([]WebhookDeliveryRecord, int, error)
	// ListAll returns all delivery records with pagination
	ListAll(ctx context.Context, limit, offset int) ([]WebhookDeliveryRecord, int, error)
}

// WebhookDeliveryRedisStore implements WebhookDeliveryRedisStoreInterface using Redis
type WebhookDeliveryRedisStore struct {
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder
}

// NewWebhookDeliveryRedisStore creates a new Redis-backed webhook delivery store
func NewWebhookDeliveryRedisStore(redisDB *db.RedisDB) *WebhookDeliveryRedisStore {
	return &WebhookDeliveryRedisStore{
		redis:   redisDB,
		builder: db.NewRedisKeyBuilder(),
	}
}

// GlobalWebhookDeliveryRedisStore is the global singleton for the unified webhook delivery store
var GlobalWebhookDeliveryRedisStore WebhookDeliveryRedisStoreInterface

// buildDeliveryKey creates the Redis key for a delivery record
func (s *WebhookDeliveryRedisStore) buildDeliveryKey(id uuid.UUID) string {
	return fmt.Sprintf("%s%s", webhookDeliveryKeyPrefix, id.String())
}

// ttlForStatus returns the appropriate TTL for a delivery record based on its status
func ttlForStatus(status string) time.Duration {
	switch status {
	case DeliveryStatusDelivered, DeliveryStatusFailed, "completed":
		return DeliveryTTLTerminal
	default:
		return DeliveryTTLActive
	}
}

// Create creates a new delivery record in Redis
func (s *WebhookDeliveryRedisStore) Create(ctx context.Context, record *WebhookDeliveryRecord) error {
	logger := slogging.Get()

	// Generate UUIDv7 if ID is nil (time-ordered for natural chronological sorting)
	if record.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			logger.Error("failed to generate UUIDv7 for delivery record: %v", err)
			return fmt.Errorf("failed to generate delivery ID: %w", err)
		}
		record.ID = id
	}

	// Set timestamps if zero
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.LastActivityAt.IsZero() {
		record.LastActivityAt = now
	}

	// Default status to pending
	if record.Status == "" {
		record.Status = DeliveryStatusPending
	}

	// Serialize to JSON
	data, err := json.Marshal(record)
	if err != nil {
		logger.Error("failed to marshal delivery record: %v", err)
		return fmt.Errorf("failed to marshal delivery record: %w", err)
	}

	// Store in Redis with appropriate TTL
	key := s.buildDeliveryKey(record.ID)
	ttl := ttlForStatus(record.Status)
	if err := s.redis.Set(ctx, key, data, ttl); err != nil {
		logger.Error("failed to store delivery record %s: %v", record.ID, err)
		return fmt.Errorf("failed to store delivery record: %w", err)
	}

	logger.Info("delivery record created: id=%s, subscription_id=%s, event_type=%s, status=%s",
		record.ID, record.SubscriptionID, record.EventType, record.Status)

	return nil
}

// Get retrieves a delivery record by ID
func (s *WebhookDeliveryRedisStore) Get(ctx context.Context, id uuid.UUID) (*WebhookDeliveryRecord, error) {
	logger := slogging.Get()

	key := s.buildDeliveryKey(id)
	data, err := s.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("delivery record not found: id=%s", id)
			return nil, fmt.Errorf("delivery record not found or expired: %s", id)
		}
		logger.Error("failed to get delivery record %s: %v", id, err)
		return nil, fmt.Errorf("failed to get delivery record: %w", err)
	}

	var record WebhookDeliveryRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		logger.Error("failed to unmarshal delivery record %s: %v", id, err)
		return nil, fmt.Errorf("failed to unmarshal delivery record: %w", err)
	}

	logger.Debug("retrieved delivery record: id=%s, status=%s", id, record.Status)
	return &record, nil
}

// Update updates an existing delivery record in Redis
func (s *WebhookDeliveryRedisStore) Update(ctx context.Context, record *WebhookDeliveryRecord) error {
	logger := slogging.Get()

	// Update activity timestamp
	record.LastActivityAt = time.Now().UTC()

	// Serialize to JSON
	data, err := json.Marshal(record)
	if err != nil {
		logger.Error("failed to marshal delivery record: %v", err)
		return fmt.Errorf("failed to marshal delivery record: %w", err)
	}

	// Store with refreshed TTL
	key := s.buildDeliveryKey(record.ID)
	ttl := ttlForStatus(record.Status)
	if err := s.redis.Set(ctx, key, data, ttl); err != nil {
		logger.Error("failed to update delivery record %s: %v", record.ID, err)
		return fmt.Errorf("failed to update delivery record: %w", err)
	}

	logger.Info("delivery record updated: id=%s, status=%s, attempts=%d",
		record.ID, record.Status, record.Attempts)

	return nil
}

// UpdateStatus updates the status (and optional delivered-at timestamp) of a delivery record
func (s *WebhookDeliveryRedisStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, deliveredAt *time.Time) error {
	logger := slogging.Get()

	record, err := s.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get delivery record for status update: %w", err)
	}

	record.Status = status
	if deliveredAt != nil {
		record.DeliveredAt = deliveredAt
	}

	if err := s.Update(ctx, record); err != nil {
		logger.Error("failed to update status for delivery %s: %v", id, err)
		return fmt.Errorf("failed to update delivery status: %w", err)
	}

	logger.Debug("delivery status updated: id=%s, status=%s", id, status)
	return nil
}

// UpdateRetry updates retry-related fields after a failed delivery attempt
func (s *WebhookDeliveryRedisStore) UpdateRetry(ctx context.Context, id uuid.UUID, attempts int, nextRetryAt *time.Time, lastError string) error {
	logger := slogging.Get()

	record, err := s.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get delivery record for retry update: %w", err)
	}

	record.Attempts = attempts
	record.NextRetryAt = nextRetryAt
	record.LastError = lastError

	if err := s.Update(ctx, record); err != nil {
		logger.Error("failed to update retry info for delivery %s: %v", id, err)
		return fmt.Errorf("failed to update delivery retry info: %w", err)
	}

	logger.Debug("delivery retry updated: id=%s, attempts=%d, next_retry_at=%v", id, attempts, nextRetryAt)
	return nil
}

// Delete removes a delivery record from Redis
func (s *WebhookDeliveryRedisStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	key := s.buildDeliveryKey(id)
	if err := s.redis.Del(ctx, key); err != nil {
		logger.Error("failed to delete delivery record %s: %v", id, err)
		return fmt.Errorf("failed to delete delivery record: %w", err)
	}

	logger.Info("delivery record deleted: id=%s", id)
	return nil
}

// scanAllRecords performs a Redis SCAN for all delivery records and returns them
func (s *WebhookDeliveryRedisStore) scanAllRecords(ctx context.Context) ([]WebhookDeliveryRecord, error) {
	logger := slogging.Get()

	client := s.redis.GetClient()
	var cursor uint64
	var allRecords []WebhookDeliveryRecord

	for {
		keys, newCursor, err := client.Scan(ctx, cursor, webhookDeliveryKeyPattern, 100).Result()
		if err != nil {
			logger.Error("failed to scan delivery keys: %v", err)
			return nil, fmt.Errorf("failed to scan delivery records: %w", err)
		}

		for _, key := range keys {
			data, err := s.redis.Get(ctx, key)
			if err != nil {
				if errors.Is(err, redis.Nil) {
					continue // key expired between scan and get
				}
				logger.Error("failed to get delivery record from key %s: %v", key, err)
				continue
			}

			var record WebhookDeliveryRecord
			if err := json.Unmarshal([]byte(data), &record); err != nil {
				logger.Error("failed to unmarshal delivery record from key %s: %v", key, err)
				continue
			}

			allRecords = append(allRecords, record)
		}

		cursor = newCursor
		if cursor == 0 {
			break
		}
	}

	return allRecords, nil
}

// ListPending returns delivery records that are pending and ready for delivery.
// A record is ready when its status is "pending" and either NextRetryAt is nil
// (first attempt) or NextRetryAt <= now (retry time has arrived).
func (s *WebhookDeliveryRedisStore) ListPending(ctx context.Context, limit int) ([]WebhookDeliveryRecord, error) {
	logger := slogging.Get()

	allRecords, err := s.scanAllRecords(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var pending []WebhookDeliveryRecord

	for _, record := range allRecords {
		if record.Status != DeliveryStatusPending {
			continue
		}

		// Ready if no retry scheduled, or retry time has arrived
		if record.NextRetryAt == nil || !record.NextRetryAt.After(now) {
			pending = append(pending, record)
		}
	}

	// Sort by CreatedAt ascending so oldest pending records are processed first
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})

	// Apply limit after sorting
	if len(pending) > limit {
		pending = pending[:limit]
	}

	logger.Debug("found %d pending delivery records (limit: %d)", len(pending), limit)
	return pending, nil
}

// ListReadyForRetry returns delivery records that are pending, have been attempted
// before, and are due for another retry (NextRetryAt != nil AND NextRetryAt <= now AND Attempts > 0).
func (s *WebhookDeliveryRedisStore) ListReadyForRetry(ctx context.Context) ([]WebhookDeliveryRecord, error) {
	logger := slogging.Get()

	allRecords, err := s.scanAllRecords(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var ready []WebhookDeliveryRecord

	for _, record := range allRecords {
		if record.Status != DeliveryStatusPending {
			continue
		}
		if record.Attempts <= 0 {
			continue
		}
		if record.NextRetryAt == nil || record.NextRetryAt.After(now) {
			continue
		}
		ready = append(ready, record)
	}

	logger.Debug("found %d delivery records ready for retry", len(ready))
	return ready, nil
}

// ListStale returns in_progress delivery records with no activity within the given timeout.
func (s *WebhookDeliveryRedisStore) ListStale(ctx context.Context, timeout time.Duration) ([]WebhookDeliveryRecord, error) {
	logger := slogging.Get()

	allRecords, err := s.scanAllRecords(ctx)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Add(-timeout)
	var stale []WebhookDeliveryRecord

	for _, record := range allRecords {
		if record.Status == DeliveryStatusInProgress && record.LastActivityAt.Before(cutoff) {
			stale = append(stale, record)
		}
	}

	logger.Debug("found %d stale delivery records (timeout: %v)", len(stale), timeout)
	return stale, nil
}

// ListBySubscription returns delivery records for a specific subscription with pagination.
// Returns the paginated records, total count of matching records, and any error.
func (s *WebhookDeliveryRedisStore) ListBySubscription(ctx context.Context, subscriptionID uuid.UUID, limit, offset int) ([]WebhookDeliveryRecord, int, error) {
	logger := slogging.Get()

	allRecords, err := s.scanAllRecords(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Filter by subscription
	var filtered []WebhookDeliveryRecord
	for _, record := range allRecords {
		if record.SubscriptionID == subscriptionID {
			filtered = append(filtered, record)
		}
	}

	// Sort by CreatedAt ascending for deterministic pagination
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})

	total := len(filtered)

	// Apply pagination
	start := min(offset, total)
	end := min(start+limit, total)

	var paginated []WebhookDeliveryRecord
	if start < total {
		paginated = filtered[start:end]
	}

	logger.Debug("listed %d delivery records for subscription %s (total: %d, limit: %d, offset: %d)",
		len(paginated), subscriptionID, total, limit, offset)

	return paginated, total, nil
}

// ListAll returns all delivery records with pagination.
// Returns the paginated records, total count of all records, and any error.
func (s *WebhookDeliveryRedisStore) ListAll(ctx context.Context, limit, offset int) ([]WebhookDeliveryRecord, int, error) {
	logger := slogging.Get()

	allRecords, err := s.scanAllRecords(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Sort by CreatedAt ascending for deterministic pagination
	sort.Slice(allRecords, func(i, j int) bool {
		return allRecords[i].CreatedAt.Before(allRecords[j].CreatedAt)
	})

	total := len(allRecords)

	// Apply pagination
	start := min(offset, total)
	end := min(start+limit, total)

	var paginated []WebhookDeliveryRecord
	if start < total {
		paginated = allRecords[start:end]
	}

	logger.Debug("listed %d delivery records (total: %d, limit: %d, offset: %d)",
		len(paginated), total, limit, offset)

	return paginated, total, nil
}
