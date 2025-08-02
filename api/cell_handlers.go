package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// CellHandler provides handlers for diagram cell operations with PATCH support and metadata
type CellHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	cellConverter    *CellConverter
}

// NewCellHandler creates a new cell handler
func NewCellHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *CellHandler {
	return &CellHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
		cellConverter:    NewCellConverter(),
	}
}

// GetCellMetadata retrieves all metadata for a diagram cell
// GET /diagrams/{diagram_id}/cells/{cell_id}/metadata
func (h *CellHandler) GetCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetCellMetadata - retrieving metadata for cell")

	// Extract cell ID from URL
	cellID := c.Param("cell_id")
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
		return
	}

	// Validate cell ID format
	if _, err := ParseUUID(cellID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid cell ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for cell %s (user: %s)", cellID, userName)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "cell", cellID)
	if err != nil {
		logger.Error("Failed to retrieve cell metadata for %s: %v", cellID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for cell %s", len(metadata), cellID)
	c.JSON(http.StatusOK, metadata)
}

// GetCellMetadataByKey retrieves a specific metadata entry by key
// GET /diagrams/{diagram_id}/cells/{cell_id}/metadata/{key}
func (h *CellHandler) GetCellMetadataByKey(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetCellMetadataByKey - retrieving specific metadata entry")

	// Extract cell ID and key from URL
	cellID := c.Param("cell_id")
	key := c.Param("key")

	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate cell ID format
	if _, err := ParseUUID(cellID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid cell ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for cell %s (user: %s)", key, cellID, userName)

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

// CreateCellMetadata creates a new metadata entry for a cell
// POST /diagrams/{diagram_id}/cells/{cell_id}/metadata
func (h *CellHandler) CreateCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateCellMetadata - creating new metadata entry")

	// Extract cell ID from URL
	cellID := c.Param("cell_id")
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
		return
	}

	// Validate cell ID format
	if _, err := ParseUUID(cellID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid cell ID format, must be a valid UUID"))
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

	logger.Debug("Creating metadata key '%s' for cell %s (user: %s)", metadata.Key, cellID, userName)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "cell", cellID, &metadata); err != nil {
		logger.Error("Failed to create cell metadata key '%s' for %s: %v", metadata.Key, cellID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Extract diagram ID for cache invalidation
	diagramID := c.Param("diagram_id")

	// Invalidate cell and diagram caches
	if h.cacheInvalidator != nil && diagramID != "" {
		event := InvalidationEvent{
			EntityType:    "cell",
			EntityID:      cellID,
			ParentType:    "diagram",
			ParentID:      diagramID,
			OperationType: "metadata_create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := h.cacheInvalidator.InvalidateSubResourceChange(c.Request.Context(), event); invErr != nil {
			logger.Error("Failed to invalidate caches after cell metadata creation: %v", invErr)
		}
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

// UpdateCellMetadata updates an existing metadata entry
// PUT /diagrams/{diagram_id}/cells/{cell_id}/metadata/{key}
func (h *CellHandler) UpdateCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateCellMetadata - updating metadata entry")

	// Extract cell ID and key from URL
	cellID := c.Param("cell_id")
	key := c.Param("key")

	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate cell ID format
	if _, err := ParseUUID(cellID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid cell ID format, must be a valid UUID"))
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

	logger.Debug("Updating metadata key '%s' for cell %s (user: %s)", key, cellID, userName)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "cell", cellID, &metadata); err != nil {
		logger.Error("Failed to update cell metadata key '%s' for %s: %v", key, cellID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Extract diagram ID for cache invalidation
	diagramID := c.Param("diagram_id")

	// Invalidate cell and diagram caches
	if h.cacheInvalidator != nil && diagramID != "" {
		event := InvalidationEvent{
			EntityType:    "cell",
			EntityID:      cellID,
			ParentType:    "diagram",
			ParentID:      diagramID,
			OperationType: "metadata_update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := h.cacheInvalidator.InvalidateSubResourceChange(c.Request.Context(), event); invErr != nil {
			logger.Error("Failed to invalidate caches after cell metadata update: %v", invErr)
		}
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

// DeleteCellMetadata deletes a metadata entry
// DELETE /diagrams/{diagram_id}/cells/{cell_id}/metadata/{key}
func (h *CellHandler) DeleteCellMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteCellMetadata - deleting metadata entry")

	// Extract cell ID and key from URL
	cellID := c.Param("cell_id")
	key := c.Param("key")

	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate cell ID format
	if _, err := ParseUUID(cellID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid cell ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for cell %s (user: %s)", key, cellID, userName)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "cell", cellID, key); err != nil {
		logger.Error("Failed to delete cell metadata key '%s' for %s: %v", key, cellID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	// Extract diagram ID for cache invalidation
	diagramID := c.Param("diagram_id")

	// Invalidate cell and diagram caches
	if h.cacheInvalidator != nil && diagramID != "" {
		event := InvalidationEvent{
			EntityType:    "cell",
			EntityID:      cellID,
			ParentType:    "diagram",
			ParentID:      diagramID,
			OperationType: "metadata_delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := h.cacheInvalidator.InvalidateSubResourceChange(c.Request.Context(), event); invErr != nil {
			logger.Error("Failed to invalidate caches after cell metadata deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted metadata key '%s' for cell %s", key, cellID)
	c.JSON(http.StatusNoContent, nil)
}

// PatchCell applies JSON patch operations to a cell (requires WebSocket connection for real-time updates)
// PATCH /diagrams/{diagram_id}/cells/{cell_id}
func (h *CellHandler) PatchCell(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("PatchCell - applying patch operations to cell")

	// Extract cell ID from URL
	cellID := c.Param("cell_id")
	if cellID == "" {
		HandleRequestError(c, InvalidIDError("Missing cell ID"))
		return
	}

	// Validate cell ID format
	if _, err := ParseUUID(cellID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid cell ID format, must be a valid UUID"))
		return
	}

	// Extract diagram ID from URL
	diagramID := c.Param("diagram_id")
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Get authenticated user
	userName, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse patch operations from request body
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(operations) == 0 {
		HandleRequestError(c, InvalidInputError("No patch operations provided"))
		return
	}

	// Validate patch authorization (ensure user can modify requested fields)
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	logger.Debug("Applying %d patch operations to cell %s in diagram %s (user: %s)",
		len(operations), cellID, diagramID, userName)

	// Note: Cell PATCH operations would typically be handled through the WebSocket hub
	// for real-time collaboration. This endpoint provides a REST alternative.

	// For now, return a message indicating that cell patches should use WebSocket
	response := map[string]interface{}{
		"message":          "Cell PATCH operations are optimized for real-time collaboration via WebSocket. Use the /ws/diagrams/{diagram_id} WebSocket endpoint for live cell updates.",
		"cell_id":          cellID,
		"diagram_id":       diagramID,
		"operations_count": len(operations),
		"websocket_url":    "/ws/diagrams/" + diagramID,
	}

	logger.Debug("Redirecting cell patch to WebSocket for cell %s", cellID)
	c.JSON(http.StatusAccepted, response)
}

// BatchPatchCells applies patch operations to multiple cells (optimized for collaboration)
// POST /diagrams/{diagram_id}/cells/batch/patch
func (h *CellHandler) BatchPatchCells(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BatchPatchCells - applying patch operations to multiple cells")

	// Extract diagram ID from URL
	diagramID := c.Param("diagram_id")
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Get authenticated user
	userName, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Define request structure for batch cell patches
	type BatchCellPatchRequest struct {
		Operations []struct {
			CellID     string           `json:"cell_id" binding:"required"`
			Operations []PatchOperation `json:"operations" binding:"required"`
		} `json:"operations" binding:"required"`
	}

	// Parse request body
	batchRequest, err := ParseRequestBody[BatchCellPatchRequest](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(batchRequest.Operations) == 0 {
		HandleRequestError(c, InvalidInputError("No cell patch operations provided"))
		return
	}

	if len(batchRequest.Operations) > 20 {
		HandleRequestError(c, InvalidInputError("Maximum 20 cell patch operations allowed per batch"))
		return
	}

	// Validate all operations
	for _, operation := range batchRequest.Operations {
		if _, err := ParseUUID(operation.CellID); err != nil {
			HandleRequestError(c, InvalidIDError("Invalid cell ID format in batch: "+operation.CellID))
			return
		}

		if len(operation.Operations) == 0 {
			HandleRequestError(c, InvalidInputError("No patch operations provided for cell: "+operation.CellID))
			return
		}

		// Validate patch authorization
		if err := ValidatePatchAuthorization(operation.Operations, userRole); err != nil {
			HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations on cell: "+operation.CellID))
			return
		}
	}

	logger.Debug("Processing batch patch for %d cells in diagram %s (user: %s)",
		len(batchRequest.Operations), diagramID, userName)

	// For batch cell operations, also redirect to WebSocket for optimal real-time performance
	response := map[string]interface{}{
		"message":       "Batch cell PATCH operations are optimized for real-time collaboration via WebSocket. Use the /ws/diagrams/{diagram_id} WebSocket endpoint for live batch cell updates.",
		"diagram_id":    diagramID,
		"cell_count":    len(batchRequest.Operations),
		"websocket_url": "/ws/diagrams/" + diagramID,
		"batch_support": true,
	}

	logger.Debug("Redirecting batch cell patch to WebSocket for diagram %s", diagramID)
	c.JSON(http.StatusAccepted, response)
}
