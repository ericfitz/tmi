package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// adminDB is the package-level GORM database handle used by admin check functions.
// Set during InitializeGormStores.
var adminDB *gorm.DB

// GetGroupUUIDsByNames looks up group UUIDs from group names for a given provider.
// This is used by admin checks and auth adapters to convert JWT group claims to UUIDs.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: fetch UUIDs for the named groups scoped to a provider (reads DB)
func GetGroupUUIDsByNames(ctx context.Context, db *gorm.DB, provider string, groupNames []string) ([]uuid.UUID, error) {
	if len(groupNames) == 0 {
		return []uuid.UUID{}, nil
	}

	logger := slogging.Get()

	var groups []models.Group
	result := db.WithContext(ctx).
		Where(map[string]any{"provider": provider, "group_name": groupNames}).
		Find(&groups)

	if result.Error != nil {
		logger.Error("Failed to look up group UUIDs: provider=%s, group_names=%v, error=%v",
			provider, groupNames, result.Error)
		return nil, fmt.Errorf("failed to look up group UUIDs: %w", result.Error)
	}

	groupUUIDs := make([]uuid.UUID, 0, len(groups))
	for _, g := range groups {
		if groupUUID, err := uuid.Parse(string(g.InternalUUID)); err == nil {
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
// SEM@1aa36c06c7b700d3f00bf6f4b22125d673b1070a: admin checker that resolves admin and security-reviewer status via group membership (pure)
type GroupBasedAdminChecker struct {
	db          *gorm.DB
	memberStore GroupMemberRepository
}

// NewGroupBasedAdminChecker creates a new admin checker backed by the Administrators group.
// SEM@1aa36c06c7b700d3f00bf6f4b22125d673b1070a: build a GroupBasedAdminChecker backed by the Administrators group store (pure)
func NewGroupBasedAdminChecker(db *gorm.DB, memberStore GroupMemberRepository) *GroupBasedAdminChecker {
	return &GroupBasedAdminChecker{
		db:          db,
		memberStore: memberStore,
	}
}

// IsAdmin checks if a user is an administrator by checking Administrators group membership.
// Implements auth.AdminChecker.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: check whether the user is a member of the Administrators group (reads DB)
func (a *GroupBasedAdminChecker) IsAdmin(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error) {
	return checkGroupMembershipFromStrings(ctx, a.memberStore, userInternalUUID, groupUUIDs, GroupAdministrators)
}

// IsSecurityReviewer checks if a user is a security reviewer by checking Security Reviewers group membership.
// Implements auth.AdminChecker.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: check whether the user is a member of the Security Reviewers group (reads DB)
func (a *GroupBasedAdminChecker) IsSecurityReviewer(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error) {
	return checkGroupMembershipFromStrings(ctx, a.memberStore, userInternalUUID, groupUUIDs, GroupSecurityReviewers)
}

// GetGroupUUIDsByNames converts group names to UUIDs.
// Implements auth.AdminChecker.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: convert group names to UUID strings for a given provider via the admin checker (reads DB)
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
