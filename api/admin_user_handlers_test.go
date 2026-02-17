package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// Mock UserStore for Admin User Handler Tests
// =============================================================================

// mockUserStore implements UserStore for testing admin user handlers.
// It supports per-operation error injection and configurable behavior for
// count, enrichment, and deletion operations.
type mockUserStore struct {
	users       map[uuid.UUID]*AdminUser
	listErr     error
	getErr      error
	updateErr   error
	deleteErr   error
	countErr    error
	enrichErr   error
	deleteStats *DeletionStats
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{
		users: make(map[uuid.UUID]*AdminUser),
	}
}

func (m *mockUserStore) List(_ context.Context, filter UserFilter) ([]AdminUser, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}

	var result []AdminUser
	for _, u := range m.users {
		// Apply provider filter
		if filter.Provider != "" && u.Provider != filter.Provider {
			continue
		}
		// Apply email filter (simple substring match)
		if filter.Email != "" {
			emailStr := string(u.Email)
			if !containsIgnoreCase(emailStr, filter.Email) {
				continue
			}
		}
		result = append(result, *u)
	}

	// Apply offset/limit
	if filter.Offset >= len(result) {
		return []AdminUser{}, nil
	}
	end := filter.Offset + filter.Limit
	if filter.Limit == 0 || end > len(result) {
		end = len(result)
	}
	return result[filter.Offset:end], nil
}

func (m *mockUserStore) Get(_ context.Context, internalUUID uuid.UUID) (*AdminUser, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if u, ok := m.users[internalUUID]; ok {
		copied := *u
		return &copied, nil
	}
	return nil, errors.New("user not found")
}

func (m *mockUserStore) GetByProviderAndID(_ context.Context, provider, providerUserID string) (*AdminUser, error) {
	for _, u := range m.users {
		if u.Provider == provider && u.ProviderUserId == providerUserID {
			copied := *u
			return &copied, nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *mockUserStore) Update(_ context.Context, user AdminUser) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.users[user.InternalUuid]; !ok {
		return errors.New("user not found")
	}
	m.users[user.InternalUuid] = &user
	return nil
}

func (m *mockUserStore) Delete(_ context.Context, provider, providerUserID string) (*DeletionStats, error) {
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	if m.deleteStats != nil {
		return m.deleteStats, nil
	}
	for id, u := range m.users {
		if u.Provider == provider && u.ProviderUserId == providerUserID {
			email := string(u.Email)
			delete(m.users, id)
			return &DeletionStats{
				ThreatModelsTransferred: 0,
				ThreatModelsDeleted:     0,
				UserEmail:               email,
			}, nil
		}
	}
	return nil, errors.New("failed to find user: user not found")
}

func (m *mockUserStore) Count(_ context.Context, filter UserFilter) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	count := 0
	for _, u := range m.users {
		if filter.Provider != "" && u.Provider != filter.Provider {
			continue
		}
		if filter.Email != "" {
			emailStr := string(u.Email)
			if !containsIgnoreCase(emailStr, filter.Email) {
				continue
			}
		}
		count++
	}
	return count, nil
}

func (m *mockUserStore) EnrichUsers(_ context.Context, users []AdminUser) ([]AdminUser, error) {
	if m.enrichErr != nil {
		return nil, m.enrichErr
	}
	// Return users with enriched fields populated
	enriched := make([]AdminUser, len(users))
	copy(enriched, users)
	for i := range enriched {
		isAdmin := false
		enriched[i].IsAdmin = &isAdmin
		activeTMs := 0
		enriched[i].ActiveThreatModels = &activeTMs
		groups := []string{}
		enriched[i].Groups = &groups
	}
	return enriched, nil
}

// addUser is a helper to add a user to the mock store
func (m *mockUserStore) addUser(user AdminUser) {
	m.users[user.InternalUuid] = &user
}

// enrichFailMockUserStore wraps mockUserStore but always fails on EnrichUsers.
// It delegates all other operations to the embedded mockUserStore.
type enrichFailMockUserStore struct {
	*mockUserStore
}

func (m *enrichFailMockUserStore) EnrichUsers(_ context.Context, _ []AdminUser) ([]AdminUser, error) {
	return nil, errors.New("enrichment service unavailable")
}

// =============================================================================
// Router Setup
// =============================================================================

