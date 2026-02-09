// Package repository provides database repository interfaces and implementations
// for the auth service. These interfaces abstract database operations to support
// multiple database backends through GORM.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Common errors returned by repositories
var (
	ErrUserNotFound             = errors.New("user not found")
	ErrClientCredentialNotFound = errors.New("client credential not found")
	ErrGroupNotFound            = errors.New("group not found")
	ErrUnauthorized             = errors.New("unauthorized")
)

// User represents a user entity for repository operations
type User struct {
	InternalUUID   string
	Provider       string
	ProviderUserID string
	Email          string
	Name           string
	EmailVerified  bool
	AccessToken    *string
	RefreshToken   *string
	TokenExpiry    *time.Time
	CreatedAt      time.Time
	ModifiedAt     time.Time
	LastLogin      *time.Time
}

// UserProvider represents a user's OAuth provider information
type UserProvider struct {
	ID             string
	UserID         string
	Provider       string
	ProviderUserID string
	Email          string
	IsPrimary      bool
	CreatedAt      time.Time
	LastLogin      time.Time
}

// ClientCredential represents an OAuth 2.0 client credential
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
type ClientCredentialCreateParams struct {
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	ExpiresAt        *time.Time
}

// DeletionResult contains statistics about user/group deletion operations
type DeletionResult struct {
	ThreatModelsTransferred int
	ThreatModelsDeleted     int
	UserEmail               string
}

// GroupDeletionResult contains statistics about group deletion operations
type GroupDeletionResult struct {
	ThreatModelsDeleted  int
	ThreatModelsRetained int
	GroupName            string
}

// TransferResult contains statistics about an ownership transfer operation
type TransferResult struct {
	ThreatModelIDs    []string
	SurveyResponseIDs []string
}

// UserRepository handles user CRUD operations
type UserRepository interface {
	// GetByEmail retrieves a user by email address
	GetByEmail(ctx context.Context, email string) (*User, error)

	// GetByID retrieves a user by internal UUID
	GetByID(ctx context.Context, id string) (*User, error)

	// GetByProviderID retrieves a user by provider and provider user ID
	GetByProviderID(ctx context.Context, provider, providerUserID string) (*User, error)

	// GetByProviderAndEmail retrieves a user by provider and email address
	GetByProviderAndEmail(ctx context.Context, provider, email string) (*User, error)

	// GetByAnyProviderID retrieves a user by provider user ID across all providers
	GetByAnyProviderID(ctx context.Context, providerUserID string) (*User, error)

	// GetProviders returns the OAuth providers for a user
	GetProviders(ctx context.Context, userID string) ([]UserProvider, error)

	// GetPrimaryProviderID returns the primary provider user ID for a user
	GetPrimaryProviderID(ctx context.Context, userID string) (string, error)

	// Create creates a new user
	Create(ctx context.Context, user *User) (*User, error)

	// Update updates an existing user
	Update(ctx context.Context, user *User) error

	// Delete deletes a user by internal UUID
	Delete(ctx context.Context, id string) error
}

// ClientCredentialRepository handles client credential operations
type ClientCredentialRepository interface {
	// Create creates a new client credential
	Create(ctx context.Context, params ClientCredentialCreateParams) (*ClientCredential, error)

	// GetByClientID retrieves an active client credential by client ID
	GetByClientID(ctx context.Context, clientID string) (*ClientCredential, error)

	// ListByOwner retrieves all client credentials owned by a user
	ListByOwner(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredential, error)

	// UpdateLastUsed updates the last_used_at timestamp for a client credential
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error

	// Deactivate deactivates a client credential (soft delete)
	Deactivate(ctx context.Context, id, ownerUUID uuid.UUID) error

	// Delete permanently deletes a client credential
	Delete(ctx context.Context, id, ownerUUID uuid.UUID) error
}

// DeletionRepository handles user and group deletion with data cleanup
type DeletionRepository interface {
	// DeleteUserAndData deletes a user and handles ownership transfer for threat models
	DeleteUserAndData(ctx context.Context, userEmail string) (*DeletionResult, error)

	// DeleteGroupAndData deletes a group by internal UUID and handles threat model cleanup
	// Uses internal_uuid for precise identification to avoid issues with duplicate group_names
	DeleteGroupAndData(ctx context.Context, internalUUID string) (*GroupDeletionResult, error)

	// TransferOwnership transfers all owned threat models and survey responses
	// from sourceUserUUID to targetUserUUID within a single transaction.
	// The source user is downgraded to "writer" role on all transferred items.
	TransferOwnership(ctx context.Context, sourceUserUUID, targetUserUUID string) (*TransferResult, error)
}
