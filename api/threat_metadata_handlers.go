package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// ThreatMetadataHandler provides handlers for threat metadata operations
type ThreatMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewThreatMetadataHandler creates a new threat metadata handler
func NewThreatMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *ThreatMetadataHandler {
	return &ThreatMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetThreatMetadata retrieves all metadata for a threat
// GET /threat_models/{threat_model_id}/threats/{threat_id}/metadata
func (h *ThreatMetadataHandler) GetThreatMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetThreatMetadata - retrieving metadata for threat")

	// Extract threat ID from URL
	threatID := c.Param("threat_id")
	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for threat %s (user: %s)", threatID, userName)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "threat", threatID)
	if err != nil {
		logger.Error("Failed to retrieve threat metadata for %s: %v", threatID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for threat %s", len(metadata), threatID)
	c.JSON(http.StatusOK, metadata)
}

// GetThreatMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}
func (h *ThreatMetadataHandler) GetThreatMetadataByKey(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetThreatMetadataByKey - retrieving specific metadata entry")

	// Extract threat ID and key from URL
	threatID := c.Param("threat_id")
	key := c.Param("key")

	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for threat %s (user: %s)", key, threatID, userName)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "threat", threatID, key)
	if err != nil {
		logger.Error("Failed to retrieve threat metadata key '%s' for %s: %v", key, threatID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for threat %s", key, threatID)
	c.JSON(http.StatusOK, metadata)
}

// CreateThreatMetadata creates a new metadata entry for a threat
// POST /threat_models/{threat_model_id}/threats/{threat_id}/metadata
func (h *ThreatMetadataHandler) CreateThreatMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateThreatMetadata - creating new metadata entry")

	// Extract threat ID from URL
	threatID := c.Param("threat_id")
	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
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

	logger.Debug("Creating metadata key '%s' for threat %s (user: %s)", metadata.Key, threatID, userName)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "threat", threatID, &metadata); err != nil {
		logger.Error("Failed to create threat metadata key '%s' for %s: %v", metadata.Key, threatID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "threat", threatID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for threat %s", metadata.Key, threatID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateThreatMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}
func (h *ThreatMetadataHandler) UpdateThreatMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateThreatMetadata - updating metadata entry")

	// Extract threat ID and key from URL
	threatID := c.Param("threat_id")
	key := c.Param("key")

	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
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

	logger.Debug("Updating metadata key '%s' for threat %s (user: %s)", key, threatID, userName)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "threat", threatID, &metadata); err != nil {
		logger.Error("Failed to update threat metadata key '%s' for %s: %v", key, threatID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "threat", threatID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for threat %s", key, threatID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteThreatMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}
func (h *ThreatMetadataHandler) DeleteThreatMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteThreatMetadata - deleting metadata entry")

	// Extract threat ID and key from URL
	threatID := c.Param("threat_id")
	key := c.Param("key")

	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for threat %s (user: %s)", key, threatID, userName)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "threat", threatID, key); err != nil {
		logger.Error("Failed to delete threat metadata key '%s' for %s: %v", key, threatID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for threat %s", key, threatID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateThreatMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk
func (h *ThreatMetadataHandler) BulkCreateThreatMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BulkCreateThreatMetadata - creating multiple metadata entries")

	// Extract threat ID from URL
	threatID := c.Param("threat_id")
	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
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

	logger.Debug("Bulk creating %d metadata entries for threat %s (user: %s)",
		len(metadataList), threatID, userName)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "threat", threatID, metadataList); err != nil {
		logger.Error("Failed to bulk create threat metadata for %s: %v", threatID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "threat", threatID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for threat %s", len(metadataList), threatID)
	c.JSON(http.StatusCreated, createdMetadata)
}
