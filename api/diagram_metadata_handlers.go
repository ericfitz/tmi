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

// NewDiagramMetadataHandlerSimple creates a new diagram metadata handler with default dependencies
func NewDiagramMetadataHandlerSimple() *DiagramMetadataHandler {
	// Create a simple in-memory metadata store for now
	// In production, this should be properly injected
	store := NewInMemoryMetadataStore()
	return &DiagramMetadataHandler{
		metadataStore:    store,
		db:               nil,
		cache:            nil,
		cacheInvalidator: nil,
	}
}

// Direct diagram metadata handlers for /diagrams/:id/metadata endpoints

// GetDirectDiagramMetadata retrieves all metadata for a diagram via direct route
// GET /diagrams/{id}/metadata
func (h *DiagramMetadataHandler) GetDirectDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDirectDiagramMetadata - retrieving metadata for diagram")

	// Extract diagram ID from URL (using 'id' parameter for direct routes)
	diagramID := c.Param("id")
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

// GetDirectDiagramMetadataByKey retrieves a specific metadata entry by key via direct route
// GET /diagrams/{id}/metadata/{key}
func (h *DiagramMetadataHandler) GetDirectDiagramMetadataByKey(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDirectDiagramMetadataByKey - retrieving specific metadata entry")

	// Extract diagram ID and key from URL
	diagramID := c.Param("id")
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

// CreateDirectDiagramMetadata creates a new metadata entry for a diagram via direct route
// POST /diagrams/{id}/metadata
func (h *DiagramMetadataHandler) CreateDirectDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateDirectDiagramMetadata - creating new metadata entry")

	// Extract diagram ID from URL
	diagramID := c.Param("id")
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

// UpdateDirectDiagramMetadata updates an existing metadata entry via direct route
// PUT /diagrams/{id}/metadata/{key}
func (h *DiagramMetadataHandler) UpdateDirectDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateDirectDiagramMetadata - updating metadata entry")

	// Extract diagram ID and key from URL
	diagramID := c.Param("id")
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

// DeleteDirectDiagramMetadata deletes a metadata entry via direct route
// DELETE /diagrams/{id}/metadata/{key}
func (h *DiagramMetadataHandler) DeleteDirectDiagramMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteDirectDiagramMetadata - deleting metadata entry")

	// Extract diagram ID and key from URL
	diagramID := c.Param("id")
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

// Direct diagram cell metadata handlers for /diagrams/:id/cells/:cell_id/metadata endpoints

// GetDirectDiagramCellMetadata retrieves all metadata for a diagram cell via direct route
// GET /diagrams/{id}/cells/{cell_id}/metadata
func (h *DiagramMetadataHandler) GetDirectDiagramCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDirectDiagramCellMetadata - retrieving metadata for diagram cell")

	// Extract diagram ID and cell ID from URL
	diagramID := c.Param("id")
	cellID := c.Param("cell_id")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
		return
	}

	// Validate diagram ID format
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Cell ID is expected to be a string (not necessarily UUID)
	// No additional validation required

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for cell %s in diagram %s (user: %s)", cellID, diagramID, userName)

	// Get metadata from store using cell entity type
	metadata, err := h.metadataStore.List(c.Request.Context(), "cell", cellID)
	if err != nil {
		logger.Error("Failed to retrieve cell metadata for %s: %v", cellID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for cell %s", len(metadata), cellID)
	c.JSON(http.StatusOK, metadata)
}

// GetDirectDiagramCellMetadataByKey retrieves a specific metadata entry by key for a diagram cell
// GET /diagrams/{id}/cells/{cell_id}/metadata/{key}
func (h *DiagramMetadataHandler) GetDirectDiagramCellMetadataByKey(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetDirectDiagramCellMetadataByKey - retrieving specific metadata entry for cell")

	// Extract parameters from URL
	diagramID := c.Param("id")
	cellID := c.Param("cell_id")
	key := c.Param("key")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
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

	logger.Debug("Retrieving metadata key '%s' for cell %s in diagram %s (user: %s)", key, cellID, diagramID, userName)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "cell", cellID, key)
	if err != nil {
		logger.Error("Failed to retrieve cell metadata key '%s' for %s: %v", key, cellID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for cell %s", key, cellID)
	c.JSON(http.StatusOK, metadata)
}

// CreateDirectDiagramCellMetadata creates a new metadata entry for a diagram cell
// POST /diagrams/{id}/cells/{cell_id}/metadata
func (h *DiagramMetadataHandler) CreateDirectDiagramCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateDirectDiagramCellMetadata - creating new metadata entry for cell")

	// Extract parameters from URL
	diagramID := c.Param("id")
	cellID := c.Param("cell_id")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
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

	logger.Debug("Creating metadata key '%s' for cell %s in diagram %s (user: %s)", metadata.Key, cellID, diagramID, userName)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "cell", cellID, &metadata); err != nil {
		logger.Error("Failed to create cell metadata key '%s' for %s: %v", metadata.Key, cellID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "cell", cellID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for cell %s", metadata.Key, cellID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateDirectDiagramCellMetadata updates an existing metadata entry for a diagram cell
// PUT /diagrams/{id}/cells/{cell_id}/metadata/{key}
func (h *DiagramMetadataHandler) UpdateDirectDiagramCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateDirectDiagramCellMetadata - updating metadata entry for cell")

	// Extract parameters from URL
	diagramID := c.Param("id")
	cellID := c.Param("cell_id")
	key := c.Param("key")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
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

	logger.Debug("Updating metadata key '%s' for cell %s in diagram %s (user: %s)", key, cellID, diagramID, userName)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "cell", cellID, &metadata); err != nil {
		logger.Error("Failed to update cell metadata key '%s' for %s: %v", key, cellID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "cell", cellID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for cell %s", key, cellID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteDirectDiagramCellMetadata deletes a metadata entry for a diagram cell
// DELETE /diagrams/{id}/cells/{cell_id}/metadata/{key}
func (h *DiagramMetadataHandler) DeleteDirectDiagramCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteDirectDiagramCellMetadata - deleting metadata entry for cell")

	// Extract parameters from URL
	diagramID := c.Param("id")
	cellID := c.Param("cell_id")
	key := c.Param("key")

	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
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

	logger.Debug("Deleting metadata key '%s' for cell %s in diagram %s (user: %s)", key, cellID, diagramID, userName)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "cell", cellID, key); err != nil {
		logger.Error("Failed to delete cell metadata key '%s' for %s: %v", key, cellID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for cell %s", key, cellID)
	c.JSON(http.StatusNoContent, nil)
}
