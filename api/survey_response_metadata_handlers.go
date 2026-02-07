package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// SurveyResponseMetadataHandler provides handlers for survey response metadata operations
// (intake full CRUD + triage read-only)
type SurveyResponseMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewSurveyResponseMetadataHandler creates a new survey response metadata handler
func NewSurveyResponseMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *SurveyResponseMetadataHandler {
	return &SurveyResponseMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// --- Intake survey response metadata (full CRUD) ---

// GetIntakeSurveyResponseMetadata retrieves all metadata for a survey response
// GET /intake/survey_responses/{survey_response_id}/metadata
func (h *SurveyResponseMetadataHandler) GetIntakeSurveyResponseMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetIntakeSurveyResponseMetadata - retrieving metadata for survey response")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for survey response %s (user: %s)", surveyResponseID, userEmail)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "survey_response", surveyResponseID)
	if err != nil {
		logger.Error("Failed to retrieve survey response metadata for %s: %v", surveyResponseID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for survey response %s", len(metadata), surveyResponseID)
	c.JSON(http.StatusOK, metadata)
}

// CreateIntakeSurveyResponseMetadata creates a new metadata entry for a survey response
// POST /intake/survey_responses/{survey_response_id}/metadata
func (h *SurveyResponseMetadataHandler) CreateIntakeSurveyResponseMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateIntakeSurveyResponseMetadata - creating new metadata entry")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

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

	logger.Debug("Creating metadata key '%s' for survey response %s (user: %s)", metadata.Key, surveyResponseID, userEmail)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "survey_response", surveyResponseID, &metadata); err != nil {
		logger.Error("Failed to create survey response metadata key '%s' for %s: %v", metadata.Key, surveyResponseID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "survey_response", surveyResponseID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for survey response %s", metadata.Key, surveyResponseID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// GetIntakeSurveyResponseMetadataByKey retrieves a specific metadata entry by key
// GET /intake/survey_responses/{survey_response_id}/metadata/{key}
func (h *SurveyResponseMetadataHandler) GetIntakeSurveyResponseMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetIntakeSurveyResponseMetadataByKey - retrieving specific metadata entry")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

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

	logger.Debug("Retrieving metadata key '%s' for survey response %s (user: %s)", key, surveyResponseID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "survey_response", surveyResponseID, key)
	if err != nil {
		logger.Error("Failed to retrieve survey response metadata key '%s' for %s: %v", key, surveyResponseID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for survey response %s", key, surveyResponseID)
	c.JSON(http.StatusOK, metadata)
}

// UpdateIntakeSurveyResponseMetadataByKey updates an existing metadata entry
// PUT /intake/survey_responses/{survey_response_id}/metadata/{key}
func (h *SurveyResponseMetadataHandler) UpdateIntakeSurveyResponseMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateIntakeSurveyResponseMetadataByKey - updating metadata entry")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

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
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Ensure the key matches the URL parameter
	metadata.Key = key

	logger.Debug("Updating metadata key '%s' for survey response %s (user: %s)", key, surveyResponseID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "survey_response", surveyResponseID, &metadata); err != nil {
		logger.Error("Failed to update survey response metadata key '%s' for %s: %v", key, surveyResponseID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "survey_response", surveyResponseID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for survey response %s", key, surveyResponseID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteIntakeSurveyResponseMetadataByKey deletes a metadata entry
// DELETE /intake/survey_responses/{survey_response_id}/metadata/{key}
func (h *SurveyResponseMetadataHandler) DeleteIntakeSurveyResponseMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteIntakeSurveyResponseMetadataByKey - deleting metadata entry")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

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

	logger.Debug("Deleting metadata key '%s' for survey response %s (user: %s)", key, surveyResponseID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "survey_response", surveyResponseID, key); err != nil {
		logger.Error("Failed to delete survey response metadata key '%s' for %s: %v", key, surveyResponseID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for survey response %s", key, surveyResponseID)
	c.Status(http.StatusNoContent)
}

// BulkCreateIntakeSurveyResponseMetadata creates multiple metadata entries in a single request
// POST /intake/survey_responses/{survey_response_id}/metadata/bulk
func (h *SurveyResponseMetadataHandler) BulkCreateIntakeSurveyResponseMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateIntakeSurveyResponseMetadata - creating multiple metadata entries")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

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

	logger.Debug("Bulk creating %d metadata entries for survey response %s (user: %s)",
		len(metadataList), surveyResponseID, userEmail)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "survey_response", surveyResponseID, metadataList); err != nil {
		logger.Error("Failed to bulk create survey response metadata for %s: %v", surveyResponseID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "survey_response", surveyResponseID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for survey response %s", len(metadataList), surveyResponseID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// BulkUpsertIntakeSurveyResponseMetadata updates multiple metadata entries in a single request
// PUT /intake/survey_responses/{survey_response_id}/metadata/bulk
func (h *SurveyResponseMetadataHandler) BulkUpsertIntakeSurveyResponseMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpsertIntakeSurveyResponseMetadata - upserting multiple metadata entries")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

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

	logger.Debug("Bulk upserting %d metadata entries for survey response %s (user: %s)",
		len(metadataList), surveyResponseID, userEmail)

	// Upsert metadata entries in store (BulkUpdate uses upsert semantics)
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "survey_response", surveyResponseID, metadataList); err != nil {
		logger.Error("Failed to bulk upsert survey response metadata for %s: %v", surveyResponseID, err)
		HandleRequestError(c, ServerError("Failed to upsert metadata entries"))
		return
	}

	// Retrieve the upserted metadata to return with timestamps
	upsertedMetadata, err := h.metadataStore.List(c.Request.Context(), "survey_response", surveyResponseID)
	if err != nil {
		// Log error but still return success since upsert succeeded
		logger.Error("Failed to retrieve upserted metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk upserted %d metadata entries for survey response %s", len(metadataList), surveyResponseID)
	c.JSON(http.StatusOK, upsertedMetadata)
}

// --- Triage survey response metadata (read-only) ---

// GetTriageSurveyResponseMetadata retrieves all metadata for a survey response (triage view)
// GET /triage/survey_responses/{survey_response_id}/metadata
func (h *SurveyResponseMetadataHandler) GetTriageSurveyResponseMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetTriageSurveyResponseMetadata - retrieving metadata for survey response (triage)")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving triage metadata for survey response %s (user: %s)", surveyResponseID, userEmail)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "survey_response", surveyResponseID)
	if err != nil {
		logger.Error("Failed to retrieve survey response metadata for %s: %v", surveyResponseID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d triage metadata items for survey response %s", len(metadata), surveyResponseID)
	c.JSON(http.StatusOK, metadata)
}

// GetTriageSurveyResponseMetadataByKey retrieves a specific metadata entry by key (triage view)
// GET /triage/survey_responses/{survey_response_id}/metadata/{key}
func (h *SurveyResponseMetadataHandler) GetTriageSurveyResponseMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetTriageSurveyResponseMetadataByKey - retrieving specific metadata entry (triage)")

	// Extract and validate survey response ID
	surveyResponseUUID, err := ExtractUUID(c, "survey_response_id")
	if err != nil {
		return // Error response already sent
	}
	surveyResponseID := surveyResponseUUID.String()

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

	logger.Debug("Retrieving triage metadata key '%s' for survey response %s (user: %s)", key, surveyResponseID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "survey_response", surveyResponseID, key)
	if err != nil {
		logger.Error("Failed to retrieve survey response metadata key '%s' for %s: %v", key, surveyResponseID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved triage metadata key '%s' for survey response %s", key, surveyResponseID)
	c.JSON(http.StatusOK, metadata)
}
