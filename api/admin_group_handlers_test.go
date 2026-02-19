package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Stores for Admin Group Handler Tests
// =============================================================================

// mockGroupStoreForAdminHandlers implements GroupStore for testing admin group handlers
type mockGroupStoreForAdminHandlers struct {
	groups    map[string]Group // keyed by InternalUUID string
	err       error            // injected error for testing error paths
	countErr  error            // separate error for Count
	enrichErr error            // separate error for EnrichGroups
	updateErr error            // separate error for Update (overrides err)
	deleteErr error            // separate error for Delete (overrides err)
}

func newMockGroupStoreForAdminHandlers() *mockGroupStoreForAdminHandlers {
	return &mockGroupStoreForAdminHandlers{
		groups: make(map[string]Group),
	}
}

func (m *mockGroupStoreForAdminHandlers) List(_ context.Context, filter GroupFilter) ([]Group, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []Group
	for _, g := range m.groups {
		// Apply provider filter
		if filter.Provider != "" && g.Provider != filter.Provider {
			continue
		}
		// Apply group name filter (case-insensitive substring)
		if filter.GroupName != "" && !strings.Contains(
			strings.ToLower(g.GroupName),
			strings.ToLower(filter.GroupName),
		) {
			continue
		}
		result = append(result, g)
	}
	// Apply pagination
	if filter.Offset > len(result) {
		return []Group{}, nil
	}
	end := filter.Offset + filter.Limit
	if filter.Limit == 0 || end > len(result) {
		end = len(result)
	}
	return result[filter.Offset:end], nil
}

func (m *mockGroupStoreForAdminHandlers) Get(_ context.Context, internalUUID uuid.UUID) (*Group, error) {
	if m.err != nil {
		return nil, m.err
	}
	if g, ok := m.groups[internalUUID.String()]; ok {
		return &g, nil
	}
	return nil, errors.New(ErrMsgGroupNotFound)
}

func (m *mockGroupStoreForAdminHandlers) GetByProviderAndName(_ context.Context, provider string, groupName string) (*Group, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, g := range m.groups {
		if g.Provider == provider && g.GroupName == groupName {
			return &g, nil
		}
	}
	return nil, errors.New(ErrMsgGroupNotFound)
}

func (m *mockGroupStoreForAdminHandlers) Create(_ context.Context, group Group) error {
	if m.err != nil {
		return m.err
	}
	// Check for duplicate provider+group_name
	for _, g := range m.groups {
		if g.Provider == group.Provider && g.GroupName == group.GroupName {
			return errors.New("group already exists for provider")
		}
	}
	m.groups[group.InternalUUID.String()] = group
	return nil
}

func (m *mockGroupStoreForAdminHandlers) Update(_ context.Context, group Group) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.err != nil {
		return m.err
	}
	if _, ok := m.groups[group.InternalUUID.String()]; !ok {
		return errors.New(ErrMsgGroupNotFound)
	}
	m.groups[group.InternalUUID.String()] = group
	return nil
}

func (m *mockGroupStoreForAdminHandlers) Delete(_ context.Context, internalUUID string) (*GroupDeletionStats, error) {
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	if m.err != nil {
		return nil, m.err
	}
	parsedUUID, err := uuid.Parse(internalUUID)
	if err != nil {
		return nil, errors.New(ErrMsgGroupNotFound)
	}
	g, ok := m.groups[parsedUUID.String()]
	if !ok {
		return nil, errors.New(ErrMsgGroupNotFound)
	}
	delete(m.groups, parsedUUID.String())
	return &GroupDeletionStats{
		GroupName:            g.GroupName,
		ThreatModelsDeleted:  0,
		ThreatModelsRetained: 0,
	}, nil
}

func (m *mockGroupStoreForAdminHandlers) Count(_ context.Context, filter GroupFilter) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	if m.err != nil {
		return 0, m.err
	}
	count := 0
	for _, g := range m.groups {
		if filter.Provider != "" && g.Provider != filter.Provider {
			continue
		}
		if filter.GroupName != "" && !strings.Contains(
			strings.ToLower(g.GroupName),
			strings.ToLower(filter.GroupName),
		) {
			continue
		}
		count++
	}
	return count, nil
}

func (m *mockGroupStoreForAdminHandlers) EnrichGroups(_ context.Context, groups []Group) ([]Group, error) {
	if m.enrichErr != nil {
		return nil, m.enrichErr
	}
	return groups, nil
}

