package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SourceSubResourceHandler provides handlers for source code sub-resource operations
type SourceSubResourceHandler struct {
	sourceStore      SourceStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewSourceSubResourceHandler creates a new source code sub-resource handler
func NewSourceSubResourceHandler(sourceStore SourceStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *SourceSubResourceHandler {
	return &SourceSubResourceHandler{
		sourceStore:      sourceStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetSources retrieves all source code references for a threat model with pagination
// GET /threat_models/{threat_model_id}/sources
func (h *SourceSubResourceHandler) GetSources(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetSources - retrieving source code references for threat model")

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving source code references for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, userEmail, offset, limit)

	// Get sources from store (authorization is handled by middleware)
	sources, err := h.sourceStore.List(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve source code references: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve source code references"))
		return
	}

	logger.Debug("Successfully retrieved %d source code references", len(sources))
	c.JSON(http.StatusOK, sources)
}

// GetSource retrieves a specific source code reference by ID
// GET /threat_models/{threat_model_id}/sources/{source_id}
func (h *SourceSubResourceHandler) GetSource(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetSource - retrieving specific source code reference")

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving source code reference %s (user: %s)", sourceID, userEmail)

	// Get source from store
	source, err := h.sourceStore.Get(c.Request.Context(), sourceID)
	if err != nil {
		logger.Error("Failed to retrieve source code reference %s: %v", sourceID, err)
		HandleRequestError(c, NotFoundError("Source code reference not found"))
		return
	}

	logger.Debug("Successfully retrieved source code reference %s", sourceID)
	c.JSON(http.StatusOK, source)
}

// CreateSource creates a new source code reference in a threat model
// POST /threat_models/{threat_model_id}/sources
func (h *SourceSubResourceHandler) CreateSource(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateSource - creating new source code reference")

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["source_create"]
	source, err := ValidateAndParseRequest[Source](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Generate UUID if not provided
	if source.Id == nil {
		id := uuid.New()
		source.Id = &id
	}

	logger.Debug("Creating source code reference %s in threat model %s (user: %s)",
		source.Id.String(), threatModelID, userEmail)

	// Create source in store
	if err := h.sourceStore.Create(c.Request.Context(), source, threatModelID); err != nil {
		logger.Error("Failed to create source code reference: %v", err)
		HandleRequestError(c, ServerError("Failed to create source code reference"))
		return
	}

	logger.Debug("Successfully created source code reference %s", source.Id.String())
	c.JSON(http.StatusCreated, source)
}

// UpdateSource updates an existing source code reference
// PUT /threat_models/{threat_model_id}/sources/{source_id}
func (h *SourceSubResourceHandler) UpdateSource(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateSource - updating existing source code reference")

	// Extract source ID from URL
	sourceID := c.Param("source_id")
	if sourceID == "" {
		HandleRequestError(c, InvalidIDError("Missing source ID"))
		return
	}

	// Validate source ID format
	sourceUUID, err := ParseUUID(sourceID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid source ID format, must be a valid UUID"))
		return
	}

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["source_update"]
	source, err := ValidateAndParseRequest[Source](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set ID from URL (override any value in body)
	source.Id = &sourceUUID

	logger.Debug("Updating source code reference %s (user: %s)", sourceID, userEmail)

	// Update source in store
	if err := h.sourceStore.Update(c.Request.Context(), source, threatModelID); err != nil {
		logger.Error("Failed to update source code reference %s: %v", sourceID, err)
		HandleRequestError(c, ServerError("Failed to update source code reference"))
		return
	}

	logger.Debug("Successfully updated source code reference %s", sourceID)
	c.JSON(http.StatusOK, source)
}

// DeleteSource deletes a source code reference
// DELETE /threat_models/{threat_model_id}/sources/{source_id}
func (h *SourceSubResourceHandler) DeleteSource(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteSource - deleting source code reference")

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting source code reference %s (user: %s)", sourceID, userEmail)

	// Delete source from store
	if err := h.sourceStore.Delete(c.Request.Context(), sourceID); err != nil {
		logger.Error("Failed to delete source code reference %s: %v", sourceID, err)
		HandleRequestError(c, ServerError("Failed to delete source code reference"))
		return
	}

	logger.Debug("Successfully deleted source code reference %s", sourceID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateSources creates multiple source code references in a single request
// POST /threat_models/{threat_model_id}/sources/bulk
func (h *SourceSubResourceHandler) BulkCreateSources(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateSources - creating multiple source code references")

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of sources
	var sources []Source
	if err := c.ShouldBindJSON(&sources); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(sources) == 0 {
		HandleRequestError(c, InvalidInputError("No source code references provided"))
		return
	}

	if len(sources) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 source code references allowed per bulk operation"))
		return
	}

	// Validate each source
	for _, source := range sources {
		if source.Url == "" {
			HandleRequestError(c, InvalidInputError("Source URL is required for all source code references"))
			return
		}
	}

	// Generate UUIDs for sources that don't have them
	for i := range sources {
		source := &sources[i]
		if source.Id == nil {
			id := uuid.New()
			source.Id = &id
		}
	}

	logger.Debug("Bulk creating %d source code references in threat model %s (user: %s)",
		len(sources), threatModelID, userEmail)

	// Create sources in store
	if err := h.sourceStore.BulkCreate(c.Request.Context(), sources, threatModelID); err != nil {
		logger.Error("Failed to bulk create source code references: %v", err)
		HandleRequestError(c, ServerError("Failed to create source code references"))
		return
	}

	logger.Debug("Successfully bulk created %d source code references", len(sources))
	c.JSON(http.StatusCreated, sources)
}
