package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListProjectNotes returns a paginated list of notes for a project.
// GET /projects/{project_id}/notes
func (s *Server) ListProjectNotes(c *gin.Context, projectId openapi_types.UUID, params ListProjectNotesParams) {
	HandleRequestError(c, NotImplementedError("ListProjectNotes not yet implemented"))
}

// CreateProjectNote creates a new note for a project.
// POST /projects/{project_id}/notes
func (s *Server) CreateProjectNote(c *gin.Context, projectId openapi_types.UUID) {
	HandleRequestError(c, NotImplementedError("CreateProjectNote not yet implemented"))
}

// GetProjectNote returns a specific project note.
// GET /projects/{project_id}/notes/{project_note_id}
func (s *Server) GetProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	HandleRequestError(c, NotImplementedError("GetProjectNote not yet implemented"))
}

// UpdateProjectNote replaces a project note.
// PUT /projects/{project_id}/notes/{project_note_id}
func (s *Server) UpdateProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	HandleRequestError(c, NotImplementedError("UpdateProjectNote not yet implemented"))
}

// PatchProjectNote partially updates a project note using JSON Patch.
// PATCH /projects/{project_id}/notes/{project_note_id}
func (s *Server) PatchProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	HandleRequestError(c, NotImplementedError("PatchProjectNote not yet implemented"))
}

// DeleteProjectNote deletes a project note.
// DELETE /projects/{project_id}/notes/{project_note_id}
func (s *Server) DeleteProjectNote(c *gin.Context, projectId openapi_types.UUID, projectNoteId ProjectNoteId) {
	HandleRequestError(c, NotImplementedError("DeleteProjectNote not yet implemented"))
}
