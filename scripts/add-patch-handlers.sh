#!/bin/bash
# Add PATCH handler methods to simple resource handlers

set -e

echo "Adding PATCH handler methods..."

# Asset handler
cat >> api/asset_sub_resource_handlers.go << 'EOF'

// PatchAsset applies JSON patch operations to an asset
// PATCH /threat_models/{threat_model_id}/assets/{asset_id}
func (h *AssetSubResourceHandler) PatchAsset(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchAsset - applying patch operations to asset")

	// Extract asset ID from URL
	assetID := c.Param("asset_id")
	if assetID == "" {
		HandleRequestError(c, InvalidIDError("Missing asset ID"))
		return
	}

	// Validate asset ID format
	if _, err := ParseUUID(assetID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid asset ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse patch operations from request body
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(operations) == 0 {
		HandleRequestError(c, InvalidInputError("No patch operations provided"))
		return
	}

	// Validate patch authorization
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	logger.Debug("Applying %d patch operations to asset %s (user: %s)",
		len(operations), assetID, userEmail)

	// Apply patch operations
	updatedAsset, err := h.assetStore.Patch(c.Request.Context(), assetID, operations)
	if err != nil {
		HandleRequestError(c, InternalServerError("Failed to patch asset", err))
		return
	}

	logger.Info("Successfully patched asset %s (user: %s)", assetID, userEmail)
	c.JSON(http.StatusOK, updatedAsset)
}
EOF

echo "  - Added PatchAsset to AssetSubResourceHandler"

# Document handler
cat >> api/document_sub_resource_handlers.go << 'EOF'

// PatchDocument applies JSON patch operations to a document
// PATCH /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) PatchDocument(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchDocument - applying patch operations to document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	if _, err := ParseUUID(documentID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse patch operations from request body
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(operations) == 0 {
		HandleRequestError(c, InvalidInputError("No patch operations provided"))
		return
	}

	// Validate patch authorization
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	logger.Debug("Applying %d patch operations to document %s (user: %s)",
		len(operations), documentID, userEmail)

	// Apply patch operations
	updatedDocument, err := h.documentStore.Patch(c.Request.Context(), documentID, operations)
	if err != nil {
		HandleRequestError(c, InternalServerError("Failed to patch document", err))
		return
	}

	logger.Info("Successfully patched document %s (user: %s)", documentID, userEmail)
	c.JSON(http.StatusOK, updatedDocument)
}
EOF

echo "  - Added PatchDocument to DocumentSubResourceHandler"

# Note handler
cat >> api/note_sub_resource_handlers.go << 'EOF'

// PatchNote applies JSON patch operations to a note
// PATCH /threat_models/{threat_model_id}/notes/{note_id}
func (h *NoteSubResourceHandler) PatchNote(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchNote - applying patch operations to note")

	// Extract note ID from URL
	noteID := c.Param("note_id")
	if noteID == "" {
		HandleRequestError(c, InvalidIDError("Missing note ID"))
		return
	}

	// Validate note ID format
	if _, err := ParseUUID(noteID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid note ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse patch operations from request body
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(operations) == 0 {
		HandleRequestError(c, InvalidInputError("No patch operations provided"))
		return
	}

	// Validate patch authorization
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	logger.Debug("Applying %d patch operations to note %s (user: %s)",
		len(operations), noteID, userEmail)

	// Apply patch operations
	updatedNote, err := h.noteStore.Patch(c.Request.Context(), noteID, operations)
	if err != nil {
		HandleRequestError(c, InternalServerError("Failed to patch note", err))
		return
	}

	logger.Info("Successfully patched note %s (user: %s)", noteID, userEmail)
	c.JSON(http.StatusOK, updatedNote)
}
EOF

echo "  - Added PatchNote to NoteSubResourceHandler"

# Repository handler
cat >> api/repository_sub_resource_handlers.go << 'EOF'

// PatchRepository applies JSON patch operations to a repository
// PATCH /threat_models/{threat_model_id}/repositories/{repository_id}
func (h *RepositorySubResourceHandler) PatchRepository(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchRepository - applying patch operations to repository")

	// Extract repository ID from URL
	repositoryID := c.Param("repository_id")
	if repositoryID == "" {
		HandleRequestError(c, InvalidIDError("Missing repository ID"))
		return
	}

	// Validate repository ID format
	if _, err := ParseUUID(repositoryID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid repository ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse patch operations from request body
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(operations) == 0 {
		HandleRequestError(c, InvalidInputError("No patch operations provided"))
		return
	}

	// Validate patch authorization
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	logger.Debug("Applying %d patch operations to repository %s (user: %s)",
		len(operations), repositoryID, userEmail)

	// Apply patch operations
	updatedRepository, err := h.repositoryStore.Patch(c.Request.Context(), repositoryID, operations)
	if err != nil {
		HandleRequestError(c, InternalServerError("Failed to patch repository", err))
		return
	}

	logger.Info("Successfully patched repository %s (user: %s)", repositoryID, userEmail)
	c.JSON(http.StatusOK, updatedRepository)
}
EOF

echo "  - Added PatchRepository to RepositorySubResourceHandler"

echo "Done! All PATCH handlers added."
