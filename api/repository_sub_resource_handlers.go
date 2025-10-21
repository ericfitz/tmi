package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RepositorySubResourceHandler provides handlers for repository code sub-resource operations
type RepositorySubResourceHandler struct {
	repositoryStore  RepositoryStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewRepositorySubResourceHandler creates a new repository code sub-resource handler
func NewRepositorySubResourceHandler(repositoryStore RepositoryStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *RepositorySubResourceHandler {
	return &RepositorySubResourceHandler{
		repositoryStore:  repositoryStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetRepositorys retrieves all repository code references for a threat model with pagination
// GET /threat_models/{threat_model_id}/repositorys
func (h *RepositorySubResourceHandler) GetRepositorys(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetRepositorys - retrieving repository code references for threat model")

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

	logger.Debug("Retrieving repository code references for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, userEmail, offset, limit)

	// Get repositorys from store (authorization is handled by middleware)
	repositorys, err := h.repositoryStore.List(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve repository code references: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve repository code references"))
		return
	}

	logger.Debug("Successfully retrieved %d repository code references", len(repositorys))
	c.JSON(http.StatusOK, repositorys)
}

// GetRepository retrieves a specific repository code reference by ID
// GET /threat_models/{threat_model_id}/repositorys/{repository_id}
func (h *RepositorySubResourceHandler) GetRepository(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetRepository - retrieving specific repository code reference")

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving repository code reference %s (user: %s)", repositoryID, userEmail)

	// Get repository from store
	repository, err := h.repositoryStore.Get(c.Request.Context(), repositoryID)
	if err != nil {
		logger.Error("Failed to retrieve repository code reference %s: %v", repositoryID, err)
		HandleRequestError(c, NotFoundError("Repository code reference not found"))
		return
	}

	logger.Debug("Successfully retrieved repository code reference %s", repositoryID)
	c.JSON(http.StatusOK, repository)
}

// CreateRepository creates a new repository code reference in a threat model
// POST /threat_models/{threat_model_id}/repositorys
func (h *RepositorySubResourceHandler) CreateRepository(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateRepository - creating new repository code reference")

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
	config := ValidationConfigs["repository_create"]
	repository, err := ValidateAndParseRequest[Repository](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Generate UUID if not provided
	if repository.Id == nil {
		id := uuid.New()
		repository.Id = &id
	}

	logger.Debug("Creating repository code reference %s in threat model %s (user: %s)",
		repository.Id.String(), threatModelID, userEmail)

	// Create repository in store
	if err := h.repositoryStore.Create(c.Request.Context(), repository, threatModelID); err != nil {
		logger.Error("Failed to create repository code reference: %v", err)
		HandleRequestError(c, ServerError("Failed to create repository code reference"))
		return
	}

	logger.Debug("Successfully created repository code reference %s", repository.Id.String())
	c.JSON(http.StatusCreated, repository)
}

// UpdateRepository updates an existing repository code reference
// PUT /threat_models/{threat_model_id}/repositorys/{repository_id}
func (h *RepositorySubResourceHandler) UpdateRepository(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateRepository - updating existing repository code reference")

	// Extract repository ID from URL
	repositoryID := c.Param("repository_id")
	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}

	// Validate repository ID format
	repositoryUUID, err := ParseUUID(repositoryID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
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
	config := ValidationConfigs["repository_update"]
	repository, err := ValidateAndParseRequest[Repository](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set ID from URL (override any value in body)
	repository.Id = &repositoryUUID

	logger.Debug("Updating repository code reference %s (user: %s)", repositoryID, userEmail)

	// Update repository in store
	if err := h.repositoryStore.Update(c.Request.Context(), repository, threatModelID); err != nil {
		logger.Error("Failed to update repository code reference %s: %v", repositoryID, err)
		HandleRequestError(c, ServerError("Failed to update repository code reference"))
		return
	}

	logger.Debug("Successfully updated repository code reference %s", repositoryID)
	c.JSON(http.StatusOK, repository)
}

// DeleteRepository deletes a repository code reference
// DELETE /threat_models/{threat_model_id}/repositorys/{repository_id}
func (h *RepositorySubResourceHandler) DeleteRepository(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteRepository - deleting repository code reference")

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting repository code reference %s (user: %s)", repositoryID, userEmail)

	// Delete repository from store
	if err := h.repositoryStore.Delete(c.Request.Context(), repositoryID); err != nil {
		logger.Error("Failed to delete repository code reference %s: %v", repositoryID, err)
		HandleRequestError(c, ServerError("Failed to delete repository code reference"))
		return
	}

	logger.Debug("Successfully deleted repository code reference %s", repositoryID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateRepositorys creates multiple repository code references in a single request
// POST /threat_models/{threat_model_id}/repositorys/bulk
func (h *RepositorySubResourceHandler) BulkCreateRepositorys(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateRepositorys - creating multiple repository code references")

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

	// Parse and validate request body as array of repositorys
	var repositorys []Repository
	if err := c.ShouldBindJSON(&repositorys); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(repositorys) == 0 {
		HandleRequestError(c, InvalidInputError("No repository code references provided"))
		return
	}

	if len(repositorys) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 repository code references allowed per bulk operation"))
		return
	}

	// Validate each repository
	for _, repository := range repositorys {
		if repository.Uri == nil || *repository.Uri == "" {
			HandleRequestError(c, InvalidInputError("Repository URI is required for all repository code references"))
			return
		}
	}

	// Generate UUIDs for repositorys that don't have them
	for i := range repositorys {
		repository := &repositorys[i]
		if repository.Id == nil {
			id := uuid.New()
			repository.Id = &id
		}
	}

	logger.Debug("Bulk creating %d repository code references in threat model %s (user: %s)",
		len(repositorys), threatModelID, userEmail)

	// Create repositorys in store
	if err := h.repositoryStore.BulkCreate(c.Request.Context(), repositorys, threatModelID); err != nil {
		logger.Error("Failed to bulk create repository code references: %v", err)
		HandleRequestError(c, ServerError("Failed to create repository code references"))
		return
	}

	logger.Debug("Successfully bulk created %d repository code references", len(repositorys))
	c.JSON(http.StatusCreated, repositorys)
}
