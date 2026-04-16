package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAuthorizeIncludeDeleted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Save and restore global stores
	origGroupMemberStore := GlobalGroupMemberRepository
	defer func() { GlobalGroupMemberRepository = origGroupMemberStore }()

	t.Run("allows admin user", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

		// Set up user context
		SetFullUserContext(c, "charlie@tmi.local", "charlie", uuid.New().String(), "tmi", []string{})

		// Mock admin check
		GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: true}

		result := AuthorizeIncludeDeleted(c)
		assert.True(t, result)
		assert.NotEqual(t, http.StatusForbidden, w.Code)
	})

	t.Run("allows owner role", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

		// Set up user context
		SetFullUserContext(c, "alice@tmi.local", "alice", uuid.New().String(), "tmi", []string{})

		// Not an admin, but has owner role from middleware
		GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: false}
		c.Set("userRole", RoleOwner)

		result := AuthorizeIncludeDeleted(c)
		assert.True(t, result)
		assert.NotEqual(t, http.StatusForbidden, w.Code)
	})

	t.Run("rejects reader role", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

		// Set up user context
		SetFullUserContext(c, "bob@tmi.local", "bob", uuid.New().String(), "tmi", []string{})

		// Not admin, reader role
		GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: false}
		c.Set("userRole", RoleReader)

		result := AuthorizeIncludeDeleted(c)
		assert.False(t, result)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("rejects writer role", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

		// Set up user context
		SetFullUserContext(c, "bob@tmi.local", "bob", uuid.New().String(), "tmi", []string{})

		// Not admin, writer role
		GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: false}
		c.Set("userRole", RoleWriter)

		result := AuthorizeIncludeDeleted(c)
		assert.False(t, result)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("rejects when no role set and not admin", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

		SetFullUserContext(c, "bob@tmi.local", "bob", uuid.New().String(), "tmi", []string{})
		GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: false}

		result := AuthorizeIncludeDeleted(c)
		assert.False(t, result)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestIncludeDeletedContext(t *testing.T) {
	t.Run("default is false", func(t *testing.T) {
		ctx := context.Background()
		assert.False(t, includeDeletedFromContext(ctx))
	})

	t.Run("set to true", func(t *testing.T) {
		ctx := ContextWithIncludeDeleted(context.Background())
		assert.True(t, includeDeletedFromContext(ctx))
	})
}
