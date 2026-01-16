package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// GroupDeletionStats contains statistics about group deletion
type GroupDeletionStats struct {
	ThreatModelsDeleted  int    `json:"threat_models_deleted"`
	ThreatModelsRetained int    `json:"threat_models_retained"`
	GroupName            string `json:"group_name"`
}

// Group represents a group in the system
type Group struct {
	InternalUUID uuid.UUID `json:"internal_uuid"`
	Provider     string    `json:"provider"`
	GroupName    string    `json:"group_name"`
	Name         string    `json:"name,omitempty"`
	Description  string    `json:"description,omitempty"`
	FirstUsed    time.Time `json:"first_used"`
	LastUsed     time.Time `json:"last_used"`
	UsageCount   int       `json:"usage_count"`

	// Enriched fields (not in database)
	UsedInAuthorizations bool `json:"used_in_authorizations,omitempty"`
	UsedInAdminGrants    bool `json:"used_in_admin_grants,omitempty"`
	MemberCount          int  `json:"member_count,omitempty"` // If available from IdP
}

// GroupFilter defines filtering options for group queries
type GroupFilter struct {
	Provider             string
	GroupName            string // Case-insensitive ILIKE %name%
	UsedInAuthorizations *bool
	Limit                int
	Offset               int
	SortBy               string // group_name, first_used, last_used, usage_count
	SortOrder            string // asc, desc
}

// GroupStore defines the interface for group storage operations
type GroupStore interface {
	// List returns groups with optional filtering and pagination
	List(ctx context.Context, filter GroupFilter) ([]Group, error)

	// Get retrieves a group by internal UUID
	Get(ctx context.Context, internalUUID uuid.UUID) (*Group, error)

	// GetByProviderAndName retrieves a group by provider and group_name
	GetByProviderAndName(ctx context.Context, provider string, groupName string) (*Group, error)

	// Create creates a new group (primarily for provider-independent groups)
	Create(ctx context.Context, group Group) error

	// Update updates group metadata (name, description)
	Update(ctx context.Context, group Group) error

	// Delete deletes a TMI-managed group by group_name (provider is always "*")
	// Returns deletion statistics
	Delete(ctx context.Context, groupName string) (*GroupDeletionStats, error)

	// Count returns total count of groups matching the filter
	Count(ctx context.Context, filter GroupFilter) (int, error)

	// EnrichGroups adds related data to groups (usage in authorizations/admin grants)
	EnrichGroups(ctx context.Context, groups []Group) ([]Group, error)

	// GetGroupsForProvider returns all groups for a specific provider (for UI autocomplete)
	GetGroupsForProvider(ctx context.Context, provider string) ([]Group, error)
}

// GlobalGroupStore is the global singleton for group storage
var GlobalGroupStore GroupStore

// GroupMemberStore defines the interface for group membership storage operations
type GroupMemberStore interface {
	ListMembers(ctx context.Context, filter GroupMemberFilter) ([]GroupMember, error)
	CountMembers(ctx context.Context, groupInternalUUID uuid.UUID) (int, error)
	AddMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error)
	RemoveMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) error
	IsMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) (bool, error)
}

// GlobalGroupMemberStore is the global singleton for group membership storage
var GlobalGroupMemberStore GroupMemberStore