func setupAdminUserRouter(userEmail, userInternalUUID string) (*gin.Engine, *Server) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	server := &Server{}

	// Add fake auth middleware that sets admin context
	r.Use(func(c *gin.Context) {
		SetFullUserContext(c, userEmail, "provider-id", userInternalUUID, "tmi", []string{"administrators"})
		c.Set("isAdmin", true)
		c.Next()
	})

	// Register admin user routes with param parsing
	r.GET("/admin/users", func(c *gin.Context) {
		var params ListAdminUsersParams
		if limitStr := c.Query("limit"); limitStr != "" {
			var l int
			if _, err := fmt.Sscanf(limitStr, "%d", &l); err == nil {
				params.Limit = &l
			}
		}
		if offsetStr := c.Query("offset"); offsetStr != "" {
			var o int
			if _, err := fmt.Sscanf(offsetStr, "%d", &o); err == nil {
				params.Offset = &o
			}
		}
		if provider := c.Query("provider"); provider != "" {
			params.Provider = &provider
		}
		if email := c.Query("email"); email != "" {
			params.Email = &email
		}
		if sortBy := c.Query("sort_by"); sortBy != "" {
			sb := ListAdminUsersParamsSortBy(sortBy)
			params.SortBy = &sb
		}
		if sortOrder := c.Query("sort_order"); sortOrder != "" {
			so := ListAdminUsersParamsSortOrder(sortOrder)
			params.SortOrder = &so
		}
		server.ListAdminUsers(c, params)
	})

	r.GET("/admin/users/:internal_uuid", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID"})
			return
		}
		server.GetAdminUser(c, parsedUUID)
	})

	r.PATCH("/admin/users/:internal_uuid", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID"})
			return
		}
		server.UpdateAdminUser(c, parsedUUID)
	})

	r.DELETE("/admin/users/:internal_uuid", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID"})
			return
		}
		server.DeleteAdminUser(c, parsedUUID)
	})

	return r, server
}

// =============================================================================
// Test Data Helpers
// =============================================================================

func makeTestAdminUser(name, email, provider string) AdminUser {
	now := time.Now().UTC()
	return AdminUser{
		InternalUuid:   uuid.New(),
		Name:           name,
		Email:          openapi_types.Email(email),
		EmailVerified:  true,
		Provider:       provider,
		ProviderUserId: "provider-" + name,
		CreatedAt:      now,
		ModifiedAt:     now,
		LastLogin:      &now,
	}
}

// =============================================================================
// ListAdminUsers Tests
// =============================================================================

