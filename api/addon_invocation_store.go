package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// AddonInvocation represents an add-on invocation stored in Redis
type AddonInvocation struct {
	ID              uuid.UUID  `json:"id"`
	AddonID         uuid.UUID  `json:"addon_id"`
	ThreatModelID   uuid.UUID  `json:"threat_model_id"`
	ObjectType      string     `json:"object_type,omitempty"`
	ObjectID        *uuid.UUID `json:"object_id,omitempty"`
	InvokedByUUID   uuid.UUID  `json:"-"`                // Internal user UUID (for rate limiting, quotas) - NEVER exposed
	InvokedByID     string     `json:"invoked_by_id"`    // Provider-assigned user ID (for API responses)
	InvokedByEmail  string     `json:"invoked_by_email"` // User email
	InvokedByName   string     `json:"invoked_by_name"`  // User display name
	Payload         string     `json:"payload"`          // JSON string
	Status          string     `json:"status"`           // pending, in_progress, completed, failed
	StatusPercent   int        `json:"status_percent"`   // 0-100
	StatusMessage   string     `json:"status_message,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StatusUpdatedAt time.Time  `json:"status_updated_at"`
	LastActivityAt  time.Time  `json:"last_activity_at"` // Track last activity for timeout detection
}

// Invocation status constants
const (
	InvocationStatusPending    = "pending"
	InvocationStatusInProgress = "in_progress"
	InvocationStatusCompleted  = "completed"
	InvocationStatusFailed     = "failed"
)

// AddonInvocationTTL is the Redis TTL for invocations (7 days)
const AddonInvocationTTL = 7 * 24 * time.Hour

// AddonInvocationTimeout is the inactivity timeout for invocations (15 minutes)
const AddonInvocationTimeout = 15 * time.Minute

// AddonInvocationStore defines the interface for invocation storage operations
type AddonInvocationStore interface {
	// Create creates a new invocation
	Create(ctx context.Context, invocation *AddonInvocation) error

	// Get retrieves an invocation by ID
	Get(ctx context.Context, id uuid.UUID) (*AddonInvocation, error)

	// Update updates an existing invocation
	Update(ctx context.Context, invocation *AddonInvocation) error

	// List retrieves invocations for a user with pagination
	// If userID is nil, returns all invocations (admin view)
	// Can filter by status if provided
	List(ctx context.Context, userID *uuid.UUID, status string, limit, offset int) ([]AddonInvocation, int, error)

	// CountActive counts pending/in_progress invocations for an add-on
	CountActive(ctx context.Context, addonID uuid.UUID) (int, error)

	// GetActiveForUser retrieves the active invocation for a user (for quota enforcement)
	GetActiveForUser(ctx context.Context, userID uuid.UUID) (*AddonInvocation, error)

	// ListActiveForUser retrieves all active invocations (pending/in_progress) for a user up to limit
	ListActiveForUser(ctx context.Context, userID uuid.UUID, limit int) ([]AddonInvocation, error)

	// Delete removes an invocation (for cleanup)
	Delete(ctx context.Context, id uuid.UUID) error

	// ListStale retrieves invocations that have timed out (no activity for AddonInvocationTimeout)
	ListStale(ctx context.Context, timeout time.Duration) ([]AddonInvocation, error)
}

// AddonInvocationRedisStore implements AddonInvocationStore using Redis
type AddonInvocationRedisStore struct {
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder
}

// NewAddonInvocationRedisStore creates a new Redis-backed invocation store
func NewAddonInvocationRedisStore(redis *db.RedisDB) *AddonInvocationRedisStore {
	return &AddonInvocationRedisStore{
		redis:   redis,
		builder: db.NewRedisKeyBuilder(),
	}
}

// buildInvocationKey creates the Redis key for an invocation
func (s *AddonInvocationRedisStore) buildInvocationKey(id uuid.UUID) string {
	return fmt.Sprintf("addon:invocation:%s", id.String())
}

// buildActiveUserKey creates the Redis key for tracking a user's active invocation
func (s *AddonInvocationRedisStore) buildActiveUserKey(userID uuid.UUID) string {
	return fmt.Sprintf("addon:active:%s", userID.String())
}

// Create creates a new invocation
func (s *AddonInvocationRedisStore) Create(ctx context.Context, invocation *AddonInvocation) error {
	logger := slogging.Get()

	// Generate ID if not provided
	if invocation.ID == uuid.Nil {
		invocation.ID = uuid.New()
	}

	// Set timestamps
	now := time.Now()
	if invocation.CreatedAt.IsZero() {
		invocation.CreatedAt = now
	}
	invocation.StatusUpdatedAt = invocation.CreatedAt
	invocation.LastActivityAt = now

	// Serialize to JSON
	data, err := json.Marshal(invocation)
	if err != nil {
		logger.Error("Failed to marshal invocation: %v", err)
		return fmt.Errorf("failed to marshal invocation: %w", err)
	}

	// Store in Redis with TTL
	key := s.buildInvocationKey(invocation.ID)
	err = s.redis.Set(ctx, key, data, AddonInvocationTTL)
	if err != nil {
		logger.Error("Failed to store invocation %s: %v", invocation.ID, err)
		return fmt.Errorf("failed to store invocation: %w", err)
	}

	// Track active invocation for user if status is pending/in_progress
	if invocation.Status == InvocationStatusPending || invocation.Status == InvocationStatusInProgress {
		activeKey := s.buildActiveUserKey(invocation.InvokedByUUID)
		err = s.redis.Set(ctx, activeKey, invocation.ID.String(), time.Hour)
		if err != nil {
			logger.Error("Failed to track active invocation for user %s: %v", invocation.InvokedByUUID, err)
			// Don't fail the create operation for this
		}
	}

	logger.Info("Invocation created: id=%s, addon_id=%s, user=%s, status=%s",
		invocation.ID, invocation.AddonID, invocation.InvokedByID, invocation.Status)

	return nil
}

// Get retrieves an invocation by ID
func (s *AddonInvocationRedisStore) Get(ctx context.Context, id uuid.UUID) (*AddonInvocation, error) {
	logger := slogging.Get()

	key := s.buildInvocationKey(id)
	data, err := s.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Invocation not found: id=%s", id)
			return nil, fmt.Errorf("invocation not found or expired: %s", id)
		}
		logger.Error("Failed to get invocation %s: %v", id, err)
		return nil, fmt.Errorf("failed to get invocation: %w", err)
	}

	var invocation AddonInvocation
	err = json.Unmarshal([]byte(data), &invocation)
	if err != nil {
		logger.Error("Failed to unmarshal invocation %s: %v", id, err)
		return nil, fmt.Errorf("failed to unmarshal invocation: %w", err)
	}

	logger.Debug("Retrieved invocation: id=%s, status=%s", id, invocation.Status)

	return &invocation, nil
}

// Update updates an existing invocation
func (s *AddonInvocationRedisStore) Update(ctx context.Context, invocation *AddonInvocation) error {
	logger := slogging.Get()

	// Update status timestamp and activity timestamp
	now := time.Now()
	invocation.StatusUpdatedAt = now
	invocation.LastActivityAt = now

	// Serialize to JSON
	data, err := json.Marshal(invocation)
	if err != nil {
		logger.Error("Failed to marshal invocation: %v", err)
		return fmt.Errorf("failed to marshal invocation: %w", err)
	}

	// Update in Redis (keep existing TTL)
	key := s.buildInvocationKey(invocation.ID)
	err = s.redis.Set(ctx, key, data, AddonInvocationTTL)
	if err != nil {
		logger.Error("Failed to update invocation %s: %v", invocation.ID, err)
		return fmt.Errorf("failed to update invocation: %w", err)
	}

	// Update active tracking if status changed to completed/failed
	if invocation.Status == InvocationStatusCompleted || invocation.Status == InvocationStatusFailed {
		activeKey := s.buildActiveUserKey(invocation.InvokedByUUID)
		err = s.redis.Del(ctx, activeKey)
		if err != nil {
			logger.Error("Failed to clear active invocation for user %s: %v", invocation.InvokedByUUID, err)
			// Don't fail the update for this
		}
	}

	logger.Info("Invocation updated: id=%s, status=%s, percent=%d",
		invocation.ID, invocation.Status, invocation.StatusPercent)

	return nil
}

// List retrieves invocations with pagination and optional filtering
func (s *AddonInvocationRedisStore) List(ctx context.Context, userID *uuid.UUID, status string, limit, offset int) ([]AddonInvocation, int, error) {
	logger := slogging.Get()

	// Scan for all invocation keys
	pattern := "addon:invocation:*"
	var cursor uint64
	var allKeys []string

	client := s.redis.GetClient()
	for {
		var keys []string
		var newCursor uint64
		keys, newCursor, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			logger.Error("Failed to scan invocation keys: %v", err)
			return nil, 0, fmt.Errorf("failed to scan invocations: %w", err)
		}

		allKeys = append(allKeys, keys...)
		cursor = newCursor

		if cursor == 0 {
			break
		}
	}

	// Fetch all invocations
	var allInvocations []AddonInvocation
	for _, key := range allKeys {
		data, err := s.redis.Get(ctx, key)
		if err != nil {
			if err == redis.Nil {
				continue // Key expired between scan and get
			}
			logger.Error("Failed to get invocation from key %s: %v", key, err)
			continue
		}

		var invocation AddonInvocation
		if err := json.Unmarshal([]byte(data), &invocation); err != nil {
			logger.Error("Failed to unmarshal invocation from key %s: %v", key, err)
			continue
		}

		// Apply filters
		if userID != nil && invocation.InvokedByUUID != *userID {
			continue
		}
		if status != "" && invocation.Status != status {
			continue
		}

		allInvocations = append(allInvocations, invocation)
	}

	total := len(allInvocations)

	// Apply pagination
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	var paginatedInvocations []AddonInvocation
	if start < total {
		paginatedInvocations = allInvocations[start:end]
	}

	logger.Debug("Listed %d invocations (total: %d, limit: %d, offset: %d)",
		len(paginatedInvocations), total, limit, offset)

	return paginatedInvocations, total, nil
}

// CountActive counts pending/in_progress invocations for an add-on
func (s *AddonInvocationRedisStore) CountActive(ctx context.Context, addonID uuid.UUID) (int, error) {
	logger := slogging.Get()

	// Scan for all invocation keys
	pattern := "addon:invocation:*"
	var cursor uint64
	count := 0

	client := s.redis.GetClient()
	for {
		var keys []string
		var newCursor uint64
		keys, newCursor, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			logger.Error("Failed to scan invocation keys: %v", err)
			return 0, fmt.Errorf("failed to scan invocations: %w", err)
		}

		// Check each invocation
		for _, key := range keys {
			data, err := s.redis.Get(ctx, key)
			if err != nil {
				if err == redis.Nil {
					continue
				}
				continue
			}

			var invocation AddonInvocation
			if err := json.Unmarshal([]byte(data), &invocation); err != nil {
				continue
			}

			// Count if it's for this add-on and status is active
			if invocation.AddonID == addonID &&
				(invocation.Status == InvocationStatusPending || invocation.Status == InvocationStatusInProgress) {
				count++
			}
		}

		cursor = newCursor
		if cursor == 0 {
			break
		}
	}

	logger.Debug("Counted %d active invocations for addon_id=%s", count, addonID)

	return count, nil
}

// GetActiveForUser retrieves the active invocation for a user
func (s *AddonInvocationRedisStore) GetActiveForUser(ctx context.Context, userID uuid.UUID) (*AddonInvocation, error) {
	logger := slogging.Get()

	activeKey := s.buildActiveUserKey(userID)
	invocationIDStr, err := s.redis.Get(ctx, activeKey)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("No active invocation for user %s", userID)
			return nil, nil // No active invocation
		}
		logger.Error("Failed to get active invocation for user %s: %v", userID, err)
		return nil, fmt.Errorf("failed to get active invocation: %w", err)
	}

	invocationID, err := uuid.Parse(invocationIDStr)
	if err != nil {
		logger.Error("Invalid invocation ID in active key for user %s: %s", userID, invocationIDStr)
		return nil, fmt.Errorf("invalid active invocation ID: %w", err)
	}

	// Get the invocation
	return s.Get(ctx, invocationID)
}

// ListActiveForUser retrieves all active invocations (pending/in_progress) for a user up to limit
func (s *AddonInvocationRedisStore) ListActiveForUser(ctx context.Context, userID uuid.UUID, limit int) ([]AddonInvocation, error) {
	logger := slogging.Get()

	// Scan for all invocation keys
	pattern := "addon:invocation:*"
	var cursor uint64
	var activeInvocations []AddonInvocation

	client := s.redis.GetClient()

	for {
		var keys []string
		var newCursor uint64
		keys, newCursor, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			logger.Error("Failed to scan invocation keys: %v", err)
			return nil, fmt.Errorf("failed to scan invocations: %w", err)
		}

		// Check each invocation
		for _, key := range keys {
			data, err := s.redis.Get(ctx, key)
			if err != nil {
				if err == redis.Nil {
					continue // Key expired between scan and get
				}
				logger.Error("Failed to get invocation from key %s: %v", key, err)
				continue
			}

			var invocation AddonInvocation
			if err := json.Unmarshal([]byte(data), &invocation); err != nil {
				logger.Error("Failed to unmarshal invocation from key %s: %v", key, err)
				continue
			}

			// Check if invocation belongs to user and is active
			if invocation.InvokedByUUID == userID &&
				(invocation.Status == InvocationStatusPending || invocation.Status == InvocationStatusInProgress) {
				activeInvocations = append(activeInvocations, invocation)

				// Stop if we've reached the limit
				if len(activeInvocations) >= limit {
					logger.Debug("Found %d active invocations for user %s (limit reached)", len(activeInvocations), userID)
					return activeInvocations, nil
				}
			}
		}

		cursor = newCursor
		if cursor == 0 {
			break
		}
	}

	logger.Debug("Found %d active invocations for user %s", len(activeInvocations), userID)

	return activeInvocations, nil
}

// Delete removes an invocation
func (s *AddonInvocationRedisStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	key := s.buildInvocationKey(id)
	err := s.redis.Del(ctx, key)
	if err != nil {
		logger.Error("Failed to delete invocation %s: %v", id, err)
		return fmt.Errorf("failed to delete invocation: %w", err)
	}

	logger.Info("Invocation deleted: id=%s", id)

	return nil
}

// ListStale retrieves invocations that have timed out (no activity for the specified timeout)
func (s *AddonInvocationRedisStore) ListStale(ctx context.Context, timeout time.Duration) ([]AddonInvocation, error) {
	logger := slogging.Get()

	// Scan for all invocation keys
	pattern := "addon:invocation:*"
	var cursor uint64
	var staleInvocations []AddonInvocation

	client := s.redis.GetClient()
	cutoffTime := time.Now().Add(-timeout)

	for {
		var keys []string
		var newCursor uint64
		keys, newCursor, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			logger.Error("Failed to scan invocation keys: %v", err)
			return nil, fmt.Errorf("failed to scan invocations: %w", err)
		}

		// Check each invocation
		for _, key := range keys {
			data, err := s.redis.Get(ctx, key)
			if err != nil {
				if err == redis.Nil {
					continue // Key expired between scan and get
				}
				logger.Error("Failed to get invocation from key %s: %v", key, err)
				continue
			}

			var invocation AddonInvocation
			if err := json.Unmarshal([]byte(data), &invocation); err != nil {
				logger.Error("Failed to unmarshal invocation from key %s: %v", key, err)
				continue
			}

			// Check if invocation is stale (pending or in_progress, and no activity for timeout duration)
			if (invocation.Status == InvocationStatusPending || invocation.Status == InvocationStatusInProgress) &&
				invocation.LastActivityAt.Before(cutoffTime) {
				staleInvocations = append(staleInvocations, invocation)
			}
		}

		cursor = newCursor
		if cursor == 0 {
			break
		}
	}

	logger.Debug("Found %d stale invocations (timeout: %v)", len(staleInvocations), timeout)

	return staleInvocations, nil
}

// GlobalAddonInvocationStore is the global singleton for invocation storage
var GlobalAddonInvocationStore AddonInvocationStore
