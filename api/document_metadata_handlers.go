package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// DocumentMetadataHandler provides handlers for document metadata operations
type DocumentMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDocumentMetadataHandler creates a new document metadata handler
func NewDocumentMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DocumentMetadataHandler {
	return &DocumentMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}


// GetDocumentMetadata retrieves all metadata for a document
// GET /threat_models/{threat_model_id}/documents/{document_id}/metadata
func (h *DocumentMetadataHandler) GetDocumentMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDocumentMetadata - retrieving metadata for document")

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

	logger.Debug("Retrieving metadata for document %s (user: %s)", documentID, userEmail)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "document", documentID)
	if err != nil {
		logger.Error("Failed to retrieve document metadata for %s: %v", documentID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for document %s", len(metadata), documentID)
	c.JSON(http.StatusOK, metadata)
}

// GetDocumentMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}
func (h *DocumentMetadataHandler) GetDocumentMetadataByKey(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDocumentMetadataByKey - retrieving specific metadata entry")

	// Extract document ID and key from URL
	documentID := c.Param("document_id")
	key := c.Param("key")

	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
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

	logger.Debug("Retrieving metadata key '%s' for document %s (user: %s)", key, documentID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "document", documentID, key)
	if err != nil {
		logger.Error("Failed to retrieve document metadata key '%s' for %s: %v", key, documentID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for document %s", key, documentID)
	c.JSON(http.StatusOK, metadata)
}

// CreateDocumentMetadata creates a new metadata entry for a document
// POST /threat_models/{threat_model_id}/documents/{document_id}/metadata
func (h *DocumentMetadataHandler) CreateDocumentMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateDocumentMetadata - creating new metadata entry")

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

	// Parse and validate request body using OpenAPI validation
	var metadata Metadata
	if err := c.ShouldBindJSON(&metadata); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	logger.Debug("Creating metadata key '%s' for document %s (user: %s)", metadata.Key, documentID, userEmail)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "document", documentID, &metadata); err != nil {
		logger.Error("Failed to create document metadata key '%s' for %s: %v", metadata.Key, documentID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "document", documentID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for document %s", metadata.Key, documentID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateDocumentMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}
func (h *DocumentMetadataHandler) UpdateDocumentMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateDocumentMetadata - updating metadata entry")

	// Extract document ID and key from URL
	documentID := c.Param("document_id")
	key := c.Param("key")

	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
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

	// Parse and validate request body using OpenAPI validation
	var metadata Metadata
	if err := c.ShouldBindJSON(&metadata); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Ensure the key matches the URL parameter
	metadata.Key = key

	logger.Debug("Updating metadata key '%s' for document %s (user: %s)", key, documentID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "document", documentID, &metadata); err != nil {
		logger.Error("Failed to update document metadata key '%s' for %s: %v", key, documentID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "document", documentID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for document %s", key, documentID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteDocumentMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}
func (h *DocumentMetadataHandler) DeleteDocumentMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteDocumentMetadata - deleting metadata entry")

	// Extract document ID and key from URL
	documentID := c.Param("document_id")
	key := c.Param("key")

	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
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

	logger.Debug("Deleting metadata key '%s' for document %s (user: %s)", key, documentID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "document", documentID, key); err != nil {
		logger.Error("Failed to delete document metadata key '%s' for %s: %v", key, documentID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for document %s", key, documentID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateDocumentMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk
func (h *DocumentMetadataHandler) BulkCreateDocumentMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BulkCreateDocumentMetadata - creating multiple metadata entries")

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

	// Parse and validate request body using OpenAPI validation
	var metadataList []Metadata
	if err := c.ShouldBindJSON(&metadataList); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Validate bulk metadata
	if len(metadataList) == 0 {
		HandleRequestError(c, InvalidInputError("No metadata entries provided"))
		return
	}

	if len(metadataList) > 20 {
		HandleRequestError(c, InvalidInputError("Maximum 20 metadata entries allowed per bulk operation"))
		return
	}

	// Check for duplicate keys within the request
	keyMap := make(map[string]bool)
	for _, metadata := range metadataList {
		if keyMap[metadata.Key] {
			HandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
			return
		}
		keyMap[metadata.Key] = true
	}

	logger.Debug("Bulk creating %d metadata entries for document %s (user: %s)",
		len(metadataList), documentID, userEmail)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "document", documentID, metadataList); err != nil {
		logger.Error("Failed to bulk create document metadata for %s: %v", documentID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "document", documentID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for document %s", len(metadataList), documentID)
	c.JSON(http.StatusCreated, createdMetadata)
}
