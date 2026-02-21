package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockAddonStore is a mock implementation of AddonStore for testing
type MockAddonStore struct {
	mock.Mock
}

func (m *MockAddonStore) Create(ctx context.Context, addon *Addon) error {
	args := m.Called(ctx, addon)
	// Set the ID on the addon if not set (simulating auto-generation)
	if addon.ID == uuid.Nil {
		addon.ID = uuid.New()
	}
	return args.Error(0)
}

func (m *MockAddonStore) Get(ctx context.Context, id uuid.UUID) (*Addon, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Addon), args.Error(1)
}

func (m *MockAddonStore) List(ctx context.Context, limit, offset int, threatModelID *uuid.UUID) ([]Addon, int, error) {
	args := m.Called(ctx, limit, offset, threatModelID)
	return args.Get(0).([]Addon), args.Int(1), args.Error(2)
}

func (m *MockAddonStore) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockAddonStore) GetByWebhookID(ctx context.Context, webhookID uuid.UUID) ([]Addon, error) {
	args := m.Called(ctx, webhookID)
	return args.Get(0).([]Addon), args.Error(1)
}

func (m *MockAddonStore) CountActiveInvocations(ctx context.Context, addonID uuid.UUID) (int, error) {
	args := m.Called(ctx, addonID)
	return args.Int(0), args.Error(1)
}

func (m *MockAddonStore) DeleteByWebhookID(ctx context.Context, webhookID uuid.UUID) (int, error) {
	args := m.Called(ctx, webhookID)
	return args.Int(0), args.Error(1)
}

// mockAdminStore reuses mockGroupMemberStoreForAdmin from authorization_middleware_test.go
// (same package, same mock type for GroupMemberStore)

// setupAddonHandlerTest creates a test router with addon handlers
func setupAddonHandlerTest(mockStore *MockAddonStore, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Save original stores
	originalAddonStore := GlobalAddonStore
	originalAdminStore := GlobalGroupMemberStore

	// Set mock stores
	GlobalAddonStore = mockStore
	GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{isAdminResult: isAdmin}

	// Cleanup function will be called when test completes
	// Note: In actual tests, we restore in the test itself

	// Add fake auth middleware that sets user context
	userUUID := uuid.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userInternalUUID", userUUID.String())
		c.Set("userProvider", "test")
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	// Register addon routes
	r.POST("/addons", CreateAddon)
	r.GET("/addons/:id", GetAddon)
	r.GET("/addons", ListAddons)
	r.DELETE("/addons/:id", DeleteAddon)

	// Store original references for cleanup
	r.Use(func(c *gin.Context) {
		c.Set("originalAddonStore", originalAddonStore)
		c.Set("originalAdminStore", originalAdminStore)
		c.Next()
	})

	return r
}

// restoreAddonStores restores original global stores after test
func restoreAddonStores(originalAddonStore AddonStore, originalAdminStore GroupMemberStore) {
	GlobalAddonStore = originalAddonStore
	GlobalGroupMemberStore = originalAdminStore
}

