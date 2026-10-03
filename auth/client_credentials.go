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
// SEM@bb016c3822e5987a6d2abf81bf6fcf80682851a4: domain model for an OAuth 2.0 client credential, with opt-in direct_write flag and addon link
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
// SEM@bb016c3822e5987a6d2abf81bf6fcf80682851a4: parameters for creating a new client credential, including direct_write flag and addon link
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
// SEM@690b6a91dd88122c76b34cde3e9c1b6e4e5d7715: store a new client credential and return the persisted entity (mutates DB)
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
// SEM@24d835d0aa601cdfaea187838ff139f49228ea26: soft-delete a client credential and revoke its issued tokens (mutates shared state)
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
// SEM@24d835d0aa601cdfaea187838ff139f49228ea26: revoke a client credential's issued tokens then permanently delete it, failing closed (mutates shared state)
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
// SEM@24d835d0aa601cdfaea187838ff139f49228ea26: revoke service-account tokens of a client credential, best-effort (reads DB)
func (s *Service) revokeCredentialTokens(ctx context.Context, id uuid.UUID) {
	if err := s.revokeCredentialTokensStrict(ctx, id); err != nil {
		slogging.Get().Warn("Client credential token revocation failed credential_id=%v error=%v", id, err)
	}
}

// revokeCredentialTokensStrict marks tokens already minted from a credential as
// revoked (#862), returning ErrRevocationStorage if Redis is unavailable or the
// write fails.
// SEM@24d835d0aa601cdfaea187838ff139f49228ea26: revoke service-account tokens of a client credential, returning storage errors (reads DB)
func (s *Service) revokeCredentialTokensStrict(ctx context.Context, id uuid.UUID) error {
	if s.dbManager == nil || s.dbManager.Redis() == nil {
		return fmt.Errorf("%w: Redis not available", ErrRevocationStorage)
	}
	return NewTokenBlacklist(s.dbManager.Redis().GetClient(), s.keyManager).
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
		AddonID:          rc.AddonID,
		LastUsedAt:       rc.LastUsedAt,
		CreatedAt:        rc.CreatedAt,
		ModifiedAt:       rc.ModifiedAt,
		ExpiresAt:        rc.ExpiresAt,
	}
}
