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

// MockAssetStore is a mock implementation of AssetStore for testing
type MockAssetStore struct {
	mock.Mock
}

func (m *MockAssetStore) Create(ctx context.Context, asset *Asset, threatModelID string) error {
	args := m.Called(ctx, asset, threatModelID)
	return args.Error(0)
}

func (m *MockAssetStore) Get(ctx context.Context, id string) (*Asset, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Asset), args.Error(1)
}

func (m *MockAssetStore) Update(ctx context.Context, asset *Asset, threatModelID string) error {
	args := m.Called(ctx, asset, threatModelID)
	return args.Error(0)
}

func (m *MockAssetStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockAssetStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Asset, error) {
	args := m.Called(ctx, threatModelID, offset, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]Asset), args.Error(1)
}

func (m *MockAssetStore) Count(ctx context.Context, threatModelID string) (int, error) {
	args := m.Called(ctx, threatModelID)
	return args.Int(0), args.Error(1)
}

func (m *MockAssetStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Asset, error) {
	args := m.Called(ctx, id, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Asset), args.Error(1)
}

func (m *MockAssetStore) BulkCreate(ctx context.Context, assets []Asset, threatModelID string) error {
	args := m.Called(ctx, assets, threatModelID)
	return args.Error(0)
}

func (m *MockAssetStore) InvalidateCache(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockAssetStore) WarmCache(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

// setupAssetSubResourceHandler creates a test router with asset sub-resource handlers
func setupAssetSubResourceHandler() (*gin.Engine, *MockAssetStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockAssetStore := &MockAssetStore{}
	handler := NewAssetSubResourceHandler(mockAssetStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register asset sub-resource routes
	r.GET("/threat_models/:threat_model_id/assets", handler.GetAssets)
	r.POST("/threat_models/:threat_model_id/assets", handler.CreateAsset)
	r.GET("/threat_models/:threat_model_id/assets/:asset_id", handler.GetAsset)
	r.PUT("/threat_models/:threat_model_id/assets/:asset_id", handler.UpdateAsset)
	r.PATCH("/threat_models/:threat_model_id/assets/:asset_id", handler.PatchAsset)
	r.DELETE("/threat_models/:threat_model_id/assets/:asset_id", handler.DeleteAsset)
	r.POST("/threat_models/:threat_model_id/assets/bulk", handler.BulkCreateAssets)

	return r, mockAssetStore
}

// TestGetAssets tests retrieving assets for a threat model
func TestGetAssets(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assets := []Asset{
			{Name: "Database Server"},
			{Name: "Web Application"},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		uuid2, _ := uuid.Parse(testUUID2)
		assets[0].Id = &uuid1
		assets[1].Id = &uuid2

		mockStore.On("List", mock.Anything, threatModelID, 0, 20).Return(assets, nil)
		mockStore.On("Count", mock.Anything, threatModelID).Return(2, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListAssetsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.Assets, 2)
		assert.Equal(t, "Database Server", response.Assets[0].Name)
		assert.Equal(t, "Web Application", response.Assets[1].Name)
		assert.Equal(t, 2, response.Total)
		assert.Equal(t, 20, response.Limit)
		assert.Equal(t, 0, response.Offset)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		req := httptest.NewRequest("GET", "/threat_models/invalid-uuid/assets", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("WithPagination", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assets := []Asset{
			{Name: "Database Server"},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		assets[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, 10, 5).Return(assets, nil)
		mockStore.On("Count", mock.Anything, threatModelID).Return(100, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidLimit", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets?limit=150", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidOffset", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets?offset=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestGetAsset tests retrieving a specific asset
func TestGetAsset(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assetID := testUUID2

		asset := &Asset{Name: "Database Server"}
		uuid1, _ := uuid.Parse(assetID)
		asset.Id = &uuid1

		mockStore.On("Get", mock.Anything, assetID).Return(asset, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets/"+assetID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Database Server", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assetID := testUUID2

		mockStore.On("Get", mock.Anything, assetID).Return(nil, NotFoundError("Asset not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets/"+assetID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidAssetID", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/assets/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCreateAsset tests creating a new asset
func TestCreateAsset(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]interface{}{
			"name":        "New Database Server",
			"description": "Primary database server",
			"type":        "hardware",
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Asset"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "New Database Server", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidRequestBody", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		// Missing required name and type fields
		requestBody := map[string]interface{}{
			"description": "An asset without required fields",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		requestBody := map[string]interface{}{
			"name": "Test Asset",
			"type": "hardware",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/assets", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateAsset tests updating an existing asset
func TestUpdateAsset(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assetID := testUUID2

		requestBody := map[string]interface{}{
			"name":        "Updated Database Server",
			"description": "Updated description",
			"type":        "hardware",
		}

		mockStore.On("Update", mock.Anything, mock.AnythingOfType("*api.Asset"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/assets/"+assetID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidAssetID", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]interface{}{
			"name": "Test Asset",
			"type": "hardware",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/assets/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPatchAsset tests applying JSON patch operations to an asset
func TestPatchAsset(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assetID := testUUID2

		patchOps := []map[string]interface{}{
			{"op": "replace", "path": "/name", "value": "Patched Name"},
		}

		updatedAsset := &Asset{Name: "Patched Name"}
		uuid1, _ := uuid.Parse(assetID)
		updatedAsset.Id = &uuid1

		mockStore.On("Patch", mock.Anything, assetID, mock.AnythingOfType("[]api.PatchOperation")).Return(updatedAsset, nil)

		body, _ := json.Marshal(patchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/assets/"+assetID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json-patch+json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Patched Name", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidAssetID", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		patchOps := []map[string]interface{}{
			{"op": "replace", "path": "/name", "value": "Patched Name"},
		}

		body, _ := json.Marshal(patchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/assets/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json-patch+json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyPatchOperations", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assetID := testUUID2

		patchOps := []map[string]interface{}{}

		body, _ := json.Marshal(patchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/assets/"+assetID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json-patch+json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestDeleteAsset tests deleting an asset
func TestDeleteAsset(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1
		assetID := testUUID2

		mockStore.On("Delete", mock.Anything, assetID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/assets/"+assetID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidAssetID", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/assets/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestBulkCreateAssets tests creating multiple assets
func TestBulkCreateAssets(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		requestBody := []map[string]interface{}{
			{"name": "Database Server", "type": "hardware"},
			{"name": "Web Application", "type": "service"},
		}

		mockStore.On("BulkCreate", mock.Anything, mock.AnythingOfType("[]api.Asset"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/bulk", bytes.NewBuffer(body))
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

	t.Run("EmptyList", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		requestBody := []map[string]interface{}{}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("TooManyAssets", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		// Create 51 assets (over the limit of 50)
		assets := make([]map[string]interface{}, 51)
		for i := 0; i < 51; i++ {
			assets[i] = map[string]interface{}{"name": "Asset " + string(rune('A'+i)), "type": "hardware"}
		}

		body, _ := json.Marshal(assets)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingRequiredField", func(t *testing.T) {
		r, _ := setupAssetSubResourceHandler()

		threatModelID := testUUID1

		requestBody := []map[string]interface{}{
			{"name": "Database Server", "type": "hardware"},
			{"description": "Missing required fields"}, // Missing required name and type fields
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/assets/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
