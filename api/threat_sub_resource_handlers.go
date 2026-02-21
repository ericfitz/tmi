package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/gin-gonic/gin"
)

// ThreatSubResourceHandler provides handlers for threat sub-resource operations
type ThreatSubResourceHandler struct {
	threatStore      ThreatStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewThreatSubResourceHandler creates a new threat sub-resource handler
func NewThreatSubResourceHandler(threatStore ThreatStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *ThreatSubResourceHandler {
	return &ThreatSubResourceHandler{
		threatStore:      threatStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetThreats retrieves all threats for a threat model with pagination
// GET /threat_models/{threat_model_id}/threats
func (h *ThreatSubResourceHandler) GetThreats(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreats - retrieving threats for threat model")

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

	// Parse pagination parameters
	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	// Validate pagination parameters
	if limit < 1 || limit > 100 {
		HandleRequestError(c, InvalidInputError("Limit must be between 1 and 100"))
		return
	}
	if offset < 0 {
		HandleRequestError(c, InvalidInputError("Offset must be non-negative"))
		return
	}

	// Get authenticated user (should be set by middleware)
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving threats for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, userEmail, offset, limit)

	// Get threats from store (authorization is handled by middleware)
	filter := ThreatFilter{
		Offset: offset,
		Limit:  limit,
	}
	threats, total, err := h.threatStore.List(c.Request.Context(), threatModelID, filter)
	if err != nil {
		logger.Error("Failed to retrieve threats: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve threats"))
		return
	}

	logger.Debug("Successfully retrieved %d threats (total: %d)", len(threats), total)
	c.JSON(http.StatusOK, ListThreatsResponse{
		Threats: threats,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}

// GetThreatsWithFilters retrieves all threats for a threat model with advanced filtering
// GET /threat_models/{threat_model_id}/threats with query parameters
func (h *ThreatSubResourceHandler) GetThreatsWithFilters(c *gin.Context, params GetThreatModelThreatsParams) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreatsWithFilters - retrieving threats with advanced filtering")

	// Extract and validate threat model ID
	threatModelID, err := h.validateThreatModelID(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Build filter from parameters
	filter, err := h.buildThreatFilter(params)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving threats for threat model %s (user: %s) with filters",
		threatModelID, userEmail)

	// Get threats from store with filtering
	threats, total, err := h.threatStore.List(c.Request.Context(), threatModelID, filter)
	if err != nil {
		logger.Error("Failed to retrieve threats with filters: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve threats"))
		return
	}

	logger.Debug("Successfully retrieved %d threats with filters (total: %d)", len(threats), total)
	c.JSON(http.StatusOK, ListThreatsResponse{
		Threats: threats,
		Total:   total,
		Limit:   filter.Limit,
		Offset:  filter.Offset,
	})
}

func (h *ThreatSubResourceHandler) validateThreatModelID(c *gin.Context) (string, error) {
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		return "", InvalidIDError("Missing threat model ID")
	}

	if _, err := ParseUUID(threatModelID); err != nil {
		return "", InvalidIDError("Invalid threat model ID format, must be a valid UUID")
	}

	return threatModelID, nil
}

func (h *ThreatSubResourceHandler) buildThreatFilter(params GetThreatModelThreatsParams) (ThreatFilter, error) {
	filter := ThreatFilter{
		Offset: 0,
		Limit:  20,
	}

	// Set pagination parameters
	if err := h.setPaginationParams(&filter, params); err != nil {
		return filter, err
	}

	// Set filtering parameters
	if err := h.setFilterParams(&filter, params); err != nil {
		return filter, err
	}

	// Set score comparison parameters
	h.setScoreParams(&filter, params)

	// Set date parameters
	h.setDateParams(&filter, params)

	// Set sorting parameter
	if params.Sort != nil {
		filter.Sort = params.Sort
	}

	return filter, nil
}

func (h *ThreatSubResourceHandler) setPaginationParams(filter *ThreatFilter, params GetThreatModelThreatsParams) error {
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > 100 {
			return InvalidInputError("Limit must be between 1 and 100")
		}
		filter.Limit = *params.Limit
	}

	if params.Offset != nil {
		if *params.Offset < 0 {
			return InvalidInputError("Offset must be non-negative")
		}
		filter.Offset = *params.Offset
	}

	return nil
}

func (h *ThreatSubResourceHandler) setFilterParams(filter *ThreatFilter, params GetThreatModelThreatsParams) error {
	filter.Name = params.Name
	filter.Description = params.Description

	// Validate and set threat types
	if params.ThreatType != nil && len(*params.ThreatType) > 0 {
		if err := h.validateThreatTypes(*params.ThreatType); err != nil {
			return err
		}
		filter.ThreatType = *params.ThreatType
	}

	// Set other filter fields
	if params.Severity != nil {
		severityStr := string(*params.Severity)
		filter.Severity = &severityStr
	}
	filter.Priority = params.Priority
	filter.Status = params.Status
	filter.DiagramID = params.DiagramId
	filter.CellID = params.CellId

	return nil
}

