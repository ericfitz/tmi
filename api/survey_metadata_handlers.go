package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// SurveyMetadataHandler provides handlers for admin survey metadata operations
type SurveyMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewSurveyMetadataHandler creates a new survey metadata handler
func NewSurveyMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *SurveyMetadataHandler {
	return &SurveyMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetAdminSurveyMetadata retrieves all metadata for a survey
// GET /admin/surveys/{survey_id}/metadata
func (h *SurveyMetadataHandler) GetAdminSurveyMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetAdminSurveyMetadata - retrieving metadata for survey")

	// Extract and validate survey ID
	surveyUUID, err := ExtractUUID(c, "survey_id")
	if err != nil {
		return // Error response already sent
	}
	surveyID := surveyUUID.String()

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for survey %s (user: %s)", surveyID, userEmail)

	// Verify survey exists before listing metadata
	if _, err := GlobalSurveyStore.Get(c.Request.Context(), surveyUUID); err != nil {
		HandleRequestError(c, NotFoundError("Survey not found"))
		return
	}

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "survey", surveyID)
	if err != nil {
		logger.Error("Failed to retrieve survey metadata for %s: %v", surveyID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for survey %s", len(metadata), surveyID)
	c.JSON(http.StatusOK, metadata)
}

// CreateAdminSurveyMetadata creates a new metadata entry for a survey
// POST /admin/surveys/{survey_id}/metadata
func (h *SurveyMetadataHandler) CreateAdminSurveyMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateAdminSurveyMetadata - creating new metadata entry")

	// Extract survey ID from URL
	surveyUUID, err := ExtractUUID(c, "survey_id")
	if err != nil {
		return // Error response already sent
	}
	surveyID := surveyUUID.String()

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Verify survey exists before creating metadata
	if _, err := GlobalSurveyStore.Get(c.Request.Context(), surveyUUID); err != nil {
		HandleRequestError(c, NotFoundError("Survey not found"))
		return
	}

	// Parse and validate request body using OpenAPI validation
	var metadata Metadata
	if err := c.ShouldBindJSON(&metadata); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	logger.Debug("Creating metadata key '%s' for survey %s (user: %s)", metadata.Key, surveyID, userEmail)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "survey", surveyID, &metadata); err != nil {
		logger.Error("Failed to create survey metadata key '%s' for %s: %v", metadata.Key, surveyID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "survey", surveyID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for survey %s", metadata.Key, surveyID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// GetAdminSurveyMetadataByKey retrieves a specific metadata entry by key
// GET /admin/surveys/{survey_id}/metadata/{key}
func (h *SurveyMetadataHandler) GetAdminSurveyMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetAdminSurveyMetadataByKey - retrieving specific metadata entry")

	// Extract and validate survey ID
	surveyUUID, err := ExtractUUID(c, "survey_id")
	if err != nil {
		return // Error response already sent
	}
	surveyID := surveyUUID.String()

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

	logger.Debug("Retrieving metadata key '%s' for survey %s (user: %s)", key, surveyID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "survey", surveyID, key)
	if err != nil {
		logger.Error("Failed to retrieve survey metadata key '%s' for %s: %v", key, surveyID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for survey %s", key, surveyID)
	c.JSON(http.StatusOK, metadata)
}

// UpdateAdminSurveyMetadataByKey updates an existing metadata entry
// PUT /admin/surveys/{survey_id}/metadata/{key}
func (h *SurveyMetadataHandler) UpdateAdminSurveyMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateAdminSurveyMetadataByKey - updating metadata entry")

	// Extract and validate survey ID
	surveyUUID, err := ExtractUUID(c, "survey_id")
	if err != nil {
		return // Error response already sent
	}
	surveyID := surveyUUID.String()

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

	// Parse and validate request body using OpenAPI validation
	var metadata Metadata
	if err := c.ShouldBindJSON(&metadata); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	// Ensure the key matches the URL parameter
	metadata.Key = key

	logger.Debug("Updating metadata key '%s' for survey %s (user: %s)", key, surveyID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "survey", surveyID, &metadata); err != nil {
		logger.Error("Failed to update survey metadata key '%s' for %s: %v", key, surveyID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "survey", surveyID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for survey %s", key, surveyID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteAdminSurveyMetadataByKey deletes a metadata entry
// DELETE /admin/surveys/{survey_id}/metadata/{key}
func (h *SurveyMetadataHandler) DeleteAdminSurveyMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteAdminSurveyMetadataByKey - deleting metadata entry")

	// Extract and validate survey ID
	surveyUUID, err := ExtractUUID(c, "survey_id")
	if err != nil {
		return // Error response already sent
	}
	surveyID := surveyUUID.String()

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

	logger.Debug("Deleting metadata key '%s' for survey %s (user: %s)", key, surveyID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "survey", surveyID, key); err != nil {
		logger.Error("Failed to delete survey metadata key '%s' for %s: %v", key, surveyID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for survey %s", key, surveyID)
	c.Status(http.StatusNoContent)
}

// BulkCreateAdminSurveyMetadata creates multiple metadata entries in a single request
// POST /admin/surveys/{survey_id}/metadata/bulk
func (h *SurveyMetadataHandler) BulkCreateAdminSurveyMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateAdminSurveyMetadata - creating multiple metadata entries")

	// Extract and validate survey ID
	surveyUUID, err := ExtractUUID(c, "survey_id")
	if err != nil {
		return // Error response already sent
	}
	surveyID := surveyUUID.String()

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Verify survey exists before creating metadata
	if _, err := GlobalSurveyStore.Get(c.Request.Context(), surveyUUID); err != nil {
		HandleRequestError(c, NotFoundError("Survey not found"))
		return
	}

	// Parse and validate request body using OpenAPI validation
	var metadataList []Metadata
	if err := c.ShouldBindJSON(&metadataList); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
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

	logger.Debug("Bulk creating %d metadata entries for survey %s (user: %s)",
		len(metadataList), surveyID, userEmail)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "survey", surveyID, metadataList); err != nil {
		logger.Error("Failed to bulk create survey metadata for %s: %v", surveyID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "survey", surveyID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for survey %s", len(metadataList), surveyID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// BulkUpsertAdminSurveyMetadata updates multiple metadata entries in a single request
// PUT /admin/surveys/{survey_id}/metadata/bulk
func (h *SurveyMetadataHandler) BulkUpsertAdminSurveyMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpsertAdminSurveyMetadata - upserting multiple metadata entries")

	// Extract and validate survey ID
	surveyUUID, err := ExtractUUID(c, "survey_id")
	if err != nil {
		return // Error response already sent
	}
	surveyID := surveyUUID.String()

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Verify survey exists before upserting metadata
	if _, err := GlobalSurveyStore.Get(c.Request.Context(), surveyUUID); err != nil {
		HandleRequestError(c, NotFoundError("Survey not found"))
		return
	}

	// Parse and validate request body using OpenAPI validation
	var metadataList []Metadata
	if err := c.ShouldBindJSON(&metadataList); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
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

	logger.Debug("Bulk upserting %d metadata entries for survey %s (user: %s)",
		len(metadataList), surveyID, userEmail)

	// Upsert metadata entries in store (BulkUpdate uses upsert semantics)
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "survey", surveyID, metadataList); err != nil {
		logger.Error("Failed to bulk upsert survey metadata for %s: %v", surveyID, err)
		HandleRequestError(c, ServerError("Failed to upsert metadata entries"))
		return
	}

	// Retrieve the upserted metadata to return with timestamps
	upsertedMetadata, err := h.metadataStore.List(c.Request.Context(), "survey", surveyID)
	if err != nil {
		// Log error but still return success since upsert succeeded
		logger.Error("Failed to retrieve upserted metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk upserted %d metadata entries for survey %s", len(metadataList), surveyID)
	c.JSON(http.StatusOK, upsertedMetadata)
}