func (m *mockGroupStoreForAdminHandlers) GetGroupsForProvider(_ context.Context, provider string) ([]Group, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []Group
	for _, g := range m.groups {
		if g.Provider == provider {
			result = append(result, g)
		}
	}
	return result, nil
}

// mockGroupMemberStoreForAdminHandlers implements GroupMemberStore for testing group member handlers
type mockGroupMemberStoreForAdminHandlers struct {
	members              []GroupMember
	listErr              error
	countVal             int
	countErr             error
	addMemberResult      *GroupMember
	addMemberErr         error
	addGroupMemberResult *GroupMember
	addGroupMemberErr    error
	removeMemberErr      error
	removeGroupMemberErr error
}

func newMockGroupMemberStoreForAdminHandlers() *mockGroupMemberStoreForAdminHandlers {
	return &mockGroupMemberStoreForAdminHandlers{}
}

func (m *mockGroupMemberStoreForAdminHandlers) ListMembers(_ context.Context, _ GroupMemberFilter) ([]GroupMember, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.members == nil {
		return []GroupMember{}, nil
	}
	return m.members, nil
}

func (m *mockGroupMemberStoreForAdminHandlers) CountMembers(_ context.Context, _ uuid.UUID) (int, error) {
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

func (m *mockGroupMemberStoreForAdminHandlers) AddMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	if m.addMemberErr != nil {
		return nil, m.addMemberErr
	}
	return m.addMemberResult, nil
}

func (m *mockGroupMemberStoreForAdminHandlers) RemoveMember(_ context.Context, _, _ uuid.UUID) error {
	return m.removeMemberErr
}

func (m *mockGroupMemberStoreForAdminHandlers) IsMember(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockGroupMemberStoreForAdminHandlers) AddGroupMember(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _ *string) (*GroupMember, error) {
	if m.addGroupMemberErr != nil {
		return nil, m.addGroupMemberErr
	}
	return m.addGroupMemberResult, nil
}

func (m *mockGroupMemberStoreForAdminHandlers) RemoveGroupMember(_ context.Context, _, _ uuid.UUID) error {
	return m.removeGroupMemberErr
}

func (m *mockGroupMemberStoreForAdminHandlers) IsEffectiveMember(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ []uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockGroupMemberStoreForAdminHandlers) HasAnyMembers(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockGroupMemberStoreForAdminHandlers) GetGroupsForUser(_ context.Context, _ uuid.UUID) ([]Group, error) {
	return nil, nil
}

// =============================================================================
// Test Setup Helpers
// =============================================================================

