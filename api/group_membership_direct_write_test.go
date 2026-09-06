package api

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nestedAdminStore stubs only the two methods IsEffectiveAdministratorByUUID
// uses; the embedded nil interface makes any other call a loud panic.
type nestedAdminStore struct {
	GroupMemberRepository
	userGroups     []Group
	groupsErr      error
	memberErr      error
	seenGroupUUIDs []uuid.UUID
	adminGroups    map[uuid.UUID]bool // group UUIDs that are members of Administrators
	directAdmin    bool
}

func (s *nestedAdminStore) GetGroupsForUser(_ context.Context, _ uuid.UUID) ([]Group, error) {
	return s.userGroups, s.groupsErr
}

func (s *nestedAdminStore) IsEffectiveMember(_ context.Context, group uuid.UUID, _ uuid.UUID, userGroupUUIDs []uuid.UUID) (bool, error) {
	s.seenGroupUUIDs = userGroupUUIDs
	if s.memberErr != nil {
		return false, s.memberErr
	}
	if group != GroupAdministrators.UUID {
		return false, nil
	}
	if s.directAdmin {
		return true, nil
	}
	for _, g := range userGroupUUIDs {
		if s.adminGroups[g] {
			return true, nil
		}
	}
	return false, nil
}

// #856 security fix: administrator membership through a nested TMI-managed
// group must be detected without an IdP group list (the credential owner is
// not the caller, so no JWT-enriched context exists).
func TestIsEffectiveAdministratorByUUID(t *testing.T) {
	user := uuid.New()
	corpAdmins := uuid.New()

	t.Run("nested group membership is detected", func(t *testing.T) {
		store := &nestedAdminStore{
			userGroups:  []Group{{InternalUUID: corpAdmins, Name: "corp-admins"}},
			adminGroups: map[uuid.UUID]bool{corpAdmins: true},
		}
		isAdmin, err := IsEffectiveAdministratorByUUID(context.Background(), store, user.String())
		require.NoError(t, err)
		assert.True(t, isAdmin)
		assert.Equal(t, []uuid.UUID{corpAdmins}, store.seenGroupUUIDs, "user's TMI groups must be passed to the membership check")
	})

	t.Run("direct membership is detected", func(t *testing.T) {
		store := &nestedAdminStore{directAdmin: true}
		isAdmin, err := IsEffectiveAdministratorByUUID(context.Background(), store, user.String())
		require.NoError(t, err)
		assert.True(t, isAdmin)
	})

	t.Run("non-admin in unrelated groups is not an admin", func(t *testing.T) {
		store := &nestedAdminStore{
			userGroups:  []Group{{InternalUUID: uuid.New(), Name: "tmi-automation"}},
			adminGroups: map[uuid.UUID]bool{corpAdmins: true},
		}
		isAdmin, err := IsEffectiveAdministratorByUUID(context.Background(), store, user.String())
		require.NoError(t, err)
		assert.False(t, isAdmin)
	})

	t.Run("errors propagate so callers fail closed", func(t *testing.T) {
		_, err := IsEffectiveAdministratorByUUID(context.Background(), &nestedAdminStore{groupsErr: errors.New("db down")}, user.String())
		assert.Error(t, err)
		_, err = IsEffectiveAdministratorByUUID(context.Background(), &nestedAdminStore{memberErr: errors.New("db down")}, user.String())
		assert.Error(t, err)
		_, err = IsEffectiveAdministratorByUUID(context.Background(), nil, user.String())
		assert.Error(t, err)
		_, err = IsEffectiveAdministratorByUUID(context.Background(), &nestedAdminStore{}, "not-a-uuid")
		assert.Error(t, err)
	})
}
