package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ericfitz/tmi/internal/slogging"
)

// validMetadataKeyPattern matches allowed metadata key characters:
// alphanumeric, hyphens, underscores, dots, colons. Length 1-256.
var validMetadataKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9._\-:]{1,256}$`)

// maxMetadataValueBytes is the maximum byte length for metadata values,
// matching the database varchar(1024) constraint.
const maxMetadataValueBytes = 1024

// validateMetadataKeyString validates a metadata key string.
// Keys must be 1-256 characters and contain only alphanumeric, hyphens, underscores, dots, and colons.
// SEM@59c58c6a840231ad2c078c9afd1e7bac7a07b651: validate a metadata key against length and character-set constraints (pure)
func validateMetadataKeyString(key string) error {
	if key == "" {
		return InvalidInputError("Metadata key must not be empty")
	}
	if !validMetadataKeyPattern.MatchString(key) {
		return InvalidInputError("Metadata key must be 1-256 characters and contain only alphanumeric characters, hyphens, underscores, dots, and colons")
	}
	return nil
}

// validateMetadataValueString validates a metadata value string.
// Values must not exceed 1024 bytes (UTF-8 encoded) to match the database constraint.
// SEM@1fe8f3ce53d582458692160f00d30b4873a1e534: validate a metadata value does not exceed the 1024-byte database limit (pure)
func validateMetadataValueString(value string) error {
	if len(value) > maxMetadataValueBytes {
		return InvalidInputError("Metadata value must not exceed 1024 bytes")
	}
	return nil
}

// sanitizeMetadataValue strips HTML tags from a metadata value and checks for
// template injection patterns. Returns the sanitized value and any validation error.
// SEM@e46ea124c184eb4520f5c1ad2f1ea19fd52b432b: strip HTML and reject injection patterns from a metadata value; return sanitized string (pure)
func sanitizeMetadataValue(value string) (string, error) {
	// Check for HTML/XSS injection in the original value before stripping tags,
	// so that payloads like <svg onload=alert('XSS')> are rejected with 400
	// rather than silently stripped to an empty string.
	if err := CheckHTMLInjection(value, "value"); err != nil {
		return "", err
	}
	sanitized := SanitizePlainText(value)
	return sanitized, nil
}

// ParentVerifier is a function that checks if a parent entity exists.
// It returns nil if the entity exists, or an error if not.
// SEM@59c58c6a840231ad2c078c9afd1e7bac7a07b651: function type that confirms a parent entity exists by UUID (reads DB)
type ParentVerifier func(ctx context.Context, id uuid.UUID) error

// GenericMetadataHandler provides handlers for metadata operations on any entity type.
// It replaces entity-specific metadata handlers with a single implementation.
// SEM@0734f383e8c73aef4842c88dc88e90d0440f048a: reusable handler struct for metadata CRUD operations on any entity type
type GenericMetadataHandler struct {
	metadataStore   MetadataRepository
	entityType      string
	parentParamName string
	verifyParent    ParentVerifier
}

// NewGenericMetadataHandler creates a new generic metadata handler.
//
// Parameters:
//   - metadataStore: the metadata repository to use
//   - entityType: the entity type string (e.g., "survey", "threat_model")
//   - parentParamName: the gin parameter name for the parent entity ID (e.g., "survey_id")
//   - verifyParent: optional function to verify parent entity exists (nil to skip)
// SEM@0734f383e8c73aef4842c88dc88e90d0440f048a: build a generic metadata handler for the given store, entity type, and optional parent verifier (pure)
func NewGenericMetadataHandler(metadataStore MetadataRepository, entityType, parentParamName string, verifyParent ParentVerifier) *GenericMetadataHandler {
	return &GenericMetadataHandler{
		metadataStore:   metadataStore,
		entityType:      entityType,
		parentParamName: parentParamName,
		verifyParent:    verifyParent,
	}
}

// extractEntityID extracts and validates the parent entity UUID from the gin context.
// SEM@59c58c6a840231ad2c078c9afd1e7bac7a07b651: parse and validate the parent entity UUID from the request path parameter (pure)
func (h *GenericMetadataHandler) extractEntityID(c *gin.Context) (uuid.UUID, string, bool) {
	entityUUID, err := ExtractUUID(c, h.parentParamName)
	if err != nil {
		return uuid.Nil, "", false // Error response already sent by ExtractUUID
	}
	return entityUUID, entityUUID.String(), true
}

// checkParentExists verifies the parent entity exists if a verifier is configured.
// SEM@59c58c6a840231ad2c078c9afd1e7bac7a07b651: verify the parent entity exists via the configured verifier; respond 404 on failure (reads DB)
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
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: list all metadata entries for a parent entity (reads DB)
func (h *GenericMetadataHandler) List(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.List - retrieving metadata for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for %s %s (user: %s)", h.entityType, entityID, user.Email)

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
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: fetch a single metadata entry by key for a parent entity (reads DB)
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

	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for %s %s (user: %s)", key, h.entityType, entityID, user.Email)

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
// SEM@3c1a01558012bffd79e59f37ab15f2ccc823c29c: store a new metadata key-value pair under a parent entity, rejecting duplicates (reads DB)
func (h *GenericMetadataHandler) Create(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.Create - creating new metadata entry for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	user, err := GetAuthenticatedUser(c)
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

	// Validate metadata key and value
	if keyErr := validateMetadataKeyString(metadata.Key); keyErr != nil {
		HandleRequestError(c, keyErr)
		return
	}
	if valErr := validateMetadataValueString(metadata.Value); valErr != nil {
		HandleRequestError(c, valErr)
		return
	}

	// Sanitize metadata value (strip HTML tags, check for template injection)
	sanitizedValue, sanitizeErr := sanitizeMetadataValue(metadata.Value)
	if sanitizeErr != nil {
		HandleRequestError(c, sanitizeErr)
		return
	}
	metadata.Value = sanitizedValue

	logger.Debug("Creating metadata key '%s' for %s %s (user: %s)", metadata.Key, h.entityType, entityID, user.Email)

	if err := h.metadataStore.Create(c.Request.Context(), h.entityType, entityID, &metadata); err != nil {
		var conflictErr *MetadataConflictError
		if errors.As(err, &conflictErr) {
			HandleRequestError(c, ConflictError(fmt.Sprintf("Metadata key already exists: %s", conflictErr.ConflictingKeys[0])))
			return
		}
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
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: replace the value of an existing metadata entry identified by key (reads DB)
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

	user, err := GetAuthenticatedUser(c)
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

	// Validate value length
	if valErr := validateMetadataValueString(body.Value); valErr != nil {
		HandleRequestError(c, valErr)
		return
	}

	// Sanitize metadata value (strip HTML tags, check for template injection)
	sanitizedValue, sanitizeErr := sanitizeMetadataValue(body.Value)
	if sanitizeErr != nil {
		HandleRequestError(c, sanitizeErr)
		return
	}

	metadata := Metadata{Key: key, Value: sanitizedValue}

	logger.Debug("Updating metadata key '%s' for %s %s (user: %s)", key, h.entityType, entityID, user.Email)

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
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: delete a metadata entry by key from a parent entity (reads DB)
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

	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for %s %s (user: %s)", key, h.entityType, entityID, user.Email)

	if err := h.metadataStore.Delete(c.Request.Context(), h.entityType, entityID, key); err != nil {
		logger.Error("Failed to delete %s metadata key '%s' for %s: %v", h.entityType, key, entityID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Metadata not found", "Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for %s %s", key, h.entityType, entityID)
	c.Status(http.StatusNoContent)
}

// BulkCreate creates multiple metadata entries in a single request.
// SEM@3c1a01558012bffd79e59f37ab15f2ccc823c29c: store multiple new metadata entries for an entity, rejecting duplicates or conflicts (reads DB)
func (h *GenericMetadataHandler) BulkCreate(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.BulkCreate - creating multiple metadata entries for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	user, err := GetAuthenticatedUser(c)
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

	// Check for duplicate keys and validate/sanitize each key and value
	keyMap := make(map[string]bool)
	for i, metadata := range metadataList {
		if keyErr := validateMetadataKeyString(metadata.Key); keyErr != nil {
			HandleRequestError(c, keyErr)
			return
		}
		if valErr := validateMetadataValueString(metadata.Value); valErr != nil {
			HandleRequestError(c, valErr)
			return
		}
		sanitizedValue, sanitizeErr := sanitizeMetadataValue(metadata.Value)
		if sanitizeErr != nil {
			HandleRequestError(c, sanitizeErr)
			return
		}
		metadataList[i].Value = sanitizedValue
		if keyMap[metadata.Key] {
			HandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
			return
		}
		keyMap[metadata.Key] = true
	}

	logger.Debug("Bulk creating %d metadata entries for %s %s (user: %s)",
		len(metadataList), h.entityType, entityID, user.Email)

	if err := h.metadataStore.BulkCreate(c.Request.Context(), h.entityType, entityID, metadataList); err != nil {
		var conflictErr *MetadataConflictError
		if errors.As(err, &conflictErr) {
			HandleRequestError(c, ConflictError(fmt.Sprintf("Metadata key(s) already exist: %s", strings.Join(conflictErr.ConflictingKeys, ", "))))
			return
		}
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
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: update or create multiple metadata entries for an entity in one request (reads DB)
func (h *GenericMetadataHandler) BulkUpsert(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.BulkUpsert - upserting multiple metadata entries for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	user, err := GetAuthenticatedUser(c)
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

	// Check for duplicate keys and validate/sanitize each key and value
	keyMap := make(map[string]bool)
	for i, metadata := range metadataList {
		if keyErr := validateMetadataKeyString(metadata.Key); keyErr != nil {
			HandleRequestError(c, keyErr)
			return
		}
		if valErr := validateMetadataValueString(metadata.Value); valErr != nil {
			HandleRequestError(c, valErr)
			return
		}
		sanitizedValue, sanitizeErr := sanitizeMetadataValue(metadata.Value)
		if sanitizeErr != nil {
			HandleRequestError(c, sanitizeErr)
			return
		}
		metadataList[i].Value = sanitizedValue
		if keyMap[metadata.Key] {
			HandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
			return
		}
		keyMap[metadata.Key] = true
	}

	logger.Debug("Bulk upserting %d metadata entries for %s %s (user: %s)",
		len(metadataList), h.entityType, entityID, user.Email)

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

// BulkReplace replaces all metadata for an entity with the provided set.
// All existing metadata is deleted and replaced with the provided entries.
// An empty array clears all metadata for the entity.
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: replace all metadata entries for an entity with the provided set, deleting previous entries (mutates shared state)
func (h *GenericMetadataHandler) BulkReplace(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GenericMetadataHandler.BulkReplace - replacing all metadata for %s", h.entityType)

	entityUUID, entityID, ok := h.extractEntityID(c)
	if !ok {
		return
	}

	user, err := GetAuthenticatedUser(c)
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

	if len(metadataList) > 20 {
		HandleRequestError(c, InvalidInputError("Maximum 20 metadata entries allowed per bulk operation"))
		return
	}

	// Check for duplicate keys and validate/sanitize each key and value
	keyMap := make(map[string]bool)
	for i, metadata := range metadataList {
		if keyErr := validateMetadataKeyString(metadata.Key); keyErr != nil {
			HandleRequestError(c, keyErr)
			return
		}
		if valErr := validateMetadataValueString(metadata.Value); valErr != nil {
			HandleRequestError(c, valErr)
			return
		}
		sanitizedValue, sanitizeErr := sanitizeMetadataValue(metadata.Value)
		if sanitizeErr != nil {
			HandleRequestError(c, sanitizeErr)
			return
		}
		metadataList[i].Value = sanitizedValue
		if keyMap[metadata.Key] {
			HandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
			return
		}
		keyMap[metadata.Key] = true
	}

	logger.Debug("Bulk replacing metadata for %s %s with %d entries (user: %s)",
		h.entityType, entityID, len(metadataList), user.Email)

	if err := h.metadataStore.BulkReplace(c.Request.Context(), h.entityType, entityID, metadataList); err != nil {
		logger.Error("Failed to bulk replace %s metadata for %s: %v", h.entityType, entityID, err)
		HandleRequestError(c, ServerError("Failed to replace metadata entries"))
		return
	}

	replacedMetadata, err := h.metadataStore.List(c.Request.Context(), h.entityType, entityID)
	if err != nil {
		logger.Error("Failed to retrieve replaced metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk replaced metadata for %s %s with %d entries", h.entityType, entityID, len(metadataList))
	c.JSON(http.StatusOK, replacedMetadata)
}
