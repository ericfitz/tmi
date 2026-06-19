// Package repository provides database repository interfaces and implementations
// for the auth service. These interfaces abstract database operations to support
// multiple database backends through GORM.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
)

// Common errors returned by repositories.
// Each wraps the corresponding dberrors sentinel so handlers can check
// either the entity-specific error or the generic category.
var (
	ErrUserNotFound             = fmt.Errorf("user: %w", dberrors.ErrNotFound)
	ErrClientCredentialNotFound = fmt.Errorf("client credential: %w", dberrors.ErrNotFound)
	ErrGroupNotFound            = fmt.Errorf("group: %w", dberrors.ErrNotFound)
	ErrUnauthorized             = fmt.Errorf("unauthorized: %w", dberrors.ErrNotFound)
)

// User represents a user entity for repository operations
// SEM@24dcbaf59ea6bfe4e66c3f1fbc4863c809cfdc0e: repository model for a user account with OAuth provider identity and token fields (pure)
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
	Automation     *bool
}

// UserProvider represents a user's OAuth provider information
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: OAuth provider linkage record associating a user to a specific provider identity (pure)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: machine-to-machine OAuth client credential with hashed secret and activity metadata (pure)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: parameters for registering a new client credential in the repository (pure)
type ClientCredentialCreateParams struct {
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	ExpiresAt        *time.Time
}

// DeletionResult contains statistics about user/group deletion operations
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: statistics returned after deleting a user, reporting threat model transfers and deletions (pure)
type DeletionResult struct {
	ThreatModelsTransferred int
	ThreatModelsDeleted     int
	UserEmail               string
}

// GroupDeletionResult contains statistics about group deletion operations
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: statistics returned after deleting a group, reporting affected threat model counts (pure)
type GroupDeletionResult struct {
	ThreatModelsDeleted  int
	ThreatModelsRetained int
	GroupName            string
}

// TransferResult contains statistics about an ownership transfer operation
// SEM@36c1f84217ecf3f5087ad65186cd974b9b4df275: identifiers of threat models and survey responses transferred during an ownership transfer (pure)
type TransferResult struct {
	ThreatModelIDs    []string
	SurveyResponseIDs []string
}

// UserRepository handles user CRUD operations
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: interface for user CRUD and provider-lookup operations against the backing store
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: interface for managing OAuth client credentials including creation, lookup, and revocation
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
// SEM@aaa9664f0ee44fef65807ddde317d73afdfcf8eb: interface for deleting users and groups with cascading ownership transfer (reads DB)
type DeletionRepository interface {
	// DeleteUserAndData deletes a user by email and handles ownership transfer for threat models.
	// Used by the self-deletion flow (DELETE /me) where identity comes from JWT email.
	DeleteUserAndData(ctx context.Context, userEmail string) (*DeletionResult, error)

	// DeleteUserByInternalUUID deletes a user by internal UUID and handles ownership transfer.
	// Used by admin deletion to avoid multi-hop identity resolution that can target the wrong user.
	DeleteUserByInternalUUID(ctx context.Context, internalUUID string) (*DeletionResult, error)

	// DeleteGroupAndData deletes a group by internal UUID and handles threat model cleanup
	// Uses internal_uuid for precise identification to avoid issues with duplicate group_names
	DeleteGroupAndData(ctx context.Context, internalUUID string) (*GroupDeletionResult, error)

	// TransferOwnership transfers all owned threat models and survey responses
	// from sourceUserUUID to targetUserUUID within a single transaction.
	// The source user is downgraded to "writer" role on all transferred items.
	TransferOwnership(ctx context.Context, sourceUserUUID, targetUserUUID string) (*TransferResult, error)
}
