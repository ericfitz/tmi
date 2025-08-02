package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// DiagramMetadataHandler provides handlers for diagram metadata operations
type DiagramMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDiagramMetadataHandler creates a new diagram metadata handler
func NewDiagramMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DiagramMetadataHandler {
	return &DiagramMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetDiagramMetadata retrieves all metadata for a diagram
// GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata
func (h *DiagramMetadataHandler) GetDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDiagramMetadata - retrieving metadata for diagram")

	// Extract diagram ID from URL
	diagramID := c.Param("diagram_id")
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for diagram %s (user: %s)", diagramID, userName)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "diagram", diagramID)
	if err != nil {
		logger.Error("Failed to retrieve diagram metadata for %s: %v", diagramID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for diagram %s", len(metadata), diagramID)
	c.JSON(http.StatusOK, metadata)
}

// GetDiagramMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
func (h *DiagramMetadataHandler) GetDiagramMetadataByKey(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDiagramMetadataByKey - retrieving specific metadata entry")

	// Extract diagram ID and key from URL
	diagramID := c.Param("diagram_id")
	key := c.Param("key")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for diagram %s (user: %s)", key, diagramID, userName)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "diagram", diagramID, key)
	if err != nil {
		logger.Error("Failed to retrieve diagram metadata key '%s' for %s: %v", key, diagramID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for diagram %s", key, diagramID)
	c.JSON(http.StatusOK, metadata)
}

// CreateDiagramMetadata creates a new metadata entry for a diagram
// POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata
func (h *DiagramMetadataHandler) CreateDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateDiagramMetadata - creating new metadata entry")

	// Extract diagram ID from URL
	diagramID := c.Param("diagram_id")
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse request body
	metadata, err := ParseRequestBody[Metadata](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate required fields
	if metadata.Key == "" {
		HandleRequestError(c, InvalidInputError("Metadata key is required"))
		return
	}
	if metadata.Value == "" {
		HandleRequestError(c, InvalidInputError("Metadata value is required"))
		return
	}

	logger.Debug("Creating metadata key '%s' for diagram %s (user: %s)", metadata.Key, diagramID, userName)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "diagram", diagramID, &metadata); err != nil {
		logger.Error("Failed to create diagram metadata key '%s' for %s: %v", metadata.Key, diagramID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "diagram", diagramID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for diagram %s", metadata.Key, diagramID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateDiagramMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
func (h *DiagramMetadataHandler) UpdateDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateDiagramMetadata - updating metadata entry")

	// Extract diagram ID and key from URL
	diagramID := c.Param("diagram_id")
	key := c.Param("key")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse request body
	metadata, err := ParseRequestBody[Metadata](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate required fields
	if metadata.Value == "" {
		HandleRequestError(c, InvalidInputError("Metadata value is required"))
		return
	}

	// Ensure the key matches the URL parameter
	metadata.Key = key

	logger.Debug("Updating metadata key '%s' for diagram %s (user: %s)", key, diagramID, userName)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "diagram", diagramID, &metadata); err != nil {
		logger.Error("Failed to update diagram metadata key '%s' for %s: %v", key, diagramID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "diagram", diagramID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for diagram %s", key, diagramID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteDiagramMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
func (h *DiagramMetadataHandler) DeleteDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteDiagramMetadata - deleting metadata entry")

	// Extract diagram ID and key from URL
	diagramID := c.Param("diagram_id")
	key := c.Param("key")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for diagram %s (user: %s)", key, diagramID, userName)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "diagram", diagramID, key); err != nil {
		logger.Error("Failed to delete diagram metadata key '%s' for %s: %v", key, diagramID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for diagram %s", key, diagramID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateDiagramMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk
func (h *DiagramMetadataHandler) BulkCreateDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BulkCreateDiagramMetadata - creating multiple metadata entries")

	// Extract diagram ID from URL
	diagramID := c.Param("diagram_id")
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse request body as array of metadata
	metadataList, err := ParseRequestBody[[]Metadata](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(metadataList) == 0 {
		HandleRequestError(c, InvalidInputError("No metadata entries provided"))
		return
	}

	if len(metadataList) > 20 {
		HandleRequestError(c, InvalidInputError("Maximum 20 metadata entries allowed per bulk operation"))
		return
	}

	// Validate all metadata entries
	for i, metadata := range metadataList {
		if metadata.Key == "" {
			HandleRequestError(c, InvalidInputError("Metadata key is required for all entries"))
			return
		}
		if metadata.Value == "" {
			HandleRequestError(c, InvalidInputError("Metadata value is required for all entries"))
			return
		}

		// Check for duplicate keys within the request
		for j := i + 1; j < len(metadataList); j++ {
			if metadataList[j].Key == metadata.Key {
				HandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
				return
			}
		}
	}

	logger.Debug("Bulk creating %d metadata entries for diagram %s (user: %s)",
		len(metadataList), diagramID, userName)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "diagram", diagramID, metadataList); err != nil {
		logger.Error("Failed to bulk create diagram metadata for %s: %v", diagramID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "diagram", diagramID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for diagram %s", len(metadataList), diagramID)
	c.JSON(http.StatusCreated, createdMetadata)
}
