package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Threat Methods - Placeholder implementations

// GetThreatModelThreats lists threats
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list threats for a threat model, enforcing include-deleted authorization
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route threat creation to the threat handler
func (s *Server) CreateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.CreateThreat(c)
}

// BulkCreateThreatModelThreats bulk creates threats
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk threat creation to the threat handler
func (s *Server) BulkCreateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkCreateThreats(c)
}

// BulkUpdateThreatModelThreats bulk updates threats
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk threat replacement to the threat handler
func (s *Server) BulkUpdateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkUpdateThreats(c)
}

// BulkPatchThreatModelThreats bulk patches threats
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk threat patch to the threat handler
func (s *Server) BulkPatchThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkPatchThreats(c)
}

// BulkDeleteThreatModelThreats bulk deletes threats
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk threat deletion to the threat handler
func (s *Server) BulkDeleteThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params BulkDeleteThreatModelThreatsParams) {
	s.threatHandler.BulkDeleteThreats(c)
}

// DeleteThreatModelThreat deletes a threat
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route single threat deletion to the threat handler
func (s *Server) DeleteThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.DeleteThreat(c)
}

// GetThreatModelThreat gets a threat
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route single threat fetch to the threat handler
func (s *Server) GetThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.GetThreat(c)
}

// PatchThreatModelThreat patches a threat
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route single threat patch to the threat handler
func (s *Server) PatchThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, _ PatchThreatModelThreatParams) {
	s.threatHandler.PatchThreat(c)
}

// UpdateThreatModelThreat updates a threat
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route single threat replacement to the threat handler
func (s *Server) UpdateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, _ UpdateThreatModelThreatParams) {
	s.threatHandler.UpdateThreat(c)
}

// Threat Metadata Methods

// GetThreatMetadata gets threat metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list all metadata entries for a threat
func (s *Server) GetThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.List(c)
}

// CreateThreatMetadata creates threat metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: create a new metadata entry on a threat
func (s *Server) CreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.Create(c)
}

// BulkCreateThreatMetadata bulk creates threat metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: bulk-create metadata entries on a threat
func (s *Server) BulkCreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.BulkCreate(c)
}

// BulkReplaceThreatMetadata replaces all threat metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: replace all metadata entries on a threat (PUT)
func (s *Server) BulkReplaceThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.BulkReplace(c)
}

// BulkUpsertThreatMetadata upserts threat metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: upsert metadata entries on a threat (PATCH)
func (s *Server) BulkUpsertThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadata.BulkUpsert(c)
}

// DeleteThreatMetadataByKey deletes threat metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delete a single threat metadata entry by key
func (s *Server) DeleteThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadata.Delete(c)
}

// GetThreatMetadataByKey gets threat metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch a single threat metadata entry by key
func (s *Server) GetThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadata.GetByKey(c)
}

// UpdateThreatMetadataByKey updates threat metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: replace a single threat metadata entry by key
func (s *Server) UpdateThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadata.Update(c)
}
