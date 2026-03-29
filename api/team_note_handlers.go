package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListTeamNotes returns a paginated list of notes for a team.
// GET /teams/{team_id}/notes
func (s *Server) ListTeamNotes(c *gin.Context, teamId openapi_types.UUID, params ListTeamNotesParams) {
	HandleRequestError(c, NotImplementedError("ListTeamNotes not yet implemented"))
}

// CreateTeamNote creates a new note for a team.
// POST /teams/{team_id}/notes
func (s *Server) CreateTeamNote(c *gin.Context, teamId openapi_types.UUID) {
	HandleRequestError(c, NotImplementedError("CreateTeamNote not yet implemented"))
}

// GetTeamNote returns a specific team note.
// GET /teams/{team_id}/notes/{team_note_id}
func (s *Server) GetTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	HandleRequestError(c, NotImplementedError("GetTeamNote not yet implemented"))
}

// UpdateTeamNote replaces a team note.
// PUT /teams/{team_id}/notes/{team_note_id}
func (s *Server) UpdateTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	HandleRequestError(c, NotImplementedError("UpdateTeamNote not yet implemented"))
}

// PatchTeamNote partially updates a team note using JSON Patch.
// PATCH /teams/{team_id}/notes/{team_note_id}
func (s *Server) PatchTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	HandleRequestError(c, NotImplementedError("PatchTeamNote not yet implemented"))
}

// DeleteTeamNote deletes a team note.
// DELETE /teams/{team_id}/notes/{team_note_id}
func (s *Server) DeleteTeamNote(c *gin.Context, teamId openapi_types.UUID, teamNoteId TeamNoteId) {
	HandleRequestError(c, NotImplementedError("DeleteTeamNote not yet implemented"))
}
