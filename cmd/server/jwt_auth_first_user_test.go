package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingGroupMemberRepo embeds the interface (nil) so only the methods
// autoPromoteFirstUser uses are implemented; any other call panics loudly.
type recordingGroupMemberRepo struct {
	api.GroupMemberRepository
	hasAdmins bool
	added     map[uuid.UUID][]uuid.UUID // group -> users
}

func (r *recordingGroupMemberRepo) HasAnyMembers(_ context.Context, _ uuid.UUID) (bool, error) {
	return r.hasAdmins, nil
}

func (r *recordingGroupMemberRepo) AddMember(_ context.Context, group, user uuid.UUID, _ *uuid.UUID, _ *string) (*api.GroupMember, error) {
	if r.added == nil {
		r.added = map[uuid.UUID][]uuid.UUID{}
	}
	r.added[group] = append(r.added[group], user)
	return &api.GroupMember{}, nil
}

// TestAutoPromoteFirstUser_AdministratorsOnly pins #1156: first-user promotion
// grants Administrators membership only, never Security Reviewers.
func TestAutoPromoteFirstUser_AdministratorsOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := api.GlobalGroupMemberRepository
	defer func() { api.GlobalGroupMemberRepository = orig }()

	repo := &recordingGroupMemberRepo{}
	api.GlobalGroupMemberRepository = repo

	userID := uuid.New()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set("userInternalUUID", userID.String())
	c.Set("userEmail", "charlie@tmi.local")
	c.Set("userProvider", "tmi")

	a := &JWTAuthenticator{}
	require.NoError(t, a.autoPromoteFirstUser(c, slogging.NewFallbackLogger()))

	adminsGroup := uuid.MustParse(api.AdministratorsGroupUUID)
	reviewersGroup := uuid.MustParse(api.SecurityReviewersGroupUUID)
	assert.Equal(t, []uuid.UUID{userID}, repo.added[adminsGroup], "first user must be added to Administrators")
	assert.Empty(t, repo.added[reviewersGroup], "first user must NOT be added to Security Reviewers")
	assert.Len(t, repo.added, 1, "no other group may be granted")
}