func TestCreateAddon(t *testing.T) {
	// Save original stores
	originalAddonStore := GlobalAddonStore
	originalAdminStore := GlobalGroupMemberStore
	defer restoreAddonStores(originalAddonStore, originalAdminStore)

	t.Run("Success", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true) // isAdmin = true

		webhookID := uuid.New()
		requestBody := map[string]any{
			"name":        "Security Scanner",
			"webhook_id":  webhookID.String(),
			"description": "Scans for security vulnerabilities",
			"icon":        "material-symbols:security",
			"objects":     []string{"threat_model", "threat"},
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Addon")).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Security Scanner", response["name"])
		assert.NotEmpty(t, response["id"])

		mockStore.AssertExpectations(t)
	})

	t.Run("Forbidden - Not Admin", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false) // isAdmin = false

		webhookID := uuid.New()
		requestBody := map[string]any{
			"name":       "Security Scanner",
			"webhook_id": webhookID.String(),
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Non-admin should be forbidden
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Invalid Name - Empty", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		webhookID := uuid.New()
		requestBody := map[string]any{
			"name":       "",
			"webhook_id": webhookID.String(),
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Invalid Icon Format", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		webhookID := uuid.New()
		requestBody := map[string]any{
			"name":       "Security Scanner",
			"webhook_id": webhookID.String(),
			"icon":       "invalid-icon-format",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Invalid Objects", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		webhookID := uuid.New()
		requestBody := map[string]any{
			"name":       "Security Scanner",
			"webhook_id": webhookID.String(),
			"objects":    []string{"invalid_object_type"},
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Store Error", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		webhookID := uuid.New()
		requestBody := map[string]any{
			"name":       "Security Scanner",
			"webhook_id": webhookID.String(),
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Addon")).Return(errors.New("database error"))

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockStore.AssertExpectations(t)
	})
}

func TestGetAddon(t *testing.T) {
	// Save original stores
	originalAddonStore := GlobalAddonStore
	originalAdminStore := GlobalGroupMemberStore
	defer restoreAddonStores(originalAddonStore, originalAdminStore)

	t.Run("Success", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false) // GetAddon doesn't require admin

		addonID := uuid.New()
		webhookID := uuid.New()
		addon := &Addon{
			ID:          addonID,
			CreatedAt:   time.Now(),
			Name:        "Test Addon",
			WebhookID:   webhookID,
			Description: "Test description",
			Icon:        "material-symbols:home",
			Objects:     []string{"threat_model"},
		}

		mockStore.On("Get", mock.Anything, addonID).Return(addon, nil)

		req := httptest.NewRequest("GET", "/addons/"+addonID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, addonID.String(), response["id"])
		assert.Equal(t, "Test Addon", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("Invalid UUID", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		req := httptest.NewRequest("GET", "/addons/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Not Found", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		addonID := uuid.New()
		mockStore.On("Get", mock.Anything, addonID).Return(nil, errors.New("not found"))

		req := httptest.NewRequest("GET", "/addons/"+addonID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})
}

func TestListAddons(t *testing.T) {
	// Save original stores
	originalAddonStore := GlobalAddonStore
	originalAdminStore := GlobalGroupMemberStore
	defer restoreAddonStores(originalAddonStore, originalAdminStore)

	t.Run("Success - Default Pagination", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		addons := []Addon{
			{
				ID:        uuid.New(),
				CreatedAt: time.Now(),
				Name:      "Addon 1",
				WebhookID: uuid.New(),
			},
			{
				ID:        uuid.New(),
				CreatedAt: time.Now(),
				Name:      "Addon 2",
				WebhookID: uuid.New(),
			},
		}

		mockStore.On("List", mock.Anything, 50, 0, (*uuid.UUID)(nil)).Return(addons, 2, nil)

		req := httptest.NewRequest("GET", "/addons", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, float64(2), response["total"])
		assert.Equal(t, float64(50), response["limit"])
		assert.Equal(t, float64(0), response["offset"])

		addonsResponse := response["addons"].([]any)
		assert.Len(t, addonsResponse, 2)

		mockStore.AssertExpectations(t)
	})

	t.Run("Success - Custom Pagination", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		addons := []Addon{
			{
				ID:        uuid.New(),
				CreatedAt: time.Now(),
				Name:      "Addon 3",
				WebhookID: uuid.New(),
			},
		}

		mockStore.On("List", mock.Anything, 10, 20, (*uuid.UUID)(nil)).Return(addons, 25, nil)

		req := httptest.NewRequest("GET", "/addons?limit=10&offset=20", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, float64(25), response["total"])
		assert.Equal(t, float64(10), response["limit"])
		assert.Equal(t, float64(20), response["offset"])

		mockStore.AssertExpectations(t)
	})

	t.Run("Success - Filter by Threat Model ID", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		threatModelID := uuid.New()
		addons := []Addon{
			{
				ID:            uuid.New(),
				CreatedAt:     time.Now(),
				Name:          "Addon for TM",
				WebhookID:     uuid.New(),
				ThreatModelID: &threatModelID,
			},
		}

		mockStore.On("List", mock.Anything, 50, 0, mock.MatchedBy(func(id *uuid.UUID) bool {
			return id != nil && *id == threatModelID
		})).Return(addons, 1, nil)

		req := httptest.NewRequest("GET", "/addons?threat_model_id="+threatModelID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, float64(1), response["total"])

		mockStore.AssertExpectations(t)
	})

	t.Run("Invalid Threat Model ID Format", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		req := httptest.NewRequest("GET", "/addons?threat_model_id=invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Limit Capped at 500", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		addons := []Addon{}
		// When limit exceeds 500, it should be capped to 500
		mockStore.On("List", mock.Anything, 500, 0, (*uuid.UUID)(nil)).Return(addons, 0, nil)

		req := httptest.NewRequest("GET", "/addons?limit=1000", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("Store Error", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false)

		mockStore.On("List", mock.Anything, 50, 0, (*uuid.UUID)(nil)).Return([]Addon{}, 0, errors.New("database error"))

		req := httptest.NewRequest("GET", "/addons", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockStore.AssertExpectations(t)
	})
}

func TestDeleteAddon(t *testing.T) {
	// Save original stores
	originalAddonStore := GlobalAddonStore
	originalAdminStore := GlobalGroupMemberStore
	defer restoreAddonStores(originalAddonStore, originalAdminStore)

	t.Run("Success", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true) // isAdmin = true

		addonID := uuid.New()

		mockStore.On("CountActiveInvocations", mock.Anything, addonID).Return(0, nil)
		mockStore.On("Delete", mock.Anything, addonID).Return(nil)

		req := httptest.NewRequest("DELETE", "/addons/"+addonID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("Forbidden - Not Admin", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, false) // isAdmin = false

		addonID := uuid.New()

		req := httptest.NewRequest("DELETE", "/addons/"+addonID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Invalid UUID", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		req := httptest.NewRequest("DELETE", "/addons/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Conflict - Active Invocations", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		addonID := uuid.New()

		mockStore.On("CountActiveInvocations", mock.Anything, addonID).Return(3, nil)

		req := httptest.NewRequest("DELETE", "/addons/"+addonID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)

		// Verify the response body contains error information
		body := w.Body.String()
		assert.Contains(t, body, "active invocations")

		mockStore.AssertExpectations(t)
	})

	t.Run("Not Found", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		addonID := uuid.New()

		mockStore.On("CountActiveInvocations", mock.Anything, addonID).Return(0, nil)
		mockStore.On("Delete", mock.Anything, addonID).Return(errors.New("not found"))

		req := httptest.NewRequest("DELETE", "/addons/"+addonID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("Count Invocations Error", func(t *testing.T) {
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)

		addonID := uuid.New()

		mockStore.On("CountActiveInvocations", mock.Anything, addonID).Return(0, errors.New("database error"))

		req := httptest.NewRequest("DELETE", "/addons/"+addonID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockStore.AssertExpectations(t)
	})
}

func TestAddonToResponse(t *testing.T) {
	t.Run("Nil Addon", func(t *testing.T) {
		response := addonToResponse(nil)
		assert.Equal(t, uuid.Nil, response.Id)
	})

	t.Run("Full Addon", func(t *testing.T) {
		addonID := uuid.New()
		webhookID := uuid.New()
		threatModelID := uuid.New()
		createdAt := time.Now()

		addon := &Addon{
			ID:            addonID,
			CreatedAt:     createdAt,
			Name:          "Test Addon",
			WebhookID:     webhookID,
			Description:   "Test description",
			Icon:          "material-symbols:test",
			Objects:       []string{"threat", "diagram"},
			ThreatModelID: &threatModelID,
		}

		response := addonToResponse(addon)

		assert.Equal(t, addonID, response.Id)
		assert.Equal(t, createdAt, response.CreatedAt)
		assert.Equal(t, "Test Addon", response.Name)
		assert.Equal(t, webhookID, response.WebhookId)
		assert.NotNil(t, response.Description)
		assert.Equal(t, "Test description", *response.Description)
		assert.NotNil(t, response.Icon)
		assert.Equal(t, "material-symbols:test", *response.Icon)
		assert.NotNil(t, response.Objects)
		assert.Equal(t, []string{"threat", "diagram"}, *response.Objects)
		assert.NotNil(t, response.ThreatModelId)
		assert.Equal(t, threatModelID, *response.ThreatModelId)
	})
}
