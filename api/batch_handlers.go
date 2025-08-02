package api

import (
	"database/sql"
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// BatchHandler provides handlers for batch operations across multiple resources
type BatchHandler struct {
	threatStore      ThreatStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// BatchThreatPatchRequest represents a batch patch request for threats
type BatchThreatPatchRequest struct {
	Operations []struct {
		ThreatID   string           `json:"threat_id" binding:"required"`
		Operations []PatchOperation `json:"operations" binding:"required"`
	} `json:"operations" binding:"required"`
}

// BatchThreatPatchResponse represents the response for batch threat patch operations
type BatchThreatPatchResponse struct {
	Results []struct {
		ThreatID string  `json:"threat_id"`
		Success  bool    `json:"success"`
		Threat   *Threat `json:"threat,omitempty"`
		Error    string  `json:"error,omitempty"`
	} `json:"results"`
	Summary struct {
		Total     int `json:"total"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"summary"`
}

// NewBatchHandler creates a new batch operations handler
func NewBatchHandler(threatStore ThreatStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *BatchHandler {
	return &BatchHandler{
		threatStore:      threatStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// BatchPatchThreats applies patch operations to multiple threats in a single request
// POST /threat_models/{threat_model_id}/threats/batch/patch
func (h *BatchHandler) BatchPatchThreats(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BatchPatchThreats - applying patch operations to multiple threats")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, userRole, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse request body
	batchRequest, err := ParseRequestBody[BatchThreatPatchRequest](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(batchRequest.Operations) == 0 {
		HandleRequestError(c, InvalidInputError("No patch operations provided"))
		return
	}

	if len(batchRequest.Operations) > 20 {
		HandleRequestError(c, InvalidInputError("Maximum 20 threat patch operations allowed per batch"))
		return
	}

	logger.Debug("Processing batch patch for %d threats in threat model %s (user: %s)",
		len(batchRequest.Operations), threatModelID, userName)

	// Initialize response
	response := BatchThreatPatchResponse{
		Results: make([]struct {
			ThreatID string  `json:"threat_id"`
			Success  bool    `json:"success"`
			Threat   *Threat `json:"threat,omitempty"`
			Error    string  `json:"error,omitempty"`
		}, len(batchRequest.Operations)),
		Summary: struct {
			Total     int `json:"total"`
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		}{
			Total: len(batchRequest.Operations),
		},
	}

	// Process each threat patch operation
	for i, operation := range batchRequest.Operations {
		result := &response.Results[i]
		result.ThreatID = operation.ThreatID

		// Validate threat ID format
		if _, err := ParseUUID(operation.ThreatID); err != nil {
			result.Success = false
			result.Error = "Invalid threat ID format, must be a valid UUID"
			response.Summary.Failed++
			continue
		}

		// Validate patch operations for this threat
		if len(operation.Operations) == 0 {
			result.Success = false
			result.Error = "No patch operations provided for threat"
			response.Summary.Failed++
			continue
		}

		// Validate patch authorization (ensure user can modify requested fields)
		if err := ValidatePatchAuthorization(operation.Operations, userRole); err != nil {
			result.Success = false
			result.Error = "Insufficient permissions for requested patch operations"
			response.Summary.Failed++
			continue
		}

		// Apply patch operations to this threat
		updatedThreat, err := h.threatStore.Patch(c.Request.Context(), operation.ThreatID, operation.Operations)
		if err != nil {
			logger.Error("Failed to patch threat %s in batch operation: %v", operation.ThreatID, err)
			result.Success = false
			result.Error = "Failed to apply patch operations: " + err.Error()
			response.Summary.Failed++
			continue
		}

		// Success
		result.Success = true
		result.Threat = updatedThreat
		response.Summary.Succeeded++

		logger.Debug("Successfully patched threat %s in batch operation", operation.ThreatID)
	}

	logger.Debug("Batch patch completed: %d succeeded, %d failed",
		response.Summary.Succeeded, response.Summary.Failed)

	// Return appropriate status code based on results
	statusCode := http.StatusOK
	if response.Summary.Failed > 0 && response.Summary.Succeeded == 0 {
		// All operations failed
		statusCode = http.StatusBadRequest
	} else if response.Summary.Failed > 0 {
		// Some operations failed
		statusCode = http.StatusMultiStatus
	}

	c.JSON(statusCode, response)
}

// BatchDeleteThreats deletes multiple threats in a single request
// DELETE /threat_models/{threat_model_id}/threats/batch
func (h *BatchHandler) BatchDeleteThreats(c *gin.Context) {
	logger := logging.GetContextLogger(c)
	logger.Debug("BatchDeleteThreats - deleting multiple threats")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse request body as array of threat IDs
	type BatchDeleteRequest struct {
		ThreatIDs []string `json:"threat_ids" binding:"required"`
	}

	deleteRequest, err := ParseRequestBody[BatchDeleteRequest](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(deleteRequest.ThreatIDs) == 0 {
		HandleRequestError(c, InvalidInputError("No threat IDs provided"))
		return
	}

	if len(deleteRequest.ThreatIDs) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 threats allowed per batch delete"))
		return
	}

	logger.Debug("Batch deleting %d threats in threat model %s (user: %s)",
		len(deleteRequest.ThreatIDs), threatModelID, userName)

	// Initialize response
	type BatchDeleteResponse struct {
		Results []struct {
			ThreatID string `json:"threat_id"`
			Success  bool   `json:"success"`
			Error    string `json:"error,omitempty"`
		} `json:"results"`
		Summary struct {
			Total     int `json:"total"`
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"summary"`
	}

	response := BatchDeleteResponse{
		Results: make([]struct {
			ThreatID string `json:"threat_id"`
			Success  bool   `json:"success"`
			Error    string `json:"error,omitempty"`
		}, len(deleteRequest.ThreatIDs)),
		Summary: struct {
			Total     int `json:"total"`
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		}{
			Total: len(deleteRequest.ThreatIDs),
		},
	}

	// Process each threat deletion
	for i, threatID := range deleteRequest.ThreatIDs {
		result := &response.Results[i]
		result.ThreatID = threatID

		// Validate threat ID format
		if _, err := ParseUUID(threatID); err != nil {
			result.Success = false
			result.Error = "Invalid threat ID format, must be a valid UUID"
			response.Summary.Failed++
			continue
		}

		// Delete threat
		if err := h.threatStore.Delete(c.Request.Context(), threatID); err != nil {
			logger.Error("Failed to delete threat %s in batch operation: %v", threatID, err)
			result.Success = false
			result.Error = "Failed to delete threat: " + err.Error()
			response.Summary.Failed++
			continue
		}

		// Success
		result.Success = true
		response.Summary.Succeeded++

		logger.Debug("Successfully deleted threat %s in batch operation", threatID)
	}

	logger.Debug("Batch delete completed: %d succeeded, %d failed",
		response.Summary.Succeeded, response.Summary.Failed)

	// Return appropriate status code based on results
	statusCode := http.StatusOK
	if response.Summary.Failed > 0 && response.Summary.Succeeded == 0 {
		// All operations failed
		statusCode = http.StatusBadRequest
	} else if response.Summary.Failed > 0 {
		// Some operations failed
		statusCode = http.StatusMultiStatus
	}

	c.JSON(statusCode, response)
}
