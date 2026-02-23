package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// BuiltInGroup represents a well-known TMI group used as an authorization fixture.
type BuiltInGroup struct {
	Name string
	UUID uuid.UUID
}

var (
	// GroupAdministrators is the built-in Administrators group.
	GroupAdministrators = BuiltInGroup{Name: AdministratorsGroup, UUID: uuid.MustParse(AdministratorsGroupUUID)}

	// GroupSecurityReviewers is the built-in Security Reviewers group.
	GroupSecurityReviewers = BuiltInGroup{Name: SecurityReviewersGroup, UUID: uuid.MustParse(SecurityReviewersGroupUUID)}
)

// MembershipContext holds the resolved user identity for group membership checks.
type MembershipContext struct {
	Email      string
	UserUUID   uuid.UUID
	Provider   string
	GroupNames []string
	GroupUUIDs []uuid.UUID // IdP groups resolved to TMI group UUIDs
}

// ResolveMembershipContext extracts user identity from the Gin context and resolves
// IdP group names to TMI group UUIDs. Returns nil with error if authentication is missing.
func ResolveMembershipContext(c *gin.Context) (*MembershipContext, error) {
	logger := slogging.Get().WithContext(c)

	email, err := GetUserEmail(c)
	if err != nil {
		return nil, err
	}

	internalUUIDStr, err := GetUserInternalUUID(c)
	if err != nil {
		return nil, err
	}

	userUUID, err := uuid.Parse(internalUUIDStr)
	if err != nil {
		logger.Error("ResolveMembershipContext: invalid user UUID format: %v", err)
		return nil, fmt.Errorf("invalid user UUID: %w", err)
	}

	provider, err := GetUserProvider(c)
	if err != nil {
		return nil, err
	}

	groupNames := GetUserGroups(c)

	// Convert group names to group UUIDs for effective membership check
	var groupUUIDs []uuid.UUID
	if adminDB != nil && len(groupNames) > 0 {
		groupUUIDs, err = GetGroupUUIDsByNames(c.Request.Context(), adminDB, provider, groupNames)
		if err != nil {
			logger.Error("ResolveMembershipContext: failed to resolve group UUIDs: %v", err)
			return nil, fmt.Errorf("failed to resolve group UUIDs: %w", err)
		}
	}

	return &MembershipContext{
		Email:      email,
		UserUUID:   userUUID,
		Provider:   provider,
		GroupNames: groupNames,
		GroupUUIDs: groupUUIDs,
	}, nil
}

// IsGroupMember checks if the user described by mc is an effective member of the given built-in group.
func IsGroupMember(ctx context.Context, mc *MembershipContext, group BuiltInGroup) (bool, error) {
	if GlobalGroupMemberStore == nil {
		return false, fmt.Errorf("group member store not initialized")
	}
	return GlobalGroupMemberStore.IsEffectiveMember(ctx, group.UUID, mc.UserUUID, mc.GroupUUIDs)
}

// IsGroupMemberFromContext is a convenience function that resolves the membership context
// from a Gin context and checks group membership in a single call.
func IsGroupMemberFromContext(c *gin.Context, group BuiltInGroup) (bool, error) {
	mc, err := ResolveMembershipContext(c)
	if err != nil {
		return false, err
	}
	return IsGroupMember(c.Request.Context(), mc, group)
}

// IsGroupMemberFromParams checks group membership using explicit parameters.
// This is used by cross-package adapters (e.g., auth package) that don't have a Gin context.
func IsGroupMemberFromParams(ctx context.Context, memberStore GroupMemberStore, userInternalUUID string, provider string, groupNames []string, group BuiltInGroup) (bool, error) {
	userUUID, err := uuid.Parse(userInternalUUID)
	if err != nil {
		return false, fmt.Errorf("invalid user UUID: %w", err)
	}

	// Convert group names to UUIDs if we have a database handle
	var groupUUIDs []uuid.UUID
	if adminDB != nil && len(groupNames) > 0 {
		groupUUIDs, err = GetGroupUUIDsByNames(ctx, adminDB, provider, groupNames)
		if err != nil {
			return false, fmt.Errorf("failed to resolve group UUIDs: %w", err)
		}
	}

	return memberStore.IsEffectiveMember(ctx, group.UUID, userUUID, groupUUIDs)
}

// checkGroupMembershipFromStrings is a shared helper for GroupBasedAdminChecker methods.
// It parses string UUIDs and calls IsEffectiveMember on the given store.
func checkGroupMembershipFromStrings(ctx context.Context, memberStore GroupMemberStore, userInternalUUID *string, groupUUIDs []string, group BuiltInGroup) (bool, error) {
	var userUUID uuid.UUID
	if userInternalUUID != nil {
		var err error
		userUUID, err = uuid.Parse(*userInternalUUID)
		if err != nil {
			return false, fmt.Errorf("invalid user UUID: %w", err)
		}
	}

	parsedGroupUUIDs := make([]uuid.UUID, 0, len(groupUUIDs))
	for _, g := range groupUUIDs {
		if parsed, err := uuid.Parse(g); err == nil {
			parsedGroupUUIDs = append(parsedGroupUUIDs, parsed)
		}
	}

	return memberStore.IsEffectiveMember(ctx, group.UUID, userUUID, parsedGroupUUIDs)
}
