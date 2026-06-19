package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Repository Methods

// GetThreatModelRepositories lists repositories
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list repositories for a threat model, authorizing include-deleted access (reads DB)
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route repository creation request to the repository handler
func (s *Server) CreateThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.CreateRepository(c)
}

// BulkCreateThreatModelRepositories bulk creates repositories
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk repository creation request to the repository handler
func (s *Server) BulkCreateThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.BulkCreateRepositorys(c)
}

// BulkUpsertThreatModelRepositories bulk upserts repositories
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk repository upsert request to the repository handler
func (s *Server) BulkUpsertThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.BulkUpdateRepositorys(c)
}

// DeleteThreatModelRepository deletes a repository
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route repository delete request to the repository handler
func (s *Server) DeleteThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.DeleteRepository(c)
}

// GetThreatModelRepository gets a repository
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route single repository fetch request to the repository handler
func (s *Server) GetThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.GetRepository(c)
}

// UpdateThreatModelRepository updates a repository
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route full repository update request to the repository handler
func (s *Server) UpdateThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.UpdateRepository(c)
}

// PatchThreatModelRepository patches a repository
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route partial repository patch request to the repository handler
func (s *Server) PatchThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryHandler.PatchRepository(c)
}

// Repository Metadata Methods

// GetRepositoryMetadata gets repository metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list all metadata entries for a repository
func (s *Server) GetRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.List(c)
}

// CreateRepositoryMetadata creates repository metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route repository metadata creation request to the metadata handler
func (s *Server) CreateRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.Create(c)
}

// BulkCreateRepositoryMetadata bulk creates repository metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk repository metadata creation request to the metadata handler
func (s *Server) BulkCreateRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.BulkCreate(c)
}

// BulkReplaceRepositoryMetadata replaces all repository metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk repository metadata replacement (PUT) request to the metadata handler
func (s *Server) BulkReplaceRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.BulkReplace(c)
}

// BulkUpsertRepositoryMetadata upserts repository metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk repository metadata upsert (PATCH) request to the metadata handler
func (s *Server) BulkUpsertRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadata.BulkUpsert(c)
}

// DeleteRepositoryMetadataByKey deletes repository metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delete a repository metadata entry by key
func (s *Server) DeleteRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadata.Delete(c)
}

// GetRepositoryMetadataByKey gets repository metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch a single repository metadata entry by key
func (s *Server) GetRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadata.GetByKey(c)
}

// UpdateRepositoryMetadataByKey updates repository metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: update a repository metadata entry by key
func (s *Server) UpdateRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadata.Update(c)
}
