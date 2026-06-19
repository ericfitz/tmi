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
	"gorm.io/gorm"
)

// ClientCredentialService handles client credential generation and management
// SEM@9bf8890e7d4a04bdbb3f0e80fb295392276e3a5d: service for creating, listing, and revoking machine-to-machine client credentials (reads DB)
type ClientCredentialService struct {
	authService *auth.Service
	gormDB      *gorm.DB
}

// NewClientCredentialService creates a new client credential service
// SEM@9bf8890e7d4a04bdbb3f0e80fb295392276e3a5d: build a ClientCredentialService from an auth service (pure)
func NewClientCredentialService(authService *auth.Service) *ClientCredentialService {
	return &ClientCredentialService{
		authService: authService,
		gormDB:      authService.GormDB(),
	}
}

// CreateClientCredentialRequest contains parameters for creating a new client credential
// SEM@99c8cc4c042f4729b89e24981a18dba21b40be17: parameters for creating a new client credential, including name and optional expiry (pure)
type CreateClientCredentialRequest struct {
	Name        string     `json:"name" binding:"required,min=1,max=100"`
	Description string     `json:"description" binding:"max=500"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// CreateClientCredentialResponse contains the response from creating a client credential
// WARNING: The client_secret is ONLY returned at creation time and cannot be retrieved later
// SEM@99c8cc4c042f4729b89e24981a18dba21b40be17: response for a new client credential including the plaintext secret shown only once (pure)
type CreateClientCredentialResponse struct {
	ID           uuid.UUID  `json:"id"`
	ClientID     string     `json:"client_id"`
	ClientSecret string     `json:"client_secret"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// ClientCredentialInfoInternal represents a client credential without the secret (internal type)
// SEM@99c8cc4c042f4729b89e24981a18dba21b40be17: client credential metadata without the secret for listing responses (pure)
type ClientCredentialInfoInternal struct {
	ID          uuid.UUID  `json:"id"`
	ClientID    string     `json:"client_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	IsActive    bool       `json:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ModifiedAt  time.Time  `json:"modified_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// Create generates a new client credential for the specified owner
// The client_secret is only returned once and cannot be retrieved later (GitHub PAT pattern)
// SEM@9bf8890e7d4a04bdbb3f0e80fb295392276e3a5d: generate and store a new bcrypt-hashed client credential, returning the plaintext secret once (reads DB)
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

	// 4. Store in database with retryable transaction
	var cred *auth.ClientCredential
	dbErr := authdb.WithRetryableGormTransaction(ctx, s.gormDB, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		var createErr error
		cred, createErr = s.authService.CreateClientCredential(ctx, auth.ClientCredentialCreateParams{
			OwnerUUID:        ownerUUID,
			ClientID:         clientID,
			ClientSecretHash: string(hash),
			Name:             req.Name,
			Description:      req.Description,
			ExpiresAt:        req.ExpiresAt,
		})
		return createErr
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
		CreatedAt:    cred.CreatedAt,
		ExpiresAt:    cred.ExpiresAt,
	}, nil
}

// List retrieves all client credentials for the specified owner (without secrets)
// SEM@9bf8890e7d4a04bdbb3f0e80fb295392276e3a5d: list all client credentials for an owner, excluding secrets (reads DB)
func (s *ClientCredentialService) List(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredentialInfoInternal, error) {
	var creds []*auth.ClientCredential
	dbErr := authdb.WithRetryableGormTransaction(ctx, s.gormDB, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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
			LastUsedAt:  cred.LastUsedAt,
			CreatedAt:   cred.CreatedAt,
			ModifiedAt:  cred.ModifiedAt,
			ExpiresAt:   cred.ExpiresAt,
		})
	}

	return result, nil
}

// Delete permanently deletes a client credential
// SEM@9bf8890e7d4a04bdbb3f0e80fb295392276e3a5d: permanently delete a client credential by ID and owner (reads DB)
func (s *ClientCredentialService) Delete(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	dbErr := authdb.WithRetryableGormTransaction(ctx, s.gormDB, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		return s.authService.DeleteClientCredential(ctx, credID, ownerUUID)
	})

	if dbErr != nil {
		if dberrors.IsFatal(dbErr) {
			dberrors.HandleFatal(fmt.Errorf("database error deleting client credential: %w", dbErr))
		}
		return dbErr
	}

	return nil
}

// Deactivate soft-deletes a client credential (sets is_active = false)
// SEM@9bf8890e7d4a04bdbb3f0e80fb295392276e3a5d: soft-disable a client credential by marking it inactive (reads DB)
func (s *ClientCredentialService) Deactivate(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.authService.DeactivateClientCredential(ctx, credID, ownerUUID); err != nil {
		if dberrors.IsFatal(err) {
			dberrors.HandleFatal(fmt.Errorf("database error deactivating client credential: %w", err))
		}
		return err
	}
	return nil
}
