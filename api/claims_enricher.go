package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GroupMembershipEnricher implements auth.ClaimsEnricher by checking
// effective membership in built-in groups (Administrators, Security Reviewers)
// and resolving the user's TMI-managed group names for inclusion in the JWT groups claim.
type GroupMembershipEnricher struct {
	memberStore GroupMemberRepository
	db          *gorm.DB
}

// NewGroupMembershipEnricher creates a new enricher for JWT claims.
func NewGroupMembershipEnricher(memberStore GroupMemberRepository, db *gorm.DB) *GroupMembershipEnricher {
	return &GroupMembershipEnricher{
		memberStore: memberStore,
		db:          db,
	}
}

// EnrichClaims checks whether the user is a member of the Administrators and
// Security Reviewers built-in groups, and returns the user's TMI-managed group names.
func (e *GroupMembershipEnricher) EnrichClaims(ctx context.Context, userInternalUUID string, provider string, groupNames []string) (bool, bool, []string, error) {
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

	// Look up the user's TMI-managed group memberships so they can be included
	// in the JWT groups claim. This is essential for the TMI provider, which does
	// not have an external IdP to supply groups — TMI is the IdP.
	var tmiGroupNames []string
	userUUID, parseErr := uuid.Parse(userInternalUUID)
	if parseErr != nil {
		logger.Warn("Claims enricher: invalid user UUID %s: %v", userInternalUUID, parseErr)
	} else {
		groups, groupErr := e.memberStore.GetGroupsForUser(ctx, userUUID)
		if groupErr != nil {
			logger.Warn("Claims enricher: failed to get TMI groups for user %s: %v", userInternalUUID, groupErr)
		} else {
			tmiGroupNames = make([]string, len(groups))
			for i, g := range groups {
				tmiGroupNames[i] = g.GroupName
			}
		}
	}

	return isAdmin, isSecReviewer, tmiGroupNames, nil
}
