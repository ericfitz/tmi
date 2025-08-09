package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// SourceMetadataHandler provides handlers for source code metadata operations
type SourceMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewSourceMetadataHandler creates a new source code metadata handler
func NewSourceMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *SourceMetadataHandler {
	return &SourceMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// NewSourceMetadataHandlerSimple creates a new source metadata handler with default dependencies
func NewSourceMetadataHandlerSimple() *SourceMetadataHandler {
	// Create a simple in-memory metadata store for now
	// In production, this should be properly injected
	store := NewInMemoryMetadataStore()
	return &SourceMetadataHandler{
		metadataStore:    store,
		db:               nil,
		cache:            nil,
		cacheInvalidator: nil,
	}
}

// GetSourceMetadata retrieves all metadata for a source code reference
// GET /threat_models/{threat_model_id}/sources/{source_id}/metadata
func (h *SourceMetadataHandler) GetSourceMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetSourceMetadata - retrieving metadata for source code reference")

	// Extract source ID from URL
	sourceID := c.Param("source_id")
	if sourceID == "" {
		HandleRequestError(c, InvalidIDError("Missing source ID"))
		return
	}

	// Validate source ID format
	if _, err := ParseUUID(sourceID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid source ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for source %s (user: %s)", sourceID, userName)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "source", sourceID)
	if err != nil {
		logger.Error("Failed to retrieve source metadata for %s: %v", sourceID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for source %s", len(metadata), sourceID)
	c.JSON(http.StatusOK, metadata)
}

// GetSourceMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}
func (h *SourceMetadataHandler) GetSourceMetadataByKey(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("GetSourceMetadataByKey - retrieving specific metadata entry")

	// Extract source ID and key from URL
	sourceID := c.Param("source_id")
	key := c.Param("key")

	if sourceID == "" {
		HandleRequestError(c, InvalidIDError("Missing source ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate source ID format
	if _, err := ParseUUID(sourceID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid source ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for source %s (user: %s)", key, sourceID, userName)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "source", sourceID, key)
	if err != nil {
		logger.Error("Failed to retrieve source metadata key '%s' for %s: %v", key, sourceID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for source %s", key, sourceID)
	c.JSON(http.StatusOK, metadata)
}

// CreateSourceMetadata creates a new metadata entry for a source code reference
// POST /threat_models/{threat_model_id}/sources/{source_id}/metadata
func (h *SourceMetadataHandler) CreateSourceMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("CreateSourceMetadata - creating new metadata entry")

	// Extract source ID from URL
	sourceID := c.Param("source_id")
	if sourceID == "" {
		HandleRequestError(c, InvalidIDError("Missing source ID"))
		return
	}

	// Validate source ID format
	if _, err := ParseUUID(sourceID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid source ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body using unified validation framework
	metadata, err := ValidateAndParseRequest[Metadata](c, ValidationConfigs["metadata_create"])
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Creating metadata key '%s' for source %s (user: %s)", metadata.Key, sourceID, userName)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "source", sourceID, metadata); err != nil {
		logger.Error("Failed to create source metadata key '%s' for %s: %v", metadata.Key, sourceID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "source", sourceID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for source %s", metadata.Key, sourceID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateSourceMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}
func (h *SourceMetadataHandler) UpdateSourceMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("UpdateSourceMetadata - updating metadata entry")

	// Extract source ID and key from URL
	sourceID := c.Param("source_id")
	key := c.Param("key")

	if sourceID == "" {
		HandleRequestError(c, InvalidIDError("Missing source ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate source ID format
	if _, err := ParseUUID(sourceID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid source ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body using unified validation framework
	metadata, err := ValidateAndParseRequest[Metadata](c, ValidationConfigs["metadata_update"])
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Ensure the key matches the URL parameter
	metadata.Key = key

	logger.Debug("Updating metadata key '%s' for source %s (user: %s)", key, sourceID, userName)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "source", sourceID, metadata); err != nil {
		logger.Error("Failed to update source metadata key '%s' for %s: %v", key, sourceID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "source", sourceID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for source %s", key, sourceID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteSourceMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}
func (h *SourceMetadataHandler) DeleteSourceMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("DeleteSourceMetadata - deleting metadata entry")

	// Extract source ID and key from URL
	sourceID := c.Param("source_id")
	key := c.Param("key")

	if sourceID == "" {
		HandleRequestError(c, InvalidIDError("Missing source ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate source ID format
	if _, err := ParseUUID(sourceID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid source ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for source %s (user: %s)", key, sourceID, userName)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "source", sourceID, key); err != nil {
		logger.Error("Failed to delete source metadata key '%s' for %s: %v", key, sourceID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for source %s", key, sourceID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateSourceMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/sources/{source_id}/metadata/bulk
func (h *SourceMetadataHandler) BulkCreateSourceMetadata(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BulkCreateSourceMetadata - creating multiple metadata entries")

	// Extract source ID from URL
	sourceID := c.Param("source_id")
	if sourceID == "" {
		HandleRequestError(c, InvalidIDError("Missing source ID"))
		return
	}

	// Validate source ID format
	if _, err := ParseUUID(sourceID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid source ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of metadata using unified validation framework
	metadataList, err := ValidateAndParseRequest[[]Metadata](c, ValidationConfig{
		ProhibitedFields: []string{},
		CustomValidators: []ValidatorFunc{
			func(data interface{}) error {
				list := data.(*[]Metadata)

				if len(*list) == 0 {
					return InvalidInputError("No metadata entries provided")
				}

				if len(*list) > 20 {
					return InvalidInputError("Maximum 20 metadata entries allowed per bulk operation")
				}

				// Check for duplicate keys within the request
				keyMap := make(map[string]bool)
				for _, metadata := range *list {
					if keyMap[metadata.Key] {
						return InvalidInputError("Duplicate metadata key found: " + metadata.Key)
					}
					keyMap[metadata.Key] = true
				}

				return nil
			},
		},
		Operation: "POST",
	})
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Bulk creating %d metadata entries for source %s (user: %s)",
		len(*metadataList), sourceID, userName)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "source", sourceID, *metadataList); err != nil {
		logger.Error("Failed to bulk create source metadata for %s: %v", sourceID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "source", sourceID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, *metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for source %s", len(*metadataList), sourceID)
	c.JSON(http.StatusCreated, createdMetadata)
}
