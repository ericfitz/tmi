package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system (from auth package but with additional fields)
type User struct {
	InternalUUID  uuid.UUID `json:"internal_uuid"`
	Provider      string    `json:"provider"`
	ProviderUserID string   `json:"provider_user_id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	ModifiedAt    time.Time `json:"modified_at"`
	LastLogin     *time.Time `json:"last_login,omitempty"`

	// Enriched fields (not in database)
	IsAdmin             bool     `json:"is_admin,omitempty"`
	Groups              []string `json:"groups,omitempty"`
	ActiveThreatModels  int      `json:"active_threat_models,omitempty"`
}

// UserFilter defines filtering options for user queries
type UserFilter struct {
	Provider        string
	Email           string      // Case-insensitive ILIKE %email%
	CreatedAfter    *time.Time
	CreatedBefore   *time.Time
	LastLoginAfter  *time.Time
	LastLoginBefore *time.Time
	Limit           int
	Offset          int
	SortBy          string      // created_at, last_login, email
	SortOrder       string      // asc, desc
}

// UserStore defines the interface for user storage operations
type UserStore interface {
	// List returns users with optional filtering and pagination
	List(ctx context.Context, filter UserFilter) ([]User, error)

	// Get retrieves a user by internal UUID
	Get(ctx context.Context, internalUUID uuid.UUID) (*User, error)

	// GetByProviderAndID retrieves a user by provider and provider_user_id
	GetByProviderAndID(ctx context.Context, provider string, providerUserID string) (*User, error)

	// Update updates user metadata (email, name, email_verified)
	Update(ctx context.Context, user User) error

	// Delete deletes a user by provider and provider_user_id
	// Returns deletion statistics
	Delete(ctx context.Context, provider string, providerUserID string) (*DeletionStats, error)

	// Count returns total count of users matching the filter
	Count(ctx context.Context, filter UserFilter) (int, error)

	// EnrichUsers adds related data to users (admin status, groups, threat model counts)
	EnrichUsers(ctx context.Context, users []User) ([]User, error)
}

// DeletionStats contains statistics about user deletion
type DeletionStats struct {
	ThreatModelsTransferred int    `json:"threat_models_transferred"`
	ThreatModelsDeleted     int    `json:"threat_models_deleted"`
	UserEmail               string `json:"user_email"`
}

// GlobalUserStore is the global singleton for user storage
var GlobalUserStore UserStore
