package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Asset Methods

// GetThreatModelAssets lists assets
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route list-assets request to the asset handler, enforcing include-deleted authorization
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route create-asset request to the asset handler for a given threat model
func (s *Server) CreateThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.CreateAsset(c)
}

// BulkCreateThreatModelAssets bulk creates assets
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-create-assets request to the asset handler for a given threat model
func (s *Server) BulkCreateThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.BulkCreateAssets(c)
}

// BulkUpsertThreatModelAssets bulk upserts assets
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-upsert-assets request to the asset handler for a given threat model
func (s *Server) BulkUpsertThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.BulkUpdateAssets(c)
}

// DeleteThreatModelAsset deletes an asset
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route delete-asset request to the asset handler for a given threat model and asset
func (s *Server) DeleteThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.DeleteAsset(c)
}

// GetThreatModelAsset gets an asset
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route fetch-asset request to the asset handler for a given threat model and asset
func (s *Server) GetThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.GetAsset(c)
}

// UpdateThreatModelAsset updates an asset
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route update-asset request to the asset handler for a given threat model and asset
func (s *Server) UpdateThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, _ UpdateThreatModelAssetParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.UpdateAsset(c)
}

// PatchThreatModelAsset patches an asset
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: route patch-asset request to the asset handler
func (s *Server) PatchThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, _ PatchThreatModelAssetParams) {
	s.assetHandler.PatchAsset(c)
}

// Asset Metadata Methods

// GetThreatModelAssetMetadata gets asset metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route list-asset-metadata request to the metadata handler
func (s *Server) GetThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.List(c)
}

// CreateThreatModelAssetMetadata creates asset metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route create-asset-metadata request to the metadata handler
func (s *Server) CreateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.Create(c)
}

// BulkCreateThreatModelAssetMetadata bulk creates asset metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-create-asset-metadata request to the metadata handler
func (s *Server) BulkCreateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.BulkCreate(c)
}

// BulkReplaceThreatModelAssetMetadata replaces all asset metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-replace-asset-metadata (PUT) request to the metadata handler
func (s *Server) BulkReplaceThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.BulkReplace(c)
}

// BulkUpsertThreatModelAssetMetadata upserts asset metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route bulk-upsert-asset-metadata (PATCH) request to the metadata handler
func (s *Server) BulkUpsertThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadata.BulkUpsert(c)
}

// DeleteThreatModelAssetMetadata deletes asset metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route delete-asset-metadata-by-key request to the metadata handler
func (s *Server) DeleteThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadata.Delete(c)
}

// GetThreatModelAssetMetadataByKey gets asset metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route fetch-asset-metadata-by-key request to the metadata handler
func (s *Server) GetThreatModelAssetMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadata.GetByKey(c)
}

// UpdateThreatModelAssetMetadata updates asset metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route update-asset-metadata-by-key request to the metadata handler
func (s *Server) UpdateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadata.Update(c)
}
