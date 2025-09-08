package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// DocumentSubResourceHandler provides handlers for document sub-resource operations
type DocumentSubResourceHandler struct {
	documentStore    DocumentStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDocumentSubResourceHandler creates a new document sub-resource handler
func NewDocumentSubResourceHandler(documentStore DocumentStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DocumentSubResourceHandler {
	return &DocumentSubResourceHandler{
		documentStore:    documentStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetDocuments retrieves all documents for a threat model with pagination
// GET /threat_models/{threat_model_id}/documents
func (h *DocumentSubResourceHandler) GetDocuments(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDocuments - retrieving documents for threat model")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Parse pagination parameters
	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	// Validate pagination parameters
	if limit < 1 || limit > 100 {
		HandleRequestError(c, InvalidInputError("Limit must be between 1 and 100"))
		return
	}
	if offset < 0 {
		HandleRequestError(c, InvalidInputError("Offset must be non-negative"))
		return
	}

	// Get authenticated user (should be set by middleware)
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving documents for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, userEmail, offset, limit)

	// Get documents from store (authorization is handled by middleware)
	documents, err := h.documentStore.List(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve documents: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve documents"))
		return
	}

	logger.Debug("Successfully retrieved %d documents", len(documents))
	c.JSON(http.StatusOK, documents)
}

// GetDocument retrieves a specific document by ID
// GET /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) GetDocument(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDocument - retrieving specific document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	if _, err := ParseUUID(documentID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving document %s (user: %s)", documentID, userEmail)

	// Get document from store
	document, err := h.documentStore.Get(c.Request.Context(), documentID)
	if err != nil {
		logger.Error("Failed to retrieve document %s: %v", documentID, err)
		HandleRequestError(c, NotFoundError("Document not found"))
		return
	}

	logger.Debug("Successfully retrieved document %s", documentID)
	c.JSON(http.StatusOK, document)
}

// CreateDocument creates a new document in a threat model
// POST /threat_models/{threat_model_id}/documents
func (h *DocumentSubResourceHandler) CreateDocument(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateDocument - creating new document")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["document_create"]
	document, err := ValidateAndParseRequest[Document](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Generate UUID if not provided
	if document.Id == nil {
		id := uuid.New()
		document.Id = &id
	}

	logger.Debug("Creating document %s in threat model %s (user: %s)",
		document.Id.String(), threatModelID, userEmail)

	// Create document in store
	if err := h.documentStore.Create(c.Request.Context(), document, threatModelID); err != nil {
		logger.Error("Failed to create document: %v", err)
		HandleRequestError(c, ServerError("Failed to create document"))
		return
	}

	logger.Debug("Successfully created document %s", document.Id.String())
	c.JSON(http.StatusCreated, document)
}

// UpdateDocument updates an existing document
// PUT /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) UpdateDocument(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateDocument - updating existing document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	documentUUID, err := ParseUUID(documentID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["document_update"]
	document, err := ValidateAndParseRequest[Document](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set ID from URL (override any value in body)
	document.Id = &documentUUID

	logger.Debug("Updating document %s (user: %s)", documentID, userEmail)

	// Update document in store
	if err := h.documentStore.Update(c.Request.Context(), document, threatModelID); err != nil {
		logger.Error("Failed to update document %s: %v", documentID, err)
		HandleRequestError(c, ServerError("Failed to update document"))
		return
	}

	logger.Debug("Successfully updated document %s", documentID)
	c.JSON(http.StatusOK, document)
}

// DeleteDocument deletes a document
// DELETE /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) DeleteDocument(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteDocument - deleting document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	if _, err := ParseUUID(documentID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting document %s (user: %s)", documentID, userEmail)

	// Delete document from store
	if err := h.documentStore.Delete(c.Request.Context(), documentID); err != nil {
		logger.Error("Failed to delete document %s: %v", documentID, err)
		HandleRequestError(c, ServerError("Failed to delete document"))
		return
	}

	logger.Debug("Successfully deleted document %s", documentID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateDocuments creates multiple documents in a single request
// POST /threat_models/{threat_model_id}/documents/bulk
func (h *DocumentSubResourceHandler) BulkCreateDocuments(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BulkCreateDocuments - creating multiple documents")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of documents
	var documents []Document
	if err := c.ShouldBindJSON(&documents); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(documents) == 0 {
		HandleRequestError(c, InvalidInputError("No documents provided"))
		return
	}

	if len(documents) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 documents allowed per bulk operation"))
		return
	}

	// Validate each document
	for _, document := range documents {
		if document.Name == "" {
			HandleRequestError(c, InvalidInputError("Document name is required for all documents"))
			return
		}
		if document.Url == "" {
			HandleRequestError(c, InvalidInputError("Document URL is required for all documents"))
			return
		}
	}

	// Generate UUIDs for documents that don't have them
	for i := range documents {
		document := &documents[i]
		if document.Id == nil {
			id := uuid.New()
			document.Id = &id
		}
	}

	logger.Debug("Bulk creating %d documents in threat model %s (user: %s)",
		len(documents), threatModelID, userEmail)

	// Create documents in store
	if err := h.documentStore.BulkCreate(c.Request.Context(), documents, threatModelID); err != nil {
		logger.Error("Failed to bulk create documents: %v", err)
		HandleRequestError(c, ServerError("Failed to create documents"))
		return
	}

	logger.Debug("Successfully bulk created %d documents", len(documents))
	c.JSON(http.StatusCreated, documents)
}
