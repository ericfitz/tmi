package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Document Methods - Placeholder implementations (not yet implemented)

// GetThreatModelDocuments lists documents
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list documents for a threat model, enforcing include-deleted authorization (reads DB)
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route document creation request to the document handler under a threat model (reads DB)
func (s *Server) CreateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.CreateDocument(c)
}

// BulkCreateThreatModelDocuments bulk creates documents
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk document creation to the document handler under a threat model (reads DB)
func (s *Server) BulkCreateThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.BulkCreateDocuments(c)
}

// BulkUpsertThreatModelDocuments bulk upserts documents
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk document upsert to the document handler under a threat model (reads DB)
func (s *Server) BulkUpsertThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.BulkUpdateDocuments(c)
}

// DeleteThreatModelDocument deletes a document
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route document deletion to the document handler, scoped to a threat model (reads DB)
func (s *Server) DeleteThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.DeleteDocument(c)
}

// GetThreatModelDocument gets a document
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route single document fetch to the document handler, scoped to a threat model (reads DB)
func (s *Server) GetThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.GetDocument(c)
}

// UpdateThreatModelDocument updates a document
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route document update to the document handler, scoped to a threat model (reads DB)
func (s *Server) UpdateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, _ UpdateThreatModelDocumentParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.UpdateDocument(c)
}

// PatchThreatModelDocument patches a document
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route document patch to the document handler (reads DB)
func (s *Server) PatchThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, _ PatchThreatModelDocumentParams) {
	s.documentHandler.PatchDocument(c)
}

// Document Metadata Methods

// GetDocumentMetadata gets document metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list all metadata entries for a document (reads DB)
func (s *Server) GetDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.List(c)
}

// CreateDocumentMetadata creates document metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: store a new metadata entry for a document (reads DB)
func (s *Server) CreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.Create(c)
}

// BulkCreateDocumentMetadata bulk creates document metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: bulk store metadata entries for a document (reads DB)
func (s *Server) BulkCreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.BulkCreate(c)
}

// BulkReplaceDocumentMetadata replaces all document metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: replace all metadata entries for a document atomically (reads DB)
func (s *Server) BulkReplaceDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.BulkReplace(c)
}

// BulkUpsertDocumentMetadata upserts document metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: upsert metadata entries for a document (reads DB)
func (s *Server) BulkUpsertDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadata.BulkUpsert(c)
}

// DeleteDocumentMetadataByKey deletes document metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delete a metadata entry for a document by key (reads DB)
func (s *Server) DeleteDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadata.Delete(c)
}

// GetDocumentMetadataByKey gets document metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch a single metadata entry for a document by key (reads DB)
func (s *Server) GetDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadata.GetByKey(c)
}

// UpdateDocumentMetadataByKey updates document metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: update a metadata entry for a document by key (reads DB)
func (s *Server) UpdateDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadata.Update(c)
}
