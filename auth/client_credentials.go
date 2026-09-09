package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// ClientCredential represents an OAuth 2.0 client credential for machine-to-machine authentication
// SEM@690b6a91dd88122c76b34cde3e9c1b6e4e5d7715: domain model for an OAuth 2.0 client credential, with an opt-in direct_write authorization flag
type ClientCredential struct {
	ID               uuid.UUID
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	IsActive         bool
	DirectWrite      bool
	LastUsedAt       *time.Time
	CreatedAt        time.Time
	ModifiedAt       time.Time
	ExpiresAt        *time.Time
}

// ClientCredentialCreateParams contains parameters for creating a new client credential
// SEM@690b6a91dd88122c76b34cde3e9c1b6e4e5d7715: parameters for creating a new client credential, including the direct_write flag
type ClientCredentialCreateParams struct {
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	DirectWrite      bool
	ExpiresAt        *time.Time
}

// CreateClientCredential creates a new client credential in the database
// SEM@690b6a91dd88122c76b34cde3e9c1b6e4e5d7715: store a new client credential and return the persisted entity (mutates DB)
func (s *Service) CreateClientCredential(ctx context.Context, params ClientCredentialCreateParams) (*ClientCredential, error) {
	repoParams := repository.ClientCredentialCreateParams{
		OwnerUUID:        params.OwnerUUID,
		ClientID:         params.ClientID,
		ClientSecretHash: params.ClientSecretHash,
		Name:             params.Name,
		Description:      params.Description,
		DirectWrite:      params.DirectWrite,
		ExpiresAt:        params.ExpiresAt,
	}

	repoCred, err := s.credRepo.Create(ctx, repoParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create client credential: %w", err)
	}

	return convertRepoCredToServiceCred(repoCred), nil
}

// GetClientCredentialByClientID retrieves a client credential by its client_id
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch a client credential by its client_id string (reads DB)
func (s *Service) GetClientCredentialByClientID(ctx context.Context, clientID string) (*ClientCredential, error) {
	repoCred, err := s.credRepo.GetByClientID(ctx, clientID)
	if err != nil {
		if errors.Is(err, repository.ErrClientCredentialNotFound) {
			return nil, fmt.Errorf("client credential not found")
		}
		return nil, fmt.Errorf("failed to get client credential: %w", err)
	}

	return convertRepoCredToServiceCred(repoCred), nil
}

// ListClientCredentialsByOwner retrieves all client credentials for a given owner
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: list all client credentials belonging to a given owner (reads DB)
func (s *Service) ListClientCredentialsByOwner(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredential, error) {
	repoCreds, err := s.credRepo.ListByOwner(ctx, ownerUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to list client credentials: %w", err)
	}

	credentials := make([]*ClientCredential, 0, len(repoCreds))
	for _, rc := range repoCreds {
		credentials = append(credentials, convertRepoCredToServiceCred(rc))
	}

	return credentials, nil
}

// UpdateClientCredentialLastUsed updates the last_used_at timestamp for a client credential
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: update the last-used timestamp for a client credential (reads DB)
func (s *Service) UpdateClientCredentialLastUsed(ctx context.Context, id uuid.UUID) error {
	err := s.credRepo.UpdateLastUsed(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrClientCredentialNotFound) {
			return fmt.Errorf("client credential not found")
		}
		return fmt.Errorf("failed to update last_used_at: %w", err)
	}
	return nil
}

// DeactivateClientCredential deactivates a client credential (soft delete)
// and revokes its outstanding service-account tokens.
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: soft-delete a client credential and revoke its issued tokens (mutates shared state)
func (s *Service) DeactivateClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.credRepo.Deactivate(ctx, id, ownerUUID); err != nil {
		return err // Repository already returns appropriate error message
	}
	s.revokeCredentialTokens(ctx, id)
	return nil
}

// DeleteClientCredential permanently deletes a client credential and revokes
// its outstanding service-account tokens.
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: permanently delete a client credential and revoke its issued tokens (mutates shared state)
func (s *Service) DeleteClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.credRepo.Delete(ctx, id, ownerUUID); err != nil {
		return err // Repository already returns appropriate error message
	}
	s.revokeCredentialTokens(ctx, id)
	return nil
}

// revokeCredentialTokens marks tokens already minted from a credential as
// revoked (#862). Best-effort by design: the credential row is already gone,
// so the delete must not be reported as failed. If Redis is unreachable the
// JWT middleware is failing closed on every request anyway (#660); the only
// residual gap is a marker lost across a Redis outage, bounded by the token
// lifetime.
// SEM@48ae1daff849c4fbb75fe51c29185be3f169d27d: revoke service-account tokens of a client credential, best-effort (reads DB)
func (s *Service) revokeCredentialTokens(ctx context.Context, id uuid.UUID) {
	if s.dbManager == nil || s.dbManager.Redis() == nil {
		slogging.Get().Warn("Client credential token revocation skipped: Redis not available credential_id=%v", id)
		return
	}
	_ = NewTokenBlacklist(s.dbManager.Redis().GetClient(), s.keyManager).
		RevokeCredential(ctx, id.String(), s.config.GetJWTDuration())
}

// convertRepoCredToServiceCred converts a repository ClientCredential to a service ClientCredential
// SEM@690b6a91dd88122c76b34cde3e9c1b6e4e5d7715: convert a repository client credential to the service-layer credential type (pure)
func convertRepoCredToServiceCred(rc *repository.ClientCredential) *ClientCredential {
	return &ClientCredential{
		ID:               rc.ID,
		OwnerUUID:        rc.OwnerUUID,
		ClientID:         rc.ClientID,
		ClientSecretHash: rc.ClientSecretHash,
		Name:             rc.Name,
		Description:      rc.Description,
		IsActive:         rc.IsActive,
		DirectWrite:      rc.DirectWrite,
		LastUsedAt:       rc.LastUsedAt,
		CreatedAt:        rc.CreatedAt,
		ModifiedAt:       rc.ModifiedAt,
		ExpiresAt:        rc.ExpiresAt,
	}
}
