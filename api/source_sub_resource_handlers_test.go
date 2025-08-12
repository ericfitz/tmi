package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockSourceStore is a mock implementation of SourceStore for testing
type MockSourceStore struct {
	mock.Mock
}

func (m *MockSourceStore) Create(ctx context.Context, source *Source, threatModelID string) error {
	args := m.Called(ctx, source, threatModelID)
	return args.Error(0)
}

func (m *MockSourceStore) Get(ctx context.Context, id string) (*Source, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Source), args.Error(1)
}

func (m *MockSourceStore) Update(ctx context.Context, source *Source, threatModelID string) error {
	args := m.Called(ctx, source, threatModelID)
	return args.Error(0)
}

func (m *MockSourceStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockSourceStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Source, error) {
	args := m.Called(ctx, threatModelID, offset, limit)
	return args.Get(0).([]Source), args.Error(1)
}

func (m *MockSourceStore) BulkCreate(ctx context.Context, sources []Source, threatModelID string) error {
	args := m.Called(ctx, sources, threatModelID)
	return args.Error(0)
}

func (m *MockSourceStore) InvalidateCache(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockSourceStore) WarmCache(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

// setupSourceSubResourceHandler creates a test router with source sub-resource handlers
func setupSourceSubResourceHandler() (*gin.Engine, *MockSourceStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockSourceStore := &MockSourceStore{}
	handler := NewSourceSubResourceHandler(mockSourceStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userName", "test@example.com")
		c.Set("userRole", Writer)
		c.Next()
	})

	// Register source sub-resource routes
	r.GET("/threat_models/:threat_model_id/sources", handler.GetSources)
	r.POST("/threat_models/:threat_model_id/sources", handler.CreateSource)
	r.GET("/threat_models/:threat_model_id/sources/:source_id", handler.GetSource)
	r.PUT("/threat_models/:threat_model_id/sources/:source_id", handler.UpdateSource)
	r.DELETE("/threat_models/:threat_model_id/sources/:source_id", handler.DeleteSource)
	r.POST("/threat_models/:threat_model_id/sources/bulk", handler.BulkCreateSources)

	return r, mockSourceStore
}

// TestGetSources tests retrieving source code references for a threat model
func TestGetSources(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sources := []Source{
			{Url: "https://github.com/user/repo1"},
			{Url: "https://github.com/user/repo2"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		uuid2, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
		sources[0].Id = &uuid1
		sources[1].Id = &uuid2
		sources[0].Name = stringPtr("Test Repo 1")
		sources[1].Name = stringPtr("Test Repo 2")

		mockStore.On("List", mock.Anything, threatModelID, 0, 20).Return(sources, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/sources", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "https://github.com/user/repo1", response[0]["url"])
		assert.Equal(t, "https://github.com/user/repo2", response[1]["url"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		req := httptest.NewRequest("GET", "/threat_models/invalid-uuid/sources", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("WithPagination", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sources := []Source{
			{Url: "https://github.com/user/repo1"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		sources[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, 10, 5).Return(sources, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/sources?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestGetSource tests retrieving a specific source code reference
func TestGetSource(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sourceID := "00000000-0000-0000-0000-000000000002"

		source := &Source{Url: "https://github.com/user/test-repo"}
		uuid1, _ := uuid.Parse(sourceID)
		source.Id = &uuid1
		source.Name = stringPtr("Test Repository")

		mockStore.On("Get", mock.Anything, sourceID).Return(source, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/sources/"+sourceID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "https://github.com/user/test-repo", response["url"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sourceID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Get", mock.Anything, sourceID).Return(nil, NotFoundError("Source not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/sources/"+sourceID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidSourceID", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/sources/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCreateSource tests creating a new source code reference
func TestCreateSource(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name":        "New Test Repository",
			"description": "A repository created for testing",
			"url":         "https://github.com/user/new-repo",
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Source"), threatModelID).Return(nil).Run(func(args mock.Arguments) {
			source := args.Get(1).(*Source)
			// Simulate setting the ID that would be set by the store
			sourceUUID, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
			source.Id = &sourceUUID
		})

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/sources", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "New Test Repository", response["name"])
		assert.Equal(t, "A repository created for testing", response["description"])
		assert.Equal(t, "https://github.com/user/new-repo", response["url"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingURL", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name": "Test Repository",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/sources", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		requestBody := map[string]interface{}{
			"url": "https://github.com/user/repo",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/sources", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateSource tests updating an existing source code reference
func TestUpdateSource(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sourceID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"name":        "Updated Test Repository",
			"description": "An updated repository description",
			"url":         "https://github.com/user/updated-repo",
		}

		mockStore.On("Update", mock.Anything, mock.AnythingOfType("*api.Source"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/sources/"+sourceID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Updated Test Repository", response["name"])
		assert.Equal(t, "https://github.com/user/updated-repo", response["url"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingURL", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sourceID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"name": "Test Repository",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/sources/"+sourceID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidSourceID", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"url": "https://github.com/user/repo",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/sources/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestDeleteSource tests deleting a source code reference
func TestDeleteSource(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sourceID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, sourceID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/sources/"+sourceID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		sourceID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, sourceID).Return(NotFoundError("Source not found"))

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/sources/"+sourceID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler converts all store errors to ServerError (500)
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidSourceID", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/sources/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestBulkCreateSources tests bulk creating multiple source code references
func TestBulkCreateSources(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
			{
				"name":        "Bulk Source 1",
				"description": "First bulk source",
				"url":         "https://github.com/user/bulk1",
			},
			{
				"name":        "Bulk Source 2",
				"description": "Second bulk source",
				"url":         "https://github.com/user/bulk2",
			},
		}

		mockStore.On("BulkCreate", mock.Anything, mock.AnythingOfType("[]api.Source"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/sources/bulk", bytes.NewBuffer(body))
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

	t.Run("TooManySources", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		// Create 51 sources (over the limit of 50)
		sources := make([]map[string]interface{}, 51)
		for i := 0; i < 51; i++ {
			sources[i] = map[string]interface{}{
				"name": "Bulk Source " + string(rune(i)),
				"url":  "https://github.com/user/repo",
			}
		}

		requestBody := sources

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/sources/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingSourceURL", func(t *testing.T) {
		r, _ := setupSourceSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
			{
				"name": "Source without URL",
			},
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/sources/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
