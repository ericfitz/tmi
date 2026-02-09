package api

import (
	"context"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ericfitz/tmi/internal/slogging"
)

// validMetadataKeyPattern matches allowed metadata key characters:
// alphanumeric, hyphens, underscores, dots, colons. Length 1-256.
var validMetadataKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9._\-:]{1,256}$`)

// validateMetadataKeyString validates a metadata key string.
// Keys must be 1-256 characters and contain only alphanumeric, hyphens, underscores, dots, and colons.
func validateMetadataKeyString(key string) error {
	if key == "" {
		return InvalidInputError("Metadata key must not be empty")
	}
	if !validMetadataKeyPattern.MatchString(key) {
		return InvalidInputError("Metadata key must be 1-256 characters and contain only alphanumeric characters, hyphens, underscores, dots, and colons")
	}
	return nil
}

// ParentVerifier is a function that checks if a parent entity exists.
// It returns nil if the entity exists, or an error if not.
type ParentVerifier func(ctx context.Context, id uuid.UUID) error

// GenericMetadataHandler provides handlers for metadata operations on any entity type.
// It replaces entity-specific metadata handlers with a single implementation.
type GenericMetadataHandler struct {
	metadataStore   MetadataStore
	entityType      string
	parentParamName string
	verifyParent    ParentVerifier
}

// NewGenericMetadataHandler creates a new generic metadata handler.
//
// Parameters:
//   - metadataStore: the metadata store to use
//   - entityType: the entity type string (e.g., "survey", "threat_model")
//   - parentParamName: the gin parameter name for the parent entity ID (e.g., "survey_id")
//   - verifyParent: optional function to verify parent entity exists (nil to skip)
func NewGenericMetadataHandler(metadataStore MetadataStore, entityType, parentParamName string, verifyParent ParentVerifier) *GenericMetadataHandler {
	return &GenericMetadataHandler{
		metadataStore:   metadataStore,
		entityType:      entityType,
		parentParamName: parentParamName,
		verifyParent:    verifyParent,
	}
}

// extractEntityID extracts and validates the parent entity UUID from the gin context.
func (h *GenericMetadataHandler) extractEntityID(c *gin.Context) (uuid.UUID, string, bool) {
	entityUUID, err := ExtractUUID(c, h.parentParamName)
	if err != nil {
		return uuid.Nil, "", false // Error response already sent by ExtractUUID
	}
	return entityUUID, entityUUID.String(), true
}

// checkParentExists verifies the parent entity exists if a verifier is configured.
func (h *GenericMetadataHandler) checkParentExists(c *gin.Context, entityUUID uuid.UUID) bool {
	if h.verifyParent == nil {
		return true
	}
	if err := h.verifyParent(c.Request.Context(), entityUUID); err != nil {
		entityLabel := h.entityType
		HandleRequestError(c, NotFoundError(entityLabel+" not found"))
		return false
	}
	return true
}

// List retrieves all metadata for an entity.
func (h *GenericMetadataHandler) List(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.List - retrieving metadata for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for %s %s (user: %s)", h.entityType, entityID, userEmail)

	if !h.checkParentExists(c, entityUUID) {
		return
	}

	metadata, err := h.metadataStore.List(c.Request.Context(), h.entityType, entityID)
	if err != nil {
		logger.Error("Failed to retrieve %s metadata for %s: %v", h.entityType, entityID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for %s %s", len(metadata), h.entityType, entityID)
	c.JSON(http.StatusOK, metadata)
}

// GetByKey retrieves a specific metadata entry by key.
func (h *GenericMetadataHandler) GetByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.GetByKey - retrieving specific metadata entry for %s", h.entityType)

	_, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	key := c.Param("key")
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for %s %s (user: %s)", key, h.entityType, entityID, userEmail)

	metadata, err := h.metadataStore.Get(c.Request.Context(), h.entityType, entityID, key)
	if err != nil {
		logger.Error("Failed to retrieve %s metadata key '%s' for %s: %v", h.entityType, key, entityID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for %s %s", key, h.entityType, entityID)
	c.JSON(http.StatusOK, metadata)
}

// Create creates a new metadata entry.
func (h *GenericMetadataHandler) Create(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.Create - creating new metadata entry for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !h.checkParentExists(c, entityUUID) {
		return
	}

	var metadata Metadata
	if err := c.ShouldBindJSON(&metadata); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	// Validate metadata key
	if keyErr := validateMetadataKeyString(metadata.Key); keyErr != nil {
		HandleRequestError(c, keyErr)
		return
	}

	logger.Debug("Creating metadata key '%s' for %s %s (user: %s)", metadata.Key, h.entityType, entityID, userEmail)

	if err := h.metadataStore.Create(c.Request.Context(), h.entityType, entityID, &metadata); err != nil {
		logger.Error("Failed to create %s metadata key '%s' for %s: %v", h.entityType, metadata.Key, entityID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to create metadata"))
		return
	}

	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), h.entityType, entityID, metadata.Key)
	if err != nil {
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for %s %s", metadata.Key, h.entityType, entityID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// Update updates an existing metadata entry.
func (h *GenericMetadataHandler) Update(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.Update - updating metadata entry for %s", h.entityType)

	_, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	key := c.Param("key")
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate metadata key from URL parameter
	if keyErr := validateMetadataKeyString(key); keyErr != nil {
		HandleRequestError(c, keyErr)
		return
	}

	// Parse only the value from request body (key comes from URL parameter).
	// We use a local struct instead of Metadata because the generated Metadata
	// struct has binding:"required" on both key and value, but the OpenAPI spec
	// defines the PUT body as containing only the value field.
	var body struct {
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	metadata := Metadata{Key: key, Value: body.Value}

	logger.Debug("Updating metadata key '%s' for %s %s (user: %s)", key, h.entityType, entityID, userEmail)

	if err := h.metadataStore.Update(c.Request.Context(), h.entityType, entityID, &metadata); err != nil {
		logger.Error("Failed to update %s metadata key '%s' for %s: %v", h.entityType, key, entityID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to update metadata"))
		return
	}

	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), h.entityType, entityID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for %s %s", key, h.entityType, entityID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// Delete deletes a metadata entry.
func (h *GenericMetadataHandler) Delete(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.Delete - deleting metadata entry for %s", h.entityType)

	_, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	key := c.Param("key")
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for %s %s (user: %s)", key, h.entityType, entityID, userEmail)

	if err := h.metadataStore.Delete(c.Request.Context(), h.entityType, entityID, key); err != nil {
		logger.Error("Failed to delete %s metadata key '%s' for %s: %v", h.entityType, key, entityID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for %s %s", key, h.entityType, entityID)
	c.Status(http.StatusNoContent)
}

// BulkCreate creates multiple metadata entries in a single request.
func (h *GenericMetadataHandler) BulkCreate(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.BulkCreate - creating multiple metadata entries for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !h.checkParentExists(c, entityUUID) {
		return
	}

	var metadataList []Metadata
	if err := c.ShouldBindJSON(&metadataList); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
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

	// Check for duplicate keys and validate each key
	keyMap := make(map[string]bool)
	for _, metadata := range metadataList {
		if keyErr := validateMetadataKeyString(metadata.Key); keyErr != nil {
			HandleRequestError(c, keyErr)
			return
		}
		if keyMap[metadata.Key] {
			HandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
			return
		}
		keyMap[metadata.Key] = true
	}

	logger.Debug("Bulk creating %d metadata entries for %s %s (user: %s)",
		len(metadataList), h.entityType, entityID, userEmail)

	if err := h.metadataStore.BulkCreate(c.Request.Context(), h.entityType, entityID, metadataList); err != nil {
		logger.Error("Failed to bulk create %s metadata for %s: %v", h.entityType, entityID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	createdMetadata, err := h.metadataStore.List(c.Request.Context(), h.entityType, entityID)
	if err != nil {
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for %s %s", len(metadataList), h.entityType, entityID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// BulkUpsert updates or creates multiple metadata entries in a single request.
func (h *GenericMetadataHandler) BulkUpsert(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.BulkUpsert - upserting multiple metadata entries for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !h.checkParentExists(c, entityUUID) {
		return
	}

	var metadataList []Metadata
	if err := c.ShouldBindJSON(&metadataList); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body"))
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

	// Check for duplicate keys and validate each key
	keyMap := make(map[string]bool)
	for _, metadata := range metadataList {
		if keyErr := validateMetadataKeyString(metadata.Key); keyErr != nil {
			HandleRequestError(c, keyErr)
			return
		}
		if keyMap[metadata.Key] {
			HandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
			return
		}
		keyMap[metadata.Key] = true
	}

	logger.Debug("Bulk upserting %d metadata entries for %s %s (user: %s)",
		len(metadataList), h.entityType, entityID, userEmail)

	if err := h.metadataStore.BulkUpdate(c.Request.Context(), h.entityType, entityID, metadataList); err != nil {
		logger.Error("Failed to bulk upsert %s metadata for %s: %v", h.entityType, entityID, err)
		HandleRequestError(c, ServerError("Failed to upsert metadata entries"))
		return
	}

	upsertedMetadata, err := h.metadataStore.List(c.Request.Context(), h.entityType, entityID)
	if err != nil {
		logger.Error("Failed to retrieve upserted metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk upserted %d metadata entries for %s %s", len(metadataList), h.entityType, entityID)
	c.JSON(http.StatusOK, upsertedMetadata)
}
