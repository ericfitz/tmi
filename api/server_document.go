package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Document Methods - Placeholder implementations (not yet implemented)

// GetThreatModelDocuments lists documents
func (s *Server) GetThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelDocumentsParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	if params.IncludeDeleted != nil && *params.IncludeDeleted {
		if !AuthorizeIncludeDeleted(c) {
			return
		}
		c.Request = c.Request.WithContext(ContextWithIncludeDeleted(c.Request.Context()))
	}
	s.documentHandler.GetDocuments(c)
}

// CreateThreatModelDocument creates a document
func (s *Server) CreateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.CreateDocument(c)
}

// BulkCreateThreatModelDocuments bulk creates documents
func (s *Server) BulkCreateThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.BulkCreateDocuments(c)
}

// BulkUpsertThreatModelDocuments bulk upserts documents
func (s *Server) BulkUpsertThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.BulkUpdateDocuments(c)
}

// DeleteThreatModelDocument deletes a document
func (s *Server) DeleteThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.DeleteDocument(c)
}

// GetThreatModelDocument gets a document
func (s *Server) GetThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.GetDocument(c)
}

// UpdateThreatModelDocument updates a document
func (s *Server) UpdateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.UpdateDocument(c)
}

// PatchThreatModelDocument patches a document
func (s *Server) PatchThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentHandler.PatchDocument(c)
}

// Document Metadata Methods

// GetDocumentMetadata gets document metadata
func (s *Server) GetDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.List(c)
}

// CreateDocumentMetadata creates document metadata
func (s *Server) CreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.Create(c)
}

// BulkCreateDocumentMetadata bulk creates document metadata
func (s *Server) BulkCreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.BulkCreate(c)
}

// BulkReplaceDocumentMetadata replaces all document metadata (PUT)
func (s *Server) BulkReplaceDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.BulkReplace(c)
}

// BulkUpsertDocumentMetadata upserts document metadata (PATCH)
func (s *Server) BulkUpsertDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.BulkUpsert(c)
}

// DeleteDocumentMetadataByKey deletes document metadata by key
func (s *Server) DeleteDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadata.Delete(c)
}

// GetDocumentMetadataByKey gets document metadata by key
func (s *Server) GetDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadata.GetByKey(c)
}

// UpdateDocumentMetadataByKey updates document metadata by key
func (s *Server) UpdateDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadata.Update(c)
}
