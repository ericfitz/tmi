package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ClientCredentialService handles client credential generation and management
// SEM@d5abf2700f59ec278f7e45a485c9d19c90b0050f: service for creating, listing, and revoking machine-to-machine client credentials (reads DB)
type ClientCredentialService struct {
	authService *auth.Service
}

// NewClientCredentialService creates a new client credential service
// SEM@d5abf2700f59ec278f7e45a485c9d19c90b0050f: build a ClientCredentialService from an auth service (pure)
func NewClientCredentialService(authService *auth.Service) *ClientCredentialService {
	return &ClientCredentialService{
		authService: authService,
	}
}

// CreateClientCredentialRequest contains parameters for creating a new client credential
// SEM@bb016c3822e5987a6d2abf81bf6fcf80682851a4: parameters for creating a new client credential, including name, direct_write flag, addon link, and optional expiry (pure)
type CreateClientCredentialRequest struct {
	Name        string     `json:"name" binding:"required,min=1,max=100"`
	Description string     `json:"description" binding:"max=500"`
	DirectWrite bool       `json:"direct_write,omitempty"`
	AddonID     string     `json:"addon_id,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// CreateClientCredentialResponse contains the response from creating a client credential
// WARNING: The client_secret is ONLY returned at creation time and cannot be retrieved later
// SEM@bb016c3822e5987a6d2abf81bf6fcf80682851a4: response for a new client credential including the plaintext secret, direct_write flag, and addon link, shown once (pure)
type CreateClientCredentialResponse struct {
	ID           uuid.UUID  `json:"id"`
	ClientID     string     `json:"client_id"`
	ClientSecret string     `json:"client_secret"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	DirectWrite  bool       `json:"direct_write"`
	AddonID      string     `json:"addon_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// ClientCredentialInfoInternal represents a client credential without the secret (internal type)
// SEM@bb016c3822e5987a6d2abf81bf6fcf80682851a4: client credential metadata including direct_write flag and addon link, without the secret, for listing responses (pure)
type ClientCredentialInfoInternal struct {
	ID          uuid.UUID  `json:"id"`
	ClientID    string     `json:"client_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	IsActive    bool       `json:"is_active"`
	DirectWrite bool       `json:"direct_write"`
	AddonID     string     `json:"addon_id,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ModifiedAt  time.Time  `json:"modified_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// Create generates a new client credential for the specified owner
// The client_secret is only returned once and cannot be retrieved later (GitHub PAT pattern)
// SEM@690b6a91dd88122c76b34cde3e9c1b6e4e5d7715: generate and store a new bcrypt-hashed client credential, returning the plaintext secret once (reads DB)
func (s *ClientCredentialService) Create(ctx context.Context, ownerUUID uuid.UUID, req CreateClientCredentialRequest) (*CreateClientCredentialResponse, error) {
	// 1. Generate client_id: tmi_cc_{base64url(16_bytes)}
	clientIDBytes := make([]byte, 16)
	if _, err := rand.Read(clientIDBytes); err != nil {
		dberrors.HandleFatal(fmt.Errorf("crypto/rand failure generating client_id: %w", err))
	}
	clientID := fmt.Sprintf("tmi_cc_%s", base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(clientIDBytes))

	// 2. Generate client_secret: 32 bytes = 43 chars base64url
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		dberrors.HandleFatal(fmt.Errorf("crypto/rand failure generating client_secret: %w", err))
	}
	clientSecret := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(secretBytes)

	// 3. Hash client_secret with bcrypt (cost 10)
	hash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), 10)
	if err != nil {
		dberrors.HandleFatal(fmt.Errorf("bcrypt failure hashing client_secret: %w", err))
	}

	// 4. Store in database. One INSERT, so no transaction; and no retry, since a
	// retry after an ambiguous commit would leave an orphaned credential whose
	// secret the caller never saw (#911).
	cred, dbErr := s.authService.CreateClientCredential(ctx, auth.ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         clientID,
		ClientSecretHash: string(hash),
		Name:             req.Name,
		Description:      req.Description,
		DirectWrite:      req.DirectWrite,
		AddonID:          req.AddonID,
		ExpiresAt:        req.ExpiresAt,
	})

	if dbErr != nil {
		if dberrors.IsFatal(dbErr) {
			dberrors.HandleFatal(fmt.Errorf("database error creating client credential: %w", dbErr))
		}
		return nil, dbErr
	}

	// 5. Return response with plaintext secret (ONLY TIME IT'S VISIBLE)
	return &CreateClientCredentialResponse{
		ID:           cred.ID,
		ClientID:     cred.ClientID,
		ClientSecret: clientSecret,
		Name:         cred.Name,
		Description:  cred.Description,
		DirectWrite:  cred.DirectWrite,
		AddonID:      cred.AddonID,
		CreatedAt:    cred.CreatedAt,
		ExpiresAt:    cred.ExpiresAt,
	}, nil
}

// List retrieves all client credentials for the specified owner (without secrets)
// SEM@690b6a91dd88122c76b34cde3e9c1b6e4e5d7715: list all client credentials for an owner, excluding secrets (reads DB)
func (s *ClientCredentialService) List(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredentialInfoInternal, error) {
	var creds []*auth.ClientCredential
	dbErr := authdb.WithRetryableGormRead(ctx, authdb.DefaultRetryConfig(), func() error {
		var err error
		creds, err = s.authService.ListClientCredentialsByOwner(ctx, ownerUUID)
		return err
	})

	if dbErr != nil {
		if dberrors.IsFatal(dbErr) {
			dberrors.HandleFatal(fmt.Errorf("database error listing client credentials: %w", dbErr))
		}
		return nil, dbErr
	}

	result := make([]*ClientCredentialInfoInternal, 0, len(creds))
	for _, cred := range creds {
		result = append(result, &ClientCredentialInfoInternal{
			ID:          cred.ID,
			ClientID:    cred.ClientID,
			Name:        cred.Name,
			Description: cred.Description,
			IsActive:    cred.IsActive,
			DirectWrite: cred.DirectWrite,
			AddonID:     cred.AddonID,
			LastUsedAt:  cred.LastUsedAt,
			CreatedAt:   cred.CreatedAt,
			ModifiedAt:  cred.ModifiedAt,
			ExpiresAt:   cred.ExpiresAt,
		})
	}

	return result, nil
}

// Delete permanently deletes a client credential
// SEM@d5abf2700f59ec278f7e45a485c9d19c90b0050f: permanently delete a client credential by ID and owner (reads DB)
func (s *ClientCredentialService) Delete(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	// Single DELETE: no transaction needed, and no retry, matching Deactivate (#911).
	dbErr := s.authService.DeleteClientCredential(ctx, credID, ownerUUID)

	if dbErr != nil {
		if dberrors.IsFatal(dbErr) {
			dberrors.HandleFatal(fmt.Errorf("database error deleting client credential: %w", dbErr))
		}
		return dbErr
	}

	return nil
}

// Deactivate soft-deletes a client credential (sets is_active = false)
// SEM@d5abf2700f59ec278f7e45a485c9d19c90b0050f: soft-disable a client credential by marking it inactive (reads DB)
func (s *ClientCredentialService) Deactivate(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.authService.DeactivateClientCredential(ctx, credID, ownerUUID); err != nil {
		if dberrors.IsFatal(err) {
			dberrors.HandleFatal(fmt.Errorf("database error deactivating client credential: %w", err))
		}
		return err
	}
	return nil
}
