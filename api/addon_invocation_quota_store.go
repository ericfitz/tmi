package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// Default quota values
const (
	DefaultMaxActiveInvocations  = 3 // Allow 3 concurrent invocations per user by default
	DefaultMaxInvocationsPerHour = 10
)

// AddonInvocationQuotaStore defines the interface for quota storage operations
type AddonInvocationQuotaStore interface {
	// Get retrieves quota for a user, returns error if not found
	Get(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error)

	// GetOrDefault retrieves quota for a user, or returns defaults if not set
	GetOrDefault(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error)

	// List retrieves all custom quotas (non-default) with pagination
	List(ctx context.Context, offset, limit int) ([]*AddonInvocationQuota, error)

	// Count returns the total number of custom quotas
	Count(ctx context.Context) (int, error)

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

// Get retrieves quota for a user, returns error if not found
func (s *AddonInvocationQuotaDatabaseStore) Get(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error) {
	logger := slogging.Get()

	query := `
		SELECT owner_internal_uuid, max_active_invocations, max_invocations_per_hour, created_at, modified_at
		FROM addon_invocation_quotas
		WHERE owner_internal_uuid = $1
	`

	quota := &AddonInvocationQuota{}
	err := s.db.QueryRowContext(ctx, query, ownerID).Scan(
		&quota.OwnerId,
		&quota.MaxActiveInvocations,
		&quota.MaxInvocationsPerHour,
		&quota.CreatedAt,
		&quota.ModifiedAt,
	)

	if err == sql.ErrNoRows {
		logger.Debug("No quota found for owner_id=%s", ownerID)
		return nil, fmt.Errorf("quota not found for owner_id=%s", ownerID)
	}

	if err != nil {
		logger.Error("Failed to get quota for owner_id=%s: %v", ownerID, err)
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}

	logger.Debug("Retrieved quota for owner_id=%s: active=%d, hourly=%d",
		ownerID, quota.MaxActiveInvocations, quota.MaxInvocationsPerHour)

	return quota, nil
}

// GetOrDefault retrieves quota for a user, or returns defaults if not set
func (s *AddonInvocationQuotaDatabaseStore) GetOrDefault(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error) {
	logger := slogging.Get()

	query := `
		SELECT owner_internal_uuid, max_active_invocations, max_invocations_per_hour, created_at, modified_at
		FROM addon_invocation_quotas
		WHERE owner_internal_uuid = $1
	`

	quota := &AddonInvocationQuota{}
	err := s.db.QueryRowContext(ctx, query, ownerID).Scan(
		&quota.OwnerId,
		&quota.MaxActiveInvocations,
		&quota.MaxInvocationsPerHour,
		&quota.CreatedAt,
		&quota.ModifiedAt,
	)

	if err == sql.ErrNoRows {
		// Return defaults
		logger.Debug("No quota found for owner_id=%s, using defaults", ownerID)
		return &AddonInvocationQuota{
			OwnerId:               ownerID,
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

// List retrieves all addon invocation quotas with pagination
func (s *AddonInvocationQuotaDatabaseStore) List(ctx context.Context, offset, limit int) ([]*AddonInvocationQuota, error) {
	logger := slogging.Get()

	query := `
		SELECT owner_internal_uuid, max_active_invocations, max_invocations_per_hour, created_at, modified_at
		FROM addon_invocation_quotas
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		logger.Error("Failed to list quotas: %v", err)
		return nil, fmt.Errorf("failed to list quotas: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var quotas []*AddonInvocationQuota
	for rows.Next() {
		quota := &AddonInvocationQuota{}
		err := rows.Scan(
			&quota.OwnerId,
			&quota.MaxActiveInvocations,
			&quota.MaxInvocationsPerHour,
			&quota.CreatedAt,
			&quota.ModifiedAt,
		)
		if err != nil {
			logger.Error("Failed to scan quota row: %v", err)
			return nil, fmt.Errorf("failed to scan quota: %w", err)
		}
		quotas = append(quotas, quota)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating quota rows: %v", err)
		return nil, fmt.Errorf("error iterating quotas: %w", err)
	}

	logger.Debug("Listed %d addon invocation quotas (offset=%d, limit=%d)", len(quotas), offset, limit)

	return quotas, nil
}

// Count returns the total number of addon invocation quotas
func (s *AddonInvocationQuotaDatabaseStore) Count(ctx context.Context) (int, error) {
	logger := slogging.Get()

	query := `SELECT COUNT(*) FROM addon_invocation_quotas`

	var count int
	err := s.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		logger.Error("Failed to count addon invocation quotas: %v", err)
		return 0, fmt.Errorf("failed to count quotas: %w", err)
	}

	return count, nil
}

// Set creates or updates quota for a user
func (s *AddonInvocationQuotaDatabaseStore) Set(ctx context.Context, quota *AddonInvocationQuota) error {
	logger := slogging.Get()

	query := `
		INSERT INTO addon_invocation_quotas (owner_internal_uuid, max_active_invocations, max_invocations_per_hour, created_at, modified_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (owner_internal_uuid) DO UPDATE
		SET max_active_invocations = EXCLUDED.max_active_invocations,
			max_invocations_per_hour = EXCLUDED.max_invocations_per_hour,
			modified_at = EXCLUDED.modified_at
		RETURNING created_at, modified_at
	`

	err := s.db.QueryRowContext(ctx, query,
		quota.OwnerId,
		quota.MaxActiveInvocations,
		quota.MaxInvocationsPerHour,
		quota.CreatedAt,
		quota.ModifiedAt,
	).Scan(&quota.CreatedAt, &quota.ModifiedAt)

	if err != nil {
		logger.Error("Failed to set quota for owner_id=%s: %v", quota.OwnerId, err)
		return fmt.Errorf("failed to set quota: %w", err)
	}

	logger.Info("Quota set for owner_id=%s: active=%d, hourly=%d",
		quota.OwnerId, quota.MaxActiveInvocations, quota.MaxInvocationsPerHour)

	return nil
}

// Delete removes quota for a user (reverts to defaults)
func (s *AddonInvocationQuotaDatabaseStore) Delete(ctx context.Context, ownerID uuid.UUID) error {
	logger := slogging.Get()

	query := `DELETE FROM addon_invocation_quotas WHERE owner_internal_uuid = $1`

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
