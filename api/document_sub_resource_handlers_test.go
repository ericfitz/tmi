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

// MockDocumentStore is a mock implementation of DocumentStore for testing
type MockDocumentStore struct {
	mock.Mock
}

func (m *MockDocumentStore) Create(ctx context.Context, document *Document, threatModelID string) error {
	args := m.Called(ctx, document, threatModelID)
	return args.Error(0)
}

func (m *MockDocumentStore) Get(ctx context.Context, id string) (*Document, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Document), args.Error(1)
}

func (m *MockDocumentStore) Update(ctx context.Context, document *Document, threatModelID string) error {
	args := m.Called(ctx, document, threatModelID)
	return args.Error(0)
}

func (m *MockDocumentStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockDocumentStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Document, error) {
	args := m.Called(ctx, threatModelID, offset, limit)
	return args.Get(0).([]Document), args.Error(1)
}

func (m *MockDocumentStore) BulkCreate(ctx context.Context, documents []Document, threatModelID string) error {
	args := m.Called(ctx, documents, threatModelID)
	return args.Error(0)
}

func (m *MockDocumentStore) InvalidateCache(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockDocumentStore) WarmCache(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

// setupDocumentSubResourceHandler creates a test router with document sub-resource handlers
func setupDocumentSubResourceHandler() (*gin.Engine, *MockDocumentStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockDocumentStore := &MockDocumentStore{}
	handler := NewDocumentSubResourceHandler(mockDocumentStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userName", "test@example.com")
		c.Set("userRole", Writer)
		c.Next()
	})

	// Register document sub-resource routes
	r.GET("/threat_models/:id/documents", handler.GetDocuments)
	r.POST("/threat_models/:id/documents", handler.CreateDocument)
	r.GET("/threat_models/:id/documents/:document_id", handler.GetDocument)
	r.PUT("/threat_models/:id/documents/:document_id", handler.UpdateDocument)
	r.DELETE("/threat_models/:id/documents/:document_id", handler.DeleteDocument)
	r.POST("/threat_models/:id/documents/bulk", handler.BulkCreateDocuments)

	return r, mockDocumentStore
}

// TestGetDocuments tests retrieving documents for a threat model
func TestGetDocuments(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documents := []Document{
			{Name: "Test Document 1", Url: "https://example.com/doc1.pdf"},
			{Name: "Test Document 2", Url: "https://example.com/doc2.pdf"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		uuid2, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
		documents[0].Id = &uuid1
		documents[1].Id = &uuid2

		mockStore.On("List", mock.Anything, threatModelID, 0, 20).Return(documents, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
		assert.Equal(t, "Test Document 1", response[0]["name"])
		assert.Equal(t, "Test Document 2", response[1]["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		req := httptest.NewRequest("GET", "/threat_models/invalid-uuid/documents", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("WithPagination", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documents := []Document{
			{Name: "Test Document 1", Url: "https://example.com/doc1.pdf"},
		}

		uuid1, _ := uuid.Parse("00000000-0000-0000-0000-000000000001")
		documents[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, 10, 5).Return(documents, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestGetDocument tests retrieving a specific document
func TestGetDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		document := &Document{Name: "Test Document", Url: "https://example.com/doc.pdf"}
		uuid1, _ := uuid.Parse(documentID)
		document.Id = &uuid1

		mockStore.On("Get", mock.Anything, documentID).Return(document, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Test Document", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Get", mock.Anything, documentID).Return(nil, NotFoundError("Document not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidDocumentID", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCreateDocument tests creating a new document
func TestCreateDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name":        "New Test Document",
			"description": "A document created for testing",
			"url":         "https://example.com/new-doc.pdf",
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).Return(nil).Run(func(args mock.Arguments) {
			document := args.Get(1).(*Document)
			// Simulate setting the ID that would be set by the store
			documentUUID, _ := uuid.Parse("00000000-0000-0000-0000-000000000002")
			document.Id = &documentUUID
		})

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "New Test Document", response["name"])
		assert.Equal(t, "A document created for testing", response["description"])
		assert.Equal(t, "https://example.com/new-doc.pdf", response["url"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingName", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"url": "https://example.com/doc.pdf",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingURL", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name": "Test Document",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		requestBody := map[string]interface{}{
			"name": "Test Document",
			"url":  "https://example.com/doc.pdf",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateDocument tests updating an existing document
func TestUpdateDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"id":          documentID,
			"name":        "Updated Test Document",
			"description": "An updated document description",
			"url":         "https://example.com/updated-doc.pdf",
		}

		mockStore.On("Update", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/documents/"+documentID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Updated Test Document", response["name"])
		assert.Equal(t, "https://example.com/updated-doc.pdf", response["url"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingName", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		requestBody := map[string]interface{}{
			"url": "https://example.com/doc.pdf",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/documents/"+documentID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidDocumentID", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := map[string]interface{}{
			"name": "Test Document",
			"url":  "https://example.com/doc.pdf",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/documents/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestDeleteDocument tests deleting a document
func TestDeleteDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, documentID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"
		documentID := "00000000-0000-0000-0000-000000000002"

		mockStore.On("Delete", mock.Anything, documentID).Return(NotFoundError("Document not found"))

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler converts all store errors to ServerError (500)
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidDocumentID", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/documents/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestBulkCreateDocuments tests bulk creating multiple documents
func TestBulkCreateDocuments(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
			{
				"name":        "Bulk Document 1",
				"description": "First bulk document",
				"url":         "https://example.com/bulk1.pdf",
			},
			{
				"name":        "Bulk Document 2",
				"description": "Second bulk document",
				"url":         "https://example.com/bulk2.pdf",
			},
		}

		mockStore.On("BulkCreate", mock.Anything, mock.AnythingOfType("[]api.Document"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/bulk", bytes.NewBuffer(body))
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

	t.Run("TooManyDocuments", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		// Create 51 documents (over the limit of 50)
		documents := make([]map[string]interface{}, 51)
		for i := 0; i < 51; i++ {
			documents[i] = map[string]interface{}{
				"name": "Bulk Document " + string(rune(i)),
				"url":  "https://example.com/doc.pdf",
			}
		}

		requestBody := documents

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingDocumentName", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
			{
				"url": "https://example.com/doc.pdf",
			},
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingDocumentURL", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := "00000000-0000-0000-0000-000000000001"

		requestBody := []map[string]interface{}{
			{
				"name": "Document without URL",
			},
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}