func TestListAdminUsers(t *testing.T) {
	// Save and restore global store
	origStore := GlobalUserStore
	defer func() {
		GlobalUserStore = origStore
	}()

	t.Run("Success_EmptyList", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 0)
		assert.Equal(t, float64(0), response["total"])
		assert.Equal(t, float64(50), response["limit"])
		assert.Equal(t, float64(0), response["offset"])
	})

	t.Run("Success_WithUsers", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user1 := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		user2 := makeTestAdminUser("Bob", "bob@example.com", "github")
		mockStore.addUser(user1)
		mockStore.addUser(user2)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 2)
		assert.Equal(t, float64(2), response["total"])
	})

	t.Run("Success_WithPagination", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		// Add 3 users
		for i := 0; i < 3; i++ {
			user := makeTestAdminUser(
				fmt.Sprintf("User%d", i),
				fmt.Sprintf("user%d@example.com", i),
				"tmi",
			)
			mockStore.addUser(user)
		}

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		// Request with limit=2, offset=0
		req, _ := http.NewRequest("GET", "/admin/users?limit=2&offset=0", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 2)
		assert.Equal(t, float64(3), response["total"])
		assert.Equal(t, float64(2), response["limit"])
		assert.Equal(t, float64(0), response["offset"])
	})

	t.Run("Success_PaginationOffset", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		// Add 3 users
		for i := 0; i < 3; i++ {
			user := makeTestAdminUser(
				fmt.Sprintf("User%d", i),
				fmt.Sprintf("user%d@example.com", i),
				"tmi",
			)
			mockStore.addUser(user)
		}

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		// Request with limit=2, offset=2 (should get 1 user)
		req, _ := http.NewRequest("GET", "/admin/users?limit=2&offset=2", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 1)
		assert.Equal(t, float64(3), response["total"])
	})

	t.Run("Success_FilterByProvider", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user1 := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		user2 := makeTestAdminUser("Bob", "bob@example.com", "github")
		user3 := makeTestAdminUser("Charlie", "charlie@example.com", "tmi")
		mockStore.addUser(user1)
		mockStore.addUser(user2)
		mockStore.addUser(user3)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users?provider=tmi", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 2)
		assert.Equal(t, float64(2), response["total"])
	})

	t.Run("Success_FilterByEmail", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user1 := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		user2 := makeTestAdminUser("Bob", "bob@other.com", "tmi")
		mockStore.addUser(user1)
		mockStore.addUser(user2)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users?email=alice", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 1)
		assert.Equal(t, float64(1), response["total"])
	})

	t.Run("Success_DefaultPagination", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Default limit is 50, offset is 0
		assert.Equal(t, float64(50), response["limit"])
		assert.Equal(t, float64(0), response["offset"])
	})

	t.Run("Success_EnrichedUsers", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		require.Len(t, users, 1)

		// Check enriched fields are present
		userMap, ok := users[0].(map[string]interface{})
		require.True(t, ok)
		_, hasIsAdmin := userMap["is_admin"]
		assert.True(t, hasIsAdmin, "enriched user should have is_admin field")
		_, hasActiveTMs := userMap["active_threat_models"]
		assert.True(t, hasActiveTMs, "enriched user should have active_threat_models field")
		_, hasGroups := userMap["groups"]
		assert.True(t, hasGroups, "enriched user should have groups field")
	})

	t.Run("Error_InvalidLimitNegative", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users?limit=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_limit")
	})

	t.Run("Error_InvalidLimitTooLarge", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/users?limit=%d", MaxPaginationLimit+1), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_limit")
	})

	t.Run("Error_InvalidOffsetNegative", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users?offset=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_offset")
	})

	t.Run("Error_StoreListFailure", func(t *testing.T) {
		mockStore := newMockUserStore()
		mockStore.listErr = errors.New("database connection lost")
		GlobalUserStore = mockStore

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("Success_CountErrorFallback", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)
		mockStore.countErr = errors.New("count query failed")

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// When Count fails, total falls back to the current page count
		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(len(users)), response["total"])
	})

	t.Run("Success_EnrichErrorFallback", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)
		mockStore.enrichErr = errors.New("enrichment failed")

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should still return users, even without enrichment
		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 1)
	})

	t.Run("Success_SortByParams", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users?sort_by=email&sort_order=asc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should succeed (sort is handled by the real store; mock doesn't sort but shouldn't error)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Success_OffsetBeyondTotal", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users?offset=100", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]interface{})
		require.True(t, ok)
		assert.Len(t, users, 0)
	})
}

// =============================================================================
// GetAdminUser Tests
// =============================================================================

func TestGetAdminUser(t *testing.T) {
	// Save and restore global store
	origStore := GlobalUserStore
	defer func() {
		GlobalUserStore = origStore
	}()

	t.Run("Success_UserFound", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response AdminUser
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, user.InternalUuid, response.InternalUuid)
		assert.Equal(t, "Alice", response.Name)
		assert.Equal(t, openapi_types.Email("alice@example.com"), response.Email)
		assert.Equal(t, "tmi", response.Provider)
	})

	t.Run("Success_EnrichedResponse", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Bob", "bob@example.com", "github")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Enriched fields should be present
		_, hasIsAdmin := response["is_admin"]
		assert.True(t, hasIsAdmin, "response should include is_admin enrichment")
		_, hasActiveTMs := response["active_threat_models"]
		assert.True(t, hasActiveTMs, "response should include active_threat_models enrichment")
		_, hasGroups := response["groups"]
		assert.True(t, hasGroups, "response should include groups enrichment")
	})

	t.Run("Error_NotFound", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		nonExistentUUID := uuid.New()
		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/users/%s", nonExistentUUID.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("Error_InvalidUUID", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users/not-a-valid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Error_StoreGetFailure", func(t *testing.T) {
		mockStore := newMockUserStore()
		mockStore.getErr = errors.New("database timeout")
		GlobalUserStore = mockStore

		someUUID := uuid.New()
		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/users/%s", someUUID.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("Success_ReturnsAllFields", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		now := time.Now().UTC().Truncate(time.Second)
		user := AdminUser{
			InternalUuid:   uuid.New(),
			Name:           "Charlie",
			Email:          openapi_types.Email("charlie@example.com"),
			EmailVerified:  true,
			Provider:       "tmi",
			ProviderUserId: "provider-charlie",
			CreatedAt:      now,
			ModifiedAt:     now,
			LastLogin:      &now,
		}
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, user.InternalUuid.String(), response["internal_uuid"])
		assert.Equal(t, "Charlie", response["name"])
		assert.Equal(t, "charlie@example.com", response["email"])
		assert.Equal(t, true, response["email_verified"])
		assert.Equal(t, "tmi", response["provider"])
		assert.Equal(t, "provider-charlie", response["provider_user_id"])
		assert.NotNil(t, response["created_at"])
		assert.NotNil(t, response["modified_at"])
		assert.NotNil(t, response["last_login"])
	})

	t.Run("Success_EnrichFailureFallsBackToNonEnriched", func(t *testing.T) {
		// Create a special mock that fails on EnrichUsers but succeeds on Get
		mockStore := &enrichFailMockUserStore{
			mockUserStore: newMockUserStore(),
		}
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should still return 200 with the non-enriched user
		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Alice", response["name"])
	})
}

