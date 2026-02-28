package api

import (
	"net/http"
	"strconv"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// TriageNoteSubResourceHandler provides handlers for triage note sub-resource operations.
// Triage notes are append-only: only create and read operations are supported.
type TriageNoteSubResourceHandler struct {
	triageNoteStore TriageNoteStore
}

// NewTriageNoteSubResourceHandler creates a new triage note sub-resource handler
func NewTriageNoteSubResourceHandler(store TriageNoteStore) *TriageNoteSubResourceHandler {
	return &TriageNoteSubResourceHandler{
		triageNoteStore: store,
	}
}

// verifySurveyResponseExists checks that the parent survey response exists, returning false
// and sending an error response if it does not. Callers should return immediately when false.
func (h *TriageNoteSubResourceHandler) verifySurveyResponseExists(c *gin.Context, surveyResponseID string) bool {
	surveyResponseUUID, _ := ParseUUID(surveyResponseID) // already validated format by caller
	resp, err := GlobalSurveyResponseStore.Get(c.Request.Context(), surveyResponseUUID)
	if err != nil {
		HandleRequestError(c, ServerError("Failed to verify survey response"))
		return false
	}
	if resp == nil {
		HandleRequestError(c, NotFoundError("Survey response not found"))
		return false
	}
	return true
}

// ListTriageNotes retrieves all triage notes for a survey response with pagination
func (h *TriageNoteSubResourceHandler) ListTriageNotes(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("ListTriageNotes - retrieving triage notes for survey response")

	surveyResponseID := c.Param("survey_response_id")
	if surveyResponseID == "" {
		HandleRequestError(c, InvalidIDError("Missing survey response ID"))
		return
	}

	if _, err := ParseUUID(surveyResponseID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid survey response ID format, must be a valid UUID"))
		return
	}

	if !h.verifySurveyResponseExists(c, surveyResponseID) {
		return
	}

	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	if limit < 1 || limit > 100 {
		HandleRequestError(c, InvalidInputError("Limit must be between 1 and 100"))
		return
	}
	if offset < 0 {
		HandleRequestError(c, InvalidInputError("Offset must be non-negative"))
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving triage notes for survey response %s (user: %s, offset: %d, limit: %d)",
		surveyResponseID, userEmail, offset, limit)

	notes, err := h.triageNoteStore.List(c.Request.Context(), surveyResponseID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve triage notes: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve triage notes"))
		return
	}

	total, err := h.triageNoteStore.Count(c.Request.Context(), surveyResponseID)
	if err != nil {
		logger.Warn("Failed to get triage note count, using page size: %v", err)
		total = len(notes)
	}

	noteItems := make([]TriageNoteListItem, 0, len(notes))
	for _, n := range notes {
		noteItems = append(noteItems, TriageNoteListItem{
			Id:        n.Id,
			Name:      n.Name,
			CreatedAt: n.CreatedAt,
			CreatedBy: n.CreatedBy,
		})
	}

	logger.Debug("Successfully retrieved %d triage notes (total: %d)", len(notes), total)
	c.JSON(http.StatusOK, ListTriageNotesResponse{
		TriageNotes: noteItems,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
	})
}

// GetTriageNote retrieves a specific triage note by ID
func (h *TriageNoteSubResourceHandler) GetTriageNote(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetTriageNote - retrieving specific triage note")

	surveyResponseID := c.Param("survey_response_id")
	if surveyResponseID == "" {
		HandleRequestError(c, InvalidIDError("Missing survey response ID"))
		return
	}

	if _, err := ParseUUID(surveyResponseID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid survey response ID format, must be a valid UUID"))
		return
	}

	if !h.verifySurveyResponseExists(c, surveyResponseID) {
		return
	}

	triageNoteIDStr := c.Param("triage_note_id")
	if triageNoteIDStr == "" {
		HandleRequestError(c, InvalidIDError("Missing triage note ID"))
		return
	}

	triageNoteID, err := strconv.Atoi(triageNoteIDStr)
	if err != nil || triageNoteID < 1 {
		HandleRequestError(c, InvalidIDError("Invalid triage note ID, must be a positive integer"))
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving triage note %d for survey response %s (user: %s)", triageNoteID, surveyResponseID, userEmail)

	note, err := h.triageNoteStore.Get(c.Request.Context(), surveyResponseID, triageNoteID)
	if err != nil {
		logger.Error("Failed to retrieve triage note %d: %v", triageNoteID, err)
		HandleRequestError(c, NotFoundError("Triage note not found"))
		return
	}

	logger.Debug("Successfully retrieved triage note %d", triageNoteID)
	c.JSON(http.StatusOK, note)
}

// CreateTriageNote creates a new triage note in a survey response
func (h *TriageNoteSubResourceHandler) CreateTriageNote(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateTriageNote - creating new triage note")

	surveyResponseID := c.Param("survey_response_id")
	if surveyResponseID == "" {
		HandleRequestError(c, InvalidIDError("Missing survey response ID"))
		return
	}

	if _, err := ParseUUID(surveyResponseID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid survey response ID format, must be a valid UUID"))
		return
	}

	if !h.verifySurveyResponseExists(c, surveyResponseID) {
		return
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get internal UUID for created_by
	userInternalUUID := ""
	if internalUUID, exists := c.Get("userInternalUUID"); exists {
		userInternalUUID, _ = internalUUID.(string)
	}
	if userInternalUUID == "" {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "User not authenticated",
		})
		return
	}

	config := ValidationConfigs["triage_note_create"]
	note, err := ValidateAndParseRequest[TriageNote](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Sanitize markdown content (strip dangerous HTML, preserve safe elements)
	note.Content = SanitizeMarkdownContent(note.Content)

	logger.Debug("Creating triage note in survey response %s (user: %s)", surveyResponseID, userEmail)

	if err := h.triageNoteStore.Create(c.Request.Context(), note, surveyResponseID, userInternalUUID); err != nil {
		logger.Error("Failed to create triage note: %v", err)
		HandleRequestError(c, ServerError("Failed to create triage note"))
		return
	}

	logger.Debug("Successfully created triage note %d", *note.Id)
	c.JSON(http.StatusCreated, note)
}
