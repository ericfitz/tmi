package api

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AssetSubResourceHandler provides handlers for asset sub-resource operations
type AssetSubResourceHandler struct {
	assetStore       AssetStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewAssetSubResourceHandler creates a new asset sub-resource handler
func NewAssetSubResourceHandler(assetStore AssetStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *AssetSubResourceHandler {
	return &AssetSubResourceHandler{
		assetStore:       assetStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetAssets retrieves all assets for a threat model with pagination
// GET /threat_models/{threat_model_id}/assets
func (h *AssetSubResourceHandler) GetAssets(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetAssets - retrieving assets for threat model")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving assets for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, userEmail, offset, limit)

	// Get assets from store (authorization is handled by middleware)
	assets, err := h.assetStore.List(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve assets: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve assets"))
		return
	}

	logger.Debug("Successfully retrieved %d assets", len(assets))
	c.JSON(http.StatusOK, assets)
}

// GetAsset retrieves a specific asset by ID
// GET /threat_models/{threat_model_id}/assets/{asset_id}
func (h *AssetSubResourceHandler) GetAsset(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetAsset - retrieving specific asset")

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

	logger.Debug("Retrieving asset %s (user: %s)", assetID, userEmail)

	// Get asset from store
	asset, err := h.assetStore.Get(c.Request.Context(), assetID)
	if err != nil {
		logger.Error("Failed to retrieve asset %s: %v", assetID, err)
		HandleRequestError(c, NotFoundError("Asset not found"))
		return
	}

	logger.Debug("Successfully retrieved asset %s", assetID)
	c.JSON(http.StatusOK, asset)
}

// CreateAsset creates a new asset in a threat model
// POST /threat_models/{threat_model_id}/assets
func (h *AssetSubResourceHandler) CreateAsset(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateAsset - creating new asset")

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

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["asset_create"]
	asset, err := ValidateAndParseRequest[Asset](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Generate UUID if not provided
	if asset.Id == nil {
		id := uuid.New()
		asset.Id = &id
	}

	logger.Debug("Creating asset %s in threat model %s (user: %s)",
		asset.Id.String(), threatModelID, userEmail)

	// Create asset in store
	if err := h.assetStore.Create(c.Request.Context(), asset, threatModelID); err != nil {
		logger.Error("Failed to create asset: %v", err)
		HandleRequestError(c, ServerError("Failed to create asset"))
		return
	}

	logger.Debug("Successfully created asset %s", asset.Id.String())
	c.JSON(http.StatusCreated, asset)
}

// UpdateAsset updates an existing asset
// PUT /threat_models/{threat_model_id}/assets/{asset_id}
func (h *AssetSubResourceHandler) UpdateAsset(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateAsset - updating existing asset")

	// Extract asset ID from URL
	assetID := c.Param("asset_id")
	if assetID == "" {
		HandleRequestError(c, InvalidIDError("Missing asset ID"))
		return
	}

	// Validate asset ID format
	assetUUID, err := ParseUUID(assetID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid asset ID format, must be a valid UUID"))
		return
	}

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
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

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["asset_update"]
	asset, err := ValidateAndParseRequest[Asset](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set ID from URL (override any value in body)
	asset.Id = &assetUUID

	logger.Debug("Updating asset %s (user: %s)", assetID, userEmail)

	// Update asset in store
	if err := h.assetStore.Update(c.Request.Context(), asset, threatModelID); err != nil {
		logger.Error("Failed to update asset %s: %v", assetID, err)
		HandleRequestError(c, ServerError("Failed to update asset"))
		return
	}

	logger.Debug("Successfully updated asset %s", assetID)
	c.JSON(http.StatusOK, asset)
}

// DeleteAsset deletes an asset
// DELETE /threat_models/{threat_model_id}/assets/{asset_id}
func (h *AssetSubResourceHandler) DeleteAsset(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteAsset - deleting asset")

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

	logger.Debug("Deleting asset %s (user: %s)", assetID, userEmail)

	// Delete asset from store
	if err := h.assetStore.Delete(c.Request.Context(), assetID); err != nil {
		logger.Error("Failed to delete asset %s: %v", assetID, err)
		HandleRequestError(c, ServerError("Failed to delete asset"))
		return
	}

	logger.Debug("Successfully deleted asset %s", assetID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateAssets creates multiple assets in a single request
// POST /threat_models/{threat_model_id}/assets/bulk
func (h *AssetSubResourceHandler) BulkCreateAssets(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateAssets - creating multiple assets")

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

	// Parse and validate request body as array of assets
	var assets []Asset
	if err := c.ShouldBindJSON(&assets); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(assets) == 0 {
		HandleRequestError(c, InvalidInputError("No assets provided"))
		return
	}

	if len(assets) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 assets allowed per bulk operation"))
		return
	}

	// Validate each asset
	validTypes := map[AssetType]bool{
		"data": true, "hardware": true, "software": true,
		"infrastructure": true, "service": true, "personnel": true,
	}

	for _, asset := range assets {
		if asset.Name == "" {
			HandleRequestError(c, InvalidInputError("Asset name is required for all assets"))
			return
		}
		if asset.Type == "" {
			HandleRequestError(c, InvalidInputError("Asset type is required for all assets"))
			return
		}
		if !validTypes[asset.Type] {
			HandleRequestError(c, InvalidInputError("Invalid asset type: "+string(asset.Type)))
			return
		}
	}

	// Generate UUIDs for assets that don't have them
	for i := range assets {
		asset := &assets[i]
		if asset.Id == nil {
			id := uuid.New()
			asset.Id = &id
		}
	}

	logger.Debug("Bulk creating %d assets in threat model %s (user: %s)",
		len(assets), threatModelID, userEmail)

	// Create assets in store
	if err := h.assetStore.BulkCreate(c.Request.Context(), assets, threatModelID); err != nil {
		logger.Error("Failed to bulk create assets: %v", err)
		HandleRequestError(c, ServerError("Failed to create assets"))
		return
	}

	logger.Debug("Successfully bulk created %d assets", len(assets))
	c.JSON(http.StatusCreated, assets)
}

// PatchAsset applies JSON patch operations to an asset
// PATCH /threat_models/{threat_model_id}/assets/{asset_id}
func (h *AssetSubResourceHandler) PatchAsset(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchAsset - applying patch operations to asset")

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
	userEmail, _, userRole, err := ValidateAuthenticatedUser(c)
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

	// Validate patch authorization
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	logger.Debug("Applying %d patch operations to asset %s (user: %s)",
		len(operations), assetID, userEmail)

	// Apply patch operations
	updatedAsset, err := h.assetStore.Patch(c.Request.Context(), assetID, operations)
	if err != nil {
		HandleRequestError(c, ServerError("Failed to patch asset"))
		return
	}

	logger.Info("Successfully patched asset %s (user: %s)", assetID, userEmail)
	c.JSON(http.StatusOK, updatedAsset)
}

// BulkUpdateAssets updates or creates multiple assets (upsert operation)
// PUT /threat_models/{threat_model_id}/assets/bulk
func (h *AssetSubResourceHandler) BulkUpdateAssets(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateAssets - upserting multiple assets")

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

	// Parse and validate request body as array of assets
	var assets []Asset
	if err := c.ShouldBindJSON(&assets); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(assets) == 0 {
		HandleRequestError(c, InvalidInputError("No assets provided"))
		return
	}

	if len(assets) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 assets allowed per bulk operation"))
		return
	}

	// Validate each asset
	for _, asset := range assets {
		if asset.Id == nil {
			HandleRequestError(c, InvalidInputError("Asset ID is required for all assets in bulk update"))
			return
		}
		if asset.Name == "" {
			HandleRequestError(c, InvalidInputError("Asset name is required for all assets"))
			return
		}
	}

	logger.Debug("Bulk updating %d assets for threat model %s (user: %s)", len(assets), threatModelID, userEmail)

	// Upsert each asset
	upsertedAssets := make([]Asset, 0, len(assets))
	for _, asset := range assets {
		// Check if asset exists
		_, err := h.assetStore.Get(c.Request.Context(), asset.Id.String())
		if err != nil {
			// Asset doesn't exist, create it
			if err := h.assetStore.Create(c.Request.Context(), &asset, threatModelID); err != nil {
				logger.Error("Failed to create asset %s: %v", asset.Id.String(), err)
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to create asset %s", asset.Id.String())))
				return
			}
			upsertedAssets = append(upsertedAssets, asset)
		} else {
			// Asset exists, update it
			if err := h.assetStore.Update(c.Request.Context(), &asset, threatModelID); err != nil {
				logger.Error("Failed to update asset %s: %v", asset.Id.String(), err)
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to update asset %s", asset.Id.String())))
				return
			}
			upsertedAssets = append(upsertedAssets, asset)
		}
	}

	logger.Info("Successfully bulk upserted %d assets for threat model %s (user: %s)", len(upsertedAssets), threatModelID, userEmail)
	c.JSON(http.StatusOK, upsertedAssets)
}
