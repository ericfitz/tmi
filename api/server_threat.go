package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Threat Methods - Placeholder implementations

// GetThreatModelThreats lists threats
func (s *Server) GetThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelThreatsParams) {
	if params.IncludeDeleted != nil && *params.IncludeDeleted {
		if !AuthorizeIncludeDeleted(c) {
			return
		}
		c.Request = c.Request.WithContext(ContextWithIncludeDeleted(c.Request.Context()))
	}
	s.threatHandler.GetThreatsWithFilters(c, params)
}

// CreateThreatModelThreat creates a threat
func (s *Server) CreateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.CreateThreat(c)
}

// BulkCreateThreatModelThreats bulk creates threats
func (s *Server) BulkCreateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkCreateThreats(c)
}

// BulkUpdateThreatModelThreats bulk updates threats
func (s *Server) BulkUpdateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkUpdateThreats(c)
}

// BulkPatchThreatModelThreats bulk patches threats
func (s *Server) BulkPatchThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkPatchThreats(c)
}

// BulkDeleteThreatModelThreats bulk deletes threats
func (s *Server) BulkDeleteThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params BulkDeleteThreatModelThreatsParams) {
	s.threatHandler.BulkDeleteThreats(c)
}

// DeleteThreatModelThreat deletes a threat
func (s *Server) DeleteThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.DeleteThreat(c)
}

// GetThreatModelThreat gets a threat
func (s *Server) GetThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.GetThreat(c)
}

// PatchThreatModelThreat patches a threat
func (s *Server) PatchThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.PatchThreat(c)
}

// UpdateThreatModelThreat updates a threat
func (s *Server) UpdateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.UpdateThreat(c)
}

// Threat Metadata Methods

// GetThreatMetadata gets threat metadata
func (s *Server) GetThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.List(c)
}

// CreateThreatMetadata creates threat metadata
func (s *Server) CreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.Create(c)
}

// BulkCreateThreatMetadata bulk creates threat metadata
func (s *Server) BulkCreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.BulkCreate(c)
}

// BulkReplaceThreatMetadata replaces all threat metadata (PUT)
func (s *Server) BulkReplaceThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.BulkReplace(c)
}

// BulkUpsertThreatMetadata upserts threat metadata (PATCH)
func (s *Server) BulkUpsertThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.BulkUpsert(c)
}

// DeleteThreatMetadataByKey deletes threat metadata by key
func (s *Server) DeleteThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadata.Delete(c)
}

// GetThreatMetadataByKey gets threat metadata by key
func (s *Server) GetThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadata.GetByKey(c)
}

// UpdateThreatMetadataByKey updates threat metadata by key
func (s *Server) UpdateThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadata.Update(c)
}
