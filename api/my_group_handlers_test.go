package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Stores for My Group Handler Tests
// =============================================================================

// mockGroupStoreForMyHandlers implements GroupStore for testing
type mockGroupStoreForMyHandlers struct {
	groups map[string]Group
	err    error
}

func newMockGroupStoreForMyHandlers() *mockGroupStoreForMyHandlers {
	return &mockGroupStoreForMyHandlers{
		groups: make(map[string]Group),
	}
}

func (m *mockGroupStoreForMyHandlers) List(_ context.Context, _ GroupFilter) ([]Group, error) {
	return nil, nil
}

func (m *mockGroupStoreForMyHandlers) Get(_ context.Context, internalUUID uuid.UUID) (*Group, error) {
	if m.err != nil {
		return nil, m.err
	}
	if g, ok := m.groups[internalUUID.String()]; ok {
		return &g, nil
	}
	return nil, errors.New(ErrMsgGroupNotFound)
}

func (m *mockGroupStoreForMyHandlers) GetByProviderAndName(_ context.Context, _ string, _ string) (*Group, error) {
	return nil, nil
}

func (m *mockGroupStoreForMyHandlers) Create(_ context.Context, _ Group) error {
	return nil
}

func (m *mockGroupStoreForMyHandlers) Update(_ context.Context, _ Group) error {
	return nil
}

func (m *mockGroupStoreForMyHandlers) Delete(_ context.Context, _ string) (*GroupDeletionStats, error) {
	return nil, nil
}

func (m *mockGroupStoreForMyHandlers) Count(_ context.Context, _ GroupFilter) (int, error) {
	return 0, nil
}

func (m *mockGroupStoreForMyHandlers) EnrichGroups(_ context.Context, groups []Group) ([]Group, error) {
	return groups, nil
}

func (m *mockGroupStoreForMyHandlers) GetGroupsForProvider(_ context.Context, _ string) ([]Group, error) {
	return nil, nil
}

// mockGroupMemberStoreForMyHandlers implements GroupMemberStore for testing
type mockGroupMemberStoreForMyHandlers struct {
	members           []GroupMember
	listErr           error
	countVal          int
	countErr          error
	isEffectiveMember bool
	isEffectiveErr    error
	userGroups        []Group
	userGroupsErr     error
}

func newMockGroupMemberStoreForMyHandlers() *mockGroupMemberStoreForMyHandlers {
	return &mockGroupMemberStoreForMyHandlers{}
}

func (m *mockGroupMemberStoreForMyHandlers) ListMembers(_ context.Context, _ GroupMemberFilter) ([]GroupMember, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.members == nil {
		return []GroupMember{}, nil
	}
	return m.members, nil
}

func (m *mockGroupMemberStoreForMyHandlers) CountMembers(_ context.Context, _ uuid.UUID) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	if m.countVal > 0 {
		return m.countVal, nil
	}
	if m.members != nil {
		return len(m.members), nil
	}
	return 0, nil
}

func (m *mockGroupMemberStoreForMyHandlers) AddMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	return nil, nil
}

func (m *mockGroupMemberStoreForMyHandlers) RemoveMember(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *mockGroupMemberStoreForMyHandlers) IsMember(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockGroupMemberStoreForMyHandlers) AddGroupMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	return nil, nil
}

func (m *mockGroupMemberStoreForMyHandlers) RemoveGroupMember(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *mockGroupMemberStoreForMyHandlers) IsEffectiveMember(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ []uuid.UUID) (bool, error) {
	if m.isEffectiveErr != nil {
		return false, m.isEffectiveErr
	}
	return m.isEffectiveMember, nil
}

func (m *mockGroupMemberStoreForMyHandlers) HasAnyMembers(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockGroupMemberStoreForMyHandlers) GetGroupsForUser(_ context.Context, _ uuid.UUID) ([]Group, error) {
	if m.userGroupsErr != nil {
		return nil, m.userGroupsErr
	}
	return m.userGroups, nil
}

// =============================================================================
// Test Setup
// =============================================================================

