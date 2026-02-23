package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockThreatStore is a mock implementation of ThreatStore for testing
type MockThreatStore struct {
	mock.Mock
}

func (m *MockThreatStore) Create(ctx context.Context, threat *Threat) error {
	args := m.Called(ctx, threat)
	return args.Error(0)
}

func (m *MockThreatStore) Get(ctx context.Context, id string) (*Threat, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Threat), args.Error(1)
}

func (m *MockThreatStore) Update(ctx context.Context, threat *Threat) error {
	args := m.Called(ctx, threat)
	return args.Error(0)
}

func (m *MockThreatStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockThreatStore) List(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, int, error) {
	args := m.Called(ctx, threatModelID, filter)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]Threat), args.Int(1), args.Error(2)
}

func (m *MockThreatStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error) {
	args := m.Called(ctx, id, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Threat), args.Error(1)
}

func (m *MockThreatStore) BulkCreate(ctx context.Context, threats []Threat) error {
	args := m.Called(ctx, threats)
	return args.Error(0)
}

func (m *MockThreatStore) BulkUpdate(ctx context.Context, threats []Threat) error {
	args := m.Called(ctx, threats)
	return args.Error(0)
}

func (m *MockThreatStore) InvalidateCache(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockThreatStore) WarmCache(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

// setupThreatSubResourceHandler creates a test router with threat sub-resource handlers
func setupThreatSubResourceHandler() (*gin.Engine, *MockThreatStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockThreatStore := &MockThreatStore{}
	handler := NewThreatSubResourceHandler(mockThreatStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register threat sub-resource routes
	r.GET("/threat_models/:threat_model_id/threats", handler.GetThreats)
	r.POST("/threat_models/:threat_model_id/threats", handler.CreateThreat)
	r.GET("/threat_models/:threat_model_id/threats/:threat_id", handler.GetThreat)
	r.PUT("/threat_models/:threat_model_id/threats/:threat_id", handler.UpdateThreat)
	r.PATCH("/threat_models/:threat_model_id/threats/:threat_id", handler.PatchThreat)
	r.DELETE("/threat_models/:threat_model_id/threats/:threat_id", handler.DeleteThreat)
	r.POST("/threat_models/:threat_model_id/threats/bulk", handler.BulkCreateThreats)
	r.PUT("/threat_models/:threat_model_id/threats/bulk", handler.BulkUpdateThreats)

	return r, mockThreatStore
}

// TestGetThreats tests retrieving threats for a threat model
func TestGetThreats(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threats := []Threat{
			{Name: "Test Threat 1"},
			{Name: "Test Threat 2"},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		uuid2, _ := uuid.Parse(testUUID2)
		threats[0].Id = &uuid1
		threats[1].Id = &uuid2

		mockStore.On("List", mock.Anything, threatModelID, mock.MatchedBy(func(f ThreatFilter) bool {
			return f.Offset == 0 && f.Limit == 20
		})).Return(threats, 2, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.Threats, 2)
		assert.Equal(t, "Test Threat 1", response.Threats[0].Name)
		assert.Equal(t, "Test Threat 2", response.Threats[1].Name)
		assert.Equal(t, 2, response.Total)
		assert.Equal(t, 20, response.Limit)
		assert.Equal(t, 0, response.Offset)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		req := httptest.NewRequest("GET", "/threat_models/invalid-uuid/threats", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("WithPagination", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threats := []Threat{
			{Name: "Test Threat 1"},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		threats[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, mock.MatchedBy(func(f ThreatFilter) bool {
			return f.Offset == 10 && f.Limit == 5
		})).Return(threats, 100, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.Threats, 1)
		assert.Equal(t, 100, response.Total)
		assert.Equal(t, 5, response.Limit)
		assert.Equal(t, 10, response.Offset)

		mockStore.AssertExpectations(t)
	})
}

// TestGetThreat tests retrieving a specific threat
func TestGetThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threatID := testUUID2

		threat := &Threat{Name: "Test Threat"}
		uuid1, _ := uuid.Parse(threatID)
		threat.Id = &uuid1

		mockStore.On("Get", mock.Anything, threatID).Return(threat, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Test Threat", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threatID := testUUID2

		mockStore.On("Get", mock.Anything, threatID).Return(nil, NotFoundError("Threat not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCreateThreat tests creating a new threat
func TestCreateThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]any{
			"name":        "New Test Threat",
			"description": "A threat created for testing",
			"severity":    "high",
			"status":      "identified",
			"threat_type": []string{"spoofing"},
			"priority":    "high",
			"mitigated":   false,
		}

		threatUUID, _ := uuid.Parse(testUUID2)

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Threat")).Return(nil).Run(func(args mock.Arguments) {
			threat := args.Get(1).(*Threat)
			// Simulate setting the ID and other fields that would be set by the store
			threat.Id = &threatUUID
		})

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "New Test Threat", response["name"])
		assert.Equal(t, "A threat created for testing", response["description"])
		assert.Equal(t, strings.ToLower("high"), strings.ToLower(response["severity"].(string)), "Severity comparison should be case-insensitive")

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidRequestBody", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		// Missing required name field
		requestBody := map[string]any{
			"description": "A threat without a name",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		requestBody := map[string]any{
			"name": "Test Threat",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/threats", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateThreat tests updating an existing threat
func TestUpdateThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threatID := testUUID2

		requestBody := map[string]any{
			"name":        "Updated Test Threat",
			"description": "An updated threat description",
			"severity":    "critical",
			"status":      "mitigated",
			"threat_type": []string{"spoofing"},
			"priority":    "critical",
			"mitigated":   true,
		}

		mockStore.On("Update", mock.Anything, mock.AnythingOfType("*api.Threat")).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/threats/"+threatID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Updated Test Threat", response["name"])
		assert.Equal(t, strings.ToLower("critical"), strings.ToLower(response["severity"].(string)), "Severity comparison should be case-insensitive")

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]any{
			"name": "Test Threat",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/threats/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPatchThreat tests patching a threat with JSON Patch operations
func TestPatchThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threatID := testUUID2

		patchOps := []PatchOperation{
			{
				Op:    "replace",
				Path:  "/name",
				Value: "Patched Threat Name",
			},
			{
				Op:    "replace",
				Path:  "/severity",
				Value: "critical",
			},
		}

		updatedThreat := &Threat{Name: "Patched Threat Name"}
		threatUUID, _ := uuid.Parse(threatID)
		updatedThreat.Id = &threatUUID

		mockStore.On("Patch", mock.Anything, threatID, patchOps).Return(updatedThreat, nil)

		body, _ := json.Marshal(patchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/threats/"+threatID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Patched Threat Name", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidPatchOperations", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threatID := testUUID2

		// Invalid patch operation - missing required fields
		invalidPatchOps := []map[string]any{
			{
				"op": "replace",
				// Missing path and value
			},
		}

		// Expect the store to be called with invalid operations and return an error
		// Handler preserves RequestError status codes (400 for InvalidInputError)
		mockStore.On("Patch", mock.Anything, threatID, mock.AnythingOfType("[]api.PatchOperation")).Return(nil, InvalidInputError("Invalid patch operation: path is required"))

		body, _ := json.Marshal(invalidPatchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/threats/"+threatID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestDeleteThreat tests deleting a threat
func TestDeleteThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threatID := testUUID2

		mockStore.On("Delete", mock.Anything, threatID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threatID := testUUID2

		mockStore.On("Delete", mock.Anything, threatID).Return(NotFoundError("Threat not found"))

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler returns NotFoundError (404) for "not found" errors
		assert.Equal(t, http.StatusNotFound, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestBulkCreateThreats tests bulk creating multiple threats
func TestBulkCreateThreats(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		requestBody := []map[string]any{
			{
				"name":        "Bulk Threat 1",
				"description": "First bulk threat",
				"severity":    "high",
			},
			{
				"name":        "Bulk Threat 2",
				"description": "Second bulk threat",
				"severity":    "medium",
			},
		}

		mockStore.On("BulkCreate", mock.Anything, mock.AnythingOfType("[]api.Threat")).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response []map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response, 2)

		mockStore.AssertExpectations(t)
	})

	t.Run("TooManyThreats", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		// Create 51 threats (over the limit of 50)
		threats := make([]map[string]any, 51)
		for i := range 51 {
			threats[i] = map[string]any{
				"name": "Bulk Threat " + string(rune(i)),
			}
		}

		requestBody := threats

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestBulkUpdateThreats tests bulk updating multiple threats
func TestBulkUpdateThreats(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := testUUID1
		threat1ID := testUUID2
		threat2ID := "00000000-0000-0000-0000-000000000003"

		requestBody := []map[string]any{
			{
				"id":          threat1ID,
				"name":        "Updated Bulk Threat 1",
				"description": "Updated first bulk threat",
				"severity":    "critical",
			},
			{
				"id":          threat2ID,
				"name":        "Updated Bulk Threat 2",
				"description": "Updated second bulk threat",
				"severity":    "high",
			},
		}

		mockStore.On("BulkUpdate", mock.Anything, mock.AnythingOfType("[]api.Threat")).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/threats/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response, 2)

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingThreatIDs", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := testUUID1

		requestBody := []map[string]any{
			{
				"name":        "Threat without ID",
				"description": "This should fail",
			},
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/threats/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
