package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func (m *MockDocumentStore) Count(ctx context.Context, threatModelID string) (int, error) {
	args := m.Called(ctx, threatModelID)
	return args.Int(0), args.Error(1)
}

func (m *MockDocumentStore) BulkCreate(ctx context.Context, documents []Document, threatModelID string) error {
	args := m.Called(ctx, documents, threatModelID)
	return args.Error(0)
}

func (m *MockDocumentStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Document, error) {
	args := m.Called(ctx, id, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Document), args.Error(1)
}

func (m *MockDocumentStore) InvalidateCache(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockDocumentStore) WarmCache(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

func (m *MockDocumentStore) SoftDelete(ctx context.Context, id string) error {
	return nil
}

func (m *MockDocumentStore) Restore(ctx context.Context, id string) error {
	return nil
}

func (m *MockDocumentStore) HardDelete(ctx context.Context, id string) error {
	return nil
}

func (m *MockDocumentStore) GetIncludingDeleted(ctx context.Context, id string) (*Document, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDocumentStore) ListByAccessStatus(ctx context.Context, status string, limit int) ([]Document, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDocumentStore) UpdateAccessStatus(ctx context.Context, id string, accessStatus string, contentSource string) error {
	args := m.Called(ctx, id, accessStatus, contentSource)
	return args.Error(0)
}

func (m *MockDocumentStore) UpdateAccessStatusWithDiagnostics(
	ctx context.Context, id string, accessStatus string, contentSource string,
	reasonCode string, reasonDetail string,
) error {
	args := m.Called(ctx, id, accessStatus, contentSource, reasonCode, reasonDetail)
	return args.Error(0)
}

func (m *MockDocumentStore) GetAccessReason(
	ctx context.Context, id string,
) (string, string, *time.Time, error) {
	args := m.Called(ctx, id)
	var updatedAt *time.Time
	if v := args.Get(2); v != nil {
		updatedAt = v.(*time.Time)
	}
	return args.String(0), args.String(1), updatedAt, args.Error(3)
}

func (m *MockDocumentStore) SetPickerMetadata(
	ctx context.Context, id string, providerID, fileID, mimeType string,
) error {
	args := m.Called(ctx, id, providerID, fileID, mimeType)
	return args.Error(0)
}

func (m *MockDocumentStore) ClearPickerMetadataForOwner(
	ctx context.Context, ownerInternalUUID, providerID string,
) (int64, error) {
	args := m.Called(ctx, ownerInternalUUID, providerID)
	return int64(args.Int(0)), args.Error(1)
}

// setupDocumentSubResourceHandler creates a test router with document sub-resource handlers
func setupDocumentSubResourceHandler() (*gin.Engine, *MockDocumentStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockDocumentStore := &MockDocumentStore{}
	handler := NewDocumentSubResourceHandler(mockDocumentStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register document sub-resource routes
	r.GET("/threat_models/:threat_model_id/documents", handler.GetDocuments)
	r.POST("/threat_models/:threat_model_id/documents", handler.CreateDocument)
	r.GET("/threat_models/:threat_model_id/documents/:document_id", handler.GetDocument)
	r.PUT("/threat_models/:threat_model_id/documents/:document_id", handler.UpdateDocument)
	r.DELETE("/threat_models/:threat_model_id/documents/:document_id", handler.DeleteDocument)
	r.POST("/threat_models/:threat_model_id/documents/bulk", handler.BulkCreateDocuments)

	return r, mockDocumentStore
}

// TestGetDocuments tests retrieving documents for a threat model
func TestGetDocuments(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := testUUID1
		documents := []Document{
			{Name: "Test Document 1", Uri: "https://example.com/doc1.pdf"},
			{Name: "Test Document 2", Uri: "https://example.com/doc2.pdf"},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		uuid2, _ := uuid.Parse(testUUID2)
		documents[0].Id = &uuid1
		documents[1].Id = &uuid2

		mockStore.On("List", mock.Anything, threatModelID, 0, 20).Return(documents, nil)
		mockStore.On("Count", mock.Anything, threatModelID).Return(2, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListDocumentsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.Documents, 2)
		assert.Equal(t, "Test Document 1", response.Documents[0].Name)
		assert.Equal(t, "Test Document 2", response.Documents[1].Name)
		assert.Equal(t, 2, response.Total)
		assert.Equal(t, 20, response.Limit)
		assert.Equal(t, 0, response.Offset)

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

		threatModelID := testUUID1
		documents := []Document{
			{Name: "Test Document 1", Uri: "https://example.com/doc1.pdf"},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		documents[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, 10, 5).Return(documents, nil)
		mockStore.On("Count", mock.Anything, threatModelID).Return(100, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListDocumentsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.Documents, 1)
		assert.Equal(t, 100, response.Total)
		assert.Equal(t, 5, response.Limit)
		assert.Equal(t, 10, response.Offset)

		mockStore.AssertExpectations(t)
	})
}

// TestGetDocument tests retrieving a specific document
func TestGetDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := testUUID1
		documentID := testUUID2

		document := &Document{Name: "Test Document", Uri: "https://example.com/doc.pdf"}
		uuid1, _ := uuid.Parse(documentID)
		document.Id = &uuid1

		mockStore.On("Get", mock.Anything, documentID).Return(document, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Test Document", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := testUUID1
		documentID := testUUID2

		mockStore.On("Get", mock.Anything, documentID).Return(nil, NotFoundError("Document not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidDocumentID", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := testUUID1

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

		threatModelID := testUUID1

		requestBody := map[string]any{
			"name":        "New Test Document",
			"description": "A document created for testing",
			"uri":         "https://example.com/new-doc.pdf",
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).Return(nil).Run(func(args mock.Arguments) {
			document := args.Get(1).(*Document)
			// Simulate setting the ID that would be set by the store
			documentUUID, _ := uuid.Parse(testUUID2)
			document.Id = &documentUUID
		})

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Debug: print actual response
		if w.Code != http.StatusCreated {
			t.Logf("Response status: %d", w.Code)
			t.Logf("Response body: %s", w.Body.String())
		}

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "New Test Document", response["name"])
		assert.Equal(t, "A document created for testing", response["description"])
		assert.Equal(t, "https://example.com/new-doc.pdf", response["uri"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingName", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]any{
			"uri": "https://example.com/doc.pdf",
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

		threatModelID := testUUID1

		requestBody := map[string]any{
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

		requestBody := map[string]any{
			"name": "Test Document",
			"uri":  "https://example.com/doc.pdf",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("TimmyEnabledDefault", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]any{
			"name": "Document Without TimmyEnabled",
			"uri":  "https://example.com/doc.pdf",
		}

		var capturedDocument *Document
		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).Return(nil).Run(func(args mock.Arguments) {
			capturedDocument = args.Get(1).(*Document)
			documentUUID, _ := uuid.Parse(testUUID2)
			capturedDocument.Id = &documentUUID
		})

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		// When timmy_enabled is omitted, the parsed struct should have nil
		assert.Nil(t, capturedDocument.TimmyEnabled, "TimmyEnabled should be nil when omitted from request")

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Document Without TimmyEnabled", response["name"])

		mockStore.AssertExpectations(t)
	})

	t.Run("TimmyEnabledExplicitFalse", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]any{
			"name":          "Document With TimmyEnabled False",
			"uri":           "https://example.com/doc.pdf",
			"timmy_enabled": false,
		}

		var capturedDocument *Document
		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).Return(nil).Run(func(args mock.Arguments) {
			capturedDocument = args.Get(1).(*Document)
			documentUUID, _ := uuid.Parse(testUUID2)
			capturedDocument.Id = &documentUUID
		})

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		// When timmy_enabled is explicitly false, it should be preserved
		require.NotNil(t, capturedDocument.TimmyEnabled, "TimmyEnabled should not be nil when explicitly set")
		assert.False(t, *capturedDocument.TimmyEnabled, "TimmyEnabled should be false when explicitly set to false")

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, false, response["timmy_enabled"])

		mockStore.AssertExpectations(t)
	})

	t.Run("ProviderNotConfigured", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()

		mockStore := &MockDocumentStore{}
		handler := NewDocumentSubResourceHandler(mockStore, nil, nil, nil)

		// Create a pipeline with only HTTPSource (no Google Drive source)
		sources := NewContentSourceRegistry()
		sources.Register(NewHTTPSource(NewURIValidator(nil, nil)))
		extractors := NewContentExtractorRegistry()
		matcher := NewURLPatternMatcher()
		pipeline := NewContentPipeline(sources, extractors, matcher)
		handler.SetContentPipeline(pipeline)

		r.Use(func(c *gin.Context) {
			c.Set("userEmail", "test@example.com")
			c.Set("userID", "test-provider-id")
			c.Set("userRole", RoleWriter)
			c.Next()
		})
		r.POST("/threat_models/:threat_model_id/documents", handler.CreateDocument)

		threatModelID := testUUID1
		requestBody := map[string]any{
			"name": "Google Doc",
			"uri":  "https://docs.google.com/document/d/abc123/edit",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "provider_not_configured", response["error"])

		// Ensure Create was never called
		mockStore.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("HTTPProviderSetsAccessStatus", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()

		mockStore := &MockDocumentStore{}
		handler := NewDocumentSubResourceHandler(mockStore, nil, nil, nil)

		// Create a pipeline with only HTTPSource
		sources := NewContentSourceRegistry()
		sources.Register(NewHTTPSource(NewURIValidator(nil, nil)))
		extractors := NewContentExtractorRegistry()
		matcher := NewURLPatternMatcher()
		pipeline := NewContentPipeline(sources, extractors, matcher)
		handler.SetContentPipeline(pipeline)

		r.Use(func(c *gin.Context) {
			c.Set("userEmail", "test@example.com")
			c.Set("userID", "test-provider-id")
			c.Set("userRole", RoleWriter)
			c.Next()
		})
		r.POST("/threat_models/:threat_model_id/documents", handler.CreateDocument)

		threatModelID := testUUID1
		requestBody := map[string]any{
			"name": "External Doc",
			"uri":  "https://example.com/doc.pdf",
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).Return(nil)
		mockStore.On("UpdateAccessStatus", mock.Anything, mock.AnythingOfType("string"), AccessStatusUnknown, ProviderHTTP).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		mockStore.AssertExpectations(t)
	})
}

