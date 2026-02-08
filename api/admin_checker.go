package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// adminDB is the package-level GORM database handle used by admin check functions.
// Set during InitializeGormStores.
var adminDB *gorm.DB

// GetGroupUUIDsByNames looks up group UUIDs from group names for a given provider.
// This is used by admin checks and auth adapters to convert JWT group claims to UUIDs.
func GetGroupUUIDsByNames(ctx context.Context, db *gorm.DB, provider string, groupNames []string) ([]uuid.UUID, error) {
	if len(groupNames) == 0 {
		return []uuid.UUID{}, nil
	}

	logger := slogging.Get()

	var groups []models.Group
	result := db.WithContext(ctx).
		Where(map[string]interface{}{"provider": provider, "group_name": groupNames}).
		Find(&groups)

	if result.Error != nil {
		logger.Error("Failed to look up group UUIDs: provider=%s, group_names=%v, error=%v",
			provider, groupNames, result.Error)
		return nil, fmt.Errorf("failed to look up group UUIDs: %w", result.Error)
	}

	groupUUIDs := make([]uuid.UUID, 0, len(groups))
	for _, g := range groups {
		if groupUUID, err := uuid.Parse(g.InternalUUID); err == nil {
			groupUUIDs = append(groupUUIDs, groupUUID)
		}
	}

	logger.Debug("Looked up %d group UUIDs from %d group names for provider %s",
		len(groupUUIDs), len(groupNames), provider)

	return groupUUIDs, nil
}

// GroupBasedAdminChecker implements auth.AdminChecker using the Administrators group.
// This replaces GormAdminCheckerAdapter by checking Administrators group membership
// instead of querying the administrators table.
type GroupBasedAdminChecker struct {
	db          *gorm.DB
	memberStore GroupMemberStore
}

// NewGroupBasedAdminChecker creates a new admin checker backed by the Administrators group.
func NewGroupBasedAdminChecker(db *gorm.DB, memberStore GroupMemberStore) *GroupBasedAdminChecker {
	return &GroupBasedAdminChecker{
		db:          db,
		memberStore: memberStore,
	}
}

// IsAdmin checks if a user is an administrator by checking Administrators group membership.
// Implements auth.AdminChecker.
func (a *GroupBasedAdminChecker) IsAdmin(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error) {
	adminsGroupUUID := uuid.MustParse(validation.AdministratorsGroupUUID)

	var userUUID uuid.UUID
	if userInternalUUID != nil {
		var err error
		userUUID, err = uuid.Parse(*userInternalUUID)
		if err != nil {
			return false, fmt.Errorf("invalid user UUID: %w", err)
		}
	}

	// Convert string group UUIDs to uuid.UUID
	parsedGroupUUIDs := make([]uuid.UUID, 0, len(groupUUIDs))
	for _, g := range groupUUIDs {
		if parsed, err := uuid.Parse(g); err == nil {
			parsedGroupUUIDs = append(parsedGroupUUIDs, parsed)
		}
	}

	return a.memberStore.IsEffectiveMember(ctx, adminsGroupUUID, userUUID, parsedGroupUUIDs)
}

// GetGroupUUIDsByNames converts group names to UUIDs.
// Implements auth.AdminChecker.
func (a *GroupBasedAdminChecker) GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]string, error) {
	uuids, err := GetGroupUUIDsByNames(ctx, a.db, provider, groupNames)
	if err != nil {
		return nil, err
	}

	result := make([]string, len(uuids))
	for i, u := range uuids {
		result[i] = u.String()
	}

	return result, nil
}
