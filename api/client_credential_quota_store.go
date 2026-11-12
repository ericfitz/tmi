package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// Default quota value
const (
	DefaultClientCredentialQuota = 10
)

// ClientCredentialQuotaStore defines the interface for client credential quota operations
type ClientCredentialQuotaStore interface {
	// GetClientCredentialQuota retrieves the maximum number of credentials allowed for a user
	GetClientCredentialQuota(ctx context.Context, userUUID uuid.UUID) (int, error)

	// GetClientCredentialCount retrieves the current number of active credentials for a user
	GetClientCredentialCount(ctx context.Context, userUUID uuid.UUID) (int, error)

	// CheckClientCredentialQuota verifies if a user can create a new credential
	CheckClientCredentialQuota(ctx context.Context, userUUID uuid.UUID) error
}

// DatabaseClientCredentialQuotaStore implements ClientCredentialQuotaStore using auth service and global quota store
type DatabaseClientCredentialQuotaStore struct {
	authService      *auth.Service
	defaultQuota     int
	globalQuotaStore UserAPIQuotaStoreInterface // Re-use existing global quota infrastructure
}

// NewDatabaseClientCredentialQuotaStore creates a new client credential quota store
func NewDatabaseClientCredentialQuotaStore(authService *auth.Service, defaultQuota int, globalStore UserAPIQuotaStoreInterface) *DatabaseClientCredentialQuotaStore {
	if defaultQuota <= 0 {
		defaultQuota = DefaultClientCredentialQuota
	}
	return &DatabaseClientCredentialQuotaStore{
		authService:      authService,
		defaultQuota:     defaultQuota,
		globalQuotaStore: globalStore,
	}
}

// GetClientCredentialQuota retrieves the maximum number of credentials allowed for a user
func (s *DatabaseClientCredentialQuotaStore) GetClientCredentialQuota(ctx context.Context, userUUID uuid.UUID) (int, error) {
	logger := slogging.Get()

	// For now, use the default quota
	// TODO: Integrate with admin quota management system for per-user overrides
	logger.Debug("Using default quota for user_uuid=%s: %d client credentials", userUUID, s.defaultQuota)
	return s.defaultQuota, nil
}

// GetClientCredentialCount retrieves the current number of active credentials for a user
func (s *DatabaseClientCredentialQuotaStore) GetClientCredentialCount(ctx context.Context, userUUID uuid.UUID) (int, error) {
	logger := slogging.Get()

	creds, err := s.authService.ListClientCredentialsByOwner(ctx, userUUID)
	if err != nil {
		logger.Error("Failed to list client credentials for user_uuid=%s: %v", userUUID, err)
		return 0, fmt.Errorf("failed to count client credentials: %w", err)
	}

	// Count only active credentials
	count := 0
	for _, cred := range creds {
		if cred.IsActive {
			count++
		}
	}

	logger.Debug("Counted %d active client credentials for user_uuid=%s", count, userUUID)
	return count, nil
}

// CheckClientCredentialQuota verifies if a user can create a new credential
func (s *DatabaseClientCredentialQuotaStore) CheckClientCredentialQuota(ctx context.Context, userUUID uuid.UUID) error {
	logger := slogging.Get()

	quota, err := s.GetClientCredentialQuota(ctx, userUUID)
	if err != nil {
		logger.Error("Failed to get quota for user_uuid=%s: %v", userUUID, err)
		return fmt.Errorf("failed to get quota: %w", err)
	}

	count, err := s.GetClientCredentialCount(ctx, userUUID)
	if err != nil {
		logger.Error("Failed to get count for user_uuid=%s: %v", userUUID, err)
		return fmt.Errorf("failed to get count: %w", err)
	}

	if count >= quota {
		logger.Warn("Client credential quota exceeded for user_uuid=%s: %d/%d", userUUID, count, quota)
		return fmt.Errorf("client credential quota exceeded: %d/%d", count, quota)
	}

	logger.Debug("Client credential quota check passed for user_uuid=%s: %d/%d", userUUID, count, quota)
	return nil
}

// GlobalClientCredentialQuotaStore is the global singleton for client credential quota
var GlobalClientCredentialQuotaStore ClientCredentialQuotaStore
