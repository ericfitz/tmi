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
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Test metadata key constants
const (
	testMetaKeyPriority    = "priority"
	testMetaKeyAuthor      = "author"
	testMetaKeyCriticality = "criticality"
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

// setupGenericMetadataHandler creates a test router with a GenericMetadataHandler for the given entity type.
func setupGenericMetadataHandler(entityType, paramName, routePrefix string, verifyParent ParentVerifier) (*gin.Engine, *MockMetadataStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockMetadataStore := &MockMetadataStore{}
	handler := NewGenericMetadataHandler(mockMetadataStore, entityType, paramName, verifyParent)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register metadata routes
	r.GET(routePrefix+"/metadata", handler.List)
	r.GET(routePrefix+"/metadata/:key", handler.GetByKey)
	r.POST(routePrefix+"/metadata", handler.Create)
	r.PUT(routePrefix+"/metadata/:key", handler.Update)
	r.DELETE(routePrefix+"/metadata/:key", handler.Delete)
	r.POST(routePrefix+"/metadata/bulk", handler.BulkCreate)
	r.PUT(routePrefix+"/metadata/bulk", handler.BulkUpsert)

	return r, mockMetadataStore
}

// setupThreatMetadataHandler creates a test router with threat metadata handlers
func setupThreatMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	return setupGenericMetadataHandler("threat", "threat_id",
		"/threat_models/:threat_model_id/threats/:threat_id", nil)
}

// setupDocumentMetadataHandler creates a test router with document metadata handlers
func setupDocumentMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	return setupGenericMetadataHandler("document", "document_id",
		"/threat_models/:threat_model_id/documents/:document_id", nil)
}

// TestThreatMetadata tests threat metadata operations
func TestThreatMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Run("GetThreatMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := testUUID1
			threatID := testUUID2

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

			threatModelID := testUUID1

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/threats/invalid-uuid/metadata", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("GetThreatMetadataByKey", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := testUUID1
			threatID := testUUID2
			key := testMetaKeyPriority

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

			threatModelID := testUUID1
			threatID := testUUID2
			key := testKeyNonexistent

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

			threatModelID := testUUID1
			threatID := testUUID2

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

			threatModelID := testUUID1
			threatID := testUUID2

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

			threatModelID := testUUID1
			threatID := testUUID2

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

			threatModelID := testUUID1
			threatID := testUUID2
			key := testMetaKeyPriority

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

			threatModelID := testUUID1
			threatID := testUUID2
			key := testMetaKeyPriority

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

			threatModelID := testUUID1
			threatID := testUUID2
			key := testMetaKeyPriority

			mockStore.On("Delete", mock.Anything, "threat", threatID, key).Return(nil)

			req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNoContent, w.Code)

			mockStore.AssertExpectations(t)
		})

		t.Run("NotFound", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := testUUID1
			threatID := testUUID2
			key := testKeyNonexistent

			mockStore.On("Delete", mock.Anything, "threat", threatID, key).Return(NotFoundError("Metadata not found"))

			req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/threats/"+threatID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// Handler returns NotFoundError (404) for "not found" errors
			assert.Equal(t, http.StatusNotFound, w.Code)

			mockStore.AssertExpectations(t)
		})
	})

	t.Run("BulkCreateThreatMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupThreatMetadataHandler()

			threatModelID := testUUID1
			threatID := testUUID2

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

			threatModelID := testUUID1
			threatID := testUUID2

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

			threatModelID := testUUID1
			threatID := testUUID2

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

		threatModelID := testUUID1
		documentID := testUUID2

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

		threatModelID := testUUID1
		documentID := testUUID2

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

		threatModelID := testUUID1
		documentID := testUUID2
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

		threatModelID := testUUID1
		documentID := testUUID2
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

			threatModelID := testUUID1
			documentID := testUUID2

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

			threatModelID := testUUID1
			documentID := testUUID2

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

			threatModelID := testUUID1
			documentID := testUUID2

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
	return setupGenericMetadataHandler("repository", "repository_id",
		"/threat_models/:threat_model_id/repositories/:repository_id", nil)
}

// setupThreatModelMetadataHandler creates a test router with threat model metadata handlers
func setupThreatModelMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	// Set up global ThreatModelStore with test threat model for parent existence checks.
	// Reset Initialized flag so other tests re-initialize properly after us.
	TestFixtures.Initialized = false
	ThreatModelStore = &MockThreatModelStore{data: map[string]ThreatModel{
		testUUID1: {Name: "Test TM"},
	}}
	DiagramStore = &MockDiagramStore{
		data:               make(map[string]DfdDiagram),
		threatModelMapping: make(map[string]string),
	}

	return setupGenericMetadataHandler("threat_model", "threat_model_id",
		"/threat_models/:threat_model_id",
		func(ctx context.Context, id uuid.UUID) error {
			_, err := ThreatModelStore.Get(id.String())
			return err
		})
}