func (h *ThreatSubResourceHandler) validateThreatTypes(types []string) error {
	if len(types) > 10 {
		return InvalidInputError("Maximum 10 threat types in filter")
	}

	for _, t := range types {
		if strings.TrimSpace(t) == "" {
			return InvalidInputError("Threat type cannot be empty")
		}
	}

	return nil
}

func (h *ThreatSubResourceHandler) setScoreParams(filter *ThreatFilter, params GetThreatModelThreatsParams) {
	filter.ScoreGT = params.ScoreGt
	filter.ScoreLT = params.ScoreLt
	filter.ScoreEQ = params.ScoreEq
	filter.ScoreGE = params.ScoreGe
	filter.ScoreLE = params.ScoreLe
}

func (h *ThreatSubResourceHandler) setDateParams(filter *ThreatFilter, params GetThreatModelThreatsParams) {
	filter.CreatedAfter = params.CreatedAfter
	filter.CreatedBefore = params.CreatedBefore
	filter.ModifiedAfter = params.ModifiedAfter
	filter.ModifiedBefore = params.ModifiedBefore
}

// GetThreat retrieves a specific threat by ID
// GET /threat_models/{threat_model_id}/threats/{threat_id}
func (h *ThreatSubResourceHandler) GetThreat(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreat - retrieving specific threat")

	// Extract threat ID from URL
	threatID := c.Param("threat_id")
	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving threat %s (user: %s)", threatID, userEmail)

	// Get threat from store
	threat, err := h.threatStore.Get(c.Request.Context(), threatID)
	if err != nil {
		logger.Error("Failed to retrieve threat %s: %v", threatID, err)
		HandleRequestError(c, NotFoundError("Threat not found"))
		return
	}

	logger.Debug("Successfully retrieved threat %s", threatID)
	c.JSON(http.StatusOK, threat)
}

