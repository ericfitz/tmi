package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Threat Model Methods (delegate to ThreatModelHandler)

// ListThreatModels lists threat models
func (s *Server) ListThreatModels(c *gin.Context, params ListThreatModelsParams) {
	s.threatModelHandler.GetThreatModels(c)
}

// CreateThreatModel creates a new threat model
func (s *Server) CreateThreatModel(c *gin.Context) {
	s.threatModelHandler.CreateThreatModel(c)
}

// GetThreatModel gets a specific threat model
func (s *Server) GetThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	// Set path parameter for handler
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.GetThreatModelByID(c)
}

// UpdateThreatModel updates a threat model
func (s *Server) UpdateThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.UpdateThreatModel(c)
}

// PatchThreatModel partially updates a threat model
func (s *Server) PatchThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.PatchThreatModel(c)
}

// DeleteThreatModel deletes a threat model
func (s *Server) DeleteThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.DeleteThreatModel(c)
}

// Threat Model Diagram Methods

// GetThreatModelDiagrams lists diagrams for a threat model
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
func (s *Server) CreateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.CreateDiagram(c, threatModelId.String())
}

// GetThreatModelDiagram gets a specific diagram
func (s *Server) GetThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramByID(c, threatModelId.String(), diagramId.String())
}

// UpdateThreatModelDiagram updates a diagram
func (s *Server) UpdateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.UpdateDiagram(c, threatModelId.String(), diagramId.String())
}

// PatchThreatModelDiagram partially updates a diagram
func (s *Server) PatchThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.PatchDiagram(c, threatModelId.String(), diagramId.String())
}

// DeleteThreatModelDiagram deletes a diagram
func (s *Server) DeleteThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.DeleteDiagram(c, threatModelId.String(), diagramId.String())
}

// Diagram Collaboration Methods (already partially implemented above)

// GetDiagramModel gets minimal diagram model for automated analysis
func (s *Server) GetDiagramModel(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, params GetDiagramModelParams) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramModel(c, threatModelId, diagramId, params)
}

// Diagram Metadata Methods

// GetDiagramMetadata gets diagram metadata
func (s *Server) GetDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.List(c)
}

// CreateDiagramMetadata creates diagram metadata
func (s *Server) CreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.Create(c)
}

// BulkCreateDiagramMetadata bulk creates diagram metadata
func (s *Server) BulkCreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.BulkCreate(c)
}

// BulkReplaceDiagramMetadata replaces all diagram metadata (PUT)
func (s *Server) BulkReplaceDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.BulkReplace(c)
}

// BulkUpsertDiagramMetadata upserts diagram metadata (PATCH)
func (s *Server) BulkUpsertDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	s.diagramMetadata.BulkUpsert(c)
}

// DeleteDiagramMetadataByKey deletes diagram metadata by key
func (s *Server) DeleteDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	s.diagramMetadata.Delete(c)
}

// GetDiagramMetadataByKey gets diagram metadata by key
func (s *Server) GetDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	s.diagramMetadata.GetByKey(c)
}

// UpdateDiagramMetadataByKey updates diagram metadata by key
func (s *Server) UpdateDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	s.diagramMetadata.Update(c)
}

// Threat Model Metadata Methods

// GetThreatModelMetadata gets threat model metadata
func (s *Server) GetThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.List(c)
}

// CreateThreatModelMetadata creates threat model metadata
func (s *Server) CreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.Create(c)
}

// BulkCreateThreatModelMetadata bulk creates threat model metadata
func (s *Server) BulkCreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.BulkCreate(c)
}

// BulkReplaceThreatModelMetadata replaces all threat model metadata (PUT)
func (s *Server) BulkReplaceThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.BulkReplace(c)
}

// BulkUpsertThreatModelMetadata upserts threat model metadata (PATCH)
func (s *Server) BulkUpsertThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadata.BulkUpsert(c)
}

// DeleteThreatModelMetadataByKey deletes threat model metadata by key
func (s *Server) DeleteThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadata.Delete(c)
}

// GetThreatModelMetadataByKey gets threat model metadata by key
func (s *Server) GetThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadata.GetByKey(c)
}

// UpdateThreatModelMetadataByKey updates threat model metadata by key
func (s *Server) UpdateThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadata.Update(c)
}
