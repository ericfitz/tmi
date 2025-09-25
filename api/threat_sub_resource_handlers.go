package api

import (
	"database/sql"
	"net/http"

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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving threats for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, userEmail, offset, limit)

	// Get threats from store (authorization is handled by middleware)
	threats, err := h.threatStore.ListSimple(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve threats: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve threats"))
		return
	}

	logger.Debug("Successfully retrieved %d threats", len(threats))
	c.JSON(http.StatusOK, threats)
}

// GetThreatsWithFilters retrieves all threats for a threat model with advanced filtering
// GET /threat_models/{threat_model_id}/threats with query parameters
func (h *ThreatSubResourceHandler) GetThreatsWithFilters(c *gin.Context, params GetThreatModelThreatsParams) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetThreatsWithFilters - retrieving threats with advanced filtering")

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

	// Get authenticated user (should be set by middleware)
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Build filter from parameters
	filter := ThreatFilter{
		Offset: 0,
		Limit:  20, // defaults
	}

	// Set pagination parameters
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > 100 {
			HandleRequestError(c, InvalidInputError("Limit must be between 1 and 100"))
			return
		}
		filter.Limit = *params.Limit
	}

	if params.Offset != nil {
		if *params.Offset < 0 {
			HandleRequestError(c, InvalidInputError("Offset must be non-negative"))
			return
		}
		filter.Offset = *params.Offset
	}

	// Set filtering parameters
	if params.Name != nil {
		filter.Name = params.Name
	}
	if params.Description != nil {
		filter.Description = params.Description
	}
	if params.ThreatType != nil {
		filter.ThreatType = params.ThreatType
	}
	if params.Severity != nil {
		severity := ThreatSeverity(string(*params.Severity))
		filter.Severity = &severity
	}
	if params.Priority != nil {
		filter.Priority = params.Priority
	}
	if params.Status != nil {
		filter.Status = params.Status
	}
	if params.DiagramId != nil {
		filter.DiagramID = params.DiagramId
	}
	if params.CellId != nil {
		filter.CellID = params.CellId
	}

	// Set score comparison parameters
	if params.ScoreGt != nil {
		filter.ScoreGT = params.ScoreGt
	}
	if params.ScoreLt != nil {
		filter.ScoreLT = params.ScoreLt
	}
	if params.ScoreEq != nil {
		filter.ScoreEQ = params.ScoreEq
	}
	if params.ScoreGe != nil {
		filter.ScoreGE = params.ScoreGe
	}
	if params.ScoreLe != nil {
		filter.ScoreLE = params.ScoreLe
	}

	// Set date parameters
	if params.CreatedAfter != nil {
		filter.CreatedAfter = params.CreatedAfter
	}
	if params.CreatedBefore != nil {
		filter.CreatedBefore = params.CreatedBefore
	}
	if params.ModifiedAfter != nil {
		filter.ModifiedAfter = params.ModifiedAfter
	}
	if params.ModifiedBefore != nil {
		filter.ModifiedBefore = params.ModifiedBefore
	}

	// Set sorting parameter
	if params.Sort != nil {
		filter.Sort = params.Sort
	}

	logger.Debug("Retrieving threats for threat model %s (user: %s) with filters",
		threatModelID, userEmail)

	// Get threats from store with filtering
	threats, err := h.threatStore.List(c.Request.Context(), threatModelID, filter)
	if err != nil {
		logger.Error("Failed to retrieve threats with filters: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve threats"))
		return
	}

	logger.Debug("Successfully retrieved %d threats with filters", len(threats))
	c.JSON(http.StatusOK, threats)
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
	userEmail, _, err := ValidateAuthenticatedUser(c)
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
	userEmail, _, err := ValidateAuthenticatedUser(c)
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
	userEmail, _, err := ValidateAuthenticatedUser(c)
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
		HandleRequestError(c, ServerError("Failed to update threat"))
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
		HandleRequestError(c, ServerError("Failed to apply patch operations"))
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
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting threat %s (user: %s)", threatID, userEmail)

	// Delete threat from store
	if err := h.threatStore.Delete(c.Request.Context(), threatID); err != nil {
		logger.Error("Failed to delete threat %s: %v", threatID, err)
		HandleRequestError(c, ServerError("Failed to delete threat"))
		return
	}

	logger.Debug("Successfully deleted threat %s", threatID)
	c.JSON(http.StatusNoContent, nil)
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
	userEmail, _, err := ValidateAuthenticatedUser(c)
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
	userEmail, _, err := ValidateAuthenticatedUser(c)
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
