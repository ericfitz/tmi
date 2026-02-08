package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/auth"
	"github.com/google/uuid"
)

// GormUserGroupsFetcher implements auth.UserGroupsFetcher by querying the
// group membership store for TMI-managed groups a user belongs to.
type GormUserGroupsFetcher struct {
	memberStore GroupMemberStore
}

// NewGormUserGroupsFetcher creates a new user groups fetcher.
func NewGormUserGroupsFetcher(memberStore GroupMemberStore) *GormUserGroupsFetcher {
	return &GormUserGroupsFetcher{
		memberStore: memberStore,
	}
}

// GetUserGroups returns the TMI-managed groups that a user is a direct member of.
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
