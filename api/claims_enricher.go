package api

import (
	"context"

	"github.com/ericfitz/tmi/internal/slogging"
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

	isAdmin, err := IsGroupMemberFromParams(ctx, e.memberStore, userInternalUUID, provider, groupNames, GroupAdministrators)
	if err != nil {
		logger.Warn("Claims enricher: failed to check admin membership: %v", err)
		isAdmin = false
	}

	isSecReviewer, err := IsGroupMemberFromParams(ctx, e.memberStore, userInternalUUID, provider, groupNames, GroupSecurityReviewers)
	if err != nil {
		logger.Warn("Claims enricher: failed to check security reviewer membership: %v", err)
		isSecReviewer = false
	}

	return isAdmin, isSecReviewer, nil
}