// setupAdminGroupRouter creates a test router with admin group handler routes
func setupAdminGroupRouter() (*gin.Engine, *Server, *mockGroupStoreForAdminHandlers, *mockGroupMemberStoreForAdminHandlers) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	server := &Server{}
	groupStore := newMockGroupStoreForAdminHandlers()
	memberStore := newMockGroupMemberStoreForAdminHandlers()

	// Add fake auth middleware that sets admin user context
	r.Use(func(c *gin.Context) {
		SetFullUserContext(c, "admin@example.com", "admin-provider-id", "admin-internal-uuid", "tmi", []string{})
		c.Next()
	})

	// Register admin group routes
	r.GET("/admin/groups", func(c *gin.Context) {
		var params ListAdminGroupsParams
		if limitStr := c.Query("limit"); limitStr != "" {
			var l int
			if err := json.Unmarshal([]byte(limitStr), &l); err == nil {
				params.Limit = &l
			}
		}
		if offsetStr := c.Query("offset"); offsetStr != "" {
			var o int
			if err := json.Unmarshal([]byte(offsetStr), &o); err == nil {
				params.Offset = &o
			}
		}
		if sortBy := c.Query("sort_by"); sortBy != "" {
			sb := ListAdminGroupsParamsSortBy(sortBy)
			params.SortBy = &sb
		}
		if sortOrder := c.Query("sort_order"); sortOrder != "" {
			so := ListAdminGroupsParamsSortOrder(sortOrder)
			params.SortOrder = &so
		}
		if provider := c.Query("provider"); provider != "" {
			params.Provider = &provider
		}
		if groupName := c.Query("group_name"); groupName != "" {
			params.GroupName = &groupName
		}
		server.ListAdminGroups(c, params)
	})

	r.GET("/admin/groups/:internal_uuid", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid", "error_description": "internal_uuid must be a valid UUID"})
			return
		}
		server.GetAdminGroup(c, parsedUUID)
	})

	r.POST("/admin/groups", server.CreateAdminGroup)

	r.PATCH("/admin/groups/:internal_uuid", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid", "error_description": "internal_uuid must be a valid UUID"})
			return
		}
		server.UpdateAdminGroup(c, parsedUUID)
	})

	r.DELETE("/admin/groups/:internal_uuid", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid", "error_description": "internal_uuid must be a valid UUID"})
			return
		}
		server.DeleteAdminGroup(c, parsedUUID)
	})

	// Register group member routes
	r.GET("/admin/groups/:internal_uuid/members", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid"})
			return
		}
		var params ListGroupMembersParams
		if limitStr := c.Query("limit"); limitStr != "" {
			var l int
			if err := json.Unmarshal([]byte(limitStr), &l); err == nil {
				params.Limit = &l
			}
		}
		if offsetStr := c.Query("offset"); offsetStr != "" {
			var o int
			if err := json.Unmarshal([]byte(offsetStr), &o); err == nil {
				params.Offset = &o
			}
		}
		server.ListGroupMembers(c, parsedUUID, params)
	})

	r.POST("/admin/groups/:internal_uuid/members", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid"})
			return
		}
		server.AddGroupMember(c, parsedUUID)
	})

	r.DELETE("/admin/groups/:internal_uuid/members/:member_uuid", func(c *gin.Context) {
		groupUUIDStr := c.Param("internal_uuid")
		memberUUIDStr := c.Param("member_uuid")
		groupUUID, err := uuid.Parse(groupUUIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid"})
			return
		}
		memberUUID, err := uuid.Parse(memberUUIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid"})
			return
		}
		var params RemoveGroupMemberParams
		if st := c.Query("subject_type"); st != "" {
			stTyped := RemoveGroupMemberParamsSubjectType(st)
			params.SubjectType = &stTyped
		}
		server.RemoveGroupMember(c, groupUUID, memberUUID, params)
	})

	return r, server, groupStore, memberStore
}

// saveAndRestoreAdminGroupStores saves global stores and returns a cleanup function
func saveAndRestoreAdminGroupStores() func() {
	origGroupStore := GlobalGroupStore
	origMemberStore := GlobalGroupMemberStore
	return func() {
		GlobalGroupStore = origGroupStore
		GlobalGroupMemberStore = origMemberStore
	}
}

// =============================================================================
// isDBValidationError Tests
// =============================================================================

func TestAdminGroupIsDBValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "nil error", err: nil, expected: false},
		{name: "generic error", err: errors.New("something went wrong"), expected: false},
		// Oracle errors
		{name: "Oracle ORA-12899 value too large", err: errors.New("ORA-12899: value too large for column"), expected: true},
		{name: "Oracle ORA-01461 can bind LONG value", err: errors.New("ORA-01461: can bind a LONG value only for insert"), expected: true},
		{name: "Oracle ORA-01704 string literal too long", err: errors.New("ORA-01704: string literal too long"), expected: true},
		{name: "Oracle ORA-22835 buffer too small", err: errors.New("ORA-22835: Buffer too small"), expected: true},
		{name: "Oracle case-insensitive match", err: errors.New("ora-12899: VALUE TOO LARGE FOR COLUMN"), expected: true},
		// PostgreSQL errors
		{name: "PostgreSQL value too long", err: errors.New("ERROR: value too long for type character varying(255)"), expected: true},
		{name: "PostgreSQL invalid byte sequence", err: errors.New("ERROR: invalid byte sequence for encoding UTF8"), expected: true},
		{name: "PostgreSQL character with byte sequence", err: errors.New("ERROR: character with byte sequence 0xc0 in encoding UTF8 has no equivalent"), expected: true},
		// Generic GORM/SQL errors
		{name: "data too long", err: errors.New("Error 1406: Data too long for column 'name'"), expected: true},
		{name: "string data right truncation", err: errors.New("string data, right truncation"), expected: true},
		// Non-validation errors
		{name: "connection refused", err: errors.New("connection refused"), expected: false},
		{name: "timeout error", err: errors.New("context deadline exceeded"), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDBValidationError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// ListAdminGroups Tests
// =============================================================================

func TestAdminGroupListAdminGroups(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("empty store returns empty list", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodGet, "/admin/groups", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(0), resp["total"])
		assert.Equal(t, float64(50), resp["limit"]) // default limit
		assert.Equal(t, float64(0), resp["offset"]) // default offset
	})

	t.Run("returns groups with default pagination", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		id1 := uuid.New()
		id2 := uuid.New()
		groupStore.groups[id1.String()] = Group{InternalUUID: id1, Provider: "*", GroupName: "test-group-1", Name: "Test Group 1"}
		groupStore.groups[id2.String()] = Group{InternalUUID: id2, Provider: "github", GroupName: "test-group-2", Name: "Test Group 2"}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(2), resp["total"])
		groups, ok := resp["groups"].([]interface{})
		require.True(t, ok)
		assert.Len(t, groups, 2)
	})

	t.Run("respects custom limit parameter", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		for i := 0; i < 5; i++ {
			id := uuid.New()
			groupStore.groups[id.String()] = Group{InternalUUID: id, Provider: "*", GroupName: fmt.Sprintf("group-%d", i), Name: fmt.Sprintf("Group %d", i)}
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?limit=2", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(2), resp["limit"])
		groups, ok := resp["groups"].([]interface{})
		require.True(t, ok)
		assert.Len(t, groups, 2)
	})

	t.Run("respects offset parameter", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		for i := 0; i < 3; i++ {
			id := uuid.New()
			groupStore.groups[id.String()] = Group{InternalUUID: id, Provider: "*", GroupName: fmt.Sprintf("group-%d", i)}
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?offset=2&limit=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(2), resp["offset"])
		groups, ok := resp["groups"].([]interface{})
		require.True(t, ok)
		assert.Len(t, groups, 1) // 3 total, offset 2 => 1 remaining
	})

	t.Run("filters by provider", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		id1 := uuid.New()
		id2 := uuid.New()
		groupStore.groups[id1.String()] = Group{InternalUUID: id1, Provider: "github", GroupName: "gh-group"}
		groupStore.groups[id2.String()] = Group{InternalUUID: id2, Provider: "*", GroupName: "tmi-group"}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?provider=github", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		groups, ok := resp["groups"].([]interface{})
		require.True(t, ok)
		assert.Len(t, groups, 1)
	})

	t.Run("filters by group_name", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		id1 := uuid.New()
		id2 := uuid.New()
		groupStore.groups[id1.String()] = Group{InternalUUID: id1, Provider: "*", GroupName: "security-team"}
		groupStore.groups[id2.String()] = Group{InternalUUID: id2, Provider: "*", GroupName: "engineering"}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?group_name=security", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		groups, ok := resp["groups"].([]interface{})
		require.True(t, ok)
		assert.Len(t, groups, 1)
	})

	t.Run("passes sort parameters", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		id1 := uuid.New()
		groupStore.groups[id1.String()] = Group{InternalUUID: id1, Provider: "*", GroupName: "alpha-group"}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?sort_by=usage_count&sort_order=desc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid negative limit returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?limit=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_limit")
	})

	t.Run("limit exceeding max returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?limit=9999", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_limit")
	})

	t.Run("invalid negative offset returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodGet, "/admin/groups?offset=-5", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_offset")
	})

	t.Run("store list error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.err = errors.New("database connection failed")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodGet, "/admin/groups", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("count error graceful fallback", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		id := uuid.New()
		groupStore.groups[id.String()] = Group{InternalUUID: id, Provider: "*", GroupName: "test-group"}
		groupStore.countErr = errors.New("count failed")

		req := httptest.NewRequest(http.MethodGet, "/admin/groups", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		// When count fails, fallback to len(groups)
		assert.Equal(t, float64(1), resp["total"])
	})

	t.Run("enrich error graceful fallback", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		id := uuid.New()
		groupStore.groups[id.String()] = Group{InternalUUID: id, Provider: "*", GroupName: "test-group", Name: "Test Group"}
		groupStore.enrichErr = errors.New("enrichment failed")

		req := httptest.NewRequest(http.MethodGet, "/admin/groups", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		groups, ok := resp["groups"].([]interface{})
		require.True(t, ok)
		assert.Len(t, groups, 1)
	})
}

// =============================================================================
// GetAdminGroup Tests
// =============================================================================

