package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ClientCredentialService handles client credential generation and management
type ClientCredentialService struct {
	authService *auth.Service
}

// NewClientCredentialService creates a new client credential service
func NewClientCredentialService(authService *auth.Service) *ClientCredentialService {
	return &ClientCredentialService{
		authService: authService,
	}
}

// CreateClientCredentialRequest contains parameters for creating a new client credential
type CreateClientCredentialRequest struct {
	Name        string     `json:"name" binding:"required,min=1,max=100"`
	Description string     `json:"description" binding:"max=500"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// CreateClientCredentialResponse contains the response from creating a client credential
// WARNING: The client_secret is ONLY returned at creation time and cannot be retrieved later
type CreateClientCredentialResponse struct {
	ID           uuid.UUID  `json:"id"`
	ClientID     string     `json:"client_id"`
	ClientSecret string     `json:"client_secret"` //nolint:gosec // G117 - OAuth client credential response field
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// ClientCredentialInfoInternal represents a client credential without the secret (internal type)
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
func (s *ClientCredentialService) Create(ctx context.Context, ownerUUID uuid.UUID, req CreateClientCredentialRequest) (*CreateClientCredentialResponse, error) {
	// 1. Generate client_id: tmi_cc_{base64url(16_bytes)}
	clientIDBytes := make([]byte, 16)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return nil, fmt.Errorf("failed to generate client_id: %w", err)
	}
	clientID := fmt.Sprintf("tmi_cc_%s", base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(clientIDBytes))

	// 2. Generate client_secret: 32 bytes = 43 chars base64url
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, fmt.Errorf("failed to generate client_secret: %w", err)
	}
	clientSecret := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(secretBytes)

	// 3. Hash client_secret with bcrypt (cost 10)
	hash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), 10)
	if err != nil {
		return nil, fmt.Errorf("failed to hash client_secret: %w", err)
	}

	// 4. Store in database
	cred, err := s.authService.CreateClientCredential(ctx, auth.ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         clientID,
		ClientSecretHash: string(hash),
		Name:             req.Name,
		Description:      req.Description,
		ExpiresAt:        req.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client credential: %w", err)
	}

	// 5. Return response with plaintext secret (ONLY TIME IT'S VISIBLE)
	return &CreateClientCredentialResponse{
		ID:           cred.ID,
		ClientID:     cred.ClientID,
		ClientSecret: clientSecret, // plaintext secret
		Name:         cred.Name,
		Description:  cred.Description,
		CreatedAt:    cred.CreatedAt,
		ExpiresAt:    cred.ExpiresAt,
	}, nil
}

// List retrieves all client credentials for the specified owner (without secrets)
func (s *ClientCredentialService) List(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredentialInfoInternal, error) {
	creds, err := s.authService.ListClientCredentialsByOwner(ctx, ownerUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to list client credentials: %w", err)
	}

	// Convert to info structs without secrets
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
func (s *ClientCredentialService) Delete(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.authService.DeleteClientCredential(ctx, credID, ownerUUID); err != nil {
		return fmt.Errorf("failed to delete client credential: %w", err)
	}
	return nil
}

// Deactivate soft-deletes a client credential (sets is_active = false)
func (s *ClientCredentialService) Deactivate(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.authService.DeactivateClientCredential(ctx, credID, ownerUUID); err != nil {
		return fmt.Errorf("failed to deactivate client credential: %w", err)
	}
	return nil
}
