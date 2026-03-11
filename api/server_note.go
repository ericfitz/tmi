package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Note Methods - Implementations

// GetThreatModelNotes lists notes
func (s *Server) GetThreatModelNotes(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelNotesParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	if params.IncludeDeleted != nil && *params.IncludeDeleted {
		if !AuthorizeIncludeDeleted(c) {
			return
		}
		c.Request = c.Request.WithContext(ContextWithIncludeDeleted(c.Request.Context()))
	}
	s.noteHandler.GetNotes(c)
}

// CreateThreatModelNote creates a note
func (s *Server) CreateThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.noteHandler.CreateNote(c)
}

// DeleteThreatModelNote deletes a note
func (s *Server) DeleteThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.DeleteNote(c)
}

// GetThreatModelNote gets a note
func (s *Server) GetThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.GetNote(c)
}

// UpdateThreatModelNote updates a note
func (s *Server) UpdateThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.UpdateNote(c)
}

// PatchThreatModelNote patches a note
func (s *Server) PatchThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteHandler.PatchNote(c)
}

// Note Metadata Methods

// GetNoteMetadata gets note metadata
func (s *Server) GetNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.List(c)
}

// CreateNoteMetadata creates note metadata
func (s *Server) CreateNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.Create(c)
}

// BulkCreateNoteMetadata bulk creates note metadata
func (s *Server) BulkCreateNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.BulkCreate(c)
}

// BulkReplaceNoteMetadata replaces all note metadata (PUT)
func (s *Server) BulkReplaceNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.BulkReplace(c)
}

// BulkUpsertNoteMetadata upserts note metadata (PATCH)
func (s *Server) BulkUpsertNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.BulkUpsert(c)
}

// DeleteNoteMetadataByKey deletes note metadata by key
func (s *Server) DeleteNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadata.Delete(c)
}

// GetNoteMetadataByKey gets note metadata by key
func (s *Server) GetNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadata.GetByKey(c)
}

// UpdateNoteMetadataByKey updates note metadata by key
func (s *Server) UpdateNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadata.Update(c)
}
