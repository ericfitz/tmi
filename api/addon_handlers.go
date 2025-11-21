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
func requireAdministrator(c *gin.Context) error {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user information from JWT claims
	userEmail, exists := c.Get("userEmail")
	if !exists {
		logger.Warn("Admin check: no userEmail in context")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		})
		return &RequestError{Status: http.StatusUnauthorized}
	}

	email, ok := userEmail.(string)
	if !ok || email == "" {
		logger.Warn("Admin check: invalid userEmail in context")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid authentication token",
		})
		return &RequestError{Status: http.StatusUnauthorized}
	}

	// Get user ID if available
	var userID *uuid.UUID
	if userIDInterface, exists := c.Get("userID"); exists {
		if userIDStr, ok := userIDInterface.(string); ok {
			if parsedID, err := uuid.Parse(userIDStr); err == nil {
				userID = &parsedID
			}
		} else if userUUID, ok := userIDInterface.(uuid.UUID); ok {
			userID = &userUUID
		}
	}

	// Get user groups from JWT claims
	var groups []string
	if groupsInterface, exists := c.Get("userGroups"); exists {
		if groupSlice, ok := groupsInterface.([]string); ok {
			groups = groupSlice
		}
	}

	// Check if user is an administrator
	if GlobalAdministratorStore == nil {
		logger.Error("Admin check: GlobalAdministratorStore is nil")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Administrator store not initialized",
		})
		return &RequestError{Status: http.StatusInternalServerError}
	}

	isAdmin, err := GlobalAdministratorStore.IsAdmin(c.Request.Context(), userID, email, groups)
	if err != nil {
		logger.Error("Admin check: failed to check admin status for email=%s: %v", email, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify administrator status",
		})
		return &RequestError{Status: http.StatusInternalServerError}
	}

	if !isAdmin {
		logger.Warn("Admin check: access denied for non-admin user: email=%s, groups=%v", email, groups)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return &RequestError{Status: http.StatusForbidden}
	}

	logger.Debug("Admin check: access granted for admin user: email=%s", email)
	return nil
}

// CreateAddon creates a new add-on (admin only)
func CreateAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Check if user is an administrator
	if err := requireAdministrator(c); err != nil {
		return // Error response already sent by requireAdministrator
	}

	// Parse request
	var req CreateAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
	if err := ValidateAddonDescription(fromStringPtr(req.Description)); err != nil {
		logger.Error("Invalid add-on description: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate icon
	if err := ValidateIcon(fromStringPtr(req.Icon)); err != nil {
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

	// Create add-on
	addon := &Addon{
		CreatedAt:     time.Now(),
		Name:          req.Name,
		WebhookID:     req.WebhookId,
		Description:   fromStringPtr(req.Description),
		Icon:          fromStringPtr(req.Icon),
		Objects:       fromObjectsSlicePtr(req.Objects),
		ThreatModelID: req.ThreatModelId,
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
func GetAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get addon ID from path
	addonIDStr := c.Param("addon_id")
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
func ListAddons(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters
	limit := 50 // default
	offset := 0 // default

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := parsePositiveInt(limitStr); err == nil {
			if parsedLimit > 500 {
				parsedLimit = 500 // max limit
			}
			limit = parsedLimit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsedOffset, err := parsePositiveInt(offsetStr); err == nil {
			offset = parsedOffset
		}
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
	var responses []AddonResponse
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
func DeleteAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Check if user is an administrator
	if err := requireAdministrator(c); err != nil {
		return // Error response already sent by requireAdministrator
	}

	// Get addon ID from path
	addonIDStr := c.Param("addon_id")
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
