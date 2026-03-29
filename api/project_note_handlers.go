package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListProjectNotes returns a paginated list of notes for a project.
// GET /projects/{project_id}/notes
func (s *Server) ListProjectNotes(c *gin.Context, projectId openapi_types.UUID, params ListProjectNotesParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a project team member or administrator to access project notes",
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

	items, total, storeErr := GlobalProjectNoteStore.List(ctx, projectId.String(), offset, limit, privileged)
	if storeErr != nil {
		logger.Error("Failed to list project notes: %v", storeErr)
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Project notes not found", "Failed to list project notes"))
		return
	}

	c.JSON(http.StatusOK, ListProjectNotesResponse{
		Notes:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// CreateProjectNote creates a new note for a project.
// POST /projects/{project_id}/notes
func (s *Server) CreateProjectNote(c *gin.Context, projectId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a project team member or administrator to create project notes",
		})
		return
	}

	var req ProjectNoteInput
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

	note := ProjectNote{
		Name:         req.Name,
		Content:      req.Content,
		Description:  req.Description,
		Sharable:     req.Sharable,
		TimmyEnabled: req.TimmyEnabled,
	}

	result, storeErr := GlobalProjectNoteStore.Create(ctx, &note, projectId.String())
	if storeErr != nil {
		logger.Error("Failed to create project note: %v", storeErr)
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Project not found", "Failed to create project note"))
		return
	}

	c.JSON(http.StatusCreated, result)
}

// GetProjectNote returns a specific project note.
// GET /projects/{project_id}/notes/{project_note_id}
func (s *Server) GetProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a project team member or administrator to access project notes",
		})
		return
	}

	note, storeErr := GlobalProjectNoteStore.Get(ctx, projectNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Project note not found", "Failed to retrieve project note"))
		return
	}

	// Non-privileged users cannot see non-sharable notes (return 404 to hide existence)
	if !isPrivilegedUser(c) && note.Sharable != nil && !*note.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Project note not found",
		})
		return
	}

	c.JSON(http.StatusOK, note)
}

// UpdateProjectNote replaces a project note.
// PUT /projects/{project_id}/notes/{project_note_id}
func (s *Server) UpdateProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a project team member or administrator to update project notes",
		})
		return
	}

	// Get existing note to check sharable status
	existing, storeErr := GlobalProjectNoteStore.Get(ctx, projectNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Project note not found", "Failed to retrieve project note"))
		return
	}

	privileged := isPrivilegedUser(c)

	// Non-privileged users cannot update non-sharable notes (return 404 to hide existence)
	if !privileged && existing.Sharable != nil && !*existing.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Project note not found",
		})
		return
	}

	var req ProjectNoteInput
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

	note := ProjectNote{
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

	result, updateErr := GlobalProjectNoteStore.Update(ctx, projectNoteId.String(), &note, projectId.String())
	if updateErr != nil {
		logger.Error("Failed to update project note: %v", updateErr)
		HandleRequestError(c, StoreErrorToRequestError(updateErr, "Project note not found", "Failed to update project note"))
		return
	}

	c.JSON(http.StatusOK, result)
}

// PatchProjectNote partially updates a project note using JSON Patch.
// PATCH /projects/{project_id}/notes/{project_note_id}
func (s *Server) PatchProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a project team member or administrator to patch project notes",
		})
		return
	}

	// Get existing note to check sharable status
	existing, storeErr := GlobalProjectNoteStore.Get(ctx, projectNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Project note not found", "Failed to retrieve project note"))
		return
	}

	privileged := isPrivilegedUser(c)

	// Non-privileged users cannot patch non-sharable notes (return 404 to hide existence)
	if !privileged && existing.Sharable != nil && !*existing.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Project note not found",
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

	result, patchErr := GlobalProjectNoteStore.Patch(ctx, projectNoteId.String(), operations)
	if patchErr != nil {
		logger.Error("Failed to patch project note: %v", patchErr)
		HandleRequestError(c, StoreErrorToRequestError(patchErr, "Project note not found", "Failed to patch project note"))
		return
	}

	c.JSON(http.StatusOK, result)
}

// DeleteProjectNote deletes a project note.
// DELETE /projects/{project_id}/notes/{project_note_id}
func (s *Server) DeleteProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	authorized, err := IsProjectTeamMemberOrAdmin(ctx, projectId.String(), userUUID, c)
	if err != nil {
		logger.Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You must be a project team member or administrator to delete project notes",
		})
		return
	}

	// Get existing note to check sharable status
	existing, storeErr := GlobalProjectNoteStore.Get(ctx, projectNoteId.String())
	if storeErr != nil {
		HandleRequestError(c, StoreErrorToRequestError(storeErr, "Project note not found", "Failed to retrieve project note"))
		return
	}

	// Non-privileged users cannot delete non-sharable notes (return 404 to hide existence)
	if !isPrivilegedUser(c) && existing.Sharable != nil && !*existing.Sharable {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Project note not found",
		})
		return
	}

	if deleteErr := GlobalProjectNoteStore.Delete(ctx, projectNoteId.String()); deleteErr != nil {
		logger.Error("Failed to delete project note: %v", deleteErr)
		HandleRequestError(c, StoreErrorToRequestError(deleteErr, "Project note not found", "Failed to delete project note"))
		return
	}

	c.Status(http.StatusNoContent)
}
