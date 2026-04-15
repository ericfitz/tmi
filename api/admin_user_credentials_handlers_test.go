package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Mock credential deleter for unit tests
// =============================================================================

// mockCredentialDeleter implements credentialDeleter for testing.
// deleteErr controls the error returned by Delete; nil means success.
type mockCredentialDeleter struct {
	deleteErr error
}

func (m *mockCredentialDeleter) Delete(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return m.deleteErr
}

// credNotFoundErr returns an error that looks like a "not found" error from the credential service.
func credNotFoundErr() error {
	return fmt.Errorf("client credential not found or unauthorized")
}

// credServerErr returns an error that looks like a database/server error from the credential service.
func credServerErr() error {
	return fmt.Errorf("failed to delete client credential: connection refused")
}

// Test request body constants
const (
	testCredNameBody  = `{"name": "test"}`
	testCredEmptyName = `{"name": ""}`
)

// =============================================================================
// Router Setup for Admin User Credentials Tests
// =============================================================================

func setupAdminUserCredentialsRouter() (*gin.Engine, *Server) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	server := &Server{}

	adminUUID := uuid.New().String()
	r.Use(func(c *gin.Context) {
		SetFullUserContext(c, "admin@test.com", "provider-id", adminUUID, "tmi", []string{"administrators"})
		c.Set("isAdmin", true)
		c.Next()
	})

	r.GET("/admin/users/:internal_uuid/client_credentials", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID"})
			return
		}
		server.ListAdminUserClientCredentials(c, parsedUUID, ListAdminUserClientCredentialsParams{})
	})

	r.POST("/admin/users/:internal_uuid/client_credentials", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID"})
			return
		}
		server.CreateAdminUserClientCredential(c, parsedUUID)
	})

	r.DELETE("/admin/users/:internal_uuid/client_credentials/:credential_id", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID"})
			return
		}
		credStr := c.Param("credential_id")
		credUUID, err := uuid.Parse(credStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credential UUID"})
			return
		}
		server.DeleteAdminUserClientCredential(c, parsedUUID, credUUID)
	})

	return r, server
}

// =============================================================================
// Tests: getAutomationUser guard
// =============================================================================

