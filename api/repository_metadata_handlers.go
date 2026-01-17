package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// RepositoryMetadataHandler provides handlers for repository code metadata operations
type RepositoryMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewRepositoryMetadataHandler creates a new repository code metadata handler
func NewRepositoryMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *RepositoryMetadataHandler {
	return &RepositoryMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetRepositoryMetadata retrieves all metadata for a repository code reference
// GET /threat_models/{threat_model_id}/repositorys/{repository_id}/metadata
func (h *RepositoryMetadataHandler) GetRepositoryMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetRepositoryMetadata - retrieving metadata for repository code reference")

	// Extract repository ID from URL
	repositoryID := c.Param("repository_id")
	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}

	// Validate repository ID format
	if _, err := ParseUUID(repositoryID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for repository %s (user: %s)", repositoryID, userEmail)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "repository", repositoryID)
	if err != nil {
		logger.Error("Failed to retrieve repository metadata for %s: %v", repositoryID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for repository %s", len(metadata), repositoryID)
	c.JSON(http.StatusOK, metadata)
}

// GetRepositoryMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/repositorys/{repository_id}/metadata/{key}
func (h *RepositoryMetadataHandler) GetRepositoryMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetRepositoryMetadataByKey - retrieving specific metadata entry")

	// Extract repository ID and key from URL
	repositoryID := c.Param("repository_id")
	key := c.Param("key")

	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate repository ID format
	if _, err := ParseUUID(repositoryID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for repository %s (user: %s)", key, repositoryID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "repository", repositoryID, key)
	if err != nil {
		logger.Error("Failed to retrieve repository metadata key '%s' for %s: %v", key, repositoryID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for repository %s", key, repositoryID)
	c.JSON(http.StatusOK, metadata)
}

// CreateRepositoryMetadata creates a new metadata entry for a repository code reference
// POST /threat_models/{threat_model_id}/repositorys/{repository_id}/metadata
func (h *RepositoryMetadataHandler) CreateRepositoryMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateRepositoryMetadata - creating new metadata entry")

	// Extract repository ID from URL
	repositoryID := c.Param("repository_id")
	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}

	// Validate repository ID format
	if _, err := ParseUUID(repositoryID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
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

	logger.Debug("Creating metadata key '%s' for repository %s (user: %s)", metadata.Key, repositoryID, userEmail)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "repository", repositoryID, &metadata); err != nil {
		logger.Error("Failed to create repository metadata key '%s' for %s: %v", metadata.Key, repositoryID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "repository", repositoryID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for repository %s", metadata.Key, repositoryID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateRepositoryMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/repositorys/{repository_id}/metadata/{key}
func (h *RepositoryMetadataHandler) UpdateRepositoryMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateRepositoryMetadata - updating metadata entry")

	// Extract repository ID and key from URL
	repositoryID := c.Param("repository_id")
	key := c.Param("key")

	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate repository ID format
	if _, err := ParseUUID(repositoryID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
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

	logger.Debug("Updating metadata key '%s' for repository %s (user: %s)", key, repositoryID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "repository", repositoryID, &metadata); err != nil {
		logger.Error("Failed to update repository metadata key '%s' for %s: %v", key, repositoryID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "repository", repositoryID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for repository %s", key, repositoryID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteRepositoryMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/repositorys/{repository_id}/metadata/{key}
func (h *RepositoryMetadataHandler) DeleteRepositoryMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteRepositoryMetadata - deleting metadata entry")

	// Extract repository ID and key from URL
	repositoryID := c.Param("repository_id")
	key := c.Param("key")

	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate repository ID format
	if _, err := ParseUUID(repositoryID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for repository %s (user: %s)", key, repositoryID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "repository", repositoryID, key); err != nil {
		logger.Error("Failed to delete repository metadata key '%s' for %s: %v", key, repositoryID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for repository %s", key, repositoryID)
	c.Status(http.StatusNoContent)
}

// BulkCreateRepositoryMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/repositorys/{repository_id}/metadata/bulk
func (h *RepositoryMetadataHandler) BulkCreateRepositoryMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateRepositoryMetadata - creating multiple metadata entries")

	// Extract repository ID from URL
	repositoryID := c.Param("repository_id")
	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}

	// Validate repository ID format
	if _, err := ParseUUID(repositoryID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
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

	logger.Debug("Bulk creating %d metadata entries for repository %s (user: %s)",
		len(metadataList), repositoryID, userEmail)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "repository", repositoryID, metadataList); err != nil {
		logger.Error("Failed to bulk create repository metadata for %s: %v", repositoryID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "repository", repositoryID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for repository %s", len(metadataList), repositoryID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// BulkUpdateRepositoryMetadata updates multiple metadata entries in a single request
// PUT /threat_models/{threat_model_id}/repositorys/{repository_id}/metadata/bulk
func (h *RepositoryMetadataHandler) BulkUpdateRepositoryMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateRepositoryMetadata - updating multiple metadata entries")

	// Extract parameters from URL
	threatmodelid := c.Param("threat_model_id")
	repositoryid := c.Param("repository_id")

	if threatmodelid == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model id ID"))
		return
	}

	// Validate threat model id ID format
	if _, err := ParseUUID(threatmodelid); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model id ID format, must be a valid UUID"))
		return
	}

	if repositoryid == "" {
		HandleRequestError(c, InvalidIDError("Missing repository id ID"))
		return
	}

	// Validate repository id ID format
	if _, err := ParseUUID(repositoryid); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository id ID format, must be a valid UUID"))
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

	logger.Debug("Bulk updating %d metadata entries for repository %s in threat model id %s (user: %s)",
		len(metadataList), repositoryid, threatmodelid, userEmail)

	// Update metadata entries in store
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "repository", repositoryid, metadataList); err != nil {
		logger.Error("Failed to bulk update repository metadata for %s: %v", repositoryid, err)
		HandleRequestError(c, ServerError("Failed to update metadata entries"))
		return
	}

	// Retrieve the updated metadata to return with timestamps
	updatedMetadata, err := h.metadataStore.List(c.Request.Context(), "repository", repositoryid)
	if err != nil {
		// Log error but still return success since update succeeded
		logger.Error("Failed to retrieve updated metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk updated %d metadata entries for repository %s", len(metadataList), repositoryid)
	c.JSON(http.StatusOK, updatedMetadata)
}