// TestUpdateDocument tests updating an existing document
func TestUpdateDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := testUUID1
		documentID := testUUID2

		requestBody := map[string]any{
			"name":        "Updated Test Document",
			"description": "An updated document description",
			"uri":         "https://example.com/updated-doc.pdf",
		}

		mockStore.On("Get", mock.Anything, documentID).Return((*Document)(nil), nil)
		mockStore.On("Update", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/documents/"+documentID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Updated Test Document", response["name"])
		assert.Equal(t, "https://example.com/updated-doc.pdf", response["uri"])

		mockStore.AssertExpectations(t)
	})

	t.Run("MissingName", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := testUUID1
		documentID := testUUID2

		requestBody := map[string]any{
			"uri": "https://example.com/doc.pdf",
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

		threatModelID := testUUID1

		requestBody := map[string]any{
			"name": "Test Document",
			"uri":  "https://example.com/doc.pdf",
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

		threatModelID := testUUID1
		documentID := testUUID2

		mockStore.On("Get", mock.Anything, documentID).Return((*Document)(nil), nil)
		mockStore.On("Delete", mock.Anything, documentID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupDocumentSubResourceHandler()

		threatModelID := testUUID1
		documentID := testUUID2

		mockStore.On("Get", mock.Anything, documentID).Return((*Document)(nil), nil)
		mockStore.On("Delete", mock.Anything, documentID).Return(NotFoundError("Document not found"))

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/documents/"+documentID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler now properly returns 404 for not found errors
		assert.Equal(t, http.StatusNotFound, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidDocumentID", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := testUUID1

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

		threatModelID := testUUID1

		requestBody := []map[string]any{
			{
				"name":        "Bulk Document 1",
				"description": "First bulk document",
				"uri":         "https://example.com/bulk1.pdf",
			},
			{
				"name":        "Bulk Document 2",
				"description": "Second bulk document",
				"uri":         "https://example.com/bulk2.pdf",
			},
		}

		mockStore.On("BulkCreate", mock.Anything, mock.AnythingOfType("[]api.Document"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents/bulk", bytes.NewBuffer(body))
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

	t.Run("TooManyDocuments", func(t *testing.T) {
		r, _ := setupDocumentSubResourceHandler()

		threatModelID := testUUID1

		// Create 51 documents (over the limit of 50)
		documents := make([]map[string]any, 51)
		for i := range 51 {
			documents[i] = map[string]any{
				"name": "Bulk Document " + string(rune(i)),
				"uri":  "https://example.com/doc.pdf",
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

		threatModelID := testUUID1

		requestBody := []map[string]any{
			{
				"uri": "https://example.com/doc.pdf",
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

		threatModelID := testUUID1

		requestBody := []map[string]any{
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

func TestGetDocument_IncludesAccessDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockStore := &MockDocumentStore{}
	mockTokens := &mockContentTokenRepo{
		listByUser: func(ctx context.Context, userID string) ([]ContentToken, error) {
			return []ContentToken{
				{ProviderID: ProviderGoogleWorkspace, Status: ContentTokenStatusActive},
			}, nil
		},
	}

	handler := NewDocumentSubResourceHandler(mockStore, nil, nil, nil)
	handler.SetContentTokens(mockTokens)
	handler.SetServiceAccountEmail("indexer@tmi.iam.gserviceaccount.com")

	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "viewer@example.com")
		c.Set("userID", "viewer-provider-id")
		c.Set("userInternalUUID", "viewer-uuid")
		c.Set("userRole", RoleReader)
		c.Next()
	})
	r.GET("/threat_models/:threat_model_id/documents/:document_id", handler.GetDocument)

	docID := uuid.New()
	tmID := uuid.New()
	pending := DocumentAccessStatusPendingAccess
	contentSource := ProviderGoogleWorkspace

	doc := &Document{
		Id:            &docID,
		Name:          "design-doc.gdoc",
		Uri:           "https://docs.google.com/document/d/abc/edit",
		AccessStatus:  &pending,
		ContentSource: &contentSource,
	}
	updatedAt := time.Now().UTC()
	mockStore.On("Get", mock.Anything, docID.String()).Return(doc, nil)
	mockStore.On("GetAccessReason", mock.Anything, docID.String()).
		Return(ReasonNoAccessibleSource, "", &updatedAt, nil)

	req := httptest.NewRequest("GET",
		"/threat_models/"+tmID.String()+"/documents/"+docID.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	diag, ok := body["access_diagnostics"].(map[string]interface{})
	require.True(t, ok, "expected access_diagnostics in response, body=%s", rec.Body.String())
	assert.Equal(t, "no_accessible_source", diag["reason_code"])
	rems, ok := diag["remediations"].([]interface{})
	require.True(t, ok)
	require.Len(t, rems, 2, "expected 2 remediations because caller has google_workspace linked")
	r0 := rems[0].(map[string]interface{})
	assert.Equal(t, "share_with_service_account", r0["action"])
	p0 := r0["params"].(map[string]interface{})
	assert.Equal(t, "indexer@tmi.iam.gserviceaccount.com", p0["service_account_email"])
	r1 := rems[1].(map[string]interface{})
	assert.Equal(t, "repick_after_share", r1["action"])

	_, hasUpdated := body["access_status_updated_at"]
	assert.True(t, hasUpdated, "expected access_status_updated_at in response")

	mockStore.AssertExpectations(t)
}

func TestGetDocument_NoDiagnosticsWhenAccessible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockStore := &MockDocumentStore{}
	handler := NewDocumentSubResourceHandler(mockStore, nil, nil, nil)

	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "viewer@example.com")
		c.Set("userID", "viewer-provider-id")
		c.Set("userRole", RoleReader)
		c.Next()
	})
	r.GET("/threat_models/:threat_model_id/documents/:document_id", handler.GetDocument)

	docID := uuid.New()
	tmID := uuid.New()
	accessible := DocumentAccessStatusAccessible

	doc := &Document{
		Id:           &docID,
		Name:         "design-doc.gdoc",
		Uri:          "https://docs.google.com/document/d/abc/edit",
		AccessStatus: &accessible,
	}
	mockStore.On("Get", mock.Anything, docID.String()).Return(doc, nil)

	req := httptest.NewRequest("GET",
		"/threat_models/"+tmID.String()+"/documents/"+docID.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Nil(t, body["access_diagnostics"], "expected no diagnostics when accessible")

	// GetAccessReason should NOT be called when status is accessible.
	mockStore.AssertNotCalled(t, "GetAccessReason", mock.Anything, mock.Anything)
}

// =============================================================================
// picker_registration tests
// =============================================================================

// newPickerTestRouter creates a router pre-wired with a ContentOAuthRegistry and
// ContentTokenRepository for picker_registration tests.
func newPickerTestRouter(
	mockStore *MockDocumentStore,
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handler := NewDocumentSubResourceHandler(mockStore, nil, nil, nil)
	handler.SetContentTokens(tokens)
	handler.SetContentOAuthRegistry(registry)

	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		c.Set("userInternalUUID", "alice-uuid")
		c.Set("userRole", RoleWriter)
		c.Next()
	})
	r.POST("/threat_models/:threat_model_id/documents", handler.CreateDocument)
	return r
}

func newPickerRegistry() *ContentOAuthProviderRegistry {
	registry := NewContentOAuthProviderRegistry()
	stub := &stubContentOAuthProvider{id: ProviderGoogleWorkspace, authURL: "https://stub/authorize"}
	registry.Register(stub)
	return registry
}

func newActiveTokenRepo() *mockContentTokenRepo {
	return &mockContentTokenRepo{
		getByUserAndProvider: func(ctx context.Context, userID, providerID string) (*ContentToken, error) {
			return &ContentToken{
				UserID:     userID,
				ProviderID: providerID,
				Status:     ContentTokenStatusActive,
			}, nil
		},
	}
}

func TestCreateDocument_WithPickerRegistration_HappyPath(t *testing.T) {
	mockStore := &MockDocumentStore{}
	registry := newPickerRegistry()
	tokens := newActiveTokenRepo()

	r := newPickerTestRouter(mockStore, tokens, registry)

	tmID := uuid.New()
	mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Document"), tmID.String()).
		Return(nil)
	mockStore.On("SetPickerMetadata", mock.Anything, mock.AnythingOfType("string"),
		ProviderGoogleWorkspace, "abc123", "application/vnd.google-apps.document").
		Return(nil)

	body := `{
		"name": "Picked design doc",
		"uri": "https://docs.google.com/document/d/abc123/edit",
		"picker_registration": {
			"provider_id": "google_workspace",
			"file_id": "abc123",
			"mime_type": "application/vnd.google-apps.document"
		}
	}`
	req := httptest.NewRequest("POST",
		"/threat_models/"+tmID.String()+"/documents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())

	var resp Document
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.AccessStatus)
	assert.Equal(t, DocumentAccessStatusUnknown, *resp.AccessStatus)
	require.NotNil(t, resp.ContentSource)
	assert.Equal(t, ProviderGoogleWorkspace, *resp.ContentSource)
	mockStore.AssertExpectations(t)
}

func TestCreateDocument_PickerRegistration_FileIDMismatch(t *testing.T) {
	mockStore := &MockDocumentStore{}
	registry := newPickerRegistry()
	tokens := newActiveTokenRepo()

	r := newPickerTestRouter(mockStore, tokens, registry)

	tmID := uuid.New()

	body := `{
		"name": "Mismatched doc",
		"uri": "https://docs.google.com/document/d/abc123/edit",
		"picker_registration": {
			"provider_id": "google_workspace",
			"file_id": "xyz999",
			"mime_type": "application/vnd.google-apps.document"
		}
	}`
	req := httptest.NewRequest("POST",
		"/threat_models/"+tmID.String()+"/documents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())

	var errBody map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, "picker_file_id_mismatch", errBody["error"])

	mockStore.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
}

func TestCreateDocument_PickerRegistration_EmptyFileID(t *testing.T) {
	mockStore := &MockDocumentStore{}
	registry := newPickerRegistry()
	tokens := newActiveTokenRepo()

	r := newPickerTestRouter(mockStore, tokens, registry)

	tmID := uuid.New()

	body := `{
		"name": "Empty file id",
		"uri": "https://docs.google.com/document/d/abc123/edit",
		"picker_registration": {
			"provider_id": "google_workspace",
			"file_id": "",
			"mime_type": "application/vnd.google-apps.document"
		}
	}`
	req := httptest.NewRequest("POST",
		"/threat_models/"+tmID.String()+"/documents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())

	var errBody map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, "invalid_picker_registration", errBody["error"])

	mockStore.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
}

func TestCreateDocument_PickerRegistration_UnknownProvider(t *testing.T) {
	mockStore := &MockDocumentStore{}
	// Registry has google_workspace registered, but body sends "nonexistent"
	registry := newPickerRegistry()
	tokens := newActiveTokenRepo()

	r := newPickerTestRouter(mockStore, tokens, registry)

	tmID := uuid.New()

	body := `{
		"name": "Unknown provider",
		"uri": "https://docs.google.com/document/d/abc123/edit",
		"picker_registration": {
			"provider_id": "nonexistent",
			"file_id": "abc123",
			"mime_type": "application/vnd.google-apps.document"
		}
	}`
	req := httptest.NewRequest("POST",
		"/threat_models/"+tmID.String()+"/documents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())

	var errBody map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, "provider_not_registered", errBody["error"])

	mockStore.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
}

func TestCreateDocument_PickerRegistration_NoLinkedToken(t *testing.T) {
	mockStore := &MockDocumentStore{}
	registry := newPickerRegistry()
	tokens := &mockContentTokenRepo{
		getByUserAndProvider: func(ctx context.Context, userID, providerID string) (*ContentToken, error) {
			return nil, ErrContentTokenNotFound
		},
	}

	r := newPickerTestRouter(mockStore, tokens, registry)

	tmID := uuid.New()

	body := `{
		"name": "No token",
		"uri": "https://docs.google.com/document/d/abc123/edit",
		"picker_registration": {
			"provider_id": "google_workspace",
			"file_id": "abc123",
			"mime_type": "application/vnd.google-apps.document"
		}
	}`
	req := httptest.NewRequest("POST",
		"/threat_models/"+tmID.String()+"/documents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())

	var errBody map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, "token_not_linked_or_failed", errBody["error"])

	mockStore.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
}

func TestCreateDocument_PickerRegistration_FailedRefreshToken(t *testing.T) {
	mockStore := &MockDocumentStore{}
	registry := newPickerRegistry()
	tokens := &mockContentTokenRepo{
		getByUserAndProvider: func(ctx context.Context, userID, providerID string) (*ContentToken, error) {
			return &ContentToken{
				UserID:     userID,
				ProviderID: providerID,
				Status:     ContentTokenStatusFailedRefresh,
			}, nil
		},
	}

	r := newPickerTestRouter(mockStore, tokens, registry)

	tmID := uuid.New()

	body := `{
		"name": "Failed refresh token",
		"uri": "https://docs.google.com/document/d/abc123/edit",
		"picker_registration": {
			"provider_id": "google_workspace",
			"file_id": "abc123",
			"mime_type": "application/vnd.google-apps.document"
		}
	}`
	req := httptest.NewRequest("POST",
		"/threat_models/"+tmID.String()+"/documents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())

	var errBody map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, "token_not_linked_or_failed", errBody["error"])

	mockStore.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
}