func setupMyGroupRouter() (*gin.Engine, *Server, *mockGroupStoreForMyHandlers, *mockGroupMemberStoreForMyHandlers) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	server := &Server{}
	groupStore := newMockGroupStoreForMyHandlers()
	memberStore := newMockGroupMemberStoreForMyHandlers()

	// Save and restore global stores
	origGroupStore := GlobalGroupStore
	origMemberStore := GlobalGroupMemberStore
	GlobalGroupStore = groupStore
	GlobalGroupMemberStore = memberStore

	// Middleware to set authenticated user context
	r.Use(func(c *gin.Context) {
		SetFullUserContext(c, "alice@example.com", "alice-provider-id", "11111111-1111-1111-1111-111111111111", "tmi", []string{})
		c.Next()
	})

	// Register routes
	r.GET("/me/groups", server.ListMyGroups)
	r.GET("/me/groups/:internal_uuid/members", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid", "error_description": "internal_uuid must be a valid UUID"})
			return
		}
		var params ListMyGroupMembersParams
		if limitStr := c.Query("limit"); limitStr != "" {
			var l int
			if jsonErr := json.Unmarshal([]byte(limitStr), &l); jsonErr == nil {
				params.Limit = &l
			}
		}
		if offsetStr := c.Query("offset"); offsetStr != "" {
			var o int
			if jsonErr := json.Unmarshal([]byte(offsetStr), &o); jsonErr == nil {
				params.Offset = &o
			}
		}
		server.ListMyGroupMembers(c, parsedUUID, params)
	})

	// Cleanup function to restore global stores is not needed in test
	// because each test call sets up fresh stores
	_ = origGroupStore
	_ = origMemberStore

	return r, server, groupStore, memberStore
}

// =============================================================================
// ListMyGroups Tests
// =============================================================================

