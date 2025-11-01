#!/bin/bash
# Add BulkUpdate operations to Asset, Document, and Repository handlers

set -e

echo "Adding bulk UPDATE (upsert) operations to simple resource handlers..."

# Add BulkUpdateAssets to AssetSubResourceHandler
cat >> api/asset_sub_resource_handlers.go << 'EOF'

// BulkUpdateAssets updates or creates multiple assets (upsert operation)
// PUT /threat_models/{threat_model_id}/assets/bulk
func (h *AssetSubResourceHandler) BulkUpdateAssets(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateAssets - upserting multiple assets")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	threatModelUUID, err := ParseUUID(threatModelID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of assets
	var assets []Asset
	if err := c.ShouldBindJSON(&assets); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(assets) == 0 {
		HandleRequestError(c, InvalidInputError("No assets provided"))
		return
	}

	if len(assets) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 assets allowed per bulk operation"))
		return
	}

	// Validate each asset
	for _, asset := range assets {
		if asset.Id == nil {
			HandleRequestError(c, InvalidInputError("Asset ID is required for all assets in bulk update"))
			return
		}
		if asset.Name == "" {
			HandleRequestError(c, InvalidInputError("Asset name is required for all assets"))
			return
		}
	}

	logger.Debug("Bulk updating %d assets for threat model %s (user: %s)", len(assets), threatModelID, userEmail)

	// Upsert each asset
	upsertedAssets := make([]Asset, 0, len(assets))
	for _, asset := range assets {
		// Set threat model ID
		asset.ThreatModelId = &threatModelUUID

		// Check if asset exists
		existingAsset, err := h.assetStore.Read(c.Request.Context(), asset.Id.String())
		if err != nil {
			// Asset doesn't exist, create it
			createdAsset, err := h.assetStore.Create(c.Request.Context(), &asset)
			if err != nil {
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to create asset %s", asset.Id.String()), err))
				return
			}
			upsertedAssets = append(upsertedAssets, *createdAsset)
		} else {
			// Asset exists, update it
			updatedAsset, err := h.assetStore.Update(c.Request.Context(), asset.Id.String(), &asset)
			if err != nil {
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to update asset %s", asset.Id.String()), err))
				return
			}
			upsertedAssets = append(upsertedAssets, *updatedAsset)
			_ = existingAsset // suppress unused variable warning
		}
	}

	logger.Info("Successfully bulk upserted %d assets for threat model %s (user: %s)", len(upsertedAssets), threatModelID, userEmail)
	c.JSON(http.StatusOK, upsertedAssets)
}
EOF

# Add BulkUpdateDocuments to DocumentSubResourceHandler
cat >> api/document_sub_resource_handlers.go << 'EOF'

// BulkUpdateDocuments updates or creates multiple documents (upsert operation)
// PUT /threat_models/{threat_model_id}/documents/bulk
func (h *DocumentSubResourceHandler) BulkUpdateDocuments(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateDocuments - upserting multiple documents")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	threatModelUUID, err := ParseUUID(threatModelID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of documents
	var documents []Document
	if err := c.ShouldBindJSON(&documents); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(documents) == 0 {
		HandleRequestError(c, InvalidInputError("No documents provided"))
		return
	}

	if len(documents) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 documents allowed per bulk operation"))
		return
	}

	// Validate each document
	for _, document := range documents {
		if document.Id == nil {
			HandleRequestError(c, InvalidInputError("Document ID is required for all documents in bulk update"))
			return
		}
		if document.Name == "" {
			HandleRequestError(c, InvalidInputError("Document name is required for all documents"))
			return
		}
	}

	logger.Debug("Bulk updating %d documents for threat model %s (user: %s)", len(documents), threatModelID, userEmail)

	// Upsert each document
	upsertedDocuments := make([]Document, 0, len(documents))
	for _, document := range documents {
		// Set threat model ID
		document.ThreatModelId = &threatModelUUID

		// Check if document exists
		existingDocument, err := h.documentStore.Read(c.Request.Context(), document.Id.String())
		if err != nil {
			// Document doesn't exist, create it
			createdDocument, err := h.documentStore.Create(c.Request.Context(), &document)
			if err != nil {
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to create document %s", document.Id.String()), err))
				return
			}
			upsertedDocuments = append(upsertedDocuments, *createdDocument)
		} else {
			// Document exists, update it
			updatedDocument, err := h.documentStore.Update(c.Request.Context(), document.Id.String(), &document)
			if err != nil {
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to update document %s", document.Id.String()), err))
				return
			}
			upsertedDocuments = append(upsertedDocuments, *updatedDocument)
			_ = existingDocument // suppress unused variable warning
		}
	}

	logger.Info("Successfully bulk upserted %d documents for threat model %s (user: %s)", len(upsertedDocuments), threatModelID, userEmail)
	c.JSON(http.StatusOK, upsertedDocuments)
}
EOF

# Add BulkUpdateRepositorys to RepositorySubResourceHandler
cat >> api/repository_sub_resource_handlers.go << 'EOF'

// BulkUpdateRepositorys updates or creates multiple repositories (upsert operation)
// PUT /threat_models/{threat_model_id}/repositories/bulk
func (h *RepositorySubResourceHandler) BulkUpdateRepositorys(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateRepositorys - upserting multiple repositories")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	threatModelUUID, err := ParseUUID(threatModelID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of repositories
	var repositories []Repository
	if err := c.ShouldBindJSON(&repositories); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(repositories) == 0 {
		HandleRequestError(c, InvalidInputError("No repositories provided"))
		return
	}

	if len(repositories) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 repositories allowed per bulk operation"))
		return
	}

	// Validate each repository
	for _, repository := range repositories {
		if repository.Id == nil {
			HandleRequestError(c, InvalidInputError("Repository ID is required for all repositories in bulk update"))
			return
		}
		if repository.Name == nil || *repository.Name == "" {
			HandleRequestError(c, InvalidInputError("Repository name is required for all repositories"))
			return
		}
	}

	logger.Debug("Bulk updating %d repositories for threat model %s (user: %s)", len(repositories), threatModelID, userEmail)

	// Upsert each repository
	upsertedRepositories := make([]Repository, 0, len(repositories))
	for _, repository := range repositories {
		// Set threat model ID
		repository.ThreatModelId = &threatModelUUID

		// Check if repository exists
		existingRepository, err := h.repositoryStore.Read(c.Request.Context(), repository.Id.String())
		if err != nil {
			// Repository doesn't exist, create it
			createdRepository, err := h.repositoryStore.Create(c.Request.Context(), &repository)
			if err != nil {
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to create repository %s", repository.Id.String()), err))
				return
			}
			upsertedRepositories = append(upsertedRepositories, *createdRepository)
		} else {
			// Repository exists, update it
			updatedRepository, err := h.repositoryStore.Update(c.Request.Context(), repository.Id.String(), &repository)
			if err != nil {
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to update repository %s", repository.Id.String()), err))
				return
			}
			upsertedRepositories = append(upsertedRepositories, *updatedRepository)
			_ = existingRepository // suppress unused variable warning
		}
	}

	logger.Info("Successfully bulk upserted %d repositories for threat model %s (user: %s)", len(upsertedRepositories), threatModelID, userEmail)
	c.JSON(http.StatusOK, upsertedRepositories)
}
EOF

echo "Done! Bulk upsert operations added to all simple resource handlers."
