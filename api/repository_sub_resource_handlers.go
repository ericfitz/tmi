package api

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RepositorySubResourceHandler provides handlers for repository code sub-resource operations
type RepositorySubResourceHandler struct {
	repositoryStore  RepositoryRepository
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	// URI validator for SSRF protection on uri fields
	repositoryURIValidator *URIValidator
}

// SetRepositoryURIValidator sets the URI validator for repository uri fields
func (h *RepositorySubResourceHandler) SetRepositoryURIValidator(v *URIValidator) {
	h.repositoryURIValidator = v
}

// NewRepositorySubResourceHandler creates a new repository code sub-resource handler
func NewRepositorySubResourceHandler(repositoryStore RepositoryRepository, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *RepositorySubResourceHandler {
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
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving repository code references for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, user.Email, offset, limit)

	// Get repositorys from store (authorization is handled by middleware)
	repositorys, err := h.repositoryStore.List(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve repository code references: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve repository code references"))
		return
	}

	// Get total count for pagination
	total, err := h.repositoryStore.Count(c.Request.Context(), threatModelID)
	if err != nil {
		logger.Warn("Failed to get repository count, using page size: %v", err)
		total = len(repositorys)
	}

	logger.Debug("Successfully retrieved %d repository code references (total: %d)", len(repositorys), total)
	c.JSON(http.StatusOK, ListRepositoriesResponse{
		Repositories: repositorys,
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	})
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
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving repository code reference %s (user: %s)", repositoryID, user.Email)

	// Get repository from store
	repository, err := h.repositoryStore.Get(c.Request.Context(), repositoryID)
	if err != nil {
		logger.Error("Failed to retrieve repository code reference %s: %v", repositoryID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Repository code reference not found", "Failed to retrieve repository code reference"))
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
	user, err := GetAuthenticatedUser(c)
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

	// Sanitize text fields (defense-in-depth)
	repository.Name = SanitizeOptionalString(repository.Name)
	repository.Description = SanitizeOptionalString(repository.Description)
	repository.Uri = SanitizePlainText(repository.Uri)
	if err := SanitizeMetadataSlice(repository.Metadata); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := validateURI(h.repositoryURIValidator, "uri", repository.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Generate UUID if not provided
	if repository.Id == nil {
		id := uuid.New()
		repository.Id = &id
	}

	logger.Debug("Creating repository code reference %s in threat model %s (user: %s)",
		repository.Id.String(), threatModelID, user.Email)

	// Create repository in store
	if err := h.repositoryStore.Create(c.Request.Context(), repository, threatModelID); err != nil {
		logger.Error("Failed to create repository code reference: %v", err)
		HandleRequestError(c, ServerError("Failed to create repository code reference"))
		return
	}

	RecordAuditCreate(c, threatModelID, "repository", repository.Id.String(), repository)
	invalidateThreatModelCaches(c, threatModelID)

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
	user, err := GetAuthenticatedUser(c)
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

	// Sanitize text fields (defense-in-depth)
	repository.Name = SanitizeOptionalString(repository.Name)
	repository.Description = SanitizeOptionalString(repository.Description)
	repository.Uri = SanitizePlainText(repository.Uri)
	if err := SanitizeMetadataSlice(repository.Metadata); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := validateURI(h.repositoryURIValidator, "uri", repository.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set ID from URL (override any value in body)
	repository.Id = &repositoryUUID

	logger.Debug("Updating repository code reference %s (user: %s)", repositoryID, user.Email)

	// Capture pre-mutation state for audit
	existingRepo, _ := h.repositoryStore.Get(c.Request.Context(), repositoryID)
	var preState []byte
	if existingRepo != nil {
		preState, _ = SerializeForAudit(existingRepo)
	}

	// Update repository in store
	if err := h.repositoryStore.Update(c.Request.Context(), repository, threatModelID); err != nil {
		logger.Error("Failed to update repository code reference %s: %v", repositoryID, err)
		HandleRequestError(c, ServerError("Failed to update repository code reference"))
		return
	}

	RecordAuditUpdate(c, "updated", threatModelID, "repository", repositoryID, preState, repository)
	invalidateThreatModelCaches(c, threatModelID)

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
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting repository code reference %s (user: %s)", repositoryID, user.Email)

	// Capture pre-deletion state for audit
	existingRepo, _ := h.repositoryStore.Get(c.Request.Context(), repositoryID)
	var preState []byte
	if existingRepo != nil {
		preState, _ = SerializeForAudit(existingRepo)
	}

	// Delete repository from store
	if err := h.repositoryStore.Delete(c.Request.Context(), repositoryID); err != nil {
		logger.Error("Failed to delete repository code reference %s: %v", repositoryID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Repository not found", "Failed to delete repository code reference"))
		return
	}

	threatModelID := c.Param("threat_model_id")
	RecordAuditDelete(c, threatModelID, "repository", repositoryID, preState)
	invalidateThreatModelCaches(c, threatModelID)

	logger.Debug("Successfully deleted repository code reference %s", repositoryID)
	c.Status(http.StatusNoContent)
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
	user, err := GetAuthenticatedUser(c)
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
		if repository.Uri == "" {
			HandleRequestError(c, InvalidInputError("Repository URI is required for all repository code references"))
			return
		}
	}

	// Generate UUIDs and sanitize text fields
	for i := range repositorys {
		repository := &repositorys[i]

		// Sanitize text fields (defense-in-depth)
		repository.Name = SanitizeOptionalString(repository.Name)
		repository.Description = SanitizeOptionalString(repository.Description)
		repository.Uri = SanitizePlainText(repository.Uri)
		if err := SanitizeMetadataSlice(repository.Metadata); err != nil {
			HandleRequestError(c, err)
			return
		}
		if err := validateURI(h.repositoryURIValidator, "uri", repository.Uri); err != nil {
			HandleRequestError(c, err)
			return
		}

		if repository.Id == nil {
			id := uuid.New()
			repository.Id = &id
		}
	}

	logger.Debug("Bulk creating %d repository code references in threat model %s (user: %s)",
		len(repositorys), threatModelID, user.Email)

	// Create repositorys in store
	if err := h.repositoryStore.BulkCreate(c.Request.Context(), repositorys, threatModelID); err != nil {
		logger.Error("Failed to bulk create repository code references: %v", err)
		HandleRequestError(c, ServerError("Failed to create repository code references"))
		return
	}

	invalidateThreatModelCaches(c, threatModelID)

	logger.Debug("Successfully bulk created %d repository code references", len(repositorys))
	c.JSON(http.StatusCreated, repositorys)
}

// PatchRepository applies JSON patch operations to a repository
// PATCH /threat_models/{threat_model_id}/repositories/{repository_id}
func (h *RepositorySubResourceHandler) PatchRepository(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchRepository - applying patch operations to repository")

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
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	userRole, err := GetResourceRole(c)
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

	// Sanitize text values in patch operations (defense-in-depth)
	SanitizePatchOperations(operations, []string{"/name", "/description", "/uri"})
	if err := ValidateURIPatchOperations(h.repositoryURIValidator, operations, []string{"/uri"}); err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Applying %d patch operations to repository %s (user: %s)",
		len(operations), repositoryID, user.Email)

	// Capture pre-mutation state for audit
	existingRepo, _ := h.repositoryStore.Get(c.Request.Context(), repositoryID)
	var preState []byte
	if existingRepo != nil {
		preState, _ = SerializeForAudit(existingRepo)
	}

	// Apply patch operations
	updatedRepository, err := h.repositoryStore.Patch(c.Request.Context(), repositoryID, operations)
	if err != nil {
		HandleRequestError(c, ServerError("Failed to patch repository"))
		return
	}

	threatModelID := c.Param("threat_model_id")
	RecordAuditUpdate(c, "patched", threatModelID, "repository", repositoryID, preState, updatedRepository)
	invalidateThreatModelCaches(c, threatModelID)

	logger.Info("Successfully patched repository %s (user: %s)", repositoryID, user.Email)
	c.JSON(http.StatusOK, updatedRepository)
}

// BulkUpdateRepositorys updates or creates multiple repositories (upsert operation)
// PUT /threat_models/{threat_model_id}/repositories/bulk
func (h *RepositorySubResourceHandler) BulkUpdateRepositorys(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateRepositorys - upserting multiple repositories")

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
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of repositories
	var repositories []Repository
	if err := c.ShouldBindJSON(&repositories); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(repositories) == 0 {
		HandleRequestError(c, InvalidInputError("No repositories provided"))
		return
	}

	if len(repositories) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 repositories allowed per bulk operation"))
		return
	}

	// Validate each repository
	for _, repository := range repositories {
		if repository.Id == nil {
			HandleRequestError(c, InvalidInputError("Repository ID is required for all repositories in bulk update"))
			return
		}
		if repository.Name == nil || *repository.Name == "" {
			HandleRequestError(c, InvalidInputError("Repository name is required for all repositories"))
			return
		}
	}

	// Sanitize text fields (defense-in-depth)
	for i := range repositories {
		repositories[i].Name = SanitizeOptionalString(repositories[i].Name)
		repositories[i].Description = SanitizeOptionalString(repositories[i].Description)
		repositories[i].Uri = SanitizePlainText(repositories[i].Uri)
		if err := SanitizeMetadataSlice(repositories[i].Metadata); err != nil {
			HandleRequestError(c, err)
			return
		}
		if err := validateURI(h.repositoryURIValidator, "uri", repositories[i].Uri); err != nil {
			HandleRequestError(c, err)
			return
		}
	}

	logger.Debug("Bulk updating %d repositories for threat model %s (user: %s)", len(repositories), threatModelID, user.Email)

	// Upsert each repository
	upsertedRepositories := make([]Repository, 0, len(repositories))
	for _, repository := range repositories {
		// Check if repository exists
		_, err := h.repositoryStore.Get(c.Request.Context(), repository.Id.String())
		if err != nil {
			// Repository doesn't exist, create it
			if err := h.repositoryStore.Create(c.Request.Context(), &repository, threatModelID); err != nil {
				logger.Error("Failed to create repository %s: %v", repository.Id.String(), err)
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to create repository %s", repository.Id.String())))
				return
			}
			upsertedRepositories = append(upsertedRepositories, repository)
		} else {
			// Repository exists, update it
			if err := h.repositoryStore.Update(c.Request.Context(), &repository, threatModelID); err != nil {
				logger.Error("Failed to update repository %s: %v", repository.Id.String(), err)
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to update repository %s", repository.Id.String())))
				return
			}
			upsertedRepositories = append(upsertedRepositories, repository)
		}
	}

	invalidateThreatModelCaches(c, threatModelID)

	logger.Info("Successfully bulk upserted %d repositories for threat model %s (user: %s)", len(upsertedRepositories), threatModelID, user.Email)
	c.JSON(http.StatusOK, upsertedRepositories)
}
