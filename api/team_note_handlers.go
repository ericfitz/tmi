package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// isPrivilegedUser checks if the user is an administrator or security reviewer.
func isPrivilegedUser(c *gin.Context) bool {
	isAdmin, _ := IsUserAdministrator(c)
	if isAdmin {
		return true
	}
	isReviewer, _ := IsGroupMemberFromContext(c, GroupSecurityReviewers)
	return isReviewer
}

// ListTeamNotes returns a paginated list of notes for a team.
// GET /teams/{team_id}/notes
func (s *Server) ListTeamNotes(c *gin.Context, teamId openapi_types.UUID, params ListTeamNotesParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be team member or admin
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a team member or administrator to access team notes",
		})
		return
	}

	privileged := isPrivilegedUser(c)

	// Pagination defaults and clamping
	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	if err := ValidatePaginationParams(params.Limit, params.Offset); err != nil {
		HandleRequestError(c, err)
		return
	}

	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	items, total, storeErr := GlobalTeamNoteStore.List(ctx, teamId.String(), offset, limit, privileged)
	if storeErr != nil {
		logger.Error("Failed to list team notes: %v", storeErr)
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Team notes not found", "Failed to list team notes"))
		return
	}

	c.JSON(http.StatusOK, ListTeamNotesResponse{
		Notes:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// CreateTeamNote creates a new note for a team.
// POST /teams/{team_id}/notes
func (s *Server) CreateTeamNote(c *gin.Context, teamId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be team member or admin
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a team member or administrator to create team notes",
		})
		return
	}

	var req TeamNoteInput
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	privileged := isPrivilegedUser(c)

	// Sharable field rules
	if !privileged && req.Sharable != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Only administrators and security reviewers can set the sharable field",
		})
		return
	}

	// Default sharable: true for regular users, false for privileged
	sharableDefault := !privileged
	if req.Sharable == nil {
		req.Sharable = &sharableDefault
	}

	// Sanitize text fields
	req.Name = SanitizePlainText(req.Name)
	req.Content = SanitizeMarkdownContent(req.Content)
	req.Description = SanitizeOptionalString(req.Description)

	note := TeamNote{
		Name:         req.Name,
		Content:      req.Content,
		Description:  req.Description,
		Sharable:     req.Sharable,
		TimmyEnabled: req.TimmyEnabled,
	}

	result, storeErr := GlobalTeamNoteStore.Create(ctx, &note, teamId.String())
	if storeErr != nil {
		logger.Error("Failed to create team note: %v", storeErr)
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Team not found", "Failed to create team note"))
		return
	}

	c.JSON(http.StatusCreated, result)
}

// GetTeamNote returns a specific team note.
// GET /teams/{team_id}/notes/{team_note_id}
func (s *Server) GetTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be team member or admin
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a team member or administrator to access team notes",
		})
		return
	}

	note, storeErr := GlobalTeamNoteStore.Get(ctx, teamNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Team note not found", "Failed to retrieve team note"))
		return
	}

	// Non-privileged users cannot see non-sharable notes (return 404 to hide existence)
	if !isPrivilegedUser(c) && note.Sharable != nil && !*note.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Team note not found",
		})
		return
	}

	c.JSON(http.StatusOK, note)
}

// UpdateTeamNote replaces a team note.
// PUT /teams/{team_id}/notes/{team_note_id}
func (s *Server) UpdateTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be team member or admin
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a team member or administrator to update team notes",
		})
		return
	}

	// Get existing note to check sharable status
	existing, storeErr := GlobalTeamNoteStore.Get(ctx, teamNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Team note not found", "Failed to retrieve team note"))
		return
	}

	privileged := isPrivilegedUser(c)

	// Non-privileged users cannot update non-sharable notes (return 404 to hide existence)
	if !privileged && existing.Sharable != nil && !*existing.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Team note not found",
		})
		return
	}

	var req TeamNoteInput
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Debug("Invalid request body: %v", err)
		HandleRequestError(c, InvalidInputError("Invalid request body"))
		return
	}

	// Non-privileged users cannot set the sharable field
	if !privileged && req.Sharable != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Only administrators and security reviewers can set the sharable field",
		})
		return
	}

	// Sanitize text fields
	req.Name = SanitizePlainText(req.Name)
	req.Content = SanitizeMarkdownContent(req.Content)
	req.Description = SanitizeOptionalString(req.Description)

	note := TeamNote{
		Name:         req.Name,
		Content:      req.Content,
		Description:  req.Description,
		Sharable:     req.Sharable,
		TimmyEnabled: req.TimmyEnabled,
	}

	// Preserve sharable if not provided (non-privileged keeps existing value)
	if req.Sharable == nil {
		note.Sharable = existing.Sharable
	}

	result, updateErr := GlobalTeamNoteStore.Update(ctx, teamNoteId.String(), &note, teamId.String())
	if updateErr != nil {
		logger.Error("Failed to update team note: %v", updateErr)
		HandleRequestError(c, StoreErrorToRequestError(updateErr, "Team note not found", "Failed to update team note"))
		return
	}

	c.JSON(http.StatusOK, result)
}

// PatchTeamNote partially updates a team note using JSON Patch.
// PATCH /teams/{team_id}/notes/{team_note_id}
func (s *Server) PatchTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be team member or admin
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a team member or administrator to patch team notes",
		})
		return
	}

	// Get existing note to check sharable status
	existing, storeErr := GlobalTeamNoteStore.Get(ctx, teamNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Team note not found", "Failed to retrieve team note"))
		return
	}

	privileged := isPrivilegedUser(c)

	// Non-privileged users cannot patch non-sharable notes (return 404 to hide existence)
	if !privileged && existing.Sharable != nil && !*existing.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Team note not found",
		})
		return
	}

	operations, parseErr := ParsePatchRequest(c)
	if parseErr != nil {
		HandleRequestError(c, parseErr)
		return
	}

	// Non-privileged users cannot patch the sharable field
	if !privileged {
		for _, op := range operations {
			if op.Path == "/sharable" {
				c.JSON(http.StatusForbidden, Error{
					Error:            "forbidden",
					ErrorDescription: "Only administrators and security reviewers can modify the sharable field",
				})
				return
			}
		}
	}

	result, patchErr := GlobalTeamNoteStore.Patch(ctx, teamNoteId.String(), operations)
	if patchErr != nil {
		logger.Error("Failed to patch team note: %v", patchErr)
		HandleRequestError(c, StoreErrorToRequestError(patchErr, "Team note not found", "Failed to patch team note"))
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeleteTeamNote deletes a team note.
// DELETE /teams/{team_id}/notes/{team_note_id}
func (s *Server) DeleteTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be team member or admin
	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check team authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a team member or administrator to delete team notes",
		})
		return
	}

	// Get existing note to check sharable status
	existing, storeErr := GlobalTeamNoteStore.Get(ctx, teamNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Team note not found", "Failed to retrieve team note"))
		return
	}

	// Non-privileged users cannot delete non-sharable notes (return 404 to hide existence)
	if !isPrivilegedUser(c) && existing.Sharable != nil && !*existing.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Team note not found",
		})
		return
	}

	if deleteErr := GlobalTeamNoteStore.Delete(ctx, teamNoteId.String()); deleteErr != nil {
		logger.Error("Failed to delete team note: %v", deleteErr)
		HandleRequestError(c, StoreErrorToRequestError(deleteErr, "Team note not found", "Failed to delete team note"))
		return
	}

	c.Status(http.StatusNoContent)
}
