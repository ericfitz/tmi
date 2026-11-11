package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// ClientCredential represents an OAuth 2.0 client credential for machine-to-machine authentication
type ClientCredential struct {
	ID               uuid.UUID
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	IsActive         bool
	DirectWrite      bool
	AddonID          string // linked addon for self-delivery suppression (#883); empty when unlinked
	LastUsedAt       *time.Time
	CreatedAt        time.Time
	ModifiedAt       time.Time
	ExpiresAt        *time.Time
}

// ClientCredentialCreateParams contains parameters for creating a new client credential
type ClientCredentialCreateParams struct {
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	DirectWrite      bool
	AddonID          string
	ExpiresAt        *time.Time
}

// CreateClientCredential creates a new client credential in the database
func (s *Service) CreateClientCredential(ctx context.Context, params ClientCredentialCreateParams) (*ClientCredential, error) {
	repoParams := repository.ClientCredentialCreateParams{
		OwnerUUID:        params.OwnerUUID,
		ClientID:         params.ClientID,
		ClientSecretHash: params.ClientSecretHash,
		Name:             params.Name,
		Description:      params.Description,
		DirectWrite:      params.DirectWrite,
		AddonID:          params.AddonID,
		ExpiresAt:        params.ExpiresAt,
	}

	repoCred, err := s.credRepo.Create(ctx, repoParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create client credential: %w", err)
	}

	return convertRepoCredToServiceCred(repoCred), nil
}

// GetClientCredentialByClientID retrieves a client credential by its client_id
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
func (s *Service) DeactivateClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.credRepo.Deactivate(ctx, id, ownerUUID); err != nil {
		return err // Repository already returns appropriate error message
	}
	s.revokeCredentialTokens(ctx, id)
	return nil
}

// DeleteClientCredential permanently deletes a client credential, revoking its
// outstanding service-account tokens first. Fail closed: if the revocation
// cannot be stored it returns ErrRevocationStorage with nothing deleted, so the
// caller can retry. Ownership is checked before revoking so a caller cannot
// blacklist another user's credential.
func (s *Service) DeleteClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	owned, err := s.credRepo.ListByOwner(ctx, ownerUUID)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(owned, func(c *repository.ClientCredential) bool { return c.ID == id }) {
		return repository.ErrClientCredentialNotFound
	}
	if err := s.revokeCredentialTokensStrict(ctx, id); err != nil {
		return err
	}
	return s.credRepo.Delete(ctx, id, ownerUUID)
}

// revokeCredentialTokens is the best-effort variant (deactivate and user-delete
// sweeps): failures are logged, not returned.
func (s *Service) revokeCredentialTokens(ctx context.Context, id uuid.UUID) {
	if err := s.revokeCredentialTokensStrict(ctx, id); err != nil {
		slogging.Get().Warn("Client credential token revocation failed credential_id=%v error=%v", id, err)
	}
}

// revokeCredentialTokensStrict marks tokens already minted from a credential as
// revoked (#862), returning ErrRevocationStorage if Redis is unavailable or the
// write fails.
func (s *Service) revokeCredentialTokensStrict(ctx context.Context, id uuid.UUID) error {
	if s.dbManager == nil || s.dbManager.Redis() == nil {
		return fmt.Errorf("%w: Redis not available", ErrRevocationStorage)
	}
	return NewTokenBlacklist(s.dbManager.Redis().GetClient(), s.keyManager).
		RevokeCredential(ctx, id.String(), s.config.GetJWTDuration())
}

// convertRepoCredToServiceCred converts a repository ClientCredential to a service ClientCredential
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
		AddonID:          rc.AddonID,
		LastUsedAt:       rc.LastUsedAt,
		CreatedAt:        rc.CreatedAt,
		ModifiedAt:       rc.ModifiedAt,
		ExpiresAt:        rc.ExpiresAt,
	}
}
