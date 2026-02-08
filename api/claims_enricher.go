package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/api/validation"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GroupMembershipEnricher implements auth.ClaimsEnricher by checking
// effective membership in built-in groups (Administrators, Security Reviewers).
type GroupMembershipEnricher struct {
	memberStore GroupMemberStore
	db          *gorm.DB
}

// NewGroupMembershipEnricher creates a new enricher for JWT claims.
func NewGroupMembershipEnricher(memberStore GroupMemberStore, db *gorm.DB) *GroupMembershipEnricher {
	return &GroupMembershipEnricher{
		memberStore: memberStore,
		db:          db,
	}
}

// EnrichClaims checks whether the user is a member of the Administrators and
// Security Reviewers built-in groups. It returns the membership status for each.
func (e *GroupMembershipEnricher) EnrichClaims(ctx context.Context, userInternalUUID string, provider string, groupNames []string) (bool, bool, error) {
	logger := slogging.Get()

	userUUID, err := uuid.Parse(userInternalUUID)
	if err != nil {
		return false, false, fmt.Errorf("invalid user UUID: %w", err)
	}

	// Convert IdP group names to UUIDs for effective membership check (nested groups)
	var groupUUIDs []uuid.UUID
	if len(groupNames) > 0 {
		groupUUIDs, err = GetGroupUUIDsByNames(ctx, e.db, provider, groupNames)
		if err != nil {
			logger.Warn("Claims enricher: failed to resolve group names to UUIDs: %v", err)
			// Continue with empty group UUIDs - direct membership will still be checked
		}
	}

	adminsGroupUUID := uuid.MustParse(validation.AdministratorsGroupUUID)
	secReviewersGroupUUID := uuid.MustParse(validation.SecurityReviewersGroupUUID)

	isAdmin, err := e.memberStore.IsEffectiveMember(ctx, adminsGroupUUID, userUUID, groupUUIDs)
	if err != nil {
		logger.Warn("Claims enricher: failed to check admin membership: %v", err)
		isAdmin = false
	}

	isSecReviewer, err := e.memberStore.IsEffectiveMember(ctx, secReviewersGroupUUID, userUUID, groupUUIDs)
	if err != nil {
		logger.Warn("Claims enricher: failed to check security reviewer membership: %v", err)
		isSecReviewer = false
	}

	return isAdmin, isSecReviewer, nil
}
