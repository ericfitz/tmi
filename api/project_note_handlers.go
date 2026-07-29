package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// requireProjectTeamMemberOrAdmin authorizes the caller against a project's
// team, writing the response and returning false when it does not hold.
//
// The reason this is a helper rather than six copies: IsProjectTeamMemberOrAdmin
// returns gorm.ErrRecordNotFound when the project itself does not exist, and
// every call site mapped that to 500 "Failed to check authorization" — a
// nonexistent project reported as a server fault, in violation of the
// zero-500 policy (#592). CATS never caught it because the fuzzing identity is
// an admin and the admin fast path skips the project lookup entirely.
//
// `action` completes "You must be a project team member or administrator to
// <action> project notes".
// SEM@e1f2a3b4c5d6e7f8091a2b3c4d5e6f7081929304: authorize a caller against a project's team, writing 404/403/500 as appropriate (reads DB)
func requireProjectTeamMemberOrAdmin(
	c *gin.Context, ctx context.Context, projectID, userUUID, action string,
) bool {
	// Existence before authorization: the admin fast path inside
	// IsProjectTeamMemberOrAdmin returns true without ever resolving the
	// project, so without this an admin got 200-with-an-empty-list where a
	// regular user correctly got 404 (#609).
	// The resolved teamID is reused below rather than discarded.
	// IsProjectTeamMemberOrAdmin re-fetches the same project row to get exactly
	// this value, so calling it here would make two round trips where one
	// suffices — on ADB every round trip is a cloud network hop, and this runs
	// on all six project-note operations (#616 note 5).
	teamID, err := GetProjectTeamID(ctx, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			HandleRequestError(c, NotFoundError("Project not found"))
			return false
		}
		slogging.Get().WithContext(c).Error("Failed to resolve project: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return false
	}

	authorized, err := IsTeamMemberOrAdmin(ctx, teamID, userUUID, c)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			HandleRequestError(c, NotFoundError("Project not found"))
			return false
		}
		slogging.Get().WithContext(c).Error("Failed to check project authorization: %v", err)
		HandleRequestError(c, ServerError("Failed to check authorization"))
		return false
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{
			Error: "forbidden",
			ErrorDescription: "You must be a project team member or administrator to " +
				action + " project notes",
		})
		return false
	}
	return true
}

// ListProjectNotes returns a paginated list of notes for a project.
// GET /projects/{project_id}/notes
// SEM@8a8c018ad8b1686dd4e43f736f31431743de5393: fetch a paginated list of project notes, filtering non-sharable notes for unprivileged users (reads DB)
func (s *Server) ListProjectNotes(c *gin.Context, projectId openapi_types.UUID, params ListProjectNotesParams) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	if !requireProjectTeamMemberOrAdmin(c, ctx, projectId.String(), userUUID, "access") {
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
// SEM@8a8c018ad8b1686dd4e43f736f31431743de5393: store a new sanitized project note, enforcing sharable field restrictions by role (reads DB)
func (s *Server) CreateProjectNote(c *gin.Context, projectId openapi_types.UUID) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	if !requireProjectTeamMemberOrAdmin(c, ctx, projectId.String(), userUUID, "create") {
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
	sanitizedContent, contentErr := SanitizeRequiredMarkdownContent("content", req.Content)
	if contentErr != nil {
		HandleRequestError(c, contentErr)
		return
	}
	req.Content = sanitizedContent
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
// SEM@8a8c018ad8b1686dd4e43f736f31431743de5393: fetch a single project note, hiding non-sharable notes from unprivileged users as 404 (reads DB)
func (s *Server) GetProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	if !requireProjectTeamMemberOrAdmin(c, ctx, projectId.String(), userUUID, "access") {
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
// SEM@8a8c018ad8b1686dd4e43f736f31431743de5393: replace a project note, enforcing sharable field and non-sharable visibility restrictions by role (reads DB)
func (s *Server) UpdateProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	if !requireProjectTeamMemberOrAdmin(c, ctx, projectId.String(), userUUID, "update") {
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
	sanitizedContent, contentErr := SanitizeRequiredMarkdownContent("content", req.Content)
	if contentErr != nil {
		HandleRequestError(c, contentErr)
		return
	}
	req.Content = sanitizedContent
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
// SEM@8a8c018ad8b1686dd4e43f736f31431743de5393: apply JSON Patch to a project note, blocking sharable field changes for unprivileged users (reads DB)
func (s *Server) PatchProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	if !requireProjectTeamMemberOrAdmin(c, ctx, projectId.String(), userUUID, "patch") {
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
// SEM@8a8c018ad8b1686dd4e43f736f31431743de5393: delete a project note, hiding non-sharable notes from unprivileged users as 404 (reads DB)
func (s *Server) DeleteProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	// Authorization: must be project team member or admin
	if !requireProjectTeamMemberOrAdmin(c, ctx, projectId.String(), userUUID, "delete") {
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
