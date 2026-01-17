package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AssetMetadataHandler provides handlers for asset metadata operations
type AssetMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewAssetMetadataHandler creates a new asset metadata handler
func NewAssetMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *AssetMetadataHandler {
	return &AssetMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetAssetMetadata retrieves all metadata for a asset
// GET /threat_models/{threat_model_id}/assets/{asset_id}/metadata
func (h *AssetMetadataHandler) GetAssetMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetAssetMetadata - retrieving metadata for asset")

	// Extract and validate asset ID
	assetUUID, err := ExtractUUID(c, "asset_id")
	if err != nil {
		return // Error response already sent
	}
	assetID := assetUUID.String()

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for asset %s (user: %s)", assetID, userEmail)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "asset", assetID)
	if err != nil {
		logger.Error("Failed to retrieve asset metadata for %s: %v", assetID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for asset %s", len(metadata), assetID)
	c.JSON(http.StatusOK, metadata)
}

// GetAssetMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key}
func (h *AssetMetadataHandler) GetAssetMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetAssetMetadataByKey - retrieving specific metadata entry")

	// Extract and validate asset ID
	assetUUID, err := ExtractUUID(c, "asset_id")
	if err != nil {
		return // Error response already sent
	}
	assetID := assetUUID.String()

	// Extract metadata key
	key := c.Param("key")
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for asset %s (user: %s)", key, assetID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "asset", assetID, key)
	if err != nil {
		logger.Error("Failed to retrieve asset metadata key '%s' for %s: %v", key, assetID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for asset %s", key, assetID)
	c.JSON(http.StatusOK, metadata)
}

// CreateAssetMetadata creates a new metadata entry for a asset
// POST /threat_models/{threat_model_id}/assets/{asset_id}/metadata
func (h *AssetMetadataHandler) CreateAssetMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateAssetMetadata - creating new metadata entry")

	// Extract asset ID from URL
	assetID := c.Param("asset_id")
	if assetID == "" {
		HandleRequestError(c, InvalidIDError("Missing asset ID"))
		return
	}

	// Validate asset ID format
	if _, err := ParseUUID(assetID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid asset ID format, must be a valid UUID"))
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

	logger.Debug("Creating metadata key '%s' for asset %s (user: %s)", metadata.Key, assetID, userEmail)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "asset", assetID, &metadata); err != nil {
		logger.Error("Failed to create asset metadata key '%s' for %s: %v", metadata.Key, assetID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "asset", assetID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for asset %s", metadata.Key, assetID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateAssetMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key}
func (h *AssetMetadataHandler) UpdateAssetMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateAssetMetadata - updating metadata entry")

	// Extract asset ID and key from URL
	assetID := c.Param("asset_id")
	key := c.Param("key")

	if assetID == "" {
		HandleRequestError(c, InvalidIDError("Missing asset ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate asset ID format
	if _, err := ParseUUID(assetID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid asset ID format, must be a valid UUID"))
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

	logger.Debug("Updating metadata key '%s' for asset %s (user: %s)", key, assetID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "asset", assetID, &metadata); err != nil {
		logger.Error("Failed to update asset metadata key '%s' for %s: %v", key, assetID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "asset", assetID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for asset %s", key, assetID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteAssetMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key}
func (h *AssetMetadataHandler) DeleteAssetMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteAssetMetadata - deleting metadata entry")

	// Extract asset ID and key from URL
	assetID := c.Param("asset_id")
	key := c.Param("key")

	if assetID == "" {
		HandleRequestError(c, InvalidIDError("Missing asset ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate asset ID format
	if _, err := ParseUUID(assetID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid asset ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for asset %s (user: %s)", key, assetID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "asset", assetID, key); err != nil {
		logger.Error("Failed to delete asset metadata key '%s' for %s: %v", key, assetID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for asset %s", key, assetID)
	c.Status(http.StatusNoContent)
}

// BulkCreateAssetMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/assets/{asset_id}/metadata/bulk
func (h *AssetMetadataHandler) BulkCreateAssetMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateAssetMetadata - creating multiple metadata entries")

	// Extract asset ID from URL
	assetID := c.Param("asset_id")
	if assetID == "" {
		HandleRequestError(c, InvalidIDError("Missing asset ID"))
		return
	}

	// Validate asset ID format
	if _, err := ParseUUID(assetID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid asset ID format, must be a valid UUID"))
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

	logger.Debug("Bulk creating %d metadata entries for asset %s (user: %s)",
		len(metadataList), assetID, userEmail)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "asset", assetID, metadataList); err != nil {
		logger.Error("Failed to bulk create asset metadata for %s: %v", assetID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "asset", assetID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for asset %s", len(metadataList), assetID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// BulkUpdateAssetMetadata updates multiple metadata entries in a single request
// PUT /threat_models/{threat_model_id}/assets/{asset_id}/metadata/bulk
func (h *AssetMetadataHandler) BulkUpdateAssetMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateAssetMetadata - updating multiple metadata entries")

	// Extract parameters from URL
	threatmodelid := c.Param("threat_model_id")
	assetid := c.Param("asset_id")

	if threatmodelid == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model id ID"))
		return
	}

	// Validate threat model id ID format
	if _, err := ParseUUID(threatmodelid); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model id ID format, must be a valid UUID"))
		return
	}

	if assetid == "" {
		HandleRequestError(c, InvalidIDError("Missing asset id ID"))
		return
	}

	// Validate asset id ID format
	if _, err := ParseUUID(assetid); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid asset id ID format, must be a valid UUID"))
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

	logger.Debug("Bulk updating %d metadata entries for asset %s in threat model id %s (user: %s)",
		len(metadataList), assetid, threatmodelid, userEmail)

	// Update metadata entries in store
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "asset", assetid, metadataList); err != nil {
		logger.Error("Failed to bulk update asset metadata for %s: %v", assetid, err)
		HandleRequestError(c, ServerError("Failed to update metadata entries"))
		return
	}

	// Retrieve the updated metadata to return with timestamps
	updatedMetadata, err := h.metadataStore.List(c.Request.Context(), "asset", assetid)
	if err != nil {
		// Log error but still return success since update succeeded
		logger.Error("Failed to retrieve updated metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk updated %d metadata entries for asset %s", len(metadataList), assetid)
	c.JSON(http.StatusOK, updatedMetadata)
}
