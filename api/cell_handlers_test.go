package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Test constants for cell handler tests
const (
	testMetadataKeyType = "type"
	testKeyNonexistent  = "nonexistent"
)

// MockCellMetadataStore is a mock implementation of MetadataStore for cell testing
type MockCellMetadataStore struct {
	mock.Mock
}

func (m *MockCellMetadataStore) Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockCellMetadataStore) Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error) {
	args := m.Called(ctx, entityType, entityID, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Metadata), args.Error(1)
}

func (m *MockCellMetadataStore) Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockCellMetadataStore) Delete(ctx context.Context, entityType, entityID, key string) error {
	args := m.Called(ctx, entityType, entityID, key)
	return args.Error(0)
}

func (m *MockCellMetadataStore) List(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	args := m.Called(ctx, entityType, entityID)
	return args.Get(0).([]Metadata), args.Error(1)
}

func (m *MockCellMetadataStore) Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockCellMetadataStore) BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockCellMetadataStore) BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	args := m.Called(ctx, entityType, entityID, metadata)
	return args.Error(0)
}

func (m *MockCellMetadataStore) BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error {
	args := m.Called(ctx, entityType, entityID, keys)
	return args.Error(0)
}

func (m *MockCellMetadataStore) GetByKey(ctx context.Context, key string) ([]Metadata, error) {
	args := m.Called(ctx, key)
	return args.Get(0).([]Metadata), args.Error(1)
}

func (m *MockCellMetadataStore) ListKeys(ctx context.Context, entityType, entityID string) ([]string, error) {
	args := m.Called(ctx, entityType, entityID)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockCellMetadataStore) InvalidateCache(ctx context.Context, entityType, entityID string) error {
	args := m.Called(ctx, entityType, entityID)
	return args.Error(0)
}

func (m *MockCellMetadataStore) WarmCache(ctx context.Context, entityType, entityID string) error {
	args := m.Called(ctx, entityType, entityID)
	return args.Error(0)
}

// setupCellHandler creates a test router with cell handlers
func setupCellHandler() (*gin.Engine, *MockCellMetadataStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockMetadataStore := &MockCellMetadataStore{}
	handler := NewCellHandler(mockMetadataStore, nil, nil, nil)

	// Note: Cell handler routes are not defined in OpenAPI spec, so no OpenAPI validation middleware

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register cell handler routes
	r.GET("/diagrams/:diagram_id/cells/:cell_id/metadata", handler.GetCellMetadata)
	r.GET("/diagrams/:diagram_id/cells/:cell_id/metadata/:key", handler.GetCellMetadataByKey)
	r.POST("/diagrams/:diagram_id/cells/:cell_id/metadata", handler.CreateCellMetadata)
	r.PUT("/diagrams/:diagram_id/cells/:cell_id/metadata/:key", handler.UpdateCellMetadata)
	r.DELETE("/diagrams/:diagram_id/cells/:cell_id/metadata/:key", handler.DeleteCellMetadata)
	r.PATCH("/diagrams/:diagram_id/cells/:cell_id", handler.PatchCell)

	return r, mockMetadataStore
}