// setupDiagramMetadataHandler creates a test router with diagram metadata handlers
func setupDiagramMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	return setupGenericMetadataHandler("diagram", "diagram_id",
		"/diagrams/:diagram_id", nil)
}

// TestRepositoryMetadata tests repository metadata operations
func TestRepositoryMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Run("GetRepositoryMetadata", func(t *testing.T) {
		r, mockStore := setupRepositoryMetadataHandler()

		threatModelID := testUUID1
		repositoryID := testUUID2

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

		threatModelID := testUUID1
		repositoryID := testUUID2

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

		threatModelID := testUUID1
		repositoryID := testUUID2
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

		threatModelID := testUUID1
		repositoryID := testUUID2
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

		threatModelID := testUUID1
		repositoryID := testUUID2

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

		threatModelID := testUUID1

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

		threatModelID := testUUID1

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

			threatModelID := testUUID1

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

			threatModelID := testUUID1

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

			threatModelID := testUUID1

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

		diagramID := testUUID1

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

		diagramID := testUUID1

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

			diagramID := testUUID1

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

			diagramID := testUUID1

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

			diagramID := testUUID1

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

// setupNoteMetadataHandler creates a test router with note metadata handlers
func setupNoteMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	return setupGenericMetadataHandler("note", "note_id",
		"/threat_models/:threat_model_id/notes/:note_id", nil)
}

// setupAssetMetadataHandler creates a test router with asset metadata handlers
func setupAssetMetadataHandler() (*gin.Engine, *MockMetadataStore) {
	return setupGenericMetadataHandler("asset", "asset_id",
		"/threat_models/:threat_model_id/assets/:asset_id", nil)
}

// TestNoteMetadata tests note metadata operations
func TestNoteMetadata(t *testing.T) {
	t.Run("GetNoteMetadata", func(t *testing.T) {
		r, mockStore := setupNoteMetadataHandler()

		threatModelID := testUUID1
		noteID := testUUID2

		metadata := []Metadata{
			{Key: "author", Value: "alice"},
			{Key: "status", Value: "draft"},
		}

		mockStore.On("List", mock.Anything, "note", noteID).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "author", response[0]["key"])
		assert.Equal(t, "alice", response[0]["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("GetNoteMetadataByKey", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2
			key := testMetaKeyAuthor

			metadata := &Metadata{Key: "author", Value: "alice"}

			mockStore.On("Get", mock.Anything, "note", noteID, key).Return(metadata, nil)

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "author", response["key"])
			assert.Equal(t, "alice", response["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("NotFound", func(t *testing.T) {
			r, mockStore := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2
			key := testKeyNonexistent

			mockStore.On("Get", mock.Anything, "note", noteID, key).Return(nil, NotFoundError("Metadata not found"))

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
			mockStore.AssertExpectations(t)
		})
	})

	t.Run("CreateNoteMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2

			requestBody := map[string]interface{}{
				"key":   "author",
				"value": "alice",
			}

			createdMetadata := &Metadata{Key: "author", Value: "alice"}

			mockStore.On("Create", mock.Anything, "note", noteID, mock.AnythingOfType("*api.Metadata")).Return(nil)
			mockStore.On("Get", mock.Anything, "note", noteID, "author").Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "author", response["key"])
			assert.Equal(t, "alice", response["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("InvalidNoteID", func(t *testing.T) {
			r, _ := setupNoteMetadataHandler()

			threatModelID := testUUID1

			requestBody := map[string]interface{}{
				"key":   "author",
				"value": "alice",
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/notes/invalid-uuid/metadata", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("UpdateNoteMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2
			key := testMetaKeyAuthor

			requestBody := map[string]interface{}{
				"key":   key,
				"value": "bob",
			}

			updatedMetadata := &Metadata{Key: "author", Value: "bob"}

			mockStore.On("Update", mock.Anything, "note", noteID, mock.AnythingOfType("*api.Metadata")).Return(nil)
			mockStore.On("Get", mock.Anything, "note", noteID, key).Return(updatedMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata/"+key, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "author", response["key"])
			assert.Equal(t, "bob", response["value"])

			mockStore.AssertExpectations(t)
		})
	})

	t.Run("DeleteNoteMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2
			key := testMetaKeyAuthor

			mockStore.On("Delete", mock.Anything, "note", noteID, key).Return(nil)

			req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNoContent, w.Code)

			mockStore.AssertExpectations(t)
		})
	})

	t.Run("BulkCreateNoteMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2

			requestBody := []map[string]interface{}{
				{"key": "author", "value": "alice"},
				{"key": "status", "value": "draft"},
			}

			createdMetadata := []Metadata{
				{Key: "author", Value: "alice"},
				{Key: "status", Value: "draft"},
			}

			mockStore.On("BulkCreate", mock.Anything, "note", noteID, mock.AnythingOfType("[]api.Metadata")).Return(nil)
			mockStore.On("List", mock.Anything, "note", noteID).Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata/bulk", bytes.NewBuffer(body))
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
			r, _ := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2

			// Create 21 metadata entries (over the limit of 20)
			metadata := make([]map[string]interface{}, 21)
			for i := 0; i < 21; i++ {
				metadata[i] = map[string]interface{}{
					"key":   fmt.Sprintf("key%d", i),
					"value": fmt.Sprintf("value%d", i),
				}
			}

			body, _ := json.Marshal(metadata)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("DuplicateKeys", func(t *testing.T) {
			r, _ := setupNoteMetadataHandler()

			threatModelID := testUUID1
			noteID := testUUID2

			requestBody := []map[string]interface{}{
				{"key": "author", "value": "alice"},
				{"key": "author", "value": "bob"}, // duplicate key
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/notes/"+noteID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})
}

// TestAssetMetadata tests asset metadata operations
func TestAssetMetadata(t *testing.T) {
	t.Run("GetAssetMetadata", func(t *testing.T) {
		r, mockStore := setupAssetMetadataHandler()

		threatModelID := testUUID1
		assetID := testUUID2

		metadata := []Metadata{
			{Key: "criticality", Value: "high"},
			{Key: "owner", Value: "security-team"},
		}

		mockStore.On("List", mock.Anything, "asset", assetID).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "criticality", response[0]["key"])
		assert.Equal(t, "high", response[0]["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("GetAssetMetadataByKey", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2
			key := testMetaKeyCriticality

			metadata := &Metadata{Key: "criticality", Value: "high"}

			mockStore.On("Get", mock.Anything, "asset", assetID, key).Return(metadata, nil)

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "criticality", response["key"])
			assert.Equal(t, "high", response["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("NotFound", func(t *testing.T) {
			r, mockStore := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2
			key := testKeyNonexistent

			mockStore.On("Get", mock.Anything, "asset", assetID, key).Return(nil, NotFoundError("Metadata not found"))

			req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
			mockStore.AssertExpectations(t)
		})
	})

	t.Run("CreateAssetMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2

			requestBody := map[string]interface{}{
				"key":   "criticality",
				"value": "high",
			}

			createdMetadata := &Metadata{Key: "criticality", Value: "high"}

			mockStore.On("Create", mock.Anything, "asset", assetID, mock.AnythingOfType("*api.Metadata")).Return(nil)
			mockStore.On("Get", mock.Anything, "asset", assetID, "criticality").Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "criticality", response["key"])
			assert.Equal(t, "high", response["value"])

			mockStore.AssertExpectations(t)
		})

		t.Run("InvalidAssetID", func(t *testing.T) {
			r, _ := setupAssetMetadataHandler()

			threatModelID := testUUID1

			requestBody := map[string]interface{}{
				"key":   "criticality",
				"value": "high",
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/invalid-uuid/metadata", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("UpdateAssetMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2
			key := testMetaKeyCriticality

			requestBody := map[string]interface{}{
				"key":   key,
				"value": "critical",
			}

			updatedMetadata := &Metadata{Key: "criticality", Value: "critical"}

			mockStore.On("Update", mock.Anything, "asset", assetID, mock.AnythingOfType("*api.Metadata")).Return(nil)
			mockStore.On("Get", mock.Anything, "asset", assetID, key).Return(updatedMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata/"+key, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "criticality", response["key"])
			assert.Equal(t, "critical", response["value"])

			mockStore.AssertExpectations(t)
		})
	})

	t.Run("DeleteAssetMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2
			key := testMetaKeyCriticality

			mockStore.On("Delete", mock.Anything, "asset", assetID, key).Return(nil)

			req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata/"+key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNoContent, w.Code)

			mockStore.AssertExpectations(t)
		})
	})

	t.Run("BulkCreateAssetMetadata", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			r, mockStore := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2

			requestBody := []map[string]interface{}{
				{"key": "criticality", "value": "high"},
				{"key": "owner", "value": "security-team"},
			}

			createdMetadata := []Metadata{
				{Key: "criticality", Value: "high"},
				{Key: "owner", Value: "security-team"},
			}

			mockStore.On("BulkCreate", mock.Anything, "asset", assetID, mock.AnythingOfType("[]api.Metadata")).Return(nil)
			mockStore.On("List", mock.Anything, "asset", assetID).Return(createdMetadata, nil)

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata/bulk", bytes.NewBuffer(body))
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
			r, _ := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2

			// Create 21 metadata entries (over the limit of 20)
			metadata := make([]map[string]interface{}, 21)
			for i := 0; i < 21; i++ {
				metadata[i] = map[string]interface{}{
					"key":   fmt.Sprintf("key%d", i),
					"value": fmt.Sprintf("value%d", i),
				}
			}

			body, _ := json.Marshal(metadata)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("DuplicateKeys", func(t *testing.T) {
			r, _ := setupAssetMetadataHandler()

			threatModelID := testUUID1
			assetID := testUUID2

			requestBody := []map[string]interface{}{
				{"key": "criticality", "value": "high"},
				{"key": "criticality", "value": "low"}, // duplicate key
			}

			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/"+assetID+"/metadata/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})
}
