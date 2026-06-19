package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/auth"
	"github.com/google/uuid"
)

// GormUserGroupsFetcher implements auth.UserGroupsFetcher by querying the
// group membership repository for TMI-managed groups a user belongs to.
// SEM@1aa36c06c7b700d3f00bf6f4b22125d673b1070a: adapter that fetches TMI-managed group memberships for a user via the group member repository
type GormUserGroupsFetcher struct {
	memberStore GroupMemberRepository
}

// NewGormUserGroupsFetcher creates a new user groups fetcher.
// SEM@1aa36c06c7b700d3f00bf6f4b22125d673b1070a: build a GormUserGroupsFetcher over the given group member repository (pure)
func NewGormUserGroupsFetcher(memberStore GroupMemberRepository) *GormUserGroupsFetcher {
	return &GormUserGroupsFetcher{
		memberStore: memberStore,
	}
}

// GetUserGroups returns the TMI-managed groups that a user is a direct member of.
// SEM@a0040890dd7b1940f542d4211d4338cd0e713cbc: fetch the TMI-managed groups a user directly belongs to and convert them to UserGroupInfo (reads DB)
func (f *GormUserGroupsFetcher) GetUserGroups(ctx context.Context, userInternalUUID string) ([]auth.UserGroupInfo, error) {
	userUUID, err := uuid.Parse(userInternalUUID)
	if err != nil {
		return nil, fmt.Errorf("invalid user UUID: %w", err)
	}

	groups, err := f.memberStore.GetGroupsForUser(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get groups for user: %w", err)
	}

	result := make([]auth.UserGroupInfo, len(groups))
	for i, g := range groups {
		result[i] = auth.UserGroupInfo{
			InternalUUID: g.InternalUUID.String(),
			GroupName:    g.GroupName,
			Name:         g.Name,
		}
	}

	return result, nil
}