// TestGetCellMetadata tests retrieving all metadata for a cell
func TestGetCellMetadata(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2

		metadata := []Metadata{
			{Key: "type", Value: "process"},
			{Key: "position", Value: "100,200"},
		}

		mockStore.On("List", mock.Anything, "cell", cellID).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "type", response[0]["key"])
		assert.Equal(t, "process", response[0]["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidCellID", func(t *testing.T) {
		r, _ := setupCellHandler()

		diagramID := testUUID1

		req := httptest.NewRequest("GET", "/diagrams/"+diagramID+"/cells/invalid-uuid/metadata", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 400 for invalid UUID format
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestGetCellMetadataByKey tests retrieving a specific metadata entry by key
func TestGetCellMetadataByKey(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2
		key := testMetadataKeyType

		metadata := &Metadata{Key: "type", Value: "process"}

		mockStore.On("Get", mock.Anything, "cell", cellID, key).Return(metadata, nil)

		req := httptest.NewRequest("GET", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata/"+key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "type", response["key"])
		assert.Equal(t, "process", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2
		key := testKeyNonexistent

		mockStore.On("Get", mock.Anything, "cell", cellID, key).Return(nil, NotFoundError("Metadata not found"))

		req := httptest.NewRequest("GET", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata/"+key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestCreateCellMetadata tests creating a new metadata entry for a cell
func TestCreateCellMetadata(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2

		requestBody := map[string]interface{}{
			"key":   "type",
			"value": "process",
		}

		createdMetadata := &Metadata{Key: "type", Value: "process"}

		mockStore.On("Create", mock.Anything, "cell", cellID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "cell", cellID, "type").Return(createdMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "type", response["key"])
		assert.Equal(t, "process", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingKey", func(t *testing.T) {
		r, _ := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2

		requestBody := map[string]interface{}{
			"value": "process",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// With binding:"required" tags, missing key should return 400 Bad Request
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingValue", func(t *testing.T) {
		r, _ := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2

		requestBody := map[string]interface{}{
			"key": "type",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// With binding:"required" tags, missing value should return 400 Bad Request
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateCellMetadata tests updating an existing metadata entry
func TestUpdateCellMetadata(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2
		key := testMetadataKeyType

		requestBody := map[string]interface{}{
			"value": "datastore",
		}

		updatedMetadata := &Metadata{Key: "type", Value: "datastore"}

		mockStore.On("Update", mock.Anything, "cell", cellID, mock.AnythingOfType("*api.Metadata")).Return(nil)
		mockStore.On("Get", mock.Anything, "cell", cellID, key).Return(updatedMetadata, nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata/"+key, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "type", response["key"])
		assert.Equal(t, "datastore", response["value"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingValue", func(t *testing.T) {
		r, _ := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2
		key := testMetadataKeyType

		requestBody := map[string]interface{}{}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata/"+key, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// With binding:"required" tags, missing value should return 400 Bad Request
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestDeleteCellMetadata tests deleting a metadata entry
func TestDeleteCellMetadata(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2
		key := testMetadataKeyType

		mockStore.On("Delete", mock.Anything, "cell", cellID, key).Return(nil)

		req := httptest.NewRequest("DELETE", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata/"+key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2
		key := testKeyNonexistent

		mockStore.On("Delete", mock.Anything, "cell", cellID, key).Return(NotFoundError("Metadata not found"))

		req := httptest.NewRequest("DELETE", "/diagrams/"+diagramID+"/cells/"+cellID+"/metadata/"+key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler returns NotFoundError (404) for "not found" errors
		assert.Equal(t, http.StatusNotFound, w.Code)

		mockStore.AssertExpectations(t)
	})
}

// TestPatchCell tests applying JSON patch operations to a cell
func TestPatchCell(t *testing.T) {
	t.Run("Success - Returns WebSocket Message", func(t *testing.T) {
		r, _ := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2

		// Since PatchCell redirects to WebSocket, we just need to test the response structure
		patchOperations := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/position/x",
				"value": 150,
			},
		}

		body, _ := json.Marshal(patchOperations)
		req := httptest.NewRequest("PATCH", "/diagrams/"+diagramID+"/cells/"+cellID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, cellID, response["cell_id"])
		assert.Equal(t, diagramID, response["diagram_id"])
		assert.Equal(t, float64(1), response["operations_count"])
		assert.Contains(t, response["message"], "WebSocket")
		assert.Contains(t, response["websocket_url"], "/ws/diagrams/")
	})

	t.Run("InvalidCellID", func(t *testing.T) {
		r, _ := setupCellHandler()

		diagramID := testUUID1

		patchOperations := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/position/x",
				"value": 150,
			},
		}

		body, _ := json.Marshal(patchOperations)
		req := httptest.NewRequest("PATCH", "/diagrams/"+diagramID+"/cells/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 400 for invalid UUID format
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyOperations", func(t *testing.T) {
		r, _ := setupCellHandler()

		diagramID := testUUID1
		cellID := testUUID2

		patchOperations := []map[string]interface{}{}

		body, _ := json.Marshal(patchOperations)
		req := httptest.NewRequest("PATCH", "/diagrams/"+diagramID+"/cells/"+cellID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 400 for empty operations array
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
