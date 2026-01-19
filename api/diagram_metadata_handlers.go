package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
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

// Direct diagram metadata handlers for /diagrams/:id/metadata endpoints

// GetDirectDiagramMetadata retrieves all metadata for a diagram via direct route
// GET /diagrams/{id}/metadata
func (h *DiagramMetadataHandler) GetDirectDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for diagram %s (user: %s)", diagramID, userEmail)

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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for diagram %s (user: %s)", key, diagramID, userEmail)

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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Creating metadata key '%s' for diagram %s (user: %s)", metadata.Key, diagramID, userEmail)

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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Updating metadata key '%s' for diagram %s (user: %s)", key, diagramID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "diagram", diagramID, &metadata); err != nil {
		logger.Error("Failed to update diagram metadata key '%s' for %s: %v", key, diagramID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to update metadata"))
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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for diagram %s (user: %s)", key, diagramID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "diagram", diagramID, key); err != nil {
		logger.Error("Failed to delete diagram metadata key '%s' for %s: %v", key, diagramID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for diagram %s", key, diagramID)
	c.Status(http.StatusNoContent)
}

// Direct diagram cell metadata handlers for /diagrams/:id/cells/:cell_id/metadata endpoints

// GetDirectDiagramCellMetadata retrieves all metadata for a diagram cell via direct route
// GET /diagrams/{id}/cells/{cell_id}/metadata
func (h *DiagramMetadataHandler) GetDirectDiagramCellMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for cell %s in diagram %s (user: %s)", cellID, diagramID, userEmail)

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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for cell %s in diagram %s (user: %s)", key, cellID, diagramID, userEmail)

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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Creating metadata key '%s' for cell %s in diagram %s (user: %s)", metadata.Key, cellID, diagramID, userEmail)

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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Updating metadata key '%s' for cell %s in diagram %s (user: %s)", key, cellID, diagramID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "cell", cellID, &metadata); err != nil {
		logger.Error("Failed to update cell metadata key '%s' for %s: %v", key, cellID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to update metadata"))
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
	logger := slogging.GetContextLogger(c)
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for cell %s in diagram %s (user: %s)", key, cellID, diagramID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "cell", cellID, key); err != nil {
		logger.Error("Failed to delete cell metadata key '%s' for %s: %v", key, cellID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for cell %s", key, cellID)
	c.Status(http.StatusNoContent)
}

// Threat model diagram metadata handlers for /threat_models/:threat_model_id/diagrams/:diagram_id/metadata endpoints

// GetThreatModelDiagramMetadata retrieves all metadata for a diagram within a threat model
// GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata
func (h *DiagramMetadataHandler) GetThreatModelDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreatModelDiagramMetadata - retrieving metadata for diagram in threat model")

	// Extract threat model ID and diagram ID from URL
	threatModelID := c.Param("threat_model_id")
	diagramID := c.Param("diagram_id")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate threat model ID and diagram ID formats
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for diagram %s in threat model %s (user: %s)", diagramID, threatModelID, userEmail)

	// Get metadata from store using diagram entity type
	metadata, err := h.metadataStore.List(c.Request.Context(), "diagram", diagramID)
	if err != nil {
		logger.Error("Failed to retrieve diagram metadata for %s: %v", diagramID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for diagram %s", len(metadata), diagramID)
	c.JSON(http.StatusOK, metadata)
}

// GetThreatModelDiagramMetadataByKey retrieves a specific metadata entry by key for a diagram within a threat model
// GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
func (h *DiagramMetadataHandler) GetThreatModelDiagramMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreatModelDiagramMetadataByKey - retrieving specific metadata entry for diagram in threat model")

	// Extract threat model ID, diagram ID, and key from URL
	threatModelID := c.Param("threat_model_id")
	diagramID := c.Param("diagram_id")
	key := c.Param("key")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat model ID and diagram ID formats
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for diagram %s in threat model %s (user: %s)", key, diagramID, threatModelID, userEmail)

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

// CreateThreatModelDiagramMetadata creates a new metadata entry for a diagram within a threat model
// POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata
func (h *DiagramMetadataHandler) CreateThreatModelDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateThreatModelDiagramMetadata - creating new metadata entry for diagram in threat model")

	// Extract threat model ID and diagram ID from URL
	threatModelID := c.Param("threat_model_id")
	diagramID := c.Param("diagram_id")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate threat model ID and diagram ID formats
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Creating metadata key '%s' for diagram %s in threat model %s (user: %s)", metadata.Key, diagramID, threatModelID, userEmail)

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

// UpdateThreatModelDiagramMetadata updates an existing metadata entry for a diagram within a threat model
// PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
func (h *DiagramMetadataHandler) UpdateThreatModelDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateThreatModelDiagramMetadata - updating metadata entry for diagram in threat model")

	// Extract threat model ID, diagram ID, and key from URL
	threatModelID := c.Param("threat_model_id")
	diagramID := c.Param("diagram_id")
	key := c.Param("key")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat model ID and diagram ID formats
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Updating metadata key '%s' for diagram %s in threat model %s (user: %s)", key, diagramID, threatModelID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "diagram", diagramID, &metadata); err != nil {
		logger.Error("Failed to update diagram metadata key '%s' for %s: %v", key, diagramID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to update metadata"))
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

// DeleteThreatModelDiagramMetadata deletes a metadata entry for a diagram within a threat model
// DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
func (h *DiagramMetadataHandler) DeleteThreatModelDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteThreatModelDiagramMetadata - deleting metadata entry for diagram in threat model")

	// Extract threat model ID, diagram ID, and key from URL
	threatModelID := c.Param("threat_model_id")
	diagramID := c.Param("diagram_id")
	key := c.Param("key")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat model ID and diagram ID formats
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for diagram %s in threat model %s (user: %s)", key, diagramID, threatModelID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "diagram", diagramID, key); err != nil {
		logger.Error("Failed to delete diagram metadata key '%s' for %s: %v", key, diagramID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for diagram %s", key, diagramID)
	c.Status(http.StatusNoContent)
}

// BulkCreateDirectDiagramMetadata creates multiple metadata entries for a diagram via direct route
// POST /diagrams/{id}/metadata/bulk
func (h *DiagramMetadataHandler) BulkCreateDirectDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateDirectDiagramMetadata - creating multiple metadata entries")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Bulk creating %d metadata entries for diagram %s (user: %s)",
		len(metadataList), diagramID, userEmail)

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

// BulkUpdateDirectDiagramMetadata updates multiple metadata entries for a diagram via direct route
// PUT /diagrams/{id}/metadata/bulk
func (h *DiagramMetadataHandler) BulkUpdateDirectDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateDirectDiagramMetadata - updating multiple metadata entries")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Bulk updating %d metadata entries for diagram %s (user: %s)",
		len(metadataList), diagramID, userEmail)

	// Update metadata entries in store
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "diagram", diagramID, metadataList); err != nil {
		logger.Error("Failed to bulk update diagram metadata for %s: %v", diagramID, err)
		HandleRequestError(c, ServerError("Failed to update metadata entries"))
		return
	}

	// Retrieve the updated metadata to return with timestamps
	updatedMetadata, err := h.metadataStore.List(c.Request.Context(), "diagram", diagramID)
	if err != nil {
		// Log error but still return success since update succeeded
		logger.Error("Failed to retrieve updated metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk updated %d metadata entries for diagram %s", len(metadataList), diagramID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// BulkCreateThreatModelDiagramMetadata creates multiple metadata entries for a diagram within a threat model
// POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk
func (h *DiagramMetadataHandler) BulkCreateThreatModelDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateThreatModelDiagramMetadata - creating multiple metadata entries for diagram in threat model")

	// Extract threat model ID and diagram ID from URL
	threatModelID := c.Param("threat_model_id")
	diagramID := c.Param("diagram_id")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate threat model ID and diagram ID formats
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Bulk creating %d metadata entries for diagram %s in threat model %s (user: %s)",
		len(metadataList), diagramID, threatModelID, userEmail)

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

// BulkUpdateThreatModelDiagramMetadata updates multiple metadata entries for a diagram within a threat model
// PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk
func (h *DiagramMetadataHandler) BulkUpdateThreatModelDiagramMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateThreatModelDiagramMetadata - updating multiple metadata entries for diagram in threat model")

	// Extract threat model ID and diagram ID from URL
	threatModelID := c.Param("threat_model_id")
	diagramID := c.Param("diagram_id")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if diagramID == "" {
		HandleRequestError(c, InvalidIDError("Missing diagram ID"))
		return
	}

	// Validate threat model ID and diagram ID formats
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Bulk updating %d metadata entries for diagram %s in threat model %s (user: %s)",
		len(metadataList), diagramID, threatModelID, userEmail)

	// Update metadata entries in store
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "diagram", diagramID, metadataList); err != nil {
		logger.Error("Failed to bulk update diagram metadata for %s: %v", diagramID, err)
		HandleRequestError(c, ServerError("Failed to update metadata entries"))
		return
	}

	// Retrieve the updated metadata to return with timestamps
	updatedMetadata, err := h.metadataStore.List(c.Request.Context(), "diagram", diagramID)
	if err != nil {
		// Log error but still return success since update succeeded
		logger.Error("Failed to retrieve updated metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk updated %d metadata entries for diagram %s", len(metadataList), diagramID)
	c.JSON(http.StatusOK, updatedMetadata)
}