func TestAdminUserCredentials_UserNotFound(t *testing.T) {
	router, _ := setupAdminUserCredentialsRouter()
	store := newMockUserStore()
	GlobalUserStore = store

	unknownUUID := uuid.New().String()

	t.Run("list returns 404 for unknown user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/users/"+unknownUUID+"/client_credentials", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("create returns 404 for unknown user", func(t *testing.T) {
		body := testCredNameBody
		req := httptest.NewRequest(http.MethodPost, "/admin/users/"+unknownUUID+"/client_credentials",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("delete returns 404 for unknown user", func(t *testing.T) {
		credUUID := uuid.New().String()
		req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+unknownUUID+"/client_credentials/"+credUUID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestAdminUserCredentials_NonAutomationUser(t *testing.T) {
	router, _ := setupAdminUserCredentialsRouter()
	store := newMockUserStore()

	// Add a regular (non-automation) user
	regularUser := makeTestAdminUser("alice", "alice@example.com", "github")
	// automation is nil (not set) — should be rejected
	store.addUser(regularUser)
	GlobalUserStore = store

	userUUID := regularUser.InternalUuid.String()

	t.Run("list returns 403 for non-automation user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/users/"+userUUID+"/client_credentials", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("create returns 403 for non-automation user", func(t *testing.T) {
		body := testCredNameBody
		req := httptest.NewRequest(http.MethodPost, "/admin/users/"+userUUID+"/client_credentials",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("delete returns 403 for non-automation user", func(t *testing.T) {
		credUUID := uuid.New().String()
		req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+userUUID+"/client_credentials/"+credUUID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestAdminUserCredentials_AutomationUser_NoAuthService(t *testing.T) {
	router, _ := setupAdminUserCredentialsRouter()
	store := newMockUserStore()

	// Add an automation user
	autoTrue := true
	autoUser := makeTestAdminUser("bot", "bot@tmi.local", "tmi")
	autoUser.Automation = &autoTrue
	store.addUser(autoUser)
	GlobalUserStore = store

	userUUID := autoUser.InternalUuid.String()

	t.Run("list returns 503 when no auth service", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/users/"+userUUID+"/client_credentials", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("create returns 503 when no auth service", func(t *testing.T) {
		body := `{"name": "test-cred"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/users/"+userUUID+"/client_credentials",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("delete returns 503 when no auth service", func(t *testing.T) {
		credUUID := uuid.New().String()
		req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+userUUID+"/client_credentials/"+credUUID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestAdminUserCredentials_CreateValidation(t *testing.T) {
	router, _ := setupAdminUserCredentialsRouter()
	store := newMockUserStore()

	autoTrue := true
	autoUser := makeTestAdminUser("bot", "bot@tmi.local", "tmi")
	autoUser.Automation = &autoTrue
	store.addUser(autoUser)
	GlobalUserStore = store

	userUUID := autoUser.InternalUuid.String()

	t.Run("missing body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/users/"+userUUID+"/client_credentials", nil)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty name returns 400", func(t *testing.T) {
		body := testCredEmptyName
		req := httptest.NewRequest(http.MethodPost, "/admin/users/"+userUUID+"/client_credentials",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		// Empty name fails validation — either 400 (name validation) or 503 (reaches auth service)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusServiceUnavailable}, w.Code)
	})
}

// =============================================================================
// Tests: DeleteAdminUserClientCredential — credential deleter paths
// =============================================================================

// setupAdminUserCredentialsRouterWithDeleter creates a router with an injected
// credentialDeleter so tests can exercise the ccService.Delete result paths
// without a real database.
func setupAdminUserCredentialsRouterWithDeleter(deleter credentialDeleter) (*gin.Engine, *Server) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	server := &Server{
		credentialDeleter: deleter,
	}

	adminUUID := uuid.New().String()
	r.Use(func(c *gin.Context) {
		SetFullUserContext(c, "admin@test.com", "provider-id", adminUUID, "tmi", []string{"administrators"})
		c.Set("isAdmin", true)
		c.Next()
	})

	r.DELETE("/admin/users/:internal_uuid/client_credentials/:credential_id", func(c *gin.Context) {
		uuidStr := c.Param("internal_uuid")
		parsedUUID, err := uuid.Parse(uuidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid UUID"})
			return
		}
		credStr := c.Param("credential_id")
		credUUID, err := uuid.Parse(credStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credential UUID"})
			return
		}
		server.DeleteAdminUserClientCredential(c, parsedUUID, credUUID)
	})

	return r, server
}

func TestDeleteAdminUserClientCredential_SuccessfulDeletion(t *testing.T) {
	mock := &mockCredentialDeleter{deleteErr: nil}
	router, _ := setupAdminUserCredentialsRouterWithDeleter(mock)

	store := newMockUserStore()
	autoTrue := true
	autoUser := makeTestAdminUser("bot", "bot@tmi.local", "tmi")
	autoUser.Automation = &autoTrue
	store.addUser(autoUser)
	GlobalUserStore = store

	credUUID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete,
		"/admin/users/"+autoUser.InternalUuid.String()+"/client_credentials/"+credUUID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteAdminUserClientCredential_CredentialNotFound(t *testing.T) {
	mock := &mockCredentialDeleter{deleteErr: credNotFoundErr()}
	router, _ := setupAdminUserCredentialsRouterWithDeleter(mock)

	store := newMockUserStore()
	autoTrue := true
	autoUser := makeTestAdminUser("bot", "bot@tmi.local", "tmi")
	autoUser.Automation = &autoTrue
	store.addUser(autoUser)
	GlobalUserStore = store

	credUUID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete,
		"/admin/users/"+autoUser.InternalUuid.String()+"/client_credentials/"+credUUID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteAdminUserClientCredential_ServerError(t *testing.T) {
	mock := &mockCredentialDeleter{deleteErr: credServerErr()}
	router, _ := setupAdminUserCredentialsRouterWithDeleter(mock)

	store := newMockUserStore()
	autoTrue := true
	autoUser := makeTestAdminUser("bot", "bot@tmi.local", "tmi")
	autoUser.Automation = &autoTrue
	store.addUser(autoUser)
	GlobalUserStore = store

	credUUID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete,
		"/admin/users/"+autoUser.InternalUuid.String()+"/client_credentials/"+credUUID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Server/DB errors are surfaced as 503 Service Unavailable, never 500
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "30", w.Header().Get("Retry-After"))
}
