package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Repository Methods

// GetThreatModelRepositories lists repositories
func (s *Server) GetThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelRepositoriesParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	if params.IncludeDeleted != nil && *params.IncludeDeleted {
		if !AuthorizeIncludeDeleted(c) {
			return
		}
		c.Request = c.Request.WithContext(ContextWithIncludeDeleted(c.Request.Context()))
	}
	s.repositoryHandler.GetRepositorys(c)
}

// CreateThreatModelRepository creates a repository
func (s *Server) CreateThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.CreateRepository(c)
}

// BulkCreateThreatModelRepositories bulk creates repositories
func (s *Server) BulkCreateThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.BulkCreateRepositorys(c)
}

// BulkUpsertThreatModelRepositories bulk upserts repositories
func (s *Server) BulkUpsertThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.BulkUpdateRepositorys(c)
}

// DeleteThreatModelRepository deletes a repository
func (s *Server) DeleteThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.DeleteRepository(c)
}

// GetThreatModelRepository gets a repository
func (s *Server) GetThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.GetRepository(c)
}

// UpdateThreatModelRepository updates a repository
func (s *Server) UpdateThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.UpdateRepository(c)
}

// PatchThreatModelRepository patches a repository
func (s *Server) PatchThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryHandler.PatchRepository(c)
}

// Repository Metadata Methods

// GetRepositoryMetadata gets repository metadata
func (s *Server) GetRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.List(c)
}

// CreateRepositoryMetadata creates repository metadata
func (s *Server) CreateRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.Create(c)
}

// BulkCreateRepositoryMetadata bulk creates repository metadata
func (s *Server) BulkCreateRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.BulkCreate(c)
}

// BulkReplaceRepositoryMetadata replaces all repository metadata (PUT)
func (s *Server) BulkReplaceRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.BulkReplace(c)
}

// BulkUpsertRepositoryMetadata upserts repository metadata (PATCH)
func (s *Server) BulkUpsertRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.BulkUpsert(c)
}

// DeleteRepositoryMetadataByKey deletes repository metadata by key
func (s *Server) DeleteRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadata.Delete(c)
}

// GetRepositoryMetadataByKey gets repository metadata by key
func (s *Server) GetRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadata.GetByKey(c)
}

// UpdateRepositoryMetadataByKey updates repository metadata by key
func (s *Server) UpdateRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadata.Update(c)
}
