package api

import (
	"github.com/gin-gonic/gin"
)

// GinHandlerFunc converts a Gin handler to an Echo handler
type GinHandlerFunc func(c *gin.Context) error

// GinServerInterface extends ServerInterface for Gin
type GinServerInterface interface {
	// Root API Info
	GetApiInfo(c *gin.Context)

	// Authentication
	GetAuthLogin(c *gin.Context)
	GetAuthCallback(c *gin.Context)
	PostAuthLogout(c *gin.Context)

	// Diagram Management
	GetDiagrams(c *gin.Context)
	PostDiagrams(c *gin.Context)
	GetDiagramsId(c *gin.Context)
	PutDiagramsId(c *gin.Context)
	PatchDiagramsId(c *gin.Context)
	DeleteDiagramsId(c *gin.Context)

	// Diagram Collaboration
	GetDiagramsIdCollaborate(c *gin.Context)
	PostDiagramsIdCollaborate(c *gin.Context)
	DeleteDiagramsIdCollaborate(c *gin.Context)

	// Diagram Metadata
	GetDiagramsIdMetadata(c *gin.Context)
	PostDiagramsIdMetadata(c *gin.Context)
	GetDiagramsIdMetadataKey(c *gin.Context)
	PutDiagramsIdMetadataKey(c *gin.Context)
	DeleteDiagramsIdMetadataKey(c *gin.Context)
	PostDiagramsIdMetadataBulk(c *gin.Context)

	// Diagram Cell Metadata
	GetDiagramsIdCellsCellIdMetadata(c *gin.Context)
	PostDiagramsIdCellsCellIdMetadata(c *gin.Context)
	GetDiagramsIdCellsCellIdMetadataKey(c *gin.Context)
	PutDiagramsIdCellsCellIdMetadataKey(c *gin.Context)
	DeleteDiagramsIdCellsCellIdMetadataKey(c *gin.Context)
	PatchDiagramsIdCellsCellId(c *gin.Context)
	PostDiagramsIdCellsBatchPatch(c *gin.Context)

	// Threat Model Management
	GetThreatModels(c *gin.Context)
	PostThreatModels(c *gin.Context)
	GetThreatModelsId(c *gin.Context)
	PutThreatModelsId(c *gin.Context)
	PatchThreatModelsId(c *gin.Context)
	DeleteThreatModelsId(c *gin.Context)

	// Threat Model Diagrams
	GetThreatModelsThreatModelIdDiagrams(c *gin.Context)
	PostThreatModelsThreatModelIdDiagrams(c *gin.Context)
	GetThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context)
	PutThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context)
	PatchThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context)
	DeleteThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context)

	// Threat Model Diagram Collaboration
	GetThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context)
	PostThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context)
	DeleteThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context)

	// Threat Model Diagram Metadata
	GetThreatModelsThreatModelIdDiagramsDiagramIdMetadata(c *gin.Context)
	PostThreatModelsThreatModelIdDiagramsDiagramIdMetadata(c *gin.Context)
	GetThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context)
	PutThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context)
	DeleteThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context)
	PostThreatModelsThreatModelIdDiagramsDiagramIdMetadataBulk(c *gin.Context)

	// Threat Model Threats
	GetThreatModelsThreatModelIdThreats(c *gin.Context)
	PostThreatModelsThreatModelIdThreats(c *gin.Context)
	GetThreatModelsThreatModelIdThreatsThreatId(c *gin.Context)
	PutThreatModelsThreatModelIdThreatsThreatId(c *gin.Context)
	PatchThreatModelsThreatModelIdThreatsThreatId(c *gin.Context)
	DeleteThreatModelsThreatModelIdThreatsThreatId(c *gin.Context)
	PostThreatModelsThreatModelIdThreatsBulk(c *gin.Context)
	PutThreatModelsThreatModelIdThreatsBulk(c *gin.Context)

	// Threat Model Threat Metadata
	GetThreatModelsThreatModelIdThreatsThreatIdMetadata(c *gin.Context)
	PostThreatModelsThreatModelIdThreatsThreatIdMetadata(c *gin.Context)
	GetThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context)
	PutThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context)
	DeleteThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context)
	PostThreatModelsThreatModelIdThreatsThreatIdMetadataBulk(c *gin.Context)

	// Threat Model Documents
	GetThreatModelsThreatModelIdDocuments(c *gin.Context)
	PostThreatModelsThreatModelIdDocuments(c *gin.Context)
	GetThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context)
	PutThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context)
	DeleteThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context)
	PostThreatModelsThreatModelIdDocumentsBulk(c *gin.Context)

	// Threat Model Document Metadata
	GetThreatModelsThreatModelIdDocumentsDocumentIdMetadata(c *gin.Context)
	PostThreatModelsThreatModelIdDocumentsDocumentIdMetadata(c *gin.Context)
	GetThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context)
	PutThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context)
	DeleteThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context)
	PostThreatModelsThreatModelIdDocumentsDocumentIdMetadataBulk(c *gin.Context)

	// Threat Model Sources
	GetThreatModelsThreatModelIdSources(c *gin.Context)
	PostThreatModelsThreatModelIdSources(c *gin.Context)
	GetThreatModelsThreatModelIdSourcesSourceId(c *gin.Context)
	PutThreatModelsThreatModelIdSourcesSourceId(c *gin.Context)
	DeleteThreatModelsThreatModelIdSourcesSourceId(c *gin.Context)
	PostThreatModelsThreatModelIdSourcesBulk(c *gin.Context)

	// Threat Model Source Metadata
	GetThreatModelsThreatModelIdSourcesSourceIdMetadata(c *gin.Context)
	PostThreatModelsThreatModelIdSourcesSourceIdMetadata(c *gin.Context)
	GetThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context)
	PutThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context)
	DeleteThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context)
	PostThreatModelsThreatModelIdSourcesSourceIdMetadataBulk(c *gin.Context)

	// Batch Operations
	PostThreatModelsThreatModelIdThreatsBatchPatch(c *gin.Context)
	DeleteThreatModelsThreatModelIdThreatsBatch(c *gin.Context)
}

