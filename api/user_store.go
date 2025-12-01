package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Note: AdminUser type is now generated from OpenAPI spec in api.go

// UserFilter defines filtering options for user queries
type UserFilter struct {
	Provider        string
	Email           string // Case-insensitive ILIKE %email%
	CreatedAfter    *time.Time
	CreatedBefore   *time.Time
	LastLoginAfter  *time.Time
	LastLoginBefore *time.Time
	Limit           int
	Offset          int
	SortBy          string // created_at, last_login, email
	SortOrder       string // asc, desc
}

// UserStore defines the interface for user storage operations
type UserStore interface {
	// List returns users with optional filtering and pagination
	List(ctx context.Context, filter UserFilter) ([]AdminUser, error)

	// Get retrieves a user by internal UUID
	Get(ctx context.Context, internalUUID uuid.UUID) (*AdminUser, error)

	// GetByProviderAndID retrieves a user by provider and provider_user_id
	GetByProviderAndID(ctx context.Context, provider string, providerUserID string) (*AdminUser, error)

	// Update updates user metadata (email, name, email_verified)
	Update(ctx context.Context, user AdminUser) error

	// Delete deletes a user by provider and provider_user_id
	// Returns deletion statistics
	Delete(ctx context.Context, provider string, providerUserID string) (*DeletionStats, error)

	// Count returns total count of users matching the filter
	Count(ctx context.Context, filter UserFilter) (int, error)

	// EnrichUsers adds related data to users (admin status, groups, threat model counts)
	EnrichUsers(ctx context.Context, users []AdminUser) ([]AdminUser, error)
}

// DeletionStats contains statistics about user deletion
type DeletionStats struct {
	ThreatModelsTransferred int    `json:"threat_models_transferred"`
	ThreatModelsDeleted     int    `json:"threat_models_deleted"`
	UserEmail               string `json:"user_email"`
}

// GlobalUserStore is the global singleton for user storage
var GlobalUserStore UserStore
