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

func (m *MockThreatStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Threat, error) {
	args := m.Called(ctx, threatModelID, offset, limit)
	return args.Get(0).([]Threat), args.Error(1)
}

func (m *MockThreatStore) Update(ctx context.Context, threat *Threat) error {
	args := m.Called(ctx, threat)
	return args.Error(0)
}

func (m *MockThreatStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error) {
	args := m.Called(ctx, id, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Threat), args.Error(1)
}

func (m *MockThreatStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
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

// setupBatchHandler creates a test router with batch handlers
func setupBatchHandler() (*gin.Engine, *MockThreatStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockThreatStore := &MockThreatStore{}
	batchHandler := NewBatchHandler(mockThreatStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register batch routes - using :id to match actual route definitions
	r.POST("/threat_models/:threat_model_id/threats/batch/patch", batchHandler.BatchPatchThreats)
	r.DELETE("/threat_models/:threat_model_id/threats/batch", batchHandler.BatchDeleteThreats)

	return r, mockThreatStore
}

// TestBatchPatchThreats tests the batch patch threats endpoint
func TestBatchPatchThreats(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupBatchHandler()

		threatID1 := "00000000-0000-0000-0000-000000000001"
		threatID2 := "00000000-0000-0000-0000-000000000002"

		// Mock successful patch operations
		uuid1, _ := uuid.Parse(threatID1)
		uuid2, _ := uuid.Parse(threatID2)

		threat1 := &Threat{Name: "Updated Threat 1"}
		threat1.Id = &uuid1
		threat2 := &Threat{Name: "Updated Threat 2"}
		threat2.Id = &uuid2

		mockStore.On("Patch", mock.Anything, threatID1, mock.AnythingOfType("[]api.PatchOperation")).Return(threat1, nil)
		mockStore.On("Patch", mock.Anything, threatID2, mock.AnythingOfType("[]api.PatchOperation")).Return(threat2, nil)

		// Create request body
		reqBody := BatchThreatPatchRequest{
			Operations: []struct {
				ThreatID   string           `json:"threat_id" binding:"required"`
				Operations []PatchOperation `json:"operations" binding:"required"`
			}{
				{
					ThreatID: threatID1,
					Operations: []PatchOperation{
						{Op: "replace", Path: "/name", Value: "Updated Threat 1"},
					},
				},
				{
					ThreatID: threatID2,
					Operations: []PatchOperation{
						{Op: "replace", Path: "/name", Value: "Updated Threat 2"},
					},
				},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch/patch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Assert response
		assert.Equal(t, http.StatusOK, w.Code)

		var response BatchThreatPatchResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify response structure
		assert.Equal(t, 2, response.Summary.Total)
		assert.Equal(t, 2, response.Summary.Succeeded)
		assert.Equal(t, 0, response.Summary.Failed)
		assert.Len(t, response.Results, 2)

		// Verify individual results
		assert.Equal(t, threatID1, response.Results[0].ThreatID)
		assert.True(t, response.Results[0].Success)
		assert.NotNil(t, response.Results[0].Threat)
		assert.Equal(t, "Updated Threat 1", response.Results[0].Threat.Name)

		assert.Equal(t, threatID2, response.Results[1].ThreatID)
		assert.True(t, response.Results[1].Success)
		assert.NotNil(t, response.Results[1].Threat)
		assert.Equal(t, "Updated Threat 2", response.Results[1].Threat.Name)

		mockStore.AssertExpectations(t)
	})

	t.Run("PartialFailure", func(t *testing.T) {
		r, mockStore := setupBatchHandler()

		threatID1 := "00000000-0000-0000-0000-000000000001"
		threatID2 := "00000000-0000-0000-0000-000000000002"

		// Mock one success and one failure
		uuid1, _ := uuid.Parse(threatID1)

		threat1 := &Threat{Name: "Updated Threat 1"}
		threat1.Id = &uuid1

		mockStore.On("Patch", mock.Anything, threatID1, mock.AnythingOfType("[]api.PatchOperation")).Return(threat1, nil)
		mockStore.On("Patch", mock.Anything, threatID2, mock.AnythingOfType("[]api.PatchOperation")).Return(nil, NotFoundError("Threat not found"))

		reqBody := BatchThreatPatchRequest{
			Operations: []struct {
				ThreatID   string           `json:"threat_id" binding:"required"`
				Operations []PatchOperation `json:"operations" binding:"required"`
			}{
				{
					ThreatID: threatID1,
					Operations: []PatchOperation{
						{Op: "replace", Path: "/name", Value: "Updated Threat 1"},
					},
				},
				{
					ThreatID: threatID2,
					Operations: []PatchOperation{
						{Op: "replace", Path: "/name", Value: "Updated Threat 2"},
					},
				},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch/patch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Assert response - should be 207 Multi-Status for partial success
		assert.Equal(t, http.StatusMultiStatus, w.Code)

		var response BatchThreatPatchResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify response structure
		assert.Equal(t, 2, response.Summary.Total)
		assert.Equal(t, 1, response.Summary.Succeeded)
		assert.Equal(t, 1, response.Summary.Failed)

		// Verify results
		assert.True(t, response.Results[0].Success)
		assert.False(t, response.Results[1].Success)
		assert.Contains(t, response.Results[1].Error, "Threat not found")

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupBatchHandler()

		reqBody := BatchThreatPatchRequest{
			Operations: []struct {
				ThreatID   string           `json:"threat_id" binding:"required"`
				Operations []PatchOperation `json:"operations" binding:"required"`
			}{
				{
					ThreatID: "00000000-0000-0000-0000-000000000001",
					Operations: []PatchOperation{
						{Op: "replace", Path: "/name", Value: "Updated Threat"},
					},
				},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/threats/batch/patch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyOperations", func(t *testing.T) {
		r, _ := setupBatchHandler()

		reqBody := BatchThreatPatchRequest{
			Operations: []struct {
				ThreatID   string           `json:"threat_id" binding:"required"`
				Operations []PatchOperation `json:"operations" binding:"required"`
			}{},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch/patch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("TooManyOperations", func(t *testing.T) {
		r, _ := setupBatchHandler()

		// Create 21 operations (exceeds limit of 20)
		operations := make([]struct {
			ThreatID   string           `json:"threat_id" binding:"required"`
			Operations []PatchOperation `json:"operations" binding:"required"`
		}, 21)

		for i := 0; i < 21; i++ {
			operations[i] = struct {
				ThreatID   string           `json:"threat_id" binding:"required"`
				Operations []PatchOperation `json:"operations" binding:"required"`
			}{
				ThreatID: "00000000-0000-0000-0000-000000000001",
				Operations: []PatchOperation{
					{Op: "replace", Path: "/name", Value: "Test"},
				},
			}
		}

		reqBody := BatchThreatPatchRequest{Operations: operations}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch/patch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupBatchHandler()

		reqBody := BatchThreatPatchRequest{
			Operations: []struct {
				ThreatID   string           `json:"threat_id" binding:"required"`
				Operations []PatchOperation `json:"operations" binding:"required"`
			}{
				{
					ThreatID: "invalid-uuid",
					Operations: []PatchOperation{
						{Op: "replace", Path: "/name", Value: "Updated Threat"},
					},
				},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch/patch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response BatchThreatPatchResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 1, response.Summary.Total)
		assert.Equal(t, 0, response.Summary.Succeeded)
		assert.Equal(t, 1, response.Summary.Failed)
		assert.Contains(t, response.Results[0].Error, "Invalid threat ID format")
	})
}

// TestBatchDeleteThreats tests the batch delete threats endpoint
func TestBatchDeleteThreats(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupBatchHandler()

		threatID1 := "00000000-0000-0000-0000-000000000001"
		threatID2 := "00000000-0000-0000-0000-000000000002"

		// Mock successful delete operations
		mockStore.On("Delete", mock.Anything, threatID1).Return(nil)
		mockStore.On("Delete", mock.Anything, threatID2).Return(nil)

		// Create request body
		type BatchDeleteRequest struct {
			ThreatIDs []string `json:"threat_ids" binding:"required"`
		}

		reqBody := BatchDeleteRequest{
			ThreatIDs: []string{threatID1, threatID2},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("DELETE", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Assert response
		assert.Equal(t, http.StatusOK, w.Code)

		type BatchDeleteResponse struct {
			Results []struct {
				ThreatID string `json:"threat_id"`
				Success  bool   `json:"success"`
				Error    string `json:"error,omitempty"`
			} `json:"results"`
			Summary struct {
				Total     int `json:"total"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"summary"`
		}

		var response BatchDeleteResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify response structure
		assert.Equal(t, 2, response.Summary.Total)
		assert.Equal(t, 2, response.Summary.Succeeded)
		assert.Equal(t, 0, response.Summary.Failed)
		assert.Len(t, response.Results, 2)

		// Verify individual results
		assert.Equal(t, threatID1, response.Results[0].ThreatID)
		assert.True(t, response.Results[0].Success)
		assert.Equal(t, threatID2, response.Results[1].ThreatID)
		assert.True(t, response.Results[1].Success)

		mockStore.AssertExpectations(t)
	})

	t.Run("PartialFailure", func(t *testing.T) {
		r, mockStore := setupBatchHandler()

		threatID1 := "00000000-0000-0000-0000-000000000001"
		threatID2 := "00000000-0000-0000-0000-000000000002"

		// Mock one success and one failure
		mockStore.On("Delete", mock.Anything, threatID1).Return(nil)
		mockStore.On("Delete", mock.Anything, threatID2).Return(NotFoundError("Threat not found"))

		type BatchDeleteRequest struct {
			ThreatIDs []string `json:"threat_ids" binding:"required"`
		}

		reqBody := BatchDeleteRequest{
			ThreatIDs: []string{threatID1, threatID2},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("DELETE", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Assert response - should be 207 Multi-Status for partial success
		assert.Equal(t, http.StatusMultiStatus, w.Code)

		type BatchDeleteResponse struct {
			Results []struct {
				ThreatID string `json:"threat_id"`
				Success  bool   `json:"success"`
				Error    string `json:"error,omitempty"`
			} `json:"results"`
			Summary struct {
				Total     int `json:"total"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"summary"`
		}

		var response BatchDeleteResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify response structure
		assert.Equal(t, 2, response.Summary.Total)
		assert.Equal(t, 1, response.Summary.Succeeded)
		assert.Equal(t, 1, response.Summary.Failed)

		// Verify results
		assert.True(t, response.Results[0].Success)
		assert.False(t, response.Results[1].Success)
		assert.Contains(t, response.Results[1].Error, "Threat not found")

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupBatchHandler()

		type BatchDeleteRequest struct {
			ThreatIDs []string `json:"threat_ids" binding:"required"`
		}

		reqBody := BatchDeleteRequest{
			ThreatIDs: []string{"00000000-0000-0000-0000-000000000001"},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("DELETE", "/threat_models/invalid-uuid/threats/batch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyThreatIDs", func(t *testing.T) {
		r, _ := setupBatchHandler()

		type BatchDeleteRequest struct {
			ThreatIDs []string `json:"threat_ids" binding:"required"`
		}

		reqBody := BatchDeleteRequest{
			ThreatIDs: []string{},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("DELETE", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("TooManyThreatIDs", func(t *testing.T) {
		r, _ := setupBatchHandler()

		// Create 51 threat IDs (exceeds limit of 50)
		threatIDs := make([]string, 51)
		for i := 0; i < 51; i++ {
			threatIDs[i] = "00000000-0000-0000-0000-000000000001"
		}

		type BatchDeleteRequest struct {
			ThreatIDs []string `json:"threat_ids" binding:"required"`
		}

		reqBody := BatchDeleteRequest{ThreatIDs: threatIDs}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("DELETE", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatID", func(t *testing.T) {
		r, _ := setupBatchHandler()

		type BatchDeleteRequest struct {
			ThreatIDs []string `json:"threat_ids" binding:"required"`
		}

		reqBody := BatchDeleteRequest{
			ThreatIDs: []string{"invalid-uuid"},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("DELETE", "/threat_models/00000000-0000-0000-0000-000000000000/threats/batch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		type BatchDeleteResponse struct {
			Results []struct {
				ThreatID string `json:"threat_id"`
				Success  bool   `json:"success"`
				Error    string `json:"error,omitempty"`
			} `json:"results"`
			Summary struct {
				Total     int `json:"total"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"summary"`
		}

		var response BatchDeleteResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 1, response.Summary.Total)
		assert.Equal(t, 0, response.Summary.Succeeded)
		assert.Equal(t, 1, response.Summary.Failed)
		assert.Contains(t, response.Results[0].Error, "Invalid threat ID format")
	})
}