// CreateThreat creates a new threat in a threat model
// POST /threat_models/{threat_model_id}/threats
func (h *ThreatSubResourceHandler) CreateThreat(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateThreat - creating new threat")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["threat_create"]
	threat, err := ValidateAndParseRequest[Threat](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set threat model ID from URL (override any value in body)
	threat.ThreatModelId = &threatModelUUID

	// Generate UUIDv7 if not provided (for better index locality)
	if threat.Id == nil {
		id := uuidgen.MustNewForEntity(uuidgen.EntityTypeThreat)
		threat.Id = &id
	}

	logger.Debug("Creating threat %s in threat model %s (user: %s)",
		threat.Id.String(), threatModelID, userEmail)

	// Create threat in store
	if err := h.threatStore.Create(c.Request.Context(), threat); err != nil {
		logger.Error("Failed to create threat: %v", err)
		HandleRequestError(c, ServerError("Failed to create threat"))
		return
	}

	logger.Debug("Successfully created threat %s", threat.Id.String())
	c.JSON(http.StatusCreated, threat)
}

// UpdateThreat updates an existing threat
// PUT /threat_models/{threat_model_id}/threats/{threat_id}
func (h *ThreatSubResourceHandler) UpdateThreat(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateThreat - updating existing threat")

	// Extract threat ID from URL
	threatID := c.Param("threat_id")
	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}

	// Validate threat ID format
	threatUUID, err := ParseUUID(threatID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
		return
	}

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	threatModelUUID, err := ParseUUID(threatModelID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["threat_update"]
	threat, err := ValidateAndParseRequest[Threat](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set IDs from URL (override any values in body)
	threat.Id = &threatUUID
	threat.ThreatModelId = &threatModelUUID

	logger.Debug("Updating threat %s (user: %s)", threatID, userEmail)

	// Update threat in store
	if err := h.threatStore.Update(c.Request.Context(), threat); err != nil {
		logger.Error("Failed to update threat %s: %v", threatID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Threat not found", "Failed to update threat"))
		return
	}

	logger.Debug("Successfully updated threat %s", threatID)
	c.JSON(http.StatusOK, threat)
}

// PatchThreat applies JSON patch operations to a threat
// PATCH /threat_models/{threat_model_id}/threats/{threat_id}
func (h *ThreatSubResourceHandler) PatchThreat(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchThreat - applying patch operations to threat")

	// Extract threat ID from URL
	threatID := c.Param("threat_id")
	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, userRole, err := ValidateAuthenticatedUser(c)
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

	// Validate patch authorization (ensure user can modify requested fields)
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	logger.Debug("Applying %d patch operations to threat %s (user: %s)",
		len(operations), threatID, userEmail)

	// Apply patch operations
	updatedThreat, err := h.threatStore.Patch(c.Request.Context(), threatID, operations)
	if err != nil {
		logger.Error("Failed to patch threat %s: %v", threatID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Threat not found", "Failed to apply patch operations"))
		return
	}

	logger.Debug("Successfully patched threat %s", threatID)
	c.JSON(http.StatusOK, updatedThreat)
}

// DeleteThreat deletes a threat
// DELETE /threat_models/{threat_model_id}/threats/{threat_id}
func (h *ThreatSubResourceHandler) DeleteThreat(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteThreat - deleting threat")

	// Extract threat ID from URL
	threatID := c.Param("threat_id")
	if threatID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat ID"))
		return
	}

	// Validate threat ID format
	if _, err := ParseUUID(threatID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting threat %s (user: %s)", threatID, userEmail)

	// Delete threat from store
	if err := h.threatStore.Delete(c.Request.Context(), threatID); err != nil {
		logger.Error("Failed to delete threat %s: %v", threatID, err)
		HandleRequestError(c, StoreErrorToRequestError(err, "Threat not found", "Failed to delete threat"))
		return
	}

	logger.Debug("Successfully deleted threat %s", threatID)
	c.Status(http.StatusNoContent)
}

// BulkCreateThreats creates multiple threats in a single request
// POST /threat_models/{threat_model_id}/threats/bulk
func (h *ThreatSubResourceHandler) BulkCreateThreats(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateThreats - creating multiple threats")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of threats
	var threats []Threat
	if err := c.ShouldBindJSON(&threats); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(threats) == 0 {
		HandleRequestError(c, InvalidInputError("No threats provided"))
		return
	}

	if len(threats) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 threats allowed per bulk operation"))
		return
	}

	// Validate each threat
	for _, threat := range threats {
		if threat.Name == "" {
			HandleRequestError(c, InvalidInputError("Threat name is required for all threats"))
			return
		}
	}

	// Prepare threats
	for i := range threats {
		threat := &threats[i]

		// Set threat model ID from URL
		threat.ThreatModelId = &threatModelUUID

		// Generate UUIDv7 if not provided (for better index locality)
		if threat.Id == nil {
			id := uuidgen.MustNewForEntity(uuidgen.EntityTypeThreat)
			threat.Id = &id
		}
	}

	logger.Debug("Bulk creating %d threats in threat model %s (user: %s)",
		len(threats), threatModelID, userEmail)

	// Create threats in store
	if err := h.threatStore.BulkCreate(c.Request.Context(), threats); err != nil {
		logger.Error("Failed to bulk create threats: %v", err)
		HandleRequestError(c, ServerError("Failed to create threats"))
		return
	}

	logger.Debug("Successfully bulk created %d threats", len(threats))
	c.JSON(http.StatusCreated, threats)
}

// BulkUpdateThreats updates multiple threats in a single request
// PUT /threat_models/{threat_model_id}/threats/bulk
func (h *ThreatSubResourceHandler) BulkUpdateThreats(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateThreats - updating multiple threats")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of threats
	var threats []Threat
	if err := c.ShouldBindJSON(&threats); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(threats) == 0 {
		HandleRequestError(c, InvalidInputError("No threats provided"))
		return
	}

	if len(threats) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 threats allowed per bulk operation"))
		return
	}

	// Validate each threat
	for _, threat := range threats {
		if threat.Id == nil {
			HandleRequestError(c, InvalidInputError("Threat ID is required for all threats in bulk update"))
			return
		}
		if threat.Name == "" {
			HandleRequestError(c, InvalidInputError("Threat name is required for all threats"))
			return
		}
	}

	// Prepare threats for update
	for i := range threats {
		threat := &threats[i]
		// Ensure threat model ID matches URL
		threat.ThreatModelId = &threatModelUUID
	}

	logger.Debug("Bulk updating %d threats in threat model %s (user: %s)",
		len(threats), threatModelID, userEmail)

	// Update threats in store
	if err := h.threatStore.BulkUpdate(c.Request.Context(), threats); err != nil {
		logger.Error("Failed to bulk update threats: %v", err)
		HandleRequestError(c, ServerError("Failed to update threats"))
		return
	}

	logger.Debug("Successfully bulk updated %d threats", len(threats))
	c.JSON(http.StatusOK, threats)
}

// BulkPatchThreats applies JSON patch operations to multiple threats
// PATCH /threat_models/{threat_model_id}/threats/bulk
func (h *ThreatSubResourceHandler) BulkPatchThreats(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkPatchThreats - applying patch operations to multiple threats")

	// Get authenticated user
	userEmail, _, userRole, err := ValidateAuthenticatedUser(c)
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
			HandleRequestError(c, ServerError(fmt.Sprintf("Failed to patch threat %s", patch.ID)))
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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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
			HandleRequestError(c, ServerError(fmt.Sprintf("Failed to delete threat %s", threatID)))
			return
		}
		deletedIDs = append(deletedIDs, threatID)
	}

	response := map[string]any{
		"deleted_count": len(deletedIDs),
		"deleted_ids":   deletedIDs,
	}

	logger.Info("Successfully bulk deleted %d threats (user: %s)", len(deletedIDs), userEmail)
	c.JSON(http.StatusOK, response)
}
