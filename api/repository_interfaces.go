package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
)

// Repository error sentinels.
// Each wraps the corresponding dberrors sentinel so handlers can check
// either the entity-specific error or the generic category.
var (
	// Not-found errors
	ErrGroupNotFound       = fmt.Errorf("group: %w", dberrors.ErrNotFound)
	ErrMetadataNotFound    = fmt.Errorf("metadata: %w", dberrors.ErrNotFound)
	ErrGroupMemberNotFound = fmt.Errorf("group member: %w", dberrors.ErrNotFound)

	// Duplicate / conflict errors
	ErrGroupDuplicate       = fmt.Errorf("group: %w", dberrors.ErrDuplicate)
	ErrGroupMemberDuplicate = fmt.Errorf("group member: %w", dberrors.ErrDuplicate)
	ErrMetadataKeyExists    = fmt.Errorf("metadata key: %w", dberrors.ErrDuplicate)

	// Business-logic errors (not DB errors)
	ErrSelfMembership = errors.New("a group cannot be a member of itself")
	ErrEveryoneGroup  = errors.New("the everyone group cannot be modified")
)

// MetadataConflictError is returned by metadata Create/BulkCreate operations when
// one or more keys already exist. ConflictingKeys contains the key names that
// caused the conflict. Unwrap returns ErrMetadataKeyExists so callers can use
// errors.Is(err, ErrMetadataKeyExists) to detect this condition.
type MetadataConflictError struct {
	ConflictingKeys []string
}

// Error implements the error interface.
func (e *MetadataConflictError) Error() string {
	return fmt.Sprintf("metadata key(s) already exist: %s", strings.Join(e.ConflictingKeys, ", "))
}

// Unwrap returns ErrMetadataKeyExists so that errors.Is and errors.As work
// against the sentinel.
func (e *MetadataConflictError) Unwrap() error {
	return ErrMetadataKeyExists
}

// GroupRepository defines the interface for group storage operations.
// This mirrors GroupStore but omits Delete (which is handled at the handler
// level via DeletionRepository) and uses repository-scoped typed errors.
type GroupRepository interface {
	// List returns groups with optional filtering and pagination.
	List(ctx context.Context, filter GroupFilter) ([]Group, error)

	// Get retrieves a group by internal UUID.
	Get(ctx context.Context, internalUUID uuid.UUID) (*Group, error)

	// GetByProviderAndName retrieves a group by provider and group_name.
	GetByProviderAndName(ctx context.Context, provider string, groupName string) (*Group, error)

	// Create creates a new group (primarily for provider-independent groups).
	Create(ctx context.Context, group Group) error

	// Update updates group metadata (name, description).
	Update(ctx context.Context, group Group) error

	// Count returns total count of groups matching the filter.
	Count(ctx context.Context, filter GroupFilter) (int, error)

	// EnrichGroups adds related data to groups (usage in authorizations/admin grants).
	EnrichGroups(ctx context.Context, groups []Group) ([]Group, error)

	// GetGroupsForProvider returns all groups for a specific provider (for UI autocomplete).
	GetGroupsForProvider(ctx context.Context, provider string) ([]Group, error)
}

// MetadataRepository defines the interface for metadata storage operations.
// Method signatures match MetadataStore exactly.
type MetadataRepository interface {
	// CRUD operations
	Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error)
	Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Delete(ctx context.Context, entityType, entityID, key string) error

	// Collection operations
	List(ctx context.Context, entityType, entityID string) ([]Metadata, error)

	// POST operations — adding metadata without specifying key upfront
	Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error

	// Bulk operations
	BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkReplace(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error

	// Key-based operations
	GetByKey(ctx context.Context, key string) ([]Metadata, error)
	ListKeys(ctx context.Context, entityType, entityID string) ([]string, error)

	// Cache management
	InvalidateCache(ctx context.Context, entityType, entityID string) error
	WarmCache(ctx context.Context, entityType, entityID string) error
}

// GroupMemberRepository defines the interface for group membership storage operations.
// Method signatures match GroupMemberStore exactly.
// Supports both user and group members (one level of group-in-group nesting).
type GroupMemberRepository interface {
	// User membership operations
	ListMembers(ctx context.Context, filter GroupMemberFilter) ([]GroupMember, error)
	CountMembers(ctx context.Context, groupInternalUUID uuid.UUID) (int, error)
	AddMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error)
	RemoveMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) error
	IsMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) (bool, error)

	// Group-as-member operations (one level of nesting)
	AddGroupMember(ctx context.Context, groupInternalUUID, memberGroupInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error)
	RemoveGroupMember(ctx context.Context, groupInternalUUID, memberGroupInternalUUID uuid.UUID) error

	// Effective membership checks (direct user membership OR via group nesting)
	IsEffectiveMember(ctx context.Context, groupInternalUUID uuid.UUID, userInternalUUID uuid.UUID, userGroupUUIDs []uuid.UUID) (bool, error)
	HasAnyMembers(ctx context.Context, groupInternalUUID uuid.UUID) (bool, error)

	// User-centric queries
	GetGroupsForUser(ctx context.Context, userInternalUUID uuid.UUID) ([]Group, error)
}
