package api

import (
	"errors"
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

// requireSurveyResponseAccessForTriageNote verifies that the parent survey
// response exists AND that the caller has the required role on it. Triage
// notes inherit confidentiality from the parent survey response — without
// this check, anyone authenticated could enumerate or write triage notes on
// arbitrary survey responses (T5, #357).
//
// Returns the parent survey-response ID string and true on success. On
// failure (not found OR access denied — collapsed into 404 to avoid existence
// disclosure) writes the error response and returns false.
func (h *TriageNoteSubResourceHandler) requireSurveyResponseAccessForTriageNote(
	c *gin.Context,
	surveyResponseID string,
	requiredRole AuthorizationRole,
) bool {
	surveyResponseUUID, _ := ParseUUID(surveyResponseID) // already validated format by caller
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseUUID, requiredRole); !ok {
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

	if !h.requireSurveyResponseAccessForTriageNote(c, surveyResponseID, AuthorizationRoleReader) {
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

	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving triage notes for survey response %s (user: %s, offset: %d, limit: %d)",
		surveyResponseID, user.Email, offset, limit)

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

	if !h.requireSurveyResponseAccessForTriageNote(c, surveyResponseID, AuthorizationRoleReader) {
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

	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving triage note %d for survey response %s (user: %s)", triageNoteID, surveyResponseID, user.Email)

	note, err := h.triageNoteStore.Get(c.Request.Context(), surveyResponseID, triageNoteID)
	if err != nil {
		if errors.Is(err, ErrTriageNoteNotFound) {
			HandleRequestError(c, NotFoundError("Triage note not found"))
		} else {
			logger.Error("Failed to retrieve triage note %d: %v", triageNoteID, err)
			HandleRequestError(c, ServerError("Failed to retrieve triage note"))
		}
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

	// Create requires writer role on the parent survey response. Triage
	// notes are append-only by design, but they still constitute a write to
	// the parent so writer (not reader) is the correct gate.
	if !h.requireSurveyResponseAccessForTriageNote(c, surveyResponseID, AuthorizationRoleWriter) {
		return
	}

	user, err := GetAuthenticatedUser(c)
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

	logger.Debug("Creating triage note in survey response %s (user: %s)", surveyResponseID, user.Email)

	if err := h.triageNoteStore.Create(c.Request.Context(), note, surveyResponseID, userInternalUUID); err != nil {
		logger.Error("Failed to create triage note: %v", err)
		HandleRequestError(c, ServerError("Failed to create triage note"))
		return
	}

	logger.Debug("Successfully created triage note %d", *note.Id)
	c.JSON(http.StatusCreated, note)
}
