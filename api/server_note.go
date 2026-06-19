package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Note Methods - Implementations

// GetThreatModelNotes lists notes
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route list-notes request for a threat model to the note handler
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route create-note request for a threat model to the note handler
func (s *Server) CreateThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.noteHandler.CreateNote(c)
}

// DeleteThreatModelNote deletes a note
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route delete-note request for a threat model note to the note handler
func (s *Server) DeleteThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.DeleteNote(c)
}

// GetThreatModelNote gets a note
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route fetch-note request for a threat model note to the note handler
func (s *Server) GetThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.GetNote(c)
}

// UpdateThreatModelNote updates a note
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route update-note request for a threat model note to the note handler
func (s *Server) UpdateThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.UpdateNote(c)
}

// PatchThreatModelNote patches a note
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route patch-note request for a threat model note to the note handler
func (s *Server) PatchThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteHandler.PatchNote(c)
}

// Note Metadata Methods

// GetNoteMetadata gets note metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route list-metadata request for a note to the metadata handler
func (s *Server) GetNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.List(c)
}

// CreateNoteMetadata creates note metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route create-metadata request for a note to the metadata handler
func (s *Server) CreateNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.Create(c)
}

// BulkCreateNoteMetadata bulk creates note metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-create-metadata request for a note to the metadata handler
func (s *Server) BulkCreateNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.BulkCreate(c)
}

// BulkReplaceNoteMetadata replaces all note metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-replace-metadata request for a note to the metadata handler
func (s *Server) BulkReplaceNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.BulkReplace(c)
}

// BulkUpsertNoteMetadata upserts note metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-upsert-metadata request for a note to the metadata handler
func (s *Server) BulkUpsertNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadata.BulkUpsert(c)
}

// DeleteNoteMetadataByKey deletes note metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route delete-metadata-by-key request for a note to the metadata handler
func (s *Server) DeleteNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadata.Delete(c)
}

// GetNoteMetadataByKey gets note metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route fetch-metadata-by-key request for a note to the metadata handler
func (s *Server) GetNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadata.GetByKey(c)
}

// UpdateNoteMetadataByKey updates note metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route update-metadata-by-key request for a note to the metadata handler
func (s *Server) UpdateNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadata.Update(c)
}