func TestListMyGroups(t *testing.T) {
	t.Run("returns user groups", func(t *testing.T) {
		router, _, _, memberStore := setupMyGroupRouter()

		groupUUID1 := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
		groupUUID2 := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
		memberStore.userGroups = []Group{
			{InternalUUID: groupUUID1, GroupName: "security-reviewers", Name: "Security Reviewers"},
			{InternalUUID: groupUUID2, GroupName: "engineering", Name: "Engineering"},
		}

		req := httptest.NewRequest(http.MethodGet, "/me/groups", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp MyGroupListResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, 2, resp.Total)
		assert.Len(t, resp.Groups, 2)
		assert.Equal(t, groupUUID1, resp.Groups[0].InternalUuid)
		assert.Equal(t, "security-reviewers", resp.Groups[0].GroupName)
		assert.NotNil(t, resp.Groups[0].Name)
		assert.Equal(t, "Security Reviewers", *resp.Groups[0].Name)
	})

	t.Run("returns empty array when user has no groups", func(t *testing.T) {
		router, _, _, memberStore := setupMyGroupRouter()
		memberStore.userGroups = []Group{}

		req := httptest.NewRequest(http.MethodGet, "/me/groups", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp MyGroupListResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, 0, resp.Total)
		assert.Len(t, resp.Groups, 0)
	})

	t.Run("handles group with empty name", func(t *testing.T) {
		router, _, _, memberStore := setupMyGroupRouter()

		groupUUID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
		memberStore.userGroups = []Group{
			{InternalUUID: groupUUID, GroupName: "custom-group", Name: ""},
		}

		req := httptest.NewRequest(http.MethodGet, "/me/groups", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp MyGroupListResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Len(t, resp.Groups, 1)
		assert.Nil(t, resp.Groups[0].Name) // empty name should be omitted
	})

	t.Run("returns 401 when unauthenticated", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		server := &Server{}
		memberStore := newMockGroupMemberStoreForMyHandlers()
		GlobalGroupMemberStore = memberStore

		// No auth middleware
		r.GET("/me/groups", server.ListMyGroups)

		req := httptest.NewRequest(http.MethodGet, "/me/groups", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 500 on store error", func(t *testing.T) {
		router, _, _, memberStore := setupMyGroupRouter()
		memberStore.userGroupsErr = errors.New("database error")

		req := httptest.NewRequest(http.MethodGet, "/me/groups", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// ListMyGroupMembers Tests
// =============================================================================

func TestListMyGroupMembers(t *testing.T) {
	groupUUID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	testNotes := "some admin notes"
	addedByEmail := openapi_types.Email("admin@example.com")
	addedByUUID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	memberEmail := openapi_types.Email("bob@example.com")
	memberUUID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	memberName := "Bob"

	t.Run("returns members with redacted admin fields", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		memberStore.isEffectiveMember = true
		memberStore.members = []GroupMember{
			{
				Id:                  uuid.New(),
				GroupInternalUuid:   groupUUID,
				SubjectType:         GroupMemberSubjectTypeUser,
				UserEmail:           &memberEmail,
				UserName:            &memberName,
				UserInternalUuid:    &memberUUID,
				AddedByEmail:        &addedByEmail,
				AddedByInternalUuid: &addedByUUID,
				Notes:               &testNotes,
				AddedAt:             time.Now(),
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, float64(1), resp["total"])
		assert.Equal(t, float64(50), resp["limit"])
		assert.Equal(t, float64(0), resp["offset"])

		members := resp["members"].([]interface{})
		require.Len(t, members, 1)

		member := members[0].(map[string]interface{})
		// User fields should be present
		assert.Equal(t, "bob@example.com", member["user_email"])
		assert.Equal(t, "Bob", member["user_name"])

		// Admin fields should be redacted (null)
		assert.Nil(t, member["added_by_email"])
		assert.Nil(t, member["added_by_internal_uuid"])
		assert.Nil(t, member["notes"])
	})

	t.Run("returns 403 when not a member", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		memberStore.isEffectiveMember = false

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("returns 404 when group not found", func(t *testing.T) {
		router, _, _, _ := setupMyGroupRouter()

		nonExistentUUID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+nonExistentUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 400 for invalid UUID", func(t *testing.T) {
		router, _, _, _ := setupMyGroupRouter()

		req := httptest.NewRequest(http.MethodGet, "/me/groups/not-a-uuid/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 401 when unauthenticated", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		server := &Server{}

		groupStore := newMockGroupStoreForMyHandlers()
		memberStore := newMockGroupMemberStoreForMyHandlers()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}

		// No auth middleware
		r.GET("/me/groups/:internal_uuid/members", func(c *gin.Context) {
			uuidStr := c.Param("internal_uuid")
			parsedUUID, _ := uuid.Parse(uuidStr)
			server.ListMyGroupMembers(c, parsedUUID, ListMyGroupMembersParams{})
		})

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("respects pagination parameters", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		memberStore.isEffectiveMember = true
		memberStore.countVal = 100
		memberStore.members = []GroupMember{}

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members?limit=10&offset=20", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(10), resp["limit"])
		assert.Equal(t, float64(20), resp["offset"])
		assert.Equal(t, float64(100), resp["total"])
	})

	t.Run("rejects limit over 200", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		memberStore.isEffectiveMember = true

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members?limit=300", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("rejects negative offset", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		memberStore.isEffectiveMember = true

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members?offset=-1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("everyone pseudo-group skips membership check", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		everyoneUUID := uuid.MustParse(EveryonePseudoGroupUUID)
		groupStore.groups[everyoneUUID.String()] = Group{
			InternalUUID: everyoneUUID,
			GroupName:    "everyone",
		}
		// isEffectiveMember is false — should still succeed because check is skipped
		memberStore.isEffectiveMember = false
		memberStore.members = []GroupMember{}

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+everyoneUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(0), resp["total"])
	})

	t.Run("returns 500 on store list error", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		memberStore.isEffectiveMember = true
		memberStore.listErr = errors.New("database error")

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("returns 500 on membership check error", func(t *testing.T) {
		router, _, groupStore, memberStore := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		memberStore.isEffectiveErr = errors.New("database error")

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("returns 500 on group store error", func(t *testing.T) {
		router, _, groupStore, _ := setupMyGroupRouter()

		groupStore.groups[groupUUID.String()] = Group{
			InternalUUID: groupUUID,
			GroupName:    "security-reviewers",
		}
		groupStore.err = errors.New("database error")

		req := httptest.NewRequest(http.MethodGet, "/me/groups/"+groupUUID.String()+"/members", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
