package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockMetadataStore is a mock implementation of MetadataStore for testing
type MockMetadataStore struct {
	mock.Mock
}

func (m *MockMetadataStore) Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockMetadataStore) Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error) {
	args := m.Called(ctx, entityType, entityID, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Metadata), args.Error(1)
}

func (m *MockMetadataStore) Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockMetadataStore) Delete(ctx context.Context, entityType, entityID, key string) error {
	args := m.Called(ctx, entityType, entityID, key)
	return args.Error(0)
}

func (m *MockMetadataStore) List(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	args := m.Called(ctx, entityType, entityID)
	return args.Get(0).([]Metadata), args.Error(1)
}

func (m *MockMetadataStore) Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockMetadataStore) BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockMetadataStore) BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockMetadataStore) BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error {
	args := m.Called(ctx, entityType, entityID, keys)
	return args.Error(0)
}

func (m *MockMetadataStore) GetByKey(ctx context.Context, key string) ([]Metadata, error) {
	args := m.Called(ctx, key)
	return args.Get(0).([]Metadata), args.Error(1)
}

func (m *MockMetadataStore) ListKeys(ctx context.Context, entityType, entityID string) ([]string, error) {
	args := m.Called(ctx, entityType, entityID)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockMetadataStore) InvalidateCache(ctx context.Context, entityType, entityID string) error {
	args := m.Called(ctx, entityType, entityID)
	return args.Error(0)
}

func (m *MockMetadataStore) WarmCache(ctx context.Context, entityType, entityID string) error {
	args := m.Called(ctx, entityType, entityID)
	return args.Error(0)
}

// setupThreatMetadataHandler creates a test router with threat metadata handlers
func setupThreatMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockMetadataStore := &MockMetadataStore{}
	handler := NewThreatMetadataHandler(mockMetadataStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register threat metadata routes
	r.GET("/threat_models/:threat_model_id/threats/:threat_id/metadata", handler.GetThreatMetadata)
	r.GET("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", handler.GetThreatMetadataByKey)
	r.POST("/threat_models/:threat_model_id/threats/:threat_id/metadata", handler.CreateThreatMetadata)
	r.PUT("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", handler.UpdateThreatMetadata)
	r.DELETE("/threat_models/:threat_model_id/threats/:threat_id/metadata/:key", handler.DeleteThreatMetadata)
	r.POST("/threat_models/:threat_model_id/threats/:threat_id/metadata/bulk", handler.BulkCreateThreatMetadata)

	return r, mockMetadataStore
}

// setupDocumentMetadataHandler creates a test router with document metadata handlers
func setupDocumentMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockMetadataStore := &MockMetadataStore{}
	handler := NewDocumentMetadataHandler(mockMetadataStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register document metadata routes
	r.GET("/threat_models/:threat_model_id/documents/:document_id/metadata", handler.GetDocumentMetadata)
	r.GET("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", handler.GetDocumentMetadataByKey)
	r.POST("/threat_models/:threat_model_id/documents/:document_id/metadata", handler.CreateDocumentMetadata)
	r.PUT("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", handler.UpdateDocumentMetadata)
	r.DELETE("/threat_models/:threat_model_id/documents/:document_id/metadata/:key", handler.DeleteDocumentMetadata)
	r.POST("/threat_models/:threat_model_id/documents/:document_id/metadata/bulk", handler.BulkCreateDocumentMetadata)

	return r, mockMetadataStore
}

