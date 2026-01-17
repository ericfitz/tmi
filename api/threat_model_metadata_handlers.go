package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ThreatModelMetadataHandler provides handlers for threat model metadata operations
type ThreatModelMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewThreatModelMetadataHandler creates a new threat model metadata handler
func NewThreatModelMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *ThreatModelMetadataHandler {
	return &ThreatModelMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetThreatModelMetadata retrieves all metadata for a threat model
// GET /threat_models/{threat_model_id}/metadata
func (h *ThreatModelMetadataHandler) GetThreatModelMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreatModelMetadata - retrieving metadata for threat model")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for threat model %s (user: %s)", threatModelID, userEmail)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "threat_model", threatModelID)
	if err != nil {
		logger.Error("Failed to retrieve threat model metadata for %s: %v", threatModelID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for threat model %s", len(metadata), threatModelID)
	c.JSON(http.StatusOK, metadata)
}

// GetThreatModelMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/metadata/{key}
func (h *ThreatModelMetadataHandler) GetThreatModelMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreatModelMetadataByKey - retrieving specific metadata entry")

	// Extract threat model ID and key from URL
	threatModelID := c.Param("threat_model_id")
	key := c.Param("key")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for threat model %s (user: %s)", key, threatModelID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "threat_model", threatModelID, key)
	if err != nil {
		logger.Error("Failed to retrieve threat model metadata key '%s' for %s: %v", key, threatModelID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for threat model %s", key, threatModelID)
	c.JSON(http.StatusOK, metadata)
}

// CreateThreatModelMetadata creates a new metadata entry for a threat model
// POST /threat_models/{threat_model_id}/metadata
func (h *ThreatModelMetadataHandler) CreateThreatModelMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateThreatModelMetadata - creating new metadata entry")

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

	logger.Debug("Creating metadata key '%s' for threat model %s (user: %s)", metadata.Key, threatModelID, userEmail)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "threat_model", threatModelID, &metadata); err != nil {
		logger.Error("Failed to create threat model metadata key '%s' for %s: %v", metadata.Key, threatModelID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "threat_model", threatModelID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for threat model %s", metadata.Key, threatModelID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateThreatModelMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/metadata/{key}
func (h *ThreatModelMetadataHandler) UpdateThreatModelMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateThreatModelMetadata - updating metadata entry")

	// Extract threat model ID and key from URL
	threatModelID := c.Param("threat_model_id")
	key := c.Param("key")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse request body - only value is required for updates (key comes from URL)
	// Use a map to allow flexible input without requiring key in body
	var requestBody map[string]string
	if err := c.ShouldBindJSON(&requestBody); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Extract value from request body
	value, hasValue := requestBody["value"]
	if !hasValue || value == "" {
		HandleRequestError(c, InvalidInputError("Missing required field: value"))
		return
	}

	// Create metadata struct with key from URL and value from body
	metadata := Metadata{
		Key:   key,
		Value: value,
	}

	logger.Debug("Updating metadata key '%s' for threat model %s (user: %s)", key, threatModelID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "threat_model", threatModelID, &metadata); err != nil {
		logger.Error("Failed to update threat model metadata key '%s' for %s: %v", key, threatModelID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "threat_model", threatModelID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for threat model %s", key, threatModelID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteThreatModelMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/metadata/{key}
func (h *ThreatModelMetadataHandler) DeleteThreatModelMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteThreatModelMetadata - deleting metadata entry")

	// Extract threat model ID and key from URL
	threatModelID := c.Param("threat_model_id")
	key := c.Param("key")

	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for threat model %s (user: %s)", key, threatModelID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "threat_model", threatModelID, key); err != nil {
		logger.Error("Failed to delete threat model metadata key '%s' for %s: %v", key, threatModelID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for threat model %s", key, threatModelID)
	c.Status(http.StatusNoContent)
}

// BulkCreateThreatModelMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/metadata/bulk
func (h *ThreatModelMetadataHandler) BulkCreateThreatModelMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateThreatModelMetadata - creating multiple metadata entries")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of metadata using OpenAPI validation
	var metadataList []Metadata
	if err := c.ShouldBindJSON(&metadataList); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Additional validation
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

	logger.Debug("Bulk creating %d metadata entries for threat model %s (user: %s)",
		len(metadataList), threatModelID, userEmail)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "threat_model", threatModelID, metadataList); err != nil {
		logger.Error("Failed to bulk create threat model metadata for %s: %v", threatModelID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "threat_model", threatModelID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for threat model %s", len(metadataList), threatModelID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// BulkUpdateThreatModelMetadata updates multiple metadata entries in a single request
// PUT /threat_models/{threat_model_id}/metadata/bulk
func (h *ThreatModelMetadataHandler) BulkUpdateThreatModelMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateThreatModelMetadata - updating multiple metadata entries")

	// Extract parameters from URL
	threatmodelid := c.Param("threat_model_id")

	if threatmodelid == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model id ID"))
		return
	}

	// Validate threat model id ID format
	if _, err := ParseUUID(threatmodelid); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model id ID format, must be a valid UUID"))
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

	logger.Debug("Bulk updating %d metadata entries for threat_model %s (user: %s)",
		len(metadataList), threatmodelid, userEmail)

	// Update metadata entries in store
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "threat_model", threatmodelid, metadataList); err != nil {
		logger.Error("Failed to bulk update threat_model metadata for %s: %v", threatmodelid, err)
		HandleRequestError(c, ServerError("Failed to update metadata entries"))
		return
	}

	// Retrieve the updated metadata to return with timestamps
	updatedMetadata, err := h.metadataStore.List(c.Request.Context(), "threat_model", threatmodelid)
	if err != nil {
		// Log error but still return success since update succeeded
		logger.Error("Failed to retrieve updated metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk updated %d metadata entries for threat_model %s", len(metadataList), threatmodelid)
	c.JSON(http.StatusOK, updatedMetadata)
}
