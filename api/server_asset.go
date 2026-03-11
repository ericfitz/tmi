package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Asset Methods

// GetThreatModelAssets lists assets
func (s *Server) GetThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelAssetsParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	if params.IncludeDeleted != nil && *params.IncludeDeleted {
		if !AuthorizeIncludeDeleted(c) {
			return
		}
		c.Request = c.Request.WithContext(ContextWithIncludeDeleted(c.Request.Context()))
	}
	s.assetHandler.GetAssets(c)
}

// CreateThreatModelAsset creates an asset
func (s *Server) CreateThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.CreateAsset(c)
}

// BulkCreateThreatModelAssets bulk creates assets
func (s *Server) BulkCreateThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.BulkCreateAssets(c)
}

// BulkUpsertThreatModelAssets bulk upserts assets
func (s *Server) BulkUpsertThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.BulkUpdateAssets(c)
}

// DeleteThreatModelAsset deletes an asset
func (s *Server) DeleteThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.DeleteAsset(c)
}

// GetThreatModelAsset gets an asset
func (s *Server) GetThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.GetAsset(c)
}

// UpdateThreatModelAsset updates an asset
func (s *Server) UpdateThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.UpdateAsset(c)
}

// PatchThreatModelAsset patches an asset
func (s *Server) PatchThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetHandler.PatchAsset(c)
}

// Asset Metadata Methods

// GetThreatModelAssetMetadata gets asset metadata
func (s *Server) GetThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.List(c)
}

// CreateThreatModelAssetMetadata creates asset metadata
func (s *Server) CreateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.Create(c)
}

// BulkCreateThreatModelAssetMetadata bulk creates asset metadata
func (s *Server) BulkCreateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.BulkCreate(c)
}

// BulkReplaceThreatModelAssetMetadata replaces all asset metadata (PUT)
func (s *Server) BulkReplaceThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.BulkReplace(c)
}

// BulkUpsertThreatModelAssetMetadata upserts asset metadata (PATCH)
func (s *Server) BulkUpsertThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.BulkUpsert(c)
}

// DeleteThreatModelAssetMetadata deletes asset metadata by key
func (s *Server) DeleteThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadata.Delete(c)
}

// GetThreatModelAssetMetadataByKey gets asset metadata by key
func (s *Server) GetThreatModelAssetMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadata.GetByKey(c)
}

// UpdateThreatModelAssetMetadata updates asset metadata by key
func (s *Server) UpdateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadata.Update(c)
}
