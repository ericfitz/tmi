package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// NoteSubResourceHandler provides handlers for note sub-resource operations
type NoteSubResourceHandler struct {
	noteStore        NoteStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewNoteSubResourceHandler creates a new note sub-resource handler
func NewNoteSubResourceHandler(noteStore NoteStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *NoteSubResourceHandler {
	return &NoteSubResourceHandler{
		noteStore:        noteStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetNotes retrieves all notes for a threat model with pagination
// GET /threat_models/{threat_model_id}/notes
func (h *NoteSubResourceHandler) GetNotes(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetNotes - retrieving notes for threat model")

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

	logger.Debug("Retrieving notes for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, userEmail, offset, limit)

	// Get notes from store (authorization is handled by middleware)
	notes, err := h.noteStore.List(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve notes: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve notes"))
		return
	}

	logger.Debug("Successfully retrieved %d notes", len(notes))
	c.JSON(http.StatusOK, notes)
}

// GetNote retrieves a specific note by ID
// GET /threat_models/{threat_model_id}/notes/{note_id}
func (h *NoteSubResourceHandler) GetNote(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetNote - retrieving specific note")

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

	logger.Debug("Retrieving note %s (user: %s)", noteID, userEmail)

	// Get note from store
	note, err := h.noteStore.Get(c.Request.Context(), noteID)
	if err != nil {
		logger.Error("Failed to retrieve note %s: %v", noteID, err)
		HandleRequestError(c, NotFoundError("Note not found"))
		return
	}

	logger.Debug("Successfully retrieved note %s", noteID)
	c.JSON(http.StatusOK, note)
}

// CreateNote creates a new note in a threat model
// POST /threat_models/{threat_model_id}/notes
func (h *NoteSubResourceHandler) CreateNote(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateNote - creating new note")

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
	config := ValidationConfigs["note_create"]
	note, err := ValidateAndParseRequest[Note](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Generate UUID if not provided
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
	}

	logger.Debug("Creating note %s in threat model %s (user: %s)",
		note.Id.String(), threatModelID, userEmail)

	// Create note in store
	if err := h.noteStore.Create(c.Request.Context(), note, threatModelID); err != nil {
		logger.Error("Failed to create note: %v", err)
		HandleRequestError(c, ServerError("Failed to create note"))
		return
	}

	logger.Debug("Successfully created note %s", note.Id.String())
	c.JSON(http.StatusCreated, note)
}

// UpdateNote updates an existing note
// PUT /threat_models/{threat_model_id}/notes/{note_id}
func (h *NoteSubResourceHandler) UpdateNote(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateNote - updating existing note")

	// Extract note ID from URL
	noteID := c.Param("note_id")
	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}

	// Validate note ID format
	noteUUID, err := ParseUUID(noteID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
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
	config := ValidationConfigs["note_update"]
	note, err := ValidateAndParseRequest[Note](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set ID from URL (override any value in body)
	note.Id = &noteUUID

	logger.Debug("Updating note %s (user: %s)", noteID, userEmail)

	// Update note in store
	if err := h.noteStore.Update(c.Request.Context(), note, threatModelID); err != nil {
		logger.Error("Failed to update note %s: %v", noteID, err)
		HandleRequestError(c, ServerError("Failed to update note"))
		return
	}

	logger.Debug("Successfully updated note %s", noteID)
	c.JSON(http.StatusOK, note)
}

// DeleteNote deletes a note
// DELETE /threat_models/{threat_model_id}/notes/{note_id}
func (h *NoteSubResourceHandler) DeleteNote(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteNote - deleting note")

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

	logger.Debug("Deleting note %s (user: %s)", noteID, userEmail)

	// Delete note from store
	if err := h.noteStore.Delete(c.Request.Context(), noteID); err != nil {
		logger.Error("Failed to delete note %s: %v", noteID, err)
		HandleRequestError(c, ServerError("Failed to delete note"))
		return
	}

	logger.Debug("Successfully deleted note %s", noteID)
	c.JSON(http.StatusNoContent, nil)
}
