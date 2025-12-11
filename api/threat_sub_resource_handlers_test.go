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

func (m *MockThreatStore) List(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
	args := m.Called(ctx, threatModelID, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]Threat), args.Error(1)
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

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threats := []Threat{
			{Name: "Test Threat 1"},
			{Name: "Test Threat 2"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		uuid2, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
		threats[0].Id = &uuid1
		threats[1].Id = &uuid2

		mockStore.On("List", mock.Anything, threatModelID, mock.MatchedBy(func(f ThreatFilter) bool {
			return f.Offset == 0 && f.Limit == 20
		})).Return(threats, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "Test Threat 1", response[0]["name"])
		assert.Equal(t, "Test Threat 2", response[1]["name"])

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

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threats := []Threat{
			{Name: "Test Threat 1"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		threats[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, mock.MatchedBy(func(f ThreatFilter) bool {
			return f.Offset == 10 && f.Limit == 5
		})).Return(threats, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestGetThreat tests retrieving a specific threat
func TestGetThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threatID := "00000000-0000-0000-0000-000000000002"

		threat := &Threat{Name: "Test Threat"}
		uuid1, _ := uuid.Parse(threatID)
		threat.Id = &uuid1

		mockStore.On("Get", mock.Anything, threatID).Return(threat, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Test Threat", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threatID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Get", mock.Anything, threatID).Return(nil, NotFoundError("Threat not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

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

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name":        "New Test Threat",
			"description": "A threat created for testing",
			"severity":    "high",
			"status":      "identified",
			"threat_type": []string{"spoofing"},
			"priority":    "high",
			"mitigated":   false,
		}

		_ = &Threat{
			Name:        "New Test Threat",
			Description: stringPtr("A threat created for testing"),
			Severity:    stringPtr("High"),
			Status:      stringPtr("identified"),
			ThreatType:  []string{"spoofing"},
			Priority:    stringPtr("high"),
			Mitigated:   boolPtr(false),
		}
		threatUUID, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")

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

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "New Test Threat", response["name"])
		assert.Equal(t, "A threat created for testing", response["description"])
		assert.Equal(t, strings.ToLower("high"), strings.ToLower(response["severity"].(string)), "Severity comparison should be case-insensitive")

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidRequestBody", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		// Missing required name field
		requestBody := map[string]interface{}{
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

		requestBody := map[string]interface{}{
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

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threatID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
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

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Updated Test Threat", response["name"])
		assert.Equal(t, strings.ToLower("critical"), strings.ToLower(response["severity"].(string)), "Severity comparison should be case-insensitive")

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
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

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threatID := "00000000-0000-0000-0000-000000000002"

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

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Patched Threat Name", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidPatchOperations", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threatID := "00000000-0000-0000-0000-000000000002"

		// Invalid patch operation - missing required fields
		invalidPatchOps := []map[string]interface{}{
			{
				"op": "replace",
				// Missing path and value
			},
		}

		// Expect the store to be called with invalid operations and return an error
		// Handler will convert any store error to ServerError (500)
		mockStore.On("Patch", mock.Anything, threatID, mock.AnythingOfType("[]api.PatchOperation")).Return(nil, InvalidInputError("Invalid patch operation: path is required"))

		body, _ := json.Marshal(invalidPatchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/threats/"+threatID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestDeleteThreat tests deleting a threat
func TestDeleteThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threatID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, threatID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threatID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, threatID).Return(NotFoundError("Threat not found"))

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler converts all store errors to ServerError (500)
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

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

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
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

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response, 2)

		mockStore.AssertExpectations(t)
	})

	t.Run("TooManyThreats", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		// Create 51 threats (over the limit of 50)
		threats := make([]map[string]interface{}, 51)
		for i := 0; i < 51; i++ {
			threats[i] = map[string]interface{}{
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

		threatModelID := "00000000-0000-0000-0000-000000000001"
		threat1ID := "00000000-0000-0000-0000-000000000002"
		threat2ID := "00000000-0000-0000-0000-000000000003"

		requestBody := []map[string]interface{}{
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

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response, 2)

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingThreatIDs", func(t *testing.T) {
		r, _ := setupThreatSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
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
