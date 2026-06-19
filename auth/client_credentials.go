package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/google/uuid"
)

// ClientCredential represents an OAuth 2.0 client credential for machine-to-machine authentication
// SEM@2e1e229947d57021bf27a7c51c052e3e2a18c98e: domain model for an OAuth 2.0 client credential used in machine-to-machine authentication
type ClientCredential struct {
	ID               uuid.UUID
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	IsActive         bool
	LastUsedAt       *time.Time
	CreatedAt        time.Time
	ModifiedAt       time.Time
	ExpiresAt        *time.Time
}

// ClientCredentialCreateParams contains parameters for creating a new client credential
// SEM@2e1e229947d57021bf27a7c51c052e3e2a18c98e: parameters for creating a new client credential
type ClientCredentialCreateParams struct {
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	ExpiresAt        *time.Time
}

// CreateClientCredential creates a new client credential in the database
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: store a new client credential and return the persisted entity (mutates DB)
func (s *Service) CreateClientCredential(ctx context.Context, params ClientCredentialCreateParams) (*ClientCredential, error) {
	repoParams := repository.ClientCredentialCreateParams{
		OwnerUUID:        params.OwnerUUID,
		ClientID:         params.ClientID,
		ClientSecretHash: params.ClientSecretHash,
		Name:             params.Name,
		Description:      params.Description,
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: soft-delete a client credential owned by a given user (mutates shared state)
func (s *Service) DeactivateClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	err := s.credRepo.Deactivate(ctx, id, ownerUUID)
	if err != nil {
		return err // Repository already returns appropriate error message
	}
	return nil
}

// DeleteClientCredential permanently deletes a client credential
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: permanently delete a client credential owned by a given user (mutates shared state)
func (s *Service) DeleteClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	err := s.credRepo.Delete(ctx, id, ownerUUID)
	if err != nil {
		return err // Repository already returns appropriate error message
	}
	return nil
}

// convertRepoCredToServiceCred converts a repository ClientCredential to a service ClientCredential
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: convert a repository client credential to the service-layer credential type (pure)
func convertRepoCredToServiceCred(rc *repository.ClientCredential) *ClientCredential {
	return &ClientCredential{
		ID:               rc.ID,
		OwnerUUID:        rc.OwnerUUID,
		ClientID:         rc.ClientID,
		ClientSecretHash: rc.ClientSecretHash,
		Name:             rc.Name,
		Description:      rc.Description,
		IsActive:         rc.IsActive,
		LastUsedAt:       rc.LastUsedAt,
		CreatedAt:        rc.CreatedAt,
		ModifiedAt:       rc.ModifiedAt,
		ExpiresAt:        rc.ExpiresAt,
	}
}
