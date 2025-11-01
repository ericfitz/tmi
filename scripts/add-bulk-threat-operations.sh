#!/bin/bash
# Add BulkPatch and BulkDelete operations to ThreatSubResourceHandler

set -e

echo "Adding bulk PATCH and DELETE operations to threat handler..."

cat >> api/threat_sub_resource_handlers.go << 'EOF'

// BulkPatchThreats applies JSON patch operations to multiple threats
// PATCH /threat_models/{threat_model_id}/threats/bulk
func (h *ThreatSubResourceHandler) BulkPatchThreats(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkPatchThreats - applying patch operations to multiple threats")

	// Get authenticated user
	userEmail, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse bulk patch request
	var bulkPatchRequest struct {
		Patches []struct {
			ID         string           `json:"id" binding:"required"`
			Operations []PatchOperation `json:"operations" binding:"required"`
		} `json:"patches" binding:"required"`
	}

	if err := c.ShouldBindJSON(&bulkPatchRequest); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid bulk patch request format"))
		return
	}

	if len(bulkPatchRequest.Patches) == 0 {
		HandleRequestError(c, InvalidInputError("No patches provided"))
		return
	}

	logger.Debug("Bulk patching %d threats (user: %s)", len(bulkPatchRequest.Patches), userEmail)

	// Apply patches to each threat
	updatedThreats := make([]Threat, 0, len(bulkPatchRequest.Patches))
	for _, patch := range bulkPatchRequest.Patches {
		// Validate threat ID
		if _, err := ParseUUID(patch.ID); err != nil {
			HandleRequestError(c, InvalidIDError(fmt.Sprintf("Invalid threat ID format: %s", patch.ID)))
			return
		}

		// Validate patch authorization
		if err := ValidatePatchAuthorization(patch.Operations, userRole); err != nil {
			HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
			return
		}

		// Apply patch
		updatedThreat, err := h.threatStore.Patch(c.Request.Context(), patch.ID, patch.Operations)
		if err != nil {
			HandleRequestError(c, InternalServerError(fmt.Sprintf("Failed to patch threat %s", patch.ID), err))
			return
		}
		updatedThreats = append(updatedThreats, *updatedThreat)
	}

	logger.Info("Successfully bulk patched %d threats (user: %s)", len(updatedThreats), userEmail)
	c.JSON(http.StatusOK, updatedThreats)
}

// BulkDeleteThreats deletes multiple threats
// DELETE /threat_models/{threat_model_id}/threats/bulk
func (h *ThreatSubResourceHandler) BulkDeleteThreats(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkDeleteThreats - deleting multiple threats")

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse bulk delete request
	var bulkDeleteRequest struct {
		ThreatIDs []string `json:"threat_ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&bulkDeleteRequest); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid bulk delete request format"))
		return
	}

	if len(bulkDeleteRequest.ThreatIDs) == 0 {
		HandleRequestError(c, InvalidInputError("No threat IDs provided"))
		return
	}

	if len(bulkDeleteRequest.ThreatIDs) > 20 {
		HandleRequestError(c, InvalidInputError("Maximum 20 threats can be deleted at once"))
		return
	}

	logger.Debug("Bulk deleting %d threats (user: %s)", len(bulkDeleteRequest.ThreatIDs), userEmail)

	// Delete each threat
	deletedIDs := make([]string, 0, len(bulkDeleteRequest.ThreatIDs))
	for _, threatID := range bulkDeleteRequest.ThreatIDs {
		// Validate threat ID
		if _, err := ParseUUID(threatID); err != nil {
			HandleRequestError(c, InvalidIDError(fmt.Sprintf("Invalid threat ID format: %s", threatID)))
			return
		}

		// Delete threat
		if err := h.threatStore.Delete(c.Request.Context(), threatID); err != nil {
			HandleRequestError(c, InternalServerError(fmt.Sprintf("Failed to delete threat %s", threatID), err))
			return
		}
		deletedIDs = append(deletedIDs, threatID)
	}

	response := map[string]interface{}{
		"deleted_count": len(deletedIDs),
		"deleted_ids":   deletedIDs,
	}

	logger.Info("Successfully bulk deleted %d threats (user: %s)", len(deletedIDs), userEmail)
	c.JSON(http.StatusOK, response)
}
EOF

echo "Done! Bulk operations added to ThreatSubResourceHandler."
