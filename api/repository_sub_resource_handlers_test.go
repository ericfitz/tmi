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

// MockRepositoryStore is a mock implementation of RepositoryStore for testing
type MockRepositoryStore struct {
	mock.Mock
}

func (m *MockRepositoryStore) Create(ctx context.Context, repository *Repository, threatModelID string) error {
	args := m.Called(ctx, repository, threatModelID)
	return args.Error(0)
}

func (m *MockRepositoryStore) Get(ctx context.Context, id string) (*Repository, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Repository), args.Error(1)
}

func (m *MockRepositoryStore) Update(ctx context.Context, repository *Repository, threatModelID string) error {
	args := m.Called(ctx, repository, threatModelID)
	return args.Error(0)
}

func (m *MockRepositoryStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepositoryStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Repository, error) {
	args := m.Called(ctx, threatModelID, offset, limit)
	return args.Get(0).([]Repository), args.Error(1)
}

func (m *MockRepositoryStore) BulkCreate(ctx context.Context, repositorys []Repository, threatModelID string) error {
	args := m.Called(ctx, repositorys, threatModelID)
	return args.Error(0)
}

func (m *MockRepositoryStore) InvalidateCache(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepositoryStore) WarmCache(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

// setupRepositorySubRerepositoryHandler creates a test router with repository sub-rerepository handlers
func setupRepositorySubRerepositoryHandler() (*gin.Engine, *MockRepositoryStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockRepositoryStore := &MockRepositoryStore{}
	handler := NewRepositorySubResourceHandler(mockRepositoryStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register repository sub-rerepository routes
	r.GET("/threat_models/:threat_model_id/repositorys", handler.GetRepositorys)
	r.POST("/threat_models/:threat_model_id/repositorys", handler.CreateRepository)
	r.GET("/threat_models/:threat_model_id/repositorys/:repository_id", handler.GetRepository)
	r.PUT("/threat_models/:threat_model_id/repositorys/:repository_id", handler.UpdateRepository)
	r.DELETE("/threat_models/:threat_model_id/repositorys/:repository_id", handler.DeleteRepository)
	r.POST("/threat_models/:threat_model_id/repositorys/bulk", handler.BulkCreateRepositorys)

	return r, mockRepositoryStore
}

// TestGetRepositorys tests retrieving repository code references for a threat model
func TestGetRepositorys(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositorys := []Repository{
			{Uri: "https://github.com/user/repo1"},
			{Uri: "https://github.com/user/repo2"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		uuid2, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
		repositorys[0].Id = &uuid1
		repositorys[1].Id = &uuid2
		repositorys[0].Name = stringPtr("Test Repo 1")
		repositorys[1].Name = stringPtr("Test Repo 2")

		mockStore.On("List", mock.Anything, threatModelID, 0, 20).Return(repositorys, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/repositorys", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "https://github.com/user/repo1", response[0]["uri"])
		assert.Equal(t, "https://github.com/user/repo2", response[1]["uri"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		req := httptest.NewRequest("GET", "/threat_models/invalid-uuid/repositorys", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("WithPagination", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositorys := []Repository{
			{Uri: "https://github.com/user/repo1"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		repositorys[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, 10, 5).Return(repositorys, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/repositorys?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestGetRepository tests retrieving a specific repository code reference
func TestGetRepository(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		repository := &Repository{Uri: "https://github.com/user/test-repo"}
		uuid1, _ := uuid.Parse(repositoryID)
		repository.Id = &uuid1
		repository.Name = stringPtr("Test Repository")

		mockStore.On("Get", mock.Anything, repositoryID).Return(repository, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/repositorys/"+repositoryID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "https://github.com/user/test-repo", response["uri"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Get", mock.Anything, repositoryID).Return(nil, NotFoundError("Repository not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/repositorys/"+repositoryID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidRepositoryID", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/repositorys/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCreateRepository tests creating a new repository code reference
func TestCreateRepository(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name":        "New Test Repository",
			"description": "A repository created for testing",
			"uri":         "https://github.com/user/new-repo",
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Repository"), threatModelID).Return(nil).Run(func(args mock.Arguments) {
			repository := args.Get(1).(*Repository)
			// Simulate setting the ID that would be set by the store
			repositoryUUID, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
			repository.Id = &repositoryUUID
		})

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/repositorys", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "New Test Repository", response["name"])
		assert.Equal(t, "A repository created for testing", response["description"])
		assert.Equal(t, "https://github.com/user/new-repo", response["uri"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingURL", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name": "Test Repository",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/repositorys", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		requestBody := map[string]interface{}{
			"uri": "https://github.com/user/repo",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/repositorys", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateRepository tests updating an existing repository code reference
func TestUpdateRepository(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"name":        "Updated Test Repository",
			"description": "An updated repository description",
			"uri":         "https://github.com/user/updated-repo",
		}

		mockStore.On("Update", mock.Anything, mock.AnythingOfType("*api.Repository"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/repositorys/"+repositoryID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Updated Test Repository", response["name"])
		assert.Equal(t, "https://github.com/user/updated-repo", response["uri"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingURL", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"name": "Test Repository",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/repositorys/"+repositoryID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidRepositoryID", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"uri": "https://github.com/user/repo",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/repositorys/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestDeleteRepository tests deleting a repository code reference
func TestDeleteRepository(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, repositoryID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/repositorys/"+repositoryID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		repositoryID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, repositoryID).Return(NotFoundError("Repository not found"))

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/repositorys/"+repositoryID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler converts all store errors to ServerError (500)
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidRepositoryID", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/repositorys/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestBulkCreateRepositorys tests bulk creating multiple repository code references
func TestBulkCreateRepositorys(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
			{
				"name":        "Bulk Repository 1",
				"description": "First bulk repository",
				"uri":         "https://github.com/user/bulk1",
			},
			{
				"name":        "Bulk Repository 2",
				"description": "Second bulk repository",
				"uri":         "https://github.com/user/bulk2",
			},
		}

		mockStore.On("BulkCreate", mock.Anything, mock.AnythingOfType("[]api.Repository"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/repositorys/bulk", bytes.NewBuffer(body))
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

	t.Run("TooManyRepositorys", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		// Create 51 repositorys (over the limit of 50)
		repositorys := make([]map[string]interface{}, 51)
		for i := 0; i < 51; i++ {
			repositorys[i] = map[string]interface{}{
				"name": "Bulk Repository " + string(rune(i)),
				"uri":  "https://github.com/user/repo",
			}
		}

		requestBody := repositorys

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/repositorys/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingRepositoryURL", func(t *testing.T) {
		r, _ := setupRepositorySubRerepositoryHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
			{
				"name": "Repository without URL",
			},
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/repositorys/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