// =============================================================================
// UpdateAdminUser Tests
// =============================================================================

func TestUpdateAdminUser(t *testing.T) {
	// Save and restore global store
	origStore := GlobalUserStore
	defer func() {
		GlobalUserStore = origStore
	}()

	t.Run("Success_UpdateEmail", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		newEmail := openapi_types.Email("newalice@example.com")
		reqBody := UpdateAdminUserRequest{
			Email: &newEmail,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response AdminUser
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, openapi_types.Email("newalice@example.com"), response.Email)
		// Name should remain unchanged
		assert.Equal(t, "Alice", response.Name)
	})

	t.Run("Success_UpdateName", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		newName := "Alice Updated"
		reqBody := UpdateAdminUserRequest{
			Name: &newName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response AdminUser
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Alice Updated", response.Name)
		// Email should remain unchanged
		assert.Equal(t, openapi_types.Email("alice@example.com"), response.Email)
	})

	t.Run("Success_UpdateEmailVerified", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		user.EmailVerified = false
		mockStore.addUser(user)

		verified := true
		reqBody := UpdateAdminUserRequest{
			EmailVerified: &verified,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response AdminUser
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response.EmailVerified)
	})

	t.Run("Success_MultipleFieldsUpdated", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		user.EmailVerified = false
		mockStore.addUser(user)

		newEmail := openapi_types.Email("newalice@example.com")
		newName := "Alice B"
		verified := true
		reqBody := UpdateAdminUserRequest{
			Email:         &newEmail,
			Name:          &newName,
			EmailVerified: &verified,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response AdminUser
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, openapi_types.Email("newalice@example.com"), response.Email)
		assert.Equal(t, "Alice B", response.Name)
		assert.True(t, response.EmailVerified)
	})

	t.Run("Success_NoChanges_ReturnsCurrent", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		// Send identical values - no actual change
		existingEmail := openapi_types.Email("alice@example.com")
		existingName := "Alice"
		reqBody := UpdateAdminUserRequest{
			Email: &existingEmail,
			Name:  &existingName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 200 with current user, no update call made
		assert.Equal(t, http.StatusOK, w.Code)

		var response AdminUser
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, openapi_types.Email("alice@example.com"), response.Email)
		assert.Equal(t, "Alice", response.Name)
	})

	t.Run("Success_EmptyBody_NoChanges", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		reqBody := UpdateAdminUserRequest{}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response AdminUser
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, openapi_types.Email("alice@example.com"), response.Email)
	})

	t.Run("Error_UserNotFoundOnGet_404", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		nonExistentUUID := uuid.New()
		newName := "NewName"
		reqBody := UpdateAdminUserRequest{
			Name: &newName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", nonExistentUUID.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("Error_UserNotFoundOnUpdate_404", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		// Simulate race: user is found on Get, but gone by the time Update is called
		mockStore.updateErr = errors.New("user not found")

		newName := "NewName"
		reqBody := UpdateAdminUserRequest{
			Name: &newName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("Error_InvalidUUID_400", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		newName := "NewName"
		reqBody := UpdateAdminUserRequest{
			Name: &newName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", "/admin/users/not-a-valid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Error_InvalidRequestBody_400", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBufferString(`{invalid json}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_request")
	})

	t.Run("Error_StoreGetFailure_500", func(t *testing.T) {
		mockStore := newMockUserStore()
		mockStore.getErr = errors.New("database timeout")
		GlobalUserStore = mockStore

		someUUID := uuid.New()
		newName := "NewName"
		reqBody := UpdateAdminUserRequest{
			Name: &newName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", someUUID.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("Error_StoreUpdateFailure_500", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		// Non-"user not found" update error triggers 500
		mockStore.updateErr = errors.New("database connection lost")

		newName := "NewName"
		reqBody := UpdateAdminUserRequest{
			Name: &newName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("Success_VerifyStorePersistence", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		newName := "Alice Updated"
		reqBody := UpdateAdminUserRequest{
			Name: &newName,
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("PATCH", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify the update is persisted in the mock store
		updatedUser, ok := mockStore.users[user.InternalUuid]
		require.True(t, ok)
		assert.Equal(t, "Alice Updated", updatedUser.Name)
	})
}

// =============================================================================
// DeleteAdminUser Tests
// =============================================================================

func TestDeleteAdminUser(t *testing.T) {
	// Save and restore global store
	origStore := GlobalUserStore
	defer func() {
		GlobalUserStore = origStore
	}()

	t.Run("Success_Deletion_204", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Body.String())
	})

	t.Run("Success_DeletionWithStats", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)
		mockStore.deleteStats = &DeletionStats{
			ThreatModelsTransferred: 3,
			ThreatModelsDeleted:     1,
			UserEmail:               "alice@example.com",
		}

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("Error_UserNotFoundOnLookup_404", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		nonExistentUUID := uuid.New()

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", nonExistentUUID.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("Error_UserNotFoundOnDelete_SpecificError_404", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		// Simulate user found on Get, but delete service returns specific "not found" error
		mockStore.deleteErr = errors.New("failed to find user: user not found")

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("Error_ServerError_500", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user)

		// Non-"not found" delete error triggers 500
		mockStore.deleteErr = errors.New("database connection failed")

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("Error_GetStoreFailure_404", func(t *testing.T) {
		mockStore := newMockUserStore()
		// The DeleteAdminUser handler treats any Get error as 404
		mockStore.getErr = errors.New("database timeout")
		GlobalUserStore = mockStore

		someUUID := uuid.New()

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", someUUID.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("Error_InvalidUUID_400", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", "/admin/users/not-a-valid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Success_DeletionWithZeroStats", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user := makeTestAdminUser("Bob", "bob@example.com", "tmi")
		mockStore.addUser(user)
		mockStore.deleteStats = &DeletionStats{
			ThreatModelsTransferred: 0,
			ThreatModelsDeleted:     0,
			UserEmail:               "bob@example.com",
		}

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("Success_DeletePassesCorrectProviderInfo", func(t *testing.T) {
		// Use a tracking mock to verify provider/providerUserID are passed correctly
		trackingStore := &trackingMockUserStore{
			mockUserStore: newMockUserStore(),
		}
		GlobalUserStore = trackingStore

		user := makeTestAdminUser("Alice", "alice@example.com", "github")
		user.ProviderUserId = "gh-alice-12345"
		trackingStore.addUser(user)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/admin/users/%s", user.InternalUuid.String()), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		// Verify the correct provider and providerUserID were passed to Delete
		assert.Equal(t, "github", trackingStore.lastDeleteProvider)
		assert.Equal(t, "gh-alice-12345", trackingStore.lastDeleteProviderUserID)
	})
}

// trackingMockUserStore extends mockUserStore to track Delete call arguments
type trackingMockUserStore struct {
	*mockUserStore
	lastDeleteProvider       string
	lastDeleteProviderUserID string
}

func (m *trackingMockUserStore) Delete(ctx context.Context, provider, providerUserID string) (*DeletionStats, error) {
	m.lastDeleteProvider = provider
	m.lastDeleteProviderUserID = providerUserID
	return m.mockUserStore.Delete(ctx, provider, providerUserID)
}
