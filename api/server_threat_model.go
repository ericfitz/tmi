package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Threat Model Methods (delegate to ThreatModelHandler)

// ListThreatModels lists threat models
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route list threat models request to the threat model handler
func (s *Server) ListThreatModels(c *gin.Context, params ListThreatModelsParams) {
	s.threatModelHandler.GetThreatModels(c)
}

// CreateThreatModel creates a new threat model
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route create threat model request to the threat model handler
func (s *Server) CreateThreatModel(c *gin.Context) {
	s.threatModelHandler.CreateThreatModel(c)
}

// GetThreatModel gets a specific threat model
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route fetch threat model by ID to the threat model handler
func (s *Server) GetThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	// Set path parameter for handler
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.GetThreatModelByID(c)
}

// UpdateThreatModel updates a threat model
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route full update of a threat model to the threat model handler
func (s *Server) UpdateThreatModel(c *gin.Context, threatModelId openapi_types.UUID, _ UpdateThreatModelParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.UpdateThreatModel(c)
}

// PatchThreatModel partially updates a threat model
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route partial update of a threat model to the threat model handler
func (s *Server) PatchThreatModel(c *gin.Context, threatModelId openapi_types.UUID, _ PatchThreatModelParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.PatchThreatModel(c)
}

// DeleteThreatModel deletes a threat model
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route delete threat model request to the threat model handler
func (s *Server) DeleteThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.DeleteThreatModel(c)
}

// Threat Model Diagram Methods

// GetThreatModelDiagrams lists diagrams for a threat model
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list diagrams for a threat model, enforcing include-deleted authorization
func (s *Server) GetThreatModelDiagrams(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelDiagramsParams) {
	if params.IncludeDeleted != nil && *params.IncludeDeleted {
		if !AuthorizeIncludeDeleted(c) {
			return
		}
		c.Request = c.Request.WithContext(ContextWithIncludeDeleted(c.Request.Context()))
	}
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}
	handler.GetDiagrams(c, threatModelId.String())
}

// CreateThreatModelDiagram creates a new diagram
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route create diagram request to the diagram handler with WebSocket hub
func (s *Server) CreateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.CreateDiagram(c, threatModelId.String())
}

// GetThreatModelDiagram gets a specific diagram
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route fetch diagram by ID to the diagram handler with WebSocket hub
func (s *Server) GetThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramByID(c, threatModelId.String(), diagramId.String())
}

// UpdateThreatModelDiagram updates a diagram
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route full update of a diagram to the diagram handler with WebSocket hub
func (s *Server) UpdateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, _ UpdateThreatModelDiagramParams) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.UpdateDiagram(c, threatModelId.String(), diagramId.String())
}

// PatchThreatModelDiagram partially updates a diagram
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route partial update of a diagram to the diagram handler with WebSocket hub
func (s *Server) PatchThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, _ PatchThreatModelDiagramParams) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.PatchDiagram(c, threatModelId.String(), diagramId.String())
}

// DeleteThreatModelDiagram deletes a diagram
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route delete diagram request to the diagram handler with WebSocket hub
func (s *Server) DeleteThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.DeleteDiagram(c, threatModelId.String(), diagramId.String())
}

// Diagram Collaboration Methods (already partially implemented above)

// GetDiagramModel gets minimal diagram model for automated analysis
// SEM@29f63eb500c26288d0d3fe23737adf6fd94bdf9c: route diagram model request to the diagram handler
func (s *Server) GetDiagramModel(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramModel(c, threatModelId, diagramId)
}

// Diagram Metadata Methods

// GetDiagramMetadata gets diagram metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list all metadata entries for a diagram (reads DB)
func (s *Server) GetDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.List(c)
}

// CreateDiagramMetadata creates diagram metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: store a new metadata entry on a diagram (mutates shared state)
func (s *Server) CreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.Create(c)
}

// BulkCreateDiagramMetadata bulk creates diagram metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: store multiple new metadata entries on a diagram in bulk (mutates shared state)
func (s *Server) BulkCreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.BulkCreate(c)
}

// BulkReplaceDiagramMetadata replaces all diagram metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: replace all metadata entries on a diagram atomically (mutates shared state)
func (s *Server) BulkReplaceDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.BulkReplace(c)
}

// BulkUpsertDiagramMetadata upserts diagram metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: upsert multiple metadata entries on a diagram (mutates shared state)
func (s *Server) BulkUpsertDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.BulkUpsert(c)
}

// DeleteDiagramMetadataByKey deletes diagram metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delete a diagram metadata entry by key (mutates shared state)
func (s *Server) DeleteDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	s.diagramMetadata.Delete(c)
}

// GetDiagramMetadataByKey gets diagram metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch a single diagram metadata entry by key (reads DB)
func (s *Server) GetDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	s.diagramMetadata.GetByKey(c)
}

// UpdateDiagramMetadataByKey updates diagram metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: update a diagram metadata entry by key (mutates shared state)
func (s *Server) UpdateDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	s.diagramMetadata.Update(c)
}

// Threat Model Metadata Methods

// GetThreatModelMetadata gets threat model metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list all metadata entries for a threat model (reads DB)
func (s *Server) GetThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.List(c)
}

// CreateThreatModelMetadata creates threat model metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: store a new metadata entry on a threat model (mutates shared state)
func (s *Server) CreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.Create(c)
}

// BulkCreateThreatModelMetadata bulk creates threat model metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: store multiple new metadata entries on a threat model in bulk (mutates shared state)
func (s *Server) BulkCreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.BulkCreate(c)
}

// BulkReplaceThreatModelMetadata replaces all threat model metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: replace all metadata entries on a threat model atomically (mutates shared state)
func (s *Server) BulkReplaceThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.BulkReplace(c)
}

// BulkUpsertThreatModelMetadata upserts threat model metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: upsert multiple metadata entries on a threat model (mutates shared state)
func (s *Server) BulkUpsertThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.BulkUpsert(c)
}

// DeleteThreatModelMetadataByKey deletes threat model metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delete a threat model metadata entry by key (mutates shared state)
func (s *Server) DeleteThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadata.Delete(c)
}

// GetThreatModelMetadataByKey gets threat model metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch a single threat model metadata entry by key (reads DB)
func (s *Server) GetThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadata.GetByKey(c)
}

// UpdateThreatModelMetadataByKey updates threat model metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: update a threat model metadata entry by key (mutates shared state)
func (s *Server) UpdateThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadata.Update(c)
}
