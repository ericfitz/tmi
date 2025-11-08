package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// AddonInvocationQuota represents per-user rate limits for add-on invocations
type AddonInvocationQuota struct {
	OwnerID              uuid.UUID `json:"owner_id"`
	MaxActiveInvocations int       `json:"max_active_invocations"`
	MaxInvocationsPerHour int      `json:"max_invocations_per_hour"`
	CreatedAt            time.Time `json:"created_at"`
	ModifiedAt           time.Time `json:"modified_at"`
}

// Default quota values
const (
	DefaultMaxActiveInvocations    = 1
	DefaultMaxInvocationsPerHour   = 10
)

// AddonInvocationQuotaStore defines the interface for quota storage operations
type AddonInvocationQuotaStore interface {
	// GetOrDefault retrieves quota for a user, or returns defaults if not set
	GetOrDefault(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error)

	// Set creates or updates quota for a user
	Set(ctx context.Context, quota *AddonInvocationQuota) error

	// Delete removes quota for a user (reverts to defaults)
	Delete(ctx context.Context, ownerID uuid.UUID) error
}

// AddonInvocationQuotaDatabaseStore implements AddonInvocationQuotaStore using PostgreSQL
type AddonInvocationQuotaDatabaseStore struct {
	db *sql.DB
}

// NewAddonInvocationQuotaDatabaseStore creates a new database-backed quota store
func NewAddonInvocationQuotaDatabaseStore(db *sql.DB) *AddonInvocationQuotaDatabaseStore {
	return &AddonInvocationQuotaDatabaseStore{db: db}
}

// GetOrDefault retrieves quota for a user, or returns defaults if not set
func (s *AddonInvocationQuotaDatabaseStore) GetOrDefault(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error) {
	logger := slogging.Get()

	query := `
		SELECT owner_id, max_active_invocations, max_invocations_per_hour, created_at, modified_at
		FROM addon_invocation_quotas
		WHERE owner_id = $1
	`

	quota := &AddonInvocationQuota{}
	err := s.db.QueryRowContext(ctx, query, ownerID).Scan(
		&quota.OwnerID,
		&quota.MaxActiveInvocations,
		&quota.MaxInvocationsPerHour,
		&quota.CreatedAt,
		&quota.ModifiedAt,
	)

	if err == sql.ErrNoRows {
		// Return defaults
		logger.Debug("No quota found for owner_id=%s, using defaults", ownerID)
		return &AddonInvocationQuota{
			OwnerID:               ownerID,
			MaxActiveInvocations:  DefaultMaxActiveInvocations,
			MaxInvocationsPerHour: DefaultMaxInvocationsPerHour,
			CreatedAt:             time.Now(),
			ModifiedAt:            time.Now(),
		}, nil
	}

	if err != nil {
		logger.Error("Failed to get quota for owner_id=%s: %v", ownerID, err)
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}

	logger.Debug("Retrieved quota for owner_id=%s: active=%d, hourly=%d",
		ownerID, quota.MaxActiveInvocations, quota.MaxInvocationsPerHour)

	return quota, nil
}

// Set creates or updates quota for a user
func (s *AddonInvocationQuotaDatabaseStore) Set(ctx context.Context, quota *AddonInvocationQuota) error {
	logger := slogging.Get()

	query := `
		INSERT INTO addon_invocation_quotas (owner_id, max_active_invocations, max_invocations_per_hour, created_at, modified_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (owner_id) DO UPDATE
		SET max_active_invocations = EXCLUDED.max_active_invocations,
			max_invocations_per_hour = EXCLUDED.max_invocations_per_hour,
			modified_at = EXCLUDED.modified_at
		RETURNING created_at, modified_at
	`

	err := s.db.QueryRowContext(ctx, query,
		quota.OwnerID,
		quota.MaxActiveInvocations,
		quota.MaxInvocationsPerHour,
		quota.CreatedAt,
		quota.ModifiedAt,
	).Scan(&quota.CreatedAt, &quota.ModifiedAt)

	if err != nil {
		logger.Error("Failed to set quota for owner_id=%s: %v", quota.OwnerID, err)
		return fmt.Errorf("failed to set quota: %w", err)
	}

	logger.Info("Quota set for owner_id=%s: active=%d, hourly=%d",
		quota.OwnerID, quota.MaxActiveInvocations, quota.MaxInvocationsPerHour)

	return nil
}

// Delete removes quota for a user (reverts to defaults)
func (s *AddonInvocationQuotaDatabaseStore) Delete(ctx context.Context, ownerID uuid.UUID) error {
	logger := slogging.Get()

	query := `DELETE FROM addon_invocation_quotas WHERE owner_id = $1`

	result, err := s.db.ExecContext(ctx, query, ownerID)
	if err != nil {
		logger.Error("Failed to delete quota for owner_id=%s: %v", ownerID, err)
		return fmt.Errorf("failed to delete quota: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected for delete: %v", err)
		return fmt.Errorf("failed to verify delete: %w", err)
	}

	if rowsAffected == 0 {
		logger.Debug("No quota to delete for owner_id=%s", ownerID)
		return nil
	}

	logger.Info("Quota deleted for owner_id=%s (reverted to defaults)", ownerID)

	return nil
}

// GlobalAddonInvocationQuotaStore is the global singleton for quota storage
var GlobalAddonInvocationQuotaStore AddonInvocationQuotaStore