// GinRouter is a simplified interface for Gin router
type GinRouter interface {
	GET(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	POST(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	PUT(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	DELETE(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	PATCH(path string, handlers ...gin.HandlerFunc) gin.IRoutes
}

// RegisterHandlers registers the API handlers to a Gin router
func RegisterGinHandlers(r GinRouter, si GinServerInterface) {
	// Root
	r.GET("/", si.GetApiInfo)

	// Auth
	r.GET("/auth/login", si.GetAuthLogin)
	r.GET("/auth/callback", si.GetAuthCallback)
	r.POST("/auth/logout", si.PostAuthLogout)

	// Diagrams
	r.GET("/diagrams", si.GetDiagrams)
	r.POST("/diagrams", si.PostDiagrams)
	r.GET("/diagrams/:id", si.GetDiagramsId)
	r.PUT("/diagrams/:id", si.PutDiagramsId)
	r.PATCH("/diagrams/:id", si.PatchDiagramsId)
	r.DELETE("/diagrams/:id", si.DeleteDiagramsId)

	// Diagram Collaboration
	r.GET("/diagrams/:id/collaborate", si.GetDiagramsIdCollaborate)
	r.POST("/diagrams/:id/collaborate", si.PostDiagramsIdCollaborate)
	r.DELETE("/diagrams/:id/collaborate", si.DeleteDiagramsIdCollaborate)

	// Threat Models
	r.GET("/threat_models", si.GetThreatModels)
	r.POST("/threat_models", si.PostThreatModels)
	r.GET("/threat_models/:id", si.GetThreatModelsId)
	r.PUT("/threat_models/:id", si.PutThreatModelsId)
	r.PATCH("/threat_models/:id", si.PatchThreatModelsId)
	r.DELETE("/threat_models/:id", si.DeleteThreatModelsId)

	// Threat Model Diagrams
	r.GET("/threat_models/:id/diagrams", si.GetThreatModelsThreatModelIdDiagrams)
	r.POST("/threat_models/:id/diagrams", si.PostThreatModelsThreatModelIdDiagrams)
	r.GET("/threat_models/:id/diagrams/:diagram_id", si.GetThreatModelsThreatModelIdDiagramsDiagramId)
	r.PUT("/threat_models/:id/diagrams/:diagram_id", si.PutThreatModelsThreatModelIdDiagramsDiagramId)
	r.PATCH("/threat_models/:id/diagrams/:diagram_id", si.PatchThreatModelsThreatModelIdDiagramsDiagramId)
	r.DELETE("/threat_models/:id/diagrams/:diagram_id", si.DeleteThreatModelsThreatModelIdDiagramsDiagramId)

	// Threat Model Diagram Collaboration
	r.GET("/threat_models/:id/diagrams/:diagram_id/collaborate", si.GetThreatModelsThreatModelIdDiagramsDiagramIdCollaborate)
	r.POST("/threat_models/:id/diagrams/:diagram_id/collaborate", si.PostThreatModelsThreatModelIdDiagramsDiagramIdCollaborate)
	r.DELETE("/threat_models/:id/diagrams/:diagram_id/collaborate", si.DeleteThreatModelsThreatModelIdDiagramsDiagramIdCollaborate)

	// Diagram Metadata
	r.GET("/diagrams/:id/metadata", si.GetDiagramsIdMetadata)
	r.POST("/diagrams/:id/metadata", si.PostDiagramsIdMetadata)
	r.GET("/diagrams/:id/metadata/:key", si.GetDiagramsIdMetadataKey)
	r.PUT("/diagrams/:id/metadata/:key", si.PutDiagramsIdMetadataKey)
	r.DELETE("/diagrams/:id/metadata/:key", si.DeleteDiagramsIdMetadataKey)
	r.POST("/diagrams/:id/metadata/bulk", si.PostDiagramsIdMetadataBulk)

	// Diagram Cell Metadata
	r.GET("/diagrams/:id/cells/:cell_id/metadata", si.GetDiagramsIdCellsCellIdMetadata)
	r.POST("/diagrams/:id/cells/:cell_id/metadata", si.PostDiagramsIdCellsCellIdMetadata)
	r.GET("/diagrams/:id/cells/:cell_id/metadata/:key", si.GetDiagramsIdCellsCellIdMetadataKey)
	r.PUT("/diagrams/:id/cells/:cell_id/metadata/:key", si.PutDiagramsIdCellsCellIdMetadataKey)
	r.DELETE("/diagrams/:id/cells/:cell_id/metadata/:key", si.DeleteDiagramsIdCellsCellIdMetadataKey)
	r.PATCH("/diagrams/:id/cells/:cell_id", si.PatchDiagramsIdCellsCellId)
	r.POST("/diagrams/:id/cells/batch/patch", si.PostDiagramsIdCellsBatchPatch)

	// Threat Model Diagram Metadata
	r.GET("/threat_models/:id/diagrams/:diagram_id/metadata", si.GetThreatModelsThreatModelIdDiagramsDiagramIdMetadata)
	r.POST("/threat_models/:id/diagrams/:diagram_id/metadata", si.PostThreatModelsThreatModelIdDiagramsDiagramIdMetadata)
	r.GET("/threat_models/:id/diagrams/:diagram_id/metadata/:key", si.GetThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey)
	r.PUT("/threat_models/:id/diagrams/:diagram_id/metadata/:key", si.PutThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey)
	r.DELETE("/threat_models/:id/diagrams/:diagram_id/metadata/:key", si.DeleteThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey)
	r.POST("/threat_models/:id/diagrams/:diagram_id/metadata/bulk", si.PostThreatModelsThreatModelIdDiagramsDiagramIdMetadataBulk)

	// Threat Model Threats
	r.GET("/threat_models/:id/threats", si.GetThreatModelsThreatModelIdThreats)
	r.POST("/threat_models/:id/threats", si.PostThreatModelsThreatModelIdThreats)
	r.GET("/threat_models/:id/threats/:threat_id", si.GetThreatModelsThreatModelIdThreatsThreatId)
	r.PUT("/threat_models/:id/threats/:threat_id", si.PutThreatModelsThreatModelIdThreatsThreatId)
	r.PATCH("/threat_models/:id/threats/:threat_id", si.PatchThreatModelsThreatModelIdThreatsThreatId)
	r.DELETE("/threat_models/:id/threats/:threat_id", si.DeleteThreatModelsThreatModelIdThreatsThreatId)
	r.POST("/threat_models/:id/threats/bulk", si.PostThreatModelsThreatModelIdThreatsBulk)
	r.PUT("/threat_models/:id/threats/bulk", si.PutThreatModelsThreatModelIdThreatsBulk)

	// Threat Model Threat Metadata
	r.GET("/threat_models/:id/threats/:threat_id/metadata", si.GetThreatModelsThreatModelIdThreatsThreatIdMetadata)
	r.POST("/threat_models/:id/threats/:threat_id/metadata", si.PostThreatModelsThreatModelIdThreatsThreatIdMetadata)
	r.GET("/threat_models/:id/threats/:threat_id/metadata/:key", si.GetThreatModelsThreatModelIdThreatsThreatIdMetadataKey)
	r.PUT("/threat_models/:id/threats/:threat_id/metadata/:key", si.PutThreatModelsThreatModelIdThreatsThreatIdMetadataKey)
	r.DELETE("/threat_models/:id/threats/:threat_id/metadata/:key", si.DeleteThreatModelsThreatModelIdThreatsThreatIdMetadataKey)
	r.POST("/threat_models/:id/threats/:threat_id/metadata/bulk", si.PostThreatModelsThreatModelIdThreatsThreatIdMetadataBulk)

	// Threat Model Documents
	r.GET("/threat_models/:id/documents", si.GetThreatModelsThreatModelIdDocuments)
	r.POST("/threat_models/:id/documents", si.PostThreatModelsThreatModelIdDocuments)
	r.GET("/threat_models/:id/documents/:document_id", si.GetThreatModelsThreatModelIdDocumentsDocumentId)
	r.PUT("/threat_models/:id/documents/:document_id", si.PutThreatModelsThreatModelIdDocumentsDocumentId)
	r.DELETE("/threat_models/:id/documents/:document_id", si.DeleteThreatModelsThreatModelIdDocumentsDocumentId)
	r.POST("/threat_models/:id/documents/bulk", si.PostThreatModelsThreatModelIdDocumentsBulk)

	// Threat Model Document Metadata
	r.GET("/threat_models/:id/documents/:document_id/metadata", si.GetThreatModelsThreatModelIdDocumentsDocumentIdMetadata)
	r.POST("/threat_models/:id/documents/:document_id/metadata", si.PostThreatModelsThreatModelIdDocumentsDocumentIdMetadata)
	r.GET("/threat_models/:id/documents/:document_id/metadata/:key", si.GetThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey)
	r.PUT("/threat_models/:id/documents/:document_id/metadata/:key", si.PutThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey)
	r.DELETE("/threat_models/:id/documents/:document_id/metadata/:key", si.DeleteThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey)
	r.POST("/threat_models/:id/documents/:document_id/metadata/bulk", si.PostThreatModelsThreatModelIdDocumentsDocumentIdMetadataBulk)

	// Threat Model Sources
	r.GET("/threat_models/:id/sources", si.GetThreatModelsThreatModelIdSources)
	r.POST("/threat_models/:id/sources", si.PostThreatModelsThreatModelIdSources)
	r.GET("/threat_models/:id/sources/:source_id", si.GetThreatModelsThreatModelIdSourcesSourceId)
	r.PUT("/threat_models/:id/sources/:source_id", si.PutThreatModelsThreatModelIdSourcesSourceId)
	r.DELETE("/threat_models/:id/sources/:source_id", si.DeleteThreatModelsThreatModelIdSourcesSourceId)
	r.POST("/threat_models/:id/sources/bulk", si.PostThreatModelsThreatModelIdSourcesBulk)

	// Threat Model Source Metadata
	r.GET("/threat_models/:id/sources/:source_id/metadata", si.GetThreatModelsThreatModelIdSourcesSourceIdMetadata)
	r.POST("/threat_models/:id/sources/:source_id/metadata", si.PostThreatModelsThreatModelIdSourcesSourceIdMetadata)
	r.GET("/threat_models/:id/sources/:source_id/metadata/:key", si.GetThreatModelsThreatModelIdSourcesSourceIdMetadataKey)
	r.PUT("/threat_models/:id/sources/:source_id/metadata/:key", si.PutThreatModelsThreatModelIdSourcesSourceIdMetadataKey)
	r.DELETE("/threat_models/:id/sources/:source_id/metadata/:key", si.DeleteThreatModelsThreatModelIdSourcesSourceIdMetadataKey)
	r.POST("/threat_models/:id/sources/:source_id/metadata/bulk", si.PostThreatModelsThreatModelIdSourcesSourceIdMetadataBulk)

	// Batch Operations
	r.POST("/threat_models/:id/threats/batch/patch", si.PostThreatModelsThreatModelIdThreatsBatchPatch)
	r.DELETE("/threat_models/:id/threats/batch", si.DeleteThreatModelsThreatModelIdThreatsBatch)
}