// TestThreatMetadata tests threat metadata operations
func TestThreatMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Run("GetThreatMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"

			metadata := []Metadata{
				{Key: "priority", Value: "high"},
				{Key: "category", Value: "spoofing"},
			}

			mockStore.On("List", mock.Anything, "threat", threatID).Return(metadata, nil)

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response []map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Len(t, response, 2)
			assert.Equal(t, "priority", response[0]["key"])
			assert.Equal(t, "high", response[0]["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("InvalidThreatID", func(t *testing.T) {
			r, _ := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/invalid-uuid/metadata", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("GetThreatMetadataByKey", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"
			key := "priority"

			metadata := &Metadata{Key: "priority", Value: "high"}

			mockStore.On("Get", mock.Anything, "threat", threatID, key).Return(metadata, nil)

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "priority", response["key"])
			assert.Equal(t, "high", response["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("NotFound", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"
			key := "nonexistent"

			mockStore.On("Get", mock.Anything, "threat", threatID, key).Return(nil, NotFoundError("Metadata not found"))

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
			mockStore.AssertExpectations(t)
		})
	})

	t.Run("CreateThreatMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"

			requestBody := map[string]interface{}{
				"key":   "priority",
				"value": "high",
			}

			createdMetadata := &Metadata{Key: "priority", Value: "high"}

			mockStore.On("Create", mock.Anything, "threat", threatID, mock.AnythingOfType("*api.Metadata")).Return(nil)
			mockStore.On("Get", mock.Anything, "threat", threatID, "priority").Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "priority", response["key"])
			assert.Equal(t, "high", response["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("MissingKey", func(t *testing.T) {
			r, _ := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"

			requestBody := map[string]interface{}{
				"value": "high",
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// With binding:"required" tags, missing key should return 400 Bad Request
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("MissingValue", func(t *testing.T) {
			r, _ := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"

			requestBody := map[string]interface{}{
				"key": "priority",
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// With binding:"required" tags, missing value should return 400 Bad Request
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("UpdateThreatMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"
			key := "priority"

			requestBody := map[string]interface{}{
				"key":   key, // Required by binding validation
				"value": "critical",
			}

			updatedMetadata := &Metadata{Key: "priority", Value: "critical"}

			mockStore.On("Update", mock.Anything, "threat", threatID, mock.AnythingOfType("*api.Metadata")).Return(nil)
			mockStore.On("Get", mock.Anything, "threat", threatID, key).Return(updatedMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "priority", response["key"])
			assert.Equal(t, "critical", response["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("MissingValue", func(t *testing.T) {
			r, _ := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"
			key := "priority"

			requestBody := map[string]interface{}{}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// With binding:"required" tags, missing value should return 400 Bad Request
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("DeleteThreatMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"
			key := "priority"

			mockStore.On("Delete", mock.Anything, "threat", threatID, key).Return(nil)

			req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNoContent, w.Code)

			mockStore.AssertExpectations(t)
		})

		t.Run("NotFound", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"
			key := "nonexistent"

			mockStore.On("Delete", mock.Anything, "threat", threatID, key).Return(NotFoundError("Metadata not found"))

			req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// Handler converts all store errors to ServerError (500)
			assert.Equal(t, http.StatusInternalServerError, w.Code)

			mockStore.AssertExpectations(t)
		})
	})

	t.Run("BulkCreateThreatMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"

			requestBody := []map[string]interface{}{
				{"key": "priority", "value": "high"},
				{"key": "category", "value": "spoofing"},
			}

			createdMetadata := []Metadata{
				{Key: "priority", Value: "high"},
				{Key: "category", Value: "spoofing"},
			}

			mockStore.On("BulkCreate", mock.Anything, "threat", threatID, mock.AnythingOfType("[]api.Metadata")).Return(nil)
			mockStore.On("List", mock.Anything, "threat", threatID).Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/bulk", bytes.NewBuffer(body))
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

		t.Run("TooManyMetadata", func(t *testing.T) {
			r, _ := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"

			// Create 21 metadata entries (over the limit of 20)
			metadata := make([]map[string]interface{}, 21)
			for i := 0; i < 21; i++ {
				metadata[i] = map[string]interface{}{
					"key":   "key" + string(rune(i)),
					"value": "value" + string(rune(i)),
				}
			}

			requestBody := metadata

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("DuplicateKeys", func(t *testing.T) {
			r, _ := setupThreatMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			threatID := "00000000-0000-0000-0000-000000000002"

			requestBody := []map[string]interface{}{
				{"key": "priority", "value": "high"},
				{"key": "priority", "value": "critical"}, // duplicate key
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})
}

// TestDocumentMetadata tests document metadata operations
func TestDocumentMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Run("GetDocumentMetadata", func(t *testing.T) {
		r, mockStore := setupDocumentMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		metadata := []Metadata{
			{Key: "format", Value: "pdf"},
			{Key: "version", Value: "1.0"},
		}

		mockStore.On("List", mock.Anything, "document", documentID).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents/"+documentID+"/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "format", response[0]["key"])
		assert.Equal(t, "pdf", response[0]["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("CreateDocumentMetadata", func(t *testing.T) {
		r, mockStore := setupDocumentMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"key":   "format",
			"value": "pdf",
		}

		createdMetadata := &Metadata{Key: "format", Value: "pdf"}

		mockStore.On("Create", mock.Anything, "document", documentID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "document", documentID, "format").Return(createdMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/"+documentID+"/metadata", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "format", response["key"])
		assert.Equal(t, "pdf", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("UpdateDocumentMetadata", func(t *testing.T) {
		r, mockStore := setupDocumentMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"
		key := "format"

		requestBody := map[string]interface{}{
			"key":   key, // Required by binding validation
			"value": "docx",
		}

		updatedMetadata := &Metadata{Key: "format", Value: "docx"}

		mockStore.On("Update", mock.Anything, "document", documentID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "document", documentID, key).Return(updatedMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/documents/"+documentID+"/metadata/"+key, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "format", response["key"])
		assert.Equal(t, "docx", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("DeleteDocumentMetadata", func(t *testing.T) {
		r, mockStore := setupDocumentMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"
		key := "format"

		mockStore.On("Delete", mock.Anything, "document", documentID, key).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/documents/"+documentID+"/metadata/"+key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("BulkCreateDocumentMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupDocumentMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			documentID := "00000000-0000-0000-0000-000000000002"

			requestBody := []map[string]interface{}{
				{"key": "format", "value": "pdf"},
				{"key": "version", "value": "1.0"},
			}

			createdMetadata := []Metadata{
				{Key: "format", Value: "pdf"},
				{Key: "version", Value: "1.0"},
			}

			mockStore.On("BulkCreate", mock.Anything, "document", documentID, mock.AnythingOfType("[]api.Metadata")).Return(nil)
			mockStore.On("List", mock.Anything, "document", documentID).Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/"+documentID+"/metadata/bulk", bytes.NewBuffer(body))
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

		t.Run("TooManyMetadata", func(t *testing.T) {
			r, _ := setupDocumentMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			documentID := "00000000-0000-0000-0000-000000000002"

			// Create 21 metadata entries (over the limit of 20)
			metadata := make([]map[string]interface{}, 21)
			for i := 0; i < 21; i++ {
				metadata[i] = map[string]interface{}{
					"key":   fmt.Sprintf("key%d", i),
					"value": fmt.Sprintf("value%d", i),
				}
			}

			body, _ := json.Marshal(metadata)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/"+documentID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("DuplicateKeys", func(t *testing.T) {
			r, _ := setupDocumentMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"
			documentID := "00000000-0000-0000-0000-000000000002"

			requestBody := []map[string]interface{}{
				{"key": "format", "value": "pdf"},
				{"key": "format", "value": "docx"}, // duplicate key
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/"+documentID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})
}

// setupRepositoryMetadataHandler creates a test router with repository metadata handlers
func setupRepositoryMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockMetadataStore := &MockMetadataStore{}
	handler := NewRepositoryMetadataHandler(mockMetadataStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register repository metadata routes
	r.GET("/threat_models/:threat_model_id/repositories/:repository_id/metadata", handler.GetRepositoryMetadata)
	r.GET("/threat_models/:threat_model_id/repositories/:repository_id/metadata/:key", handler.GetRepositoryMetadataByKey)
	r.POST("/threat_models/:threat_model_id/repositories/:repository_id/metadata", handler.CreateRepositoryMetadata)
	r.PUT("/threat_models/:threat_model_id/repositories/:repository_id/metadata/:key", handler.UpdateRepositoryMetadata)
	r.DELETE("/threat_models/:threat_model_id/repositories/:repository_id/metadata/:key", handler.DeleteRepositoryMetadata)
	r.POST("/threat_models/:threat_model_id/repositories/:repository_id/metadata/bulk", handler.BulkCreateRepositoryMetadata)

	return r, mockMetadataStore
}

// setupThreatModelMetadataHandler creates a test router with threat model metadata handlers
func setupThreatModelMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockMetadataStore := &MockMetadataStore{}
	handler := NewThreatModelMetadataHandler(mockMetadataStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register threat model metadata routes
	r.GET("/threat_models/:threat_model_id/metadata", handler.GetThreatModelMetadata)
	r.GET("/threat_models/:threat_model_id/metadata/:key", handler.GetThreatModelMetadataByKey)
	r.POST("/threat_models/:threat_model_id/metadata", handler.CreateThreatModelMetadata)
	r.PUT("/threat_models/:threat_model_id/metadata/:key", handler.UpdateThreatModelMetadata)
	r.DELETE("/threat_models/:threat_model_id/metadata/:key", handler.DeleteThreatModelMetadata)
	r.POST("/threat_models/:threat_model_id/metadata/bulk", handler.BulkCreateThreatModelMetadata)

	return r, mockMetadataStore
}

// setupDiagramMetadataHandler creates a test router with diagram metadata handlers
func setupDiagramMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockMetadataStore := &MockMetadataStore{}
	handler := NewDiagramMetadataHandler(mockMetadataStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register diagram metadata routes
	r.GET("/diagrams/:id/metadata", handler.GetDirectDiagramMetadata)
	r.GET("/diagrams/:id/metadata/:key", handler.GetDirectDiagramMetadataByKey)
	r.POST("/diagrams/:id/metadata", handler.CreateDirectDiagramMetadata)
	r.PUT("/diagrams/:id/metadata/:key", handler.UpdateDirectDiagramMetadata)
	r.DELETE("/diagrams/:id/metadata/:key", handler.DeleteDirectDiagramMetadata)
	r.POST("/diagrams/:id/metadata/bulk", handler.BulkCreateDirectDiagramMetadata)

	return r, mockMetadataStore
}

// TestRepositoryMetadata tests repository metadata operations
func TestRepositoryMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Run("GetRepositoryMetadata", func(t *testing.T) {
		r, mockStore := setupRepositoryMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		metadata := []Metadata{
			{Key: "repository_type", Value: "git"},
			{Key: "main_branch", Value: "main"},
		}

		mockStore.On("List", mock.Anything, "repository", repositoryID).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/repositories/"+repositoryID+"/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "repository_type", response[0]["key"])
		assert.Equal(t, "git", response[0]["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("CreateRepositoryMetadata", func(t *testing.T) {
		r, mockStore := setupRepositoryMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"key":   "repository_type",
			"value": "git",
		}

		createdMetadata := &Metadata{Key: "repository_type", Value: "git"}

		mockStore.On("Create", mock.Anything, "repository", repositoryID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "repository", repositoryID, "repository_type").Return(createdMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/repositories/"+repositoryID+"/metadata", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "repository_type", response["key"])
		assert.Equal(t, "git", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("UpdateRepositoryMetadata", func(t *testing.T) {
		r, mockStore := setupRepositoryMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"
		key := "repository_type"

		requestBody := map[string]interface{}{
			"key":   key, // Required by binding validation
			"value": "svn",
		}

		updatedMetadata := &Metadata{Key: "repository_type", Value: "svn"}

		mockStore.On("Update", mock.Anything, "repository", repositoryID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "repository", repositoryID, key).Return(updatedMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/repositories/"+repositoryID+"/metadata/"+key, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "repository_type", response["key"])
		assert.Equal(t, "svn", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("DeleteRepositoryMetadata", func(t *testing.T) {
		r, mockStore := setupRepositoryMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"
		key := "repository_type"

		mockStore.On("Delete", mock.Anything, "repository", repositoryID, key).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/repositories/"+repositoryID+"/metadata/"+key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("BulkCreateRepositoryMetadata", func(t *testing.T) {
		r, mockStore := setupRepositoryMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		requestBody := []map[string]interface{}{
			{"key": "repository_type", "value": "git"},
			{"key": "main_branch", "value": "main"},
		}

		createdMetadata := []Metadata{
			{Key: "repository_type", Value: "git"},
			{Key: "main_branch", Value: "main"},
		}

		mockStore.On("BulkCreate", mock.Anything, "repository", repositoryID, mock.AnythingOfType("[]api.Metadata")).Return(nil)
		mockStore.On("List", mock.Anything, "repository", repositoryID).Return(createdMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/repositories/"+repositoryID+"/metadata/bulk", bytes.NewBuffer(body))
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
}

// TestThreatModelMetadata tests threat model metadata operations
func TestThreatModelMetadata(t *testing.T) {
	t.Run("GetThreatModelMetadata", func(t *testing.T) {
		r, mockStore := setupThreatModelMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		metadata := []Metadata{
			{Key: "methodology", Value: "STRIDE"},
			{Key: "version", Value: "2.1"},
		}

		mockStore.On("List", mock.Anything, "threat_model", threatModelID).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "methodology", response[0]["key"])
		assert.Equal(t, "STRIDE", response[0]["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("CreateThreatModelMetadata", func(t *testing.T) {
		r, mockStore := setupThreatModelMetadataHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"key":   "methodology",
			"value": "STRIDE",
		}

		createdMetadata := &Metadata{Key: "methodology", Value: "STRIDE"}

		mockStore.On("Create", mock.Anything, "threat_model", threatModelID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "threat_model", threatModelID, "methodology").Return(createdMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/metadata", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "methodology", response["key"])
		assert.Equal(t, "STRIDE", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("BulkCreateThreatModelMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatModelMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"

			requestBody := []map[string]interface{}{
				{"key": "methodology", "value": "STRIDE"},
				{"key": "phase", "value": "design"},
			}

			createdMetadata := []Metadata{
				{Key: "methodology", Value: "STRIDE"},
				{Key: "phase", Value: "design"},
			}

			mockStore.On("BulkCreate", mock.Anything, "threat_model", threatModelID, mock.AnythingOfType("[]api.Metadata")).Return(nil)
			mockStore.On("List", mock.Anything, "threat_model", threatModelID).Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/metadata/bulk", bytes.NewBuffer(body))
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

		t.Run("TooManyMetadata", func(t *testing.T) {
			r, _ := setupThreatModelMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"

			// Create 21 metadata entries (over the limit of 20)
			metadata := make([]map[string]interface{}, 21)
			for i := 0; i < 21; i++ {
				metadata[i] = map[string]interface{}{
					"key":   fmt.Sprintf("key%d", i),
					"value": fmt.Sprintf("value%d", i),
				}
			}

			body, _ := json.Marshal(metadata)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("DuplicateKeys", func(t *testing.T) {
			r, _ := setupThreatModelMetadataHandler()

			threatModelID := "00000000-0000-0000-0000-000000000001"

			requestBody := []map[string]interface{}{
				{"key": "methodology", "value": "STRIDE"},
				{"key": "methodology", "value": "PASTA"}, // duplicate key
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})
}

// TestDiagramMetadata tests diagram metadata operations
func TestDiagramMetadata(t *testing.T) {
	t.Run("GetDiagramMetadata", func(t *testing.T) {
		r, mockStore := setupDiagramMetadataHandler()

		diagramID := "00000000-0000-0000-0000-000000000001"

		metadata := []Metadata{
			{Key: "tool", Value: "draw.io"},
			{Key: "layout", Value: "hierarchical"},
		}

		mockStore.On("List", mock.Anything, "diagram", diagramID).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/diagrams/"+diagramID+"/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "tool", response[0]["key"])
		assert.Equal(t, "draw.io", response[0]["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("CreateDiagramMetadata", func(t *testing.T) {
		r, mockStore := setupDiagramMetadataHandler()

		diagramID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"key":   "tool",
			"value": "draw.io",
		}

		createdMetadata := &Metadata{Key: "tool", Value: "draw.io"}

		mockStore.On("Create", mock.Anything, "diagram", diagramID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "diagram", diagramID, "tool").Return(createdMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/diagrams/"+diagramID+"/metadata", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "tool", response["key"])
		assert.Equal(t, "draw.io", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("BulkCreateDiagramMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupDiagramMetadataHandler()

			diagramID := "00000000-0000-0000-0000-000000000001"

			requestBody := []map[string]interface{}{
				{"key": "tool", "value": "draw.io"},
				{"key": "layout", "value": "hierarchical"},
			}

			createdMetadata := []Metadata{
				{Key: "tool", Value: "draw.io"},
				{Key: "layout", Value: "hierarchical"},
			}

			mockStore.On("BulkCreate", mock.Anything, "diagram", diagramID, mock.AnythingOfType("[]api.Metadata")).Return(nil)
			mockStore.On("List", mock.Anything, "diagram", diagramID).Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/diagrams/"+diagramID+"/metadata/bulk", bytes.NewBuffer(body))
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

		t.Run("TooManyMetadata", func(t *testing.T) {
			r, _ := setupDiagramMetadataHandler()

			diagramID := "00000000-0000-0000-0000-000000000001"

			// Create 21 metadata entries (over the limit of 20)
			metadata := make([]map[string]interface{}, 21)
			for i := 0; i < 21; i++ {
				metadata[i] = map[string]interface{}{
					"key":   fmt.Sprintf("key%d", i),
					"value": fmt.Sprintf("value%d", i),
				}
			}

			body, _ := json.Marshal(metadata)
			req := httptest.NewRequest("POST", "/diagrams/"+diagramID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("DuplicateKeys", func(t *testing.T) {
			r, _ := setupDiagramMetadataHandler()

			diagramID := "00000000-0000-0000-0000-000000000001"

			requestBody := []map[string]interface{}{
				{"key": "tool", "value": "draw.io"},
				{"key": "tool", "value": "lucidchart"}, // duplicate key
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/diagrams/"+diagramID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})
}
