package api

import (
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Note: Type definitions (CreateAddonRequest, AddonResponse, ListAddonsResponse)
// are now generated in api.go from the OpenAPI specification

// requireAdministrator checks if the current user is an administrator
// Returns an error if not authorized (and sends HTTP response)
//
// Deprecated: Use RequireAdministrator from auth_helpers.go instead
// SEM@a5548be4c61d9f98ed2f3edd998abd909cd5f4ab: authorize the current user as an administrator, returning an error if denied
func requireAdministrator(c *gin.Context) error {
	_, err := RequireAdministrator(c)
	return err
}

// CreateAddon creates a new add-on (admin only)
// SEM@15af4eb93978e65654702a2b47f0ebe20df650dc: handle POST /addons: validate and store a new addon, restricted to administrators (reads DB)
func CreateAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Check if user is an administrator
	if err := requireAdministrator(c); err != nil {
		return // Error response already sent by requireAdministrator
	}

	// Parse request
	req, err := ParseRequestBody[CreateAddonRequest](c)
	if err != nil {
		logger.Error("Failed to parse create add-on request: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Validate name
	if err := ValidateAddonName(req.Name); err != nil {
		logger.Error("Invalid add-on name: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate description
	if err := ValidateAddonDescription(strFromPtr(req.Description)); err != nil {
		logger.Error("Invalid add-on description: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate icon
	if err := ValidateIcon(strFromPtr(req.Icon)); err != nil {
		logger.Error("Invalid add-on icon: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate objects
	if err := ValidateObjects(fromObjectsSlicePtr(req.Objects)); err != nil {
		logger.Error("Invalid add-on objects: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate parameters
	params := fromAddonParametersPtr(req.Parameters)
	if err := ValidateAddonParameters(params); err != nil {
		logger.Error("Invalid add-on parameters: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Sanitize text fields (defense-in-depth)
	req.Name = SanitizePlainText(req.Name)
	req.Description = SanitizeOptionalString(req.Description)

	// Create add-on
	addon := &Addon{
		CreatedAt:     time.Now(),
		Name:          req.Name,
		WebhookID:     req.WebhookId,
		Description:   strFromPtr(req.Description),
		Icon:          strFromPtr(req.Icon),
		Objects:       fromObjectsSlicePtr(req.Objects),
		ThreatModelID: req.ThreatModelId,
		Parameters:    params,
	}

	if err := GlobalAddonStore.Create(c.Request.Context(), addon); err != nil {
		logger.Error("Failed to create add-on: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to create add-on",
		})
		return
	}

	// Return response
	response := addonToResponse(addon)

	logger.Info("Add-on created: id=%s, name=%s", addon.ID, addon.Name)
	c.JSON(http.StatusCreated, response)
}

// GetAddon retrieves a single add-on by ID
// SEM@c9781319675ccc8efddf4bcf30faed097d0c028a: handle GET /addons/{id}: fetch and return a single addon by ID (reads DB)
func GetAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get addon ID from path (OpenAPI routes use "id" as the parameter name)
	addonIDStr := c.Param("id")
	addonID, err := uuid.Parse(addonIDStr)
	if err != nil {
		logger.Error("Invalid add-on ID: %s", addonIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid add-on ID format",
		})
		return
	}

	// Get add-on
	addon, err := GlobalAddonStore.Get(c.Request.Context(), addonID)
	if err != nil {
		logger.Error("Failed to get add-on: id=%s, error=%v", addonID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Add-on not found",
		})
		return
	}

	// Return response
	response := addonToResponse(addon)

	c.JSON(http.StatusOK, response)
}

// ListAddons retrieves add-ons with pagination
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: handle GET /addons: list addons with pagination and optional threat model filter (reads DB)
func ListAddons(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters with safe defaults
	limit := 50 // default
	offset := 0 // default

	// Use SafeParseInt to prevent crashes from malformed input
	if limitStr := c.Query("limit"); limitStr != "" {
		parsedLimit := min(SafeParseInt(limitStr, 50),
			// max limit
			500)
		limit = parsedLimit
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		offset = SafeParseInt(offsetStr, 0)
	}

	// Optional threat model filter
	var threatModelID *uuid.UUID
	if tmIDStr := c.Query("threat_model_id"); tmIDStr != "" {
		if tmID, err := uuid.Parse(tmIDStr); err == nil {
			threatModelID = &tmID
		} else {
			logger.Error("Invalid threat_model_id: %s", tmIDStr)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: "Invalid threat_model_id format",
			})
			return
		}
	}

	// List add-ons
	addons, total, err := GlobalAddonStore.List(c.Request.Context(), limit, offset, threatModelID)
	if err != nil {
		logger.Error("Failed to list add-ons: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list add-ons",
		})
		return
	}

	// Convert to response format
	// Initialize with empty slice instead of nil to ensure JSON serializes as [] not null
	responses := make([]AddonResponse, 0, len(addons))
	for _, addon := range addons {
		responses = append(responses, addonToResponse(&addon))
	}

	// Return paginated response
	response := ListAddonsResponse{
		Addons: responses,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	c.JSON(http.StatusOK, response)
}

// DeleteAddon deletes an add-on (admin only)
// SEM@c9781319675ccc8efddf4bcf30faed097d0c028a: handle DELETE /addons/{id}: delete an addon, rejecting if active invocations exist (reads DB)
func DeleteAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Check if user is an administrator
	if err := requireAdministrator(c); err != nil {
		return // Error response already sent by requireAdministrator
	}

	// Get addon ID from path (OpenAPI routes use "id" as the parameter name)
	addonIDStr := c.Param("id")
	addonID, err := uuid.Parse(addonIDStr)
	if err != nil {
		logger.Error("Invalid add-on ID: %s", addonIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid add-on ID format",
		})
		return
	}

	// Check for active invocations
	activeCount, err := GlobalAddonStore.CountActiveInvocations(c.Request.Context(), addonID)
	if err != nil {
		logger.Error("Failed to count active invocations for add-on: id=%s, error=%v", addonID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify add-on status",
		})
		return
	}

	if activeCount > 0 {
		logger.Warn("Cannot delete add-on with active invocations: id=%s, count=%d", addonID, activeCount)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "conflict",
			Message: "Cannot delete add-on with active invocations. Please wait for invocations to complete.",
		})
		return
	}

	// Delete add-on
	if err := GlobalAddonStore.Delete(c.Request.Context(), addonID); err != nil {
		logger.Error("Failed to delete add-on: id=%s, error=%v", addonID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Add-on not found",
		})
		return
	}

	logger.Info("Add-on deleted: id=%s", addonID)
	c.Status(http.StatusNoContent)
}