func TestAdminGroupGetAdminGroup(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("returns existing group", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
			Name: "Test Group", Description: "A test group",
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, groupID.String(), resp["internal_uuid"])
		assert.Equal(t, "test-group", resp["group_name"])
		assert.Equal(t, "Test Group", resp["name"])
		assert.Equal(t, "A test group", resp["description"])
	})

	t.Run("not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("invalid UUID returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/not-a-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_uuid")
	})

	t.Run("store error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.err = errors.New("database error")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("enrichment failure graceful degradation", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
			Name: "Test Group", Description: "desc",
		}
		groupStore.enrichErr = errors.New("enrichment failed")

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should still return 200 with non-enriched data
		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "test-group", resp["group_name"])
	})
}

// =============================================================================
// CreateAdminGroup Tests
// =============================================================================

func TestAdminGroupCreateAdminGroup(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("creates group successfully with status 201", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		body := map[string]interface{}{
			"group_name":  "new-test-group",
			"name":        "New Test Group",
			"description": "A newly created group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "new-test-group", resp["group_name"])
		assert.Equal(t, "New Test Group", resp["name"])
		assert.Equal(t, "A newly created group", resp["description"])
		assert.Equal(t, "*", resp["provider"])
		assert.NotEmpty(t, resp["internal_uuid"])

		// Verify stored
		assert.Len(t, groupStore.groups, 1)
	})

	t.Run("creates group without description", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		body := map[string]interface{}{
			"group_name": "minimal-group",
			"name":       "Minimal Group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Len(t, groupStore.groups, 1)
	})

	t.Run("duplicate group returns 409", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		// Pre-populate with a group
		existingID := uuid.New()
		groupStore.groups[existingID.String()] = Group{
			InternalUUID: existingID, Provider: "*", GroupName: "existing-group", Name: "Existing Group",
		}

		body := map[string]interface{}{
			"group_name": "existing-group",
			"name":       "Duplicate Group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "duplicate_group")
	})

	t.Run("database validation error returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.err = errors.New("ERROR: value too long for type character varying(255)")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		body := map[string]interface{}{
			"group_name":  "a-group",
			"name":        "A Group",
			"description": strings.Repeat("x", 10000),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "validation_error")
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodPost, "/admin/groups", bytes.NewReader([]byte("not valid json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_request")
	})

	t.Run("store server error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.err = errors.New("connection reset by peer")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		body := map[string]interface{}{
			"group_name": "a-group",
			"name":       "A Group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

// =============================================================================
// UpdateAdminGroup Tests
// =============================================================================

func TestAdminGroupUpdateAdminGroup(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("updates name successfully", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
			Name: "Old Name", Description: "Original description",
		}

		body := map[string]interface{}{"name": "New Name"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "New Name", resp["name"])

		// Verify stored
		updated := groupStore.groups[groupID.String()]
		assert.Equal(t, "New Name", updated.Name)
	})

	t.Run("updates description successfully", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
			Name: "Test Group", Description: "Original",
		}

		body := map[string]interface{}{"description": "Updated description"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		updated := groupStore.groups[groupID.String()]
		assert.Equal(t, "Updated description", updated.Description)
	})

	t.Run("no changes returns current group", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
			Name: "Same Name", Description: "Same Description",
		}

		body := map[string]interface{}{"name": "Same Name", "description": "Same Description"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "Same Name", resp["name"])
	})

	t.Run("empty name returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group", Name: "Has Name",
		}

		body := map[string]interface{}{"name": ""}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "name cannot be empty")
	})

	t.Run("group not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		nonExistentID := uuid.New()
		body := map[string]interface{}{"name": "Updated"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+nonExistentID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("invalid UUID returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		body := map[string]interface{}{"name": "Updated"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/not-valid-uuid", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_uuid")
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group", Name: "Test",
		}

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader([]byte("{bad json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_request")
	})

	t.Run("built-in group cannot rename returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "administrators", Name: "Administrators",
		}
		groupStore.updateErr = errors.New("cannot rename built-in group")

		body := map[string]interface{}{"name": "Renamed Admins"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "protected_group")
	})

	t.Run("built-in group cannot clear display name returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "administrators", Name: "Administrators",
		}
		groupStore.updateErr = errors.New("cannot clear the display name of built-in group")

		body := map[string]interface{}{"name": "X"} // Changed value triggers update path
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "protected_group")
	})

	t.Run("built-in group cannot change description returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "administrators",
			Name: "Administrators", Description: "Built-in admin group",
		}
		groupStore.updateErr = errors.New("cannot change the description of built-in group")

		body := map[string]interface{}{"description": "Changed description"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "protected_group")
	})

	t.Run("built-in group cannot clear description returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "administrators",
			Name: "Administrators", Description: "Built-in admin group",
		}
		groupStore.updateErr = errors.New("cannot clear the description of built-in group")

		body := map[string]interface{}{"description": ""}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "protected_group")
	})

	t.Run("update store not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group", Name: "Test",
		}
		groupStore.updateErr = errors.New(ErrMsgGroupNotFound)

		body := map[string]interface{}{"name": "New Name"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("update store server error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group", Name: "Test",
		}
		groupStore.updateErr = errors.New("disk I/O error")

		body := map[string]interface{}{"name": "New Name"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("get store server error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.err = errors.New("database timeout")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		body := map[string]interface{}{"name": "New Name"}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPatch, "/admin/groups/"+groupID.String(), bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

// =============================================================================
// DeleteAdminGroup Tests
// =============================================================================

func TestAdminGroupDeleteAdminGroup(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("deletes group successfully with 204", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "deletable-group", Name: "Deletable Group",
		}

		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Body.String())
		assert.Len(t, groupStore.groups, 0)
	})

	t.Run("group not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("invalid UUID returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/not-a-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("cannot delete built-in group returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.deleteErr = errors.New("cannot delete built-in group 'administrators'")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "protected_group")
	})

	t.Run("cannot delete protected group returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.deleteErr = errors.New("cannot delete protected group: administrators")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "protected_group")
	})

	t.Run("store server error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.deleteErr = errors.New("unexpected database failure")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

// =============================================================================
// ListGroupMembers Tests
// =============================================================================

func TestAdminGroupListGroupMembers(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("returns members with default pagination", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group", Name: "Test Group",
		}

		memberID := uuid.New()
		userUUID := uuid.New()
		memberStore.members = []GroupMember{
			{
				Id: memberID, GroupInternalUuid: groupID,
				UserInternalUuid: &userUUID, AddedAt: time.Now().UTC(),
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(50), resp["limit"])
		assert.Equal(t, float64(0), resp["offset"])
		members, ok := resp["members"].([]interface{})
		require.True(t, ok)
		assert.Len(t, members, 1)
	})

	t.Run("custom pagination", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}
		memberStore.members = []GroupMember{}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members?limit=10&offset=5", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(10), resp["limit"])
		assert.Equal(t, float64(5), resp["offset"])
	})

	t.Run("invalid limit too high returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members?limit=201", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_limit")
	})

	t.Run("invalid negative limit returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members?limit=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_limit")
	})

	t.Run("invalid negative offset returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members?offset=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_offset")
	})

	t.Run("group not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+nonExistentID.String()+"/members", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("store list error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}
		memberStore.listErr = errors.New("database error")

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("count error graceful fallback", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}
		memberStore.members = []GroupMember{}
		memberStore.countErr = errors.New("count failed")

		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(0), resp["total"])
	})

	t.Run("group store get error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		groupStore.err = errors.New("database connection failed")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/admin/groups/"+groupID.String()+"/members", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

// =============================================================================
// AddGroupMember Tests
// =============================================================================

func TestAdminGroupAddGroupMember(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("add user member with default subject_type", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}

		userUUID := uuid.New()
		memberID := uuid.New()
		memberStore.addMemberResult = &GroupMember{
			Id: memberID, GroupInternalUuid: groupID,
			UserInternalUuid: &userUUID, AddedAt: time.Now().UTC(),
		}

		body := map[string]interface{}{
			"user_internal_uuid": userUUID.String(),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, groupID.String(), resp["group_internal_uuid"])
	})

	t.Run("add group member with subject_type group", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}

		memberGroupID := uuid.New()
		memberID := uuid.New()
		memberStore.addGroupMemberResult = &GroupMember{
			Id: memberID, GroupInternalUuid: groupID,
			MemberGroupInternalUuid: &memberGroupID, AddedAt: time.Now().UTC(),
		}

		body := map[string]interface{}{
			"member_group_internal_uuid": memberGroupID.String(),
			"subject_type":               "group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotNil(t, resp["member_group_internal_uuid"])
	})

	t.Run("missing user_internal_uuid for user subject returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		body := map[string]interface{}{}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user_internal_uuid is required")
	})

	t.Run("missing member_group_internal_uuid for group subject returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		body := map[string]interface{}{
			"subject_type": "group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "member_group_internal_uuid is required")
	})

	t.Run("self-referential group membership returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addGroupMemberErr = errors.New("a group cannot be a member of itself")

		body := map[string]interface{}{
			"member_group_internal_uuid": groupID.String(),
			"subject_type":               "group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "A group cannot be a member of itself")
	})

	t.Run("duplicate user membership returns 409", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addMemberErr = errors.New("user is already a member of this group")

		body := map[string]interface{}{
			"user_internal_uuid": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "duplicate_membership")
	})

	t.Run("duplicate group membership returns 409", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addGroupMemberErr = errors.New("group is already a member of this group")

		body := map[string]interface{}{
			"member_group_internal_uuid": uuid.New().String(),
			"subject_type":               "group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "duplicate_membership")
	})

	t.Run("everyone pseudo-group returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addMemberErr = errors.New("cannot add members to the 'everyone' pseudo-group")

		body := map[string]interface{}{
			"user_internal_uuid": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "forbidden")
	})

	t.Run("group not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addMemberErr = errors.New(ErrMsgGroupNotFound)

		body := map[string]interface{}{
			"user_internal_uuid": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Group not found")
	})

	t.Run("user not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addMemberErr = errors.New(ErrMsgUserNotFound)

		body := map[string]interface{}{
			"user_internal_uuid": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "User not found")
	})

	t.Run("member group not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addGroupMemberErr = errors.New("member group not found")

		body := map[string]interface{}{
			"member_group_internal_uuid": uuid.New().String(),
			"subject_type":               "group",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Member group not found")
	})

	t.Run("invalid request body returns 400", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader([]byte("{invalid")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_request")
	})

	t.Run("server error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberStore.addMemberErr = errors.New("unexpected database error")

		body := map[string]interface{}{
			"user_internal_uuid": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("add member with notes", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		groupStore.groups[groupID.String()] = Group{
			InternalUUID: groupID, Provider: "*", GroupName: "test-group",
		}

		userUUID := uuid.New()
		memberID := uuid.New()
		notes := "Added for project X"
		memberStore.addMemberResult = &GroupMember{
			Id: memberID, GroupInternalUuid: groupID,
			UserInternalUuid: &userUUID, Notes: &notes, AddedAt: time.Now().UTC(),
		}

		body := map[string]interface{}{
			"user_internal_uuid": userUUID.String(),
			"notes":              "Added for project X",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/admin/groups/"+groupID.String()+"/members", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "Added for project X", resp["notes"])
	})
}

// =============================================================================
// RemoveGroupMember Tests
// =============================================================================

func TestAdminGroupRemoveGroupMember(t *testing.T) {
	defer saveAndRestoreAdminGroupStores()()

	t.Run("remove user member successfully", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberUUID := uuid.New()

		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String()+"/members/"+memberUUID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("remove group member with subject_type group", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberGroupUUID := uuid.New()

		req := httptest.NewRequest(http.MethodDelete,
			"/admin/groups/"+groupID.String()+"/members/"+memberGroupUUID.String()+"?subject_type=group", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("membership not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		memberStore.removeMemberErr = errors.New("membership not found")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberUUID := uuid.New()

		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String()+"/members/"+memberUUID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Membership not found")
	})

	t.Run("group membership not found returns 404", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		memberStore.removeGroupMemberErr = errors.New("group membership not found")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberGroupUUID := uuid.New()

		req := httptest.NewRequest(http.MethodDelete,
			"/admin/groups/"+groupID.String()+"/members/"+memberGroupUUID.String()+"?subject_type=group", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Membership not found")
	})

	t.Run("everyone pseudo-group returns 403", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		memberStore.removeMemberErr = errors.New("cannot remove members from the 'everyone' pseudo-group")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberUUID := uuid.New()

		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String()+"/members/"+memberUUID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "everyone")
	})

	t.Run("server error returns 500", func(t *testing.T) {
		r, _, groupStore, memberStore := setupAdminGroupRouter()
		memberStore.removeMemberErr = errors.New("unexpected database error")
		GlobalGroupStore = groupStore
		GlobalGroupMemberStore = memberStore

		groupID := uuid.New()
		memberUUID := uuid.New()

		req := httptest.NewRequest(http.MethodDelete, "/admin/groups/"+groupID.String()+"/members/"+memberUUID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}
