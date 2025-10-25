package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// NoteMetadataHandler provides handlers for note metadata operations
type NoteMetadataHandler struct {
	metadataStore    MetadataStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewNoteMetadataHandler creates a new note metadata handler
func NewNoteMetadataHandler(metadataStore MetadataStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *NoteMetadataHandler {
	return &NoteMetadataHandler{
		metadataStore:    metadataStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetNoteMetadata retrieves all metadata for a note
// GET /threat_models/{threat_model_id}/notes/{note_id}/metadata
func (h *NoteMetadataHandler) GetNoteMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetNoteMetadata - retrieving metadata for note")

	// Extract note ID from URL
	noteID := c.Param("note_id")
	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata for note %s (user: %s)", noteID, userEmail)

	// Get metadata from store
	metadata, err := h.metadataStore.List(c.Request.Context(), "note", noteID)
	if err != nil {
		logger.Error("Failed to retrieve note metadata for %s: %v", noteID, err)
		HandleRequestError(c, ServerError("Failed to retrieve metadata"))
		return
	}

	logger.Debug("Successfully retrieved %d metadata items for note %s", len(metadata), noteID)
	c.JSON(http.StatusOK, metadata)
}

// GetNoteMetadataByKey retrieves a specific metadata entry by key
// GET /threat_models/{threat_model_id}/notes/{note_id}/metadata/{key}
func (h *NoteMetadataHandler) GetNoteMetadataByKey(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetNoteMetadataByKey - retrieving specific metadata entry")

	// Extract note ID and key from URL
	noteID := c.Param("note_id")
	key := c.Param("key")

	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving metadata key '%s' for note %s (user: %s)", key, noteID, userEmail)

	// Get metadata entry from store
	metadata, err := h.metadataStore.Get(c.Request.Context(), "note", noteID, key)
	if err != nil {
		logger.Error("Failed to retrieve note metadata key '%s' for %s: %v", key, noteID, err)
		HandleRequestError(c, NotFoundError("Metadata entry not found"))
		return
	}

	logger.Debug("Successfully retrieved metadata key '%s' for note %s", key, noteID)
	c.JSON(http.StatusOK, metadata)
}

// CreateNoteMetadata creates a new metadata entry for a note
// POST /threat_models/{threat_model_id}/notes/{note_id}/metadata
func (h *NoteMetadataHandler) CreateNoteMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateNoteMetadata - creating new metadata entry")

	// Extract note ID from URL
	noteID := c.Param("note_id")
	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Creating metadata key '%s' for note %s (user: %s)", metadata.Key, noteID, userEmail)

	// Create metadata entry in store
	if err := h.metadataStore.Create(c.Request.Context(), "note", noteID, &metadata); err != nil {
		logger.Error("Failed to create note metadata key '%s' for %s: %v", metadata.Key, noteID, err)
		HandleRequestError(c, ServerError("Failed to create metadata"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.Get(c.Request.Context(), "note", noteID, metadata.Key)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadata)
		return
	}

	logger.Debug("Successfully created metadata key '%s' for note %s", metadata.Key, noteID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// UpdateNoteMetadata updates an existing metadata entry
// PUT /threat_models/{threat_model_id}/notes/{note_id}/metadata/{key}
func (h *NoteMetadataHandler) UpdateNoteMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateNoteMetadata - updating metadata entry")

	// Extract note ID and key from URL
	noteID := c.Param("note_id")
	key := c.Param("key")

	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Updating metadata key '%s' for note %s (user: %s)", key, noteID, userEmail)

	// Update metadata entry in store
	if err := h.metadataStore.Update(c.Request.Context(), "note", noteID, &metadata); err != nil {
		logger.Error("Failed to update note metadata key '%s' for %s: %v", key, noteID, err)
		HandleRequestError(c, ServerError("Failed to update metadata"))
		return
	}

	// Retrieve the updated metadata to return
	updatedMetadata, err := h.metadataStore.Get(c.Request.Context(), "note", noteID, key)
	if err != nil {
		logger.Error("Failed to retrieve updated metadata: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve updated metadata"))
		return
	}

	logger.Debug("Successfully updated metadata key '%s' for note %s", key, noteID)
	c.JSON(http.StatusOK, updatedMetadata)
}

// DeleteNoteMetadata deletes a metadata entry
// DELETE /threat_models/{threat_model_id}/notes/{note_id}/metadata/{key}
func (h *NoteMetadataHandler) DeleteNoteMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteNoteMetadata - deleting metadata entry")

	// Extract note ID and key from URL
	noteID := c.Param("note_id")
	key := c.Param("key")

	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}
	if key == "" {
		HandleRequestError(c, InvalidInputError("Missing metadata key"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting metadata key '%s' for note %s (user: %s)", key, noteID, userEmail)

	// Delete metadata entry from store
	if err := h.metadataStore.Delete(c.Request.Context(), "note", noteID, key); err != nil {
		logger.Error("Failed to delete note metadata key '%s' for %s: %v", key, noteID, err)
		HandleRequestError(c, ServerError("Failed to delete metadata"))
		return
	}

	logger.Debug("Successfully deleted metadata key '%s' for note %s", key, noteID)
	c.JSON(http.StatusNoContent, nil)
}

// BulkCreateNoteMetadata creates multiple metadata entries in a single request
// POST /threat_models/{threat_model_id}/notes/{note_id}/metadata/bulk
func (h *NoteMetadataHandler) BulkCreateNoteMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateNoteMetadata - creating multiple metadata entries")

	// Extract note ID from URL
	noteID := c.Param("note_id")
	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Bulk creating %d metadata entries for note %s (user: %s)",
		len(metadataList), noteID, userEmail)

	// Create metadata entries in store
	if err := h.metadataStore.BulkCreate(c.Request.Context(), "note", noteID, metadataList); err != nil {
		logger.Error("Failed to bulk create note metadata for %s: %v", noteID, err)
		HandleRequestError(c, ServerError("Failed to create metadata entries"))
		return
	}

	// Retrieve the created metadata to return with timestamps
	createdMetadata, err := h.metadataStore.List(c.Request.Context(), "note", noteID)
	if err != nil {
		// Log error but still return success since creation succeeded
		logger.Error("Failed to retrieve created metadata: %v", err)
		c.JSON(http.StatusCreated, metadataList)
		return
	}

	logger.Debug("Successfully bulk created %d metadata entries for note %s", len(metadataList), noteID)
	c.JSON(http.StatusCreated, createdMetadata)
}

// BulkUpdateNoteMetadata updates multiple metadata entries in a single request
// PUT /threat_models/{threat_model_id}/notes/{note_id}/metadata/bulk
func (h *NoteMetadataHandler) BulkUpdateNoteMetadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateNoteMetadata - updating multiple metadata entries")

	// Extract note ID from URL
	noteID := c.Param("note_id")
	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
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

	logger.Debug("Bulk updating %d metadata entries for note %s (user: %s)",
		len(metadataList), noteID, userEmail)

	// Update metadata entries in store
	if err := h.metadataStore.BulkUpdate(c.Request.Context(), "note", noteID, metadataList); err != nil {
		logger.Error("Failed to bulk update note metadata for %s: %v", noteID, err)
		HandleRequestError(c, ServerError("Failed to update metadata entries"))
		return
	}

	// Retrieve the updated metadata to return with timestamps
	updatedMetadata, err := h.metadataStore.List(c.Request.Context(), "note", noteID)
	if err != nil {
		// Log error but still return success since update succeeded
		logger.Error("Failed to retrieve updated metadata: %v", err)
		c.JSON(http.StatusOK, metadataList)
		return
	}

	logger.Debug("Successfully bulk updated %d metadata entries for note %s", len(metadataList), noteID)
	c.JSON(http.StatusOK, updatedMetadata)
}